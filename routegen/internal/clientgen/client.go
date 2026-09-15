// Package clientgen renders ts/src/client.ts: one class per surface, one
// typed method per rpc, over a caller-supplied transport. It extends each
// model.Route with the descriptor detail only a TypeScript client needs, and
// rejects the one shape it alone cannot encode — see rejectBytes.
//
// One file, several classes. The shared envelope (Transport, ApiError) has to
// be one declaration — two ApiError classes would make `instanceof` depend on
// the import path — and nothing under src/ may take a value import, so the
// classes that throw it live beside it. Which surface a consumer gets is
// decided by the entry point that re-exports it: ts/index.ts and ts/public.ts.
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

	"github.com/metacensus/api/routegen/internal/model"
)

//go:embed client.ts.tmpl
var tmplSrc string

// funcs is what client.ts.tmpl may call beyond the builtins. Every TS string
// literal goes through the builtin printf "%q" rather than a helper.
var funcs = template.FuncMap{
	"lowerFirst":   lowerFirst,
	"pathTemplate": pathTemplate,
	"join":         func(sep string, items []string) string { return strings.Join(items, sep) },
}

var tmpl = template.Must(template.New("client.ts.tmpl").Funcs(funcs).Parse(tmplSrc))

// field is enough of a request field's descriptor to choose an encoding.
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

// methodView is clientRoute plus the lines the template cannot work out for
// itself: ExtraLines branches per field on Kind and List, and Src is the
// prefix path fields are read off — "" once destructured out of req, "req."
// otherwise. Decided here rather than with nested {{if}} in the template.
type methodView struct {
	clientRoute
	ExtraLines []string
	Src        string
	BodyExpr   string
}

// A "*" body is everything the path did not bind, and destructuring is how
// the path fields leave it. model.Walk has already refused a named body, so
// "*" and "" are the only cases.
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

// fileImport is one generated file and the type names pulled from it, sorted
// so re-running the generator is stable.
type fileImport struct {
	File  string
	Names []string
}

// Render writes ts/src/client.ts: one Client class, one typed method per rpc,
// over a caller-supplied Transport.
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
	if err := execute(&b, "header", nil, "", ""); err != nil {
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

	// One default prefix per surface, each emitted unexported. An exported
	// one would collide with route-manifest.ts's under ts/index.ts's
	// `export *` — an ambiguous star export, which tsc rejects (TS2308).
	for _, pkg := range withRoutes(methods) {
		if err := execute(&b, "prefixConst", pkg, "", ""); err != nil {
			return nil, err
		}
	}
	// param and query are emitted only where a route needs them: nothing
	// under src/ may reach for anything at runtime, and dead code in a
	// package whose whole claim is that it ships almost nothing is a claim
	// it does not have to make. No route declares a query field today.
	var needsParam, needsQuery bool
	for _, m := range methods {
		needsParam = needsParam || len(m.PathFields) > 0 || len(m.QueryFields) > 0
		needsQuery = needsQuery || len(m.QueryFields) > 0
	}
	if needsParam {
		if err := execute(&b, "param", nil, "", ""); err != nil {
			return nil, err
		}
	}
	if needsQuery {
		if err := execute(&b, "query", nil, "", ""); err != nil {
			return nil, err
		}
	}

	if err := execute(&b, "staticBody", nil, "", ""); err != nil {
		return nil, err
	}

	// One class per surface, in Packages order, each holding only its own
	// routes. Iterating the table rather than the methods keeps the class
	// order stable and independent of the route order within a surface.
	for _, pkg := range withRoutes(methods) {
		if err := execute(&b, "classOpen", pkg, "", ""); err != nil {
			return nil, err
		}
		for _, m := range methods {
			if m.Pkg.Proto != pkg.Proto {
				continue
			}
			if err := execute(&b, "route", m, m.Service, m.RPC); err != nil {
				return nil, err
			}
		}
		if err := execute(&b, "classClose", nil, "", ""); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// withRoutes is model.Packages narrowed to the surfaces these routes actually
// cover, in table order. A surface with no routes has already failed
// model.Walk, so this is a guard against emitting an empty class rather than
// a case that is expected to arise.
func withRoutes(methods []methodView) []model.Package {
	var out []model.Package
	for _, pkg := range model.Packages {
		for _, m := range methods {
			if m.Pkg.Proto == pkg.Proto {
				out = append(out, pkg)
				break
			}
		}
	}
	return out
}

// execute runs one named block, naming the template, the block and the route
// on failure.
func execute(b *bytes.Buffer, block string, data any, service, rpc string) error {
	if err := tmpl.ExecuteTemplate(b, block, data); err != nil {
		if rpc != "" {
			return fmt.Errorf("client.ts.tmpl: %s: route %s.%s: %w", block, service, rpc, err)
		}
		return fmt.Errorf("client.ts.tmpl: %s: %w", block, err)
	}
	return nil
}
