// Package clientgen renders ts/src/client.ts: one class per surface, one
// typed method per rpc, over a caller-supplied transport. The shared envelope
// (Transport, ApiError) is one declaration — two ApiError classes would make
// `instanceof` depend on the import path.
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

// funcs is what client.ts.tmpl may call beyond the builtins.
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
// ts-proto names nested declarations Parent_Child.
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
	// Both directions: request and response trees must each be bytes-free.
	for _, tree := range []protoreflect.MessageDescriptor{req, md.Output()} {
		if err := rejectBytes(tree, map[protoreflect.FullName]bool{}); err != nil {
			return cr, fmt.Errorf("%s: %w", md.Name(), err)
		}
	}
	return cr, nil
}

// rejectBytes refuses a message tree with a bytes field: ts-proto types bytes
// as Uint8Array, which JSON.stringify/parse cannot round-trip as protojson's
// base64. The Go side handles bytes natively, so this check belongs here and
// not in model.Walk.
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
		// From open, matching model.rewriteParams.
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

// methodView is clientRoute plus what the template can't work out for itself:
// ExtraLines per field, and Src, the prefix path fields are read off — ""
// once destructured out of req, "req." otherwise.
type methodView struct {
	clientRoute
	ExtraLines []string
	Src        string
	BodyExpr   string
}

// A "*" body is everything the path did not bind; destructuring is how the
// path fields leave it.
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
			// Guards are for a caller who is not TypeScript: an absent
			// field would otherwise send the literal "undefined".
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

	// Unexported: exported would collide with route-manifest.ts's under
	// ts/index.ts's `export *` (an ambiguous star export, TS2308).
	for _, pkg := range withRoutes(methods) {
		if err := execute(&b, "prefixConst", pkg, "", ""); err != nil {
			return nil, err
		}
	}
	// param and query are emitted only where a route needs them, to avoid
	// dead code in a package that ships almost nothing.
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

	// Iterating the table rather than the methods keeps class order stable.
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
// cover, in table order.
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
