package main

// client.go renders ts/src/client.ts: one typed method per rpc over a
// caller-supplied transport. It is a further render target of the same
// descriptor walk that produces the manifests and the Go server — nothing
// here reads the .proto text or ts-proto's output, only the compiled
// descriptors, and it runs after describe() has already rejected any route
// shape the Go server side cannot bind (a named body, a non-string path
// param, a message-typed query field): those checks are properties of the
// route itself, shared by both halves. What is left here is client-specific:
// a request tree containing bytes cannot survive JSON.stringify as protojson
// expects, so that rejection is the client's own.
import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const clientOut = "ts/src/client.ts"

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
	route
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

// describeClient extends a manifest route (already vetted by describe: only
// "*" or no body, only string path params, only scalar or repeated-scalar
// query fields) with the descriptor detail the client needs, and rejects the
// one shape it alone cannot encode: bytes anywhere in the request tree.
func describeClient(md protoreflect.MethodDescriptor, r route) (clientRoute, error) {
	req := md.Input()
	cr := clientRoute{route: r, In: refOf(req), Out: refOf(md.Output())}

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
	if err := rejectBytes(req, map[protoreflect.FullName]bool{}); err != nil {
		return cr, fmt.Errorf("%s: %w", md.Name(), err)
	}
	return cr, nil
}

// rejectBytes refuses a request tree containing a bytes field: ts-proto types
// bytes as Uint8Array, which JSON.stringify renders as an index-keyed object
// (`{"0":1,"1":2}`), not the base64 string protojson expects. The Go server
// side has no such gap — contract.Marshal/Unmarshal handle bytes natively —
// so this rejection is specific to the client renderer, not describe().
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
		shut := strings.IndexByte(path, '}')
		b.WriteString(path[:open])
		fmt.Fprintf(&b, "${param(%s%s)}", src, path[open+1:shut])
		path = path[shut+1:]
	}
	b.WriteString("`")
	return b.String()
}

func renderClient(routes []clientRoute) []byte {
	var b bytes.Buffer

	b.WriteString("// Code generated by cmd/routegen. DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "// A typed client for %s: one method per rpc, over a transport the\n", apiPrefix)
	b.WriteString("// caller supplies. The client builds the path from the request's path fields,\n")
	b.WriteString("// the query string from its query fields, and serialises the body exactly once;\n")
	b.WriteString("// `ClientRequest.body` is the string that goes on the wire, so a transport that\n")
	b.WriteString("// signs the body signs those octets. The caller owns everything about the\n")
	b.WriteString("// transport itself: auth headers, X-Signature over req.body, and status\n")
	b.WriteString("// handling beyond \"2xx parses, the rest throws ApiError\" — a 401 policy,\n")
	b.WriteString("// retries. The client has no persistence and no cache of its own.\n\n")

	// Type-only imports, grouped by generated file.
	byFile := map[string]map[string]bool{}
	for _, r := range routes {
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

	// The prefix is written here as well as in route-manifest.ts: the same
	// literal, emitted twice by the same generator, deliberately NOT exported
	// under this name. A value import of ./route-manifest.js would be a
	// runtime edge under src/ (check-no-runtime.mjs forbids that), and
	// re-exporting a second `apiPrefix` here would collide with
	// route-manifest.ts's own export of that name through ts/index.ts's
	// `export *` — an ambiguous export that TypeScript silently drops from
	// the package root instead of erroring, breaking `import { apiPrefix }
	// from "@metacensus/api"` for anyone who already relies on it. Kept
	// private; only Client's own default constructor argument uses it.
	fmt.Fprintf(&b, "const defaultPrefix = %q;\n\n", apiPrefix)

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

// ApiError is any non-2xx the transport did not itself act on. The contract
// declares no error message, so body is the response text, unparsed; a
// caller that wants the {error, code} shape the reference Go server emits
// can parse ApiError.body itself.
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
    const res = await this.transport({ method, path: this.prefix + path, body });
    if (res.status < 200 || res.status >= 300) {
      throw new ApiError(method, path, res.status, res.body);
    }
    // A cast: with onlyTypes there is no runtime schema to validate against.
    return (res.body === "" ? {} : JSON.parse(res.body)) as T;
  }
`)

	for _, r := range routes {
		fmt.Fprintf(&b, "\n  // %s.%s: %s %s\n", r.Service, r.RPC, r.Method, r.Path)
		fmt.Fprintf(&b, "  %s(req: %s): Promise<%s> {\n", lowerFirst(r.RPC), r.In.Name, r.Out.Name)

		// Body first: a "*" body is "everything the path did not bind", and
		// destructuring is how the path fields leave it. describe() has
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
				if f.List {
					fmt.Fprintf(&b, "    for (const v of req.%s) q.push([%q, String(v)]);\n", name, name)
				} else {
					fmt.Fprintf(&b, "    q.push([%q, String(req.%s)]);\n", name, name)
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
	return b.Bytes()
}
