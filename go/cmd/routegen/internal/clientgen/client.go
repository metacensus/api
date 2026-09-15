// Package clientgen renders ts/src/client.ts: one typed method per rpc over
// a caller-supplied transport. It extends each model.Route (already vetted
// by model.Walk: only "*" or no body, only string path params, only scalar
// or repeated-scalar query fields) with the descriptor detail only a
// TypeScript client needs — field kinds, message types for import
// references — and rejects the one shape it alone cannot encode: bytes
// anywhere in the request or response tree. A request tree containing bytes
// cannot survive JSON.stringify as protojson expects, so that rejection is
// the client's own; the Go server side has no such gap.
//
// client.ts.tmpl is embedded and parsed at init so a broken template fails
// `make gen` immediately. Its named blocks are executed by a Go loop that
// mirrors the original imperative writer, one route at a time, so an
// execution error carries the block and — for the per-route "route" block —
// the rpc that was being rendered.
package clientgen

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/metacensus/api/go/cmd/routegen/internal/model"
)

//go:embed client.ts.tmpl
var tmplSrc string

// funcs are client.ts.tmpl's own helpers: lowerFirst and pathTemplate build TS
// syntax no builtin covers, and join composes a comma list for the import
// lines. Every Go or TS string literal in the template goes through the
// builtin printf "%q" instead — see client.ts.tmpl.
var funcs = template.FuncMap{
	"lowerFirst":   lowerFirst,
	"pathTemplate": pathTemplate,
	"join":         func(sep string, items []string) string { return strings.Join(items, sep) },
}

var tmpl = template.Must(template.New("client.ts.tmpl").Funcs(funcs).Parse(tmplSrc))

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

// methodView adds the derived values client.ts.tmpl's "route" block needs
// beyond clientRoute: the request-destructuring and query-binding lines
// (ExtraLines), which branch per query field on Kind and List and so are
// built once here rather than re-decided inside the template, Src (the
// prefix pathTemplate reads path-bound fields off: "" once they have been
// destructured out of req, "req." otherwise), and BodyExpr, the serialised
// body the call sends.
type methodView struct {
	clientRoute
	ExtraLines []string
	Src        string
	BodyExpr   string
}

// newMethodView applies the same decisions the original renderer made
// inline: a "*" body is "everything the path did not bind", and
// destructuring is how the path fields leave it. describeClient has already
// ensured Body is "*" or "" — never a named field — so those are the only
// two cases here.
func newMethodView(cr clientRoute) methodView {
	mv := methodView{clientRoute: cr, Src: "req.", BodyExpr: "undefined"}
	if cr.Body == "*" {
		if len(cr.PathFields) > 0 {
			var names []string
			for _, f := range cr.PathFields {
				names = append(names, f.JSONName)
			}
			mv.ExtraLines = append(mv.ExtraLines, fmt.Sprintf("    const { %s, ...body } = req;", strings.Join(names, ", ")))
			mv.Src = ""
		} else {
			mv.ExtraLines = append(mv.ExtraLines, "    const body = req;")
		}
		mv.BodyExpr = "JSON.stringify(body)"
	}
	if len(cr.QueryFields) > 0 {
		mv.ExtraLines = append(mv.ExtraLines, "    const q: (readonly [string, string])[] = [];")
		for _, f := range cr.QueryFields {
			name := f.JSONName
			// useOptionals=messages types these as required, so the
			// guards are for a caller who is not TypeScript. Without
			// them an absent field sends the literal "undefined", which
			// a string-typed parameter binds without complaint.
			if f.List {
				mv.ExtraLines = append(mv.ExtraLines, fmt.Sprintf("    for (const v of req.%s ?? []) q.push([%q, String(v)]);", name, name))
			} else {
				mv.ExtraLines = append(mv.ExtraLines, fmt.Sprintf("    if (req.%s !== undefined && req.%s !== null) q.push([%q, String(req.%s)]);", name, name, name, name))
			}
		}
	}
	return mv
}

// fileImport is client.ts.tmpl's "import" block data: one generated file and
// the type names pulled from it, both already sorted so re-running the
// generator is stable.
type fileImport struct {
	File  string
	Names []string
}

// Render extends every route with client-specific descriptor detail and
// writes ts/src/client.ts: one Client class, one typed method per rpc, over
// a caller-supplied Transport.
func Render(routes []model.Route) ([]byte, error) {
	methods := make([]methodView, 0, len(routes))
	for _, r := range routes {
		cr, err := describeClient(r)
		if err != nil {
			return nil, err
		}
		methods = append(methods, newMethodView(cr))
	}

	var b bytes.Buffer
	if err := execute(&b, "header", struct{ APIPrefix string }{model.APIPrefix}, "", ""); err != nil {
		return nil, err
	}

	// Type-only imports, grouped by generated file.
	byFile := map[string]map[string]bool{}
	for _, m := range methods {
		for _, ref := range []tsRef{m.In, m.Out} {
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
		if err := execute(&b, "import", fileImport{File: f, Names: names}, "", ""); err != nil {
			return nil, err
		}
	}

	// The same literal as route-manifest.ts's apiPrefix, emitted twice by the
	// one generator that owns it, because importing it would be a value
	// import under src/ and check-no-runtime.mjs forbids those. Unexported,
	// so ts/index.ts's `export *` still has exactly one apiPrefix; a second
	// would be an ambiguous star export, which tsc rejects (TS2308) and
	// `npm run check` would therefore catch.
	if err := execute(&b, "prefixConst", struct{ APIPrefix string }{model.APIPrefix}, "", ""); err != nil {
		return nil, err
	}
	if err := execute(&b, "staticBody", nil, "", ""); err != nil {
		return nil, err
	}

	for _, m := range methods {
		if err := execute(&b, "route", m, m.Service, m.RPC); err != nil {
			return nil, err
		}
	}

	if err := execute(&b, "footer", nil, "", ""); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// execute runs one named block of client.ts.tmpl and wraps a failure with the
// block and — for the per-route "route" block — the rpc that was being
// rendered. Every route reaching here already passed describeClient, so a
// failure means a broken template rather than bad input.
func execute(b *bytes.Buffer, block string, data any, service, rpc string) error {
	if err := tmpl.ExecuteTemplate(b, block, data); err != nil {
		if rpc != "" {
			return fmt.Errorf("client.ts.tmpl: %s: route %s.%s: %w", block, service, rpc, err)
		}
		return fmt.Errorf("client.ts.tmpl: %s: %w", block, err)
	}
	return nil
}
