// Package clientgen renders ts/src/client.ts: one typed method per rpc over
// a caller-supplied transport. It extends each model.Route (already vetted
// by model.Walk: only "*" or no body, only string path params, only scalar
// or repeated-scalar query fields) with the descriptor detail only a
// TypeScript client needs — field kinds, message types for import
// references — and rejects the one shape it alone cannot encode: bytes
// anywhere in the request or response tree. A request tree containing bytes
// cannot survive JSON.stringify as protojson expects, so that rejection is
// the client's own; the Go server side has no such gap.
package clientgen

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/metacensus/api/go/cmd/routegen/internal/model"
)

// field is what the client renderer needs per request field beyond its JSON
// name: enough of the descriptor to choose an encoding. The manifest and
// server renderers discard all of this.
type field struct {
	JSONName string
	Kind     protoreflect.Kind
	List     bool
	Map      bool
	Message  protoreflect.FullName // set when Kind is MessageKind
}

// tsRef names a ts-proto type and the generated file that declares it.
// ts-proto names nested declarations Parent_Child; the request and response
// messages of every route today are top-level, but the rule is applied anyway.
type tsRef struct {
	Name string
	File string // e.g. "./metacensus/v1/topic.js"
}

// clientRoute is the per-route information the client renderer consumes.
type clientRoute struct {
	model.Route
	PathFields  []field
	QueryFields []field
	In, Out     tsRef
}

func fieldOf(fd protoreflect.FieldDescriptor) field {
	f := field{JSONName: fd.JSONName(), Kind: fd.Kind(), List: fd.IsList(), Map: fd.IsMap()}
	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		f.Message = fd.Message().FullName()
	}
	return f
}

func refOf(md protoreflect.MessageDescriptor) tsRef {
	var parts []string
	for d := protoreflect.Descriptor(md); d != nil; d = d.Parent() {
		if _, isFile := d.(protoreflect.FileDescriptor); isFile {
			break
		}
		parts = append([]string{string(d.Name())}, parts...)
	}
	path := md.ParentFile().Path()
	path = "./" + strings.TrimSuffix(path, ".proto") + ".js"
	return tsRef{Name: strings.Join(parts, "_"), File: path}
}

// describeClient extends a route (already vetted by model.Walk) with the
// descriptor detail the client needs, read off r.Descriptor, and rejects
// the one shape it alone cannot encode: bytes anywhere in the request tree.
func describeClient(r model.Route) (clientRoute, error) {
	md := r.Descriptor
	req := md.Input()
	cr := clientRoute{Route: r, In: refOf(req), Out: refOf(md.Output())}

	byJSON := map[string]protoreflect.FieldDescriptor{}
	for i := 0; i < req.Fields().Len(); i++ {
		fd := req.Fields().Get(i)
		byJSON[fd.JSONName()] = fd
	}

	for _, p := range r.Params {
		cr.PathFields = append(cr.PathFields, fieldOf(byJSON[p]))
	}
	for _, q := range r.Query {
		cr.QueryFields = append(cr.QueryFields, fieldOf(byJSON[q]))
	}
	// Both directions: a request tree cannot be encoded, and a response tree
	// cannot be decoded, without lying about the type.
	for _, tree := range []protoreflect.MessageDescriptor{req, md.Output()} {
		if err := rejectBytes(tree, map[protoreflect.FullName]bool{}); err != nil {
			return cr, fmt.Errorf("%s: %w", md.Name(), err)
		}
	}
	return cr, nil
}

// rejectBytes refuses a message tree containing a bytes field. ts-proto types
// bytes as Uint8Array: JSON.stringify renders that as an index-keyed object
// (`{"0":1,"1":2}`) rather than the base64 protojson expects on the way out,
// and JSON.parse hands back the base64 string cast to Uint8Array on the way
// back. The Go server side has no such gap — contract.Marshal/Unmarshal
// handle bytes natively — so this rejection is the client renderer's, not
// model.Walk's.
func rejectBytes(md protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) error {
	if seen[md.FullName()] {
		return nil
	}
	seen[md.FullName()] = true
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		if fd.Kind() == protoreflect.BytesKind {
			return fmt.Errorf("field %s is bytes; JSON.stringify cannot encode Uint8Array as protojson base64", fd.FullName())
		}
		if fd.Kind() == protoreflect.MessageKind {
			if err := rejectBytes(fd.Message(), seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// pathTemplate turns "/topic/{topicId}/prop" into a TS template literal that
// percent-encodes each parameter.
func pathTemplate(path string, src string) string {
	var b strings.Builder
	b.WriteString("`")
	for len(path) > 0 {
		open := strings.IndexByte(path, '{')
		if open < 0 {
			b.WriteString(path)
			break
		}
		// From open, matching model.rewriteParams: two walks of the same
		// string disagreeing about where to look is how one of them
		// eventually panics.
		shut := strings.IndexByte(path[open:], '}')
		if shut < 0 {
			b.WriteString(path)
			break
		}
		shut += open
		b.WriteString(path[:open])
		fmt.Fprintf(&b, "${param(%s%s)}", src, path[open+1:shut])
		path = path[shut+1:]
	}
	b.WriteString("`")
	return b.String()
}

// Render extends every route with client-specific descriptor detail and
// writes ts/src/client.ts: one Client class, one typed method per rpc, over
// a caller-supplied Transport.
func Render(routes []model.Route) ([]byte, error) {
	clientRoutes := make([]clientRoute, 0, len(routes))
	for _, r := range routes {
		cr, err := describeClient(r)
		if err != nil {
			return nil, err
		}
		clientRoutes = append(clientRoutes, cr)
	}

	var b bytes.Buffer

	b.WriteString("// Code generated by cmd/routegen. DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "// A typed client for %s: one method per rpc, over a transport the\n", model.APIPrefix)
	b.WriteString("// caller supplies. The client builds the path from the request's path fields,\n")
	b.WriteString("// the query string from its query fields, and serialises the body exactly once;\n")
	b.WriteString("// `ClientRequest.body` is the string that goes on the wire, so a transport that\n")
	b.WriteString("// signs the body signs those octets. The caller owns everything about the\n")
	b.WriteString("// transport itself: auth headers, X-Signature over req.body, and status\n")
	b.WriteString("// handling beyond \"2xx parses, the rest throws ApiError\" — a 401 policy,\n")
	b.WriteString("// retries. The client has no persistence and no cache of its own.\n\n")

	// Type-only imports, grouped by generated file.
	byFile := map[string]map[string]bool{}
	for _, r := range clientRoutes {
		for _, ref := range []tsRef{r.In, r.Out} {
			if byFile[ref.File] == nil {
				byFile[ref.File] = map[string]bool{}
			}
			byFile[ref.File][ref.Name] = true
		}
	}
	var files []string
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		var names []string
		for n := range byFile[f] {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "import type { %s } from %q;\n", strings.Join(names, ", "), f)
	}
	b.WriteString("\n")

	// The same literal as route-manifest.ts's apiPrefix, emitted twice by the
	// one generator that owns it, because importing it would be a value
	// import under src/ and check-no-runtime.mjs forbids those. Unexported,
	// so ts/index.ts's `export *` still has exactly one apiPrefix; a second
	// would be an ambiguous star export, which tsc rejects (TS2308) and
	// `npm run check` would therefore catch.
	fmt.Fprintf(&b, "const defaultPrefix = %q;\n\n", model.APIPrefix)

	b.WriteString(`// What the client hands the transport. path is absolute (prefix, route, query),
// every segment and query value percent-encoded. body is present exactly when
// the route carries one, already serialised; send it unchanged — a transport
// that signs the body must sign these exact octets, not a re-encoding.
export interface ClientRequest {
  readonly method: string;
  readonly path: string;
  readonly body?: string;
}

// What the transport hands back: the status and the response text.
export interface ClientResponse {
  readonly status: number;
  readonly body: string;
}

// The caller supplies this: how a request actually goes over the wire. This
// is where auth headers, X-Signature (computed over req.body, the same
// octets sent), and the SPA's 401 policy belong — none of that is the
// contract's concern.
export type Transport = (req: ClientRequest) => Promise<ClientResponse>;

// ApiError is any non-2xx the transport did not itself act on. path is the
// path that was requested, prefix included. The contract declares no error
// message, so body is the response text, unparsed — and a 404 or 405 comes
// from the router rather than from a handler, so it is often not JSON at all;
// a caller that wants the {error, code} shape the reference Go server emits
// must parse ApiError.body defensively.
export class ApiError extends Error {
  constructor(
    readonly method: string,
    readonly path: string,
    readonly status: number,
    readonly body: string,
  ) {
    super(` + "`${method} ${path}: HTTP ${status}`" + `);
    this.name = "ApiError";
  }
}

function param(v: string | number | boolean): string {
  return encodeURIComponent(String(v));
}

function query(pairs: readonly (readonly [string, string])[]): string {
  if (pairs.length === 0) return "";
  return "?" + pairs.map(([k, v]) => param(k) + "=" + param(v)).join("&");
}

export class Client {
  // prefix defaults to the contract's own apiPrefix (see route-manifest.ts),
  // but can be overridden — mirroring server.Runtime.Prefix on the Go side,
  // which defaults to routes.Prefix the same way. Neither side needs this
  // for a real deployment (the environment-specific part is the transport's
  // base URL); it exists so a test harness can mount the same routes
  // elsewhere.
  constructor(
    private readonly transport: Transport,
    private readonly prefix: string = defaultPrefix,
  ) {}

  private async call<T>(method: string, path: string, body?: string): Promise<T> {
    const full = this.prefix + path;
    const res = await this.transport({ method, path: full, body });
    if (res.status < 200 || res.status >= 300) {
      throw new ApiError(method, full, res.status, res.body);
    }
    // A cast: with onlyTypes there is no runtime schema to validate against.
    return (res.body === "" ? {} : JSON.parse(res.body)) as T;
  }
`)

	for _, r := range clientRoutes {
		fmt.Fprintf(&b, "\n  // %s.%s: %s %s\n", r.Service, r.RPC, r.Method, r.Path)
		fmt.Fprintf(&b, "  %s(req: %s): Promise<%s> {\n", lowerFirst(r.RPC), r.In.Name, r.Out.Name)

		// Body first: a "*" body is "everything the path did not bind", and
		// destructuring is how the path fields leave it. model.Walk has
		// already ensured Body is "*" or "" — never a named field — so those
		// are the only two cases here.
		src := "req."
		bodyExpr := "undefined"
		if r.Body == "*" {
			if len(r.PathFields) > 0 {
				var names []string
				for _, f := range r.PathFields {
					names = append(names, f.JSONName)
				}
				fmt.Fprintf(&b, "    const { %s, ...body } = req;\n", strings.Join(names, ", "))
				src = ""
			} else {
				b.WriteString("    const body = req;\n")
			}
			bodyExpr = "JSON.stringify(body)"
		}

		if len(r.QueryFields) > 0 {
			b.WriteString("    const q: (readonly [string, string])[] = [];\n")
			for _, f := range r.QueryFields {
				name := f.JSONName
				// useOptionals=messages types these as required, so the
				// guards are for a caller who is not TypeScript. Without
				// them an absent field sends the literal "undefined", which
				// a string-typed parameter binds without complaint.
				if f.List {
					fmt.Fprintf(&b, "    for (const v of req.%s ?? []) q.push([%q, String(v)]);\n", name, name)
				} else {
					fmt.Fprintf(&b, "    if (req.%s !== undefined && req.%s !== null) q.push([%q, String(req.%s)]);\n", name, name, name, name)
				}
			}
		}

		path := pathTemplate(r.Path, src)
		if len(r.PathFields) == 0 {
			path = fmt.Sprintf("%q", r.Path)
		}
		if len(r.QueryFields) > 0 {
			path += " + query(q)"
		}
		fmt.Fprintf(&b, "    return this.call(%q, %s, %s);\n", r.Method, path, bodyExpr)
		b.WriteString("  }\n")
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}
