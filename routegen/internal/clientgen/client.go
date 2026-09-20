// Package clientgen renders ts/src/client.ts: one class per surface, one
// typed method per rpc, over a caller-supplied transport. The shared envelope
// (Transport, ApiError) is one declaration — two ApiError classes would make
// `instanceof` depend on the import path.
//
// It also renders the sugar layer beneath the raw classes: a Client that wraps
// ClientSigned and returns flat views ({id, recorded, ...content}) rather than
// the signed envelopes. Reads flatten the response; writes take flat content
// plus a session signer and assemble the envelope.
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

// sugarClass is one sugar client; HasWrites gates the signer machinery.
type sugarClass struct {
	Name      string // e.g. "Client"
	TSClient  string // the raw class it wraps, e.g. "ClientSigned"
	TSConst   string // the surface's prefix constant, e.g. "apiPrefix"
	HasWrites bool
}

// viewType is a flat view alias and its flatten helper, emitted once per
// content type however many routes return it.
type viewType struct {
	View     string // "TopicView"
	Content  string // "Topic"
	Envelope string // "TopicSigned"
	HasId    bool
	Flatten  string // "flattenTopic", "" when the record is already flat
}

// sugarMethod is one method of a sugar client: a read that flattens an
// envelope (or returns an already-flat record), a list that does the same
// per item, or a signed write that wraps flat content before sending it.
type sugarMethod struct {
	Name     string // lowerFirst(RPC)
	Service  string
	RPC      string
	HTTP     string // GET/POST, for the doc comment
	Path     string
	Req      string // request type name
	List     bool   // response is {items: T[]}
	Write    bool   // signed write: take flat content, assemble the envelope
	Envelope bool   // element is a *Signed needing a flatten() call
	View     string // element view type: "TopicView", or "Member" when flat
	Flatten  string // "flattenTopic", "" when not an envelope
	CType    string // contentType string a write signs under
}

// listElem returns the element type of an {items: T[]} list envelope and true,
// or md unchanged and false.
func listElem(md protoreflect.MessageDescriptor) (protoreflect.MessageDescriptor, bool) {
	if md.Fields().Len() == 1 {
		f := md.Fields().Get(0)
		if f.JSONName() == "items" && f.IsList() && f.Kind() == protoreflect.MessageKind {
			return f.Message(), true
		}
	}
	return md, false
}

// envelopeContent returns the content message of a {content, userSignature}
// envelope, and whether it also carries a server-minted id (Vote does not).
func envelopeContent(md protoreflect.MessageDescriptor) (protoreflect.MessageDescriptor, bool, bool) {
	c := md.Fields().ByName(model.ContentField)
	s := md.Fields().ByName(model.SignatureField)
	if c == nil || s == nil || c.Kind() != protoreflect.MessageKind {
		return nil, false, false
	}
	return c.Message(), md.Fields().ByName("id") != nil, true
}

// describeSugar classifies one route's response for the sugar layer. include
// is false for a plain response (no record to flatten), which drops the route.
// When the element is an envelope it also returns the content ref, so the view
// alias's `& Topic` half can be imported.
func describeSugar(cr clientRoute) (sm sugarMethod, vt viewType, content tsRef, include bool) {
	elem, list := listElem(cr.Descriptor.Output())
	sm = sugarMethod{
		Name:    lowerFirst(cr.RPC),
		Service: cr.Service,
		RPC:     cr.RPC,
		HTTP:    cr.Method,
		Path:    cr.Path,
		Req:     cr.In.Name,
		List:    list,
	}

	if cmd, hasID, ok := envelopeContent(elem); ok {
		cRef := refOf(cmd)
		sm.Envelope = true
		sm.View = cRef.Name + "View"
		sm.Flatten = "flatten" + cRef.Name
		vt = viewType{View: sm.View, Content: cRef.Name, Envelope: refOf(elem).Name, HasId: hasID, Flatten: sm.Flatten}
		if cr.Signed {
			// contentType the write signs under: the request's content field.
			sm.Write = true
			sm.CType = string(cr.Descriptor.Input().Fields().ByName(model.ContentField).Message().FullName())
		}
		return sm, vt, cRef, true
	}

	// Member: a flat record, no envelope — returned as-is.
	if elem.Fields().ByName("id") != nil && elem.Fields().ByName(model.ContentField) == nil {
		sm.View = refOf(elem).Name
		return sm, viewType{}, tsRef{}, true
	}

	return sugarMethod{}, viewType{}, tsRef{}, false
}

// sugarSurface is one surface's sugar rendering, computed once so its imports
// can be merged before the import block and its body emitted after the raw
// classes.
type sugarSurface struct {
	class   sugarClass
	methods []sugarMethod
	views   []viewType
	refs    []tsRef // extra type imports the sugar needs
}

// describeSugarSurface builds the sugar for one surface, or ok=false when the
// surface has no sugar. A surface gets a sugar client only when its raw class
// name ends in "Signed" (the authenticated surface): the sugar's whole job is
// unwrapping signed records, and the name it takes is that suffix dropped —
// ClientSigned becomes Client, the name index.ts reserves. A surface with no
// record route (the public one) yields nothing even so.
func describeSugarSurface(pkg model.Package, methods []methodView) (sugarSurface, bool) {
	name := strings.TrimSuffix(pkg.TSClient, "Signed")
	if name == pkg.TSClient {
		return sugarSurface{}, false
	}

	s := sugarSurface{class: sugarClass{Name: name, TSClient: pkg.TSClient, TSConst: pkg.TSConst}}
	seen := map[string]bool{}
	var sigRef tsRef
	for _, m := range methods {
		if m.Pkg.Proto != pkg.Proto {
			continue
		}
		sm, vt, content, ok := describeSugar(m.clientRoute)
		if !ok {
			continue
		}
		s.methods = append(s.methods, sm)
		if sm.Envelope {
			s.refs = append(s.refs, content)
			if !seen[vt.View] {
				seen[vt.View] = true
				s.views = append(s.views, vt)
			}
		}
		if sm.Write {
			s.class.HasWrites = true
			sigRef = refOf(m.Descriptor.Input().Fields().ByName(model.SignatureField).Message())
		}
	}
	if len(s.methods) == 0 {
		return sugarSurface{}, false
	}
	if s.class.HasWrites {
		s.refs = append(s.refs, sigRef) // UserSignature, for the Signer type
	}
	sort.Slice(s.views, func(i, j int) bool { return s.views[i].View < s.views[j].View })
	return s, true
}

// Render writes ts/src/client.ts: a raw class per surface and the sugar Client
// beneath, each with one typed method per rpc, over a caller-supplied Transport.
func Render(routes []model.Route) ([]byte, error) {
	methods := make([]methodView, 0, len(routes))
	for _, r := range routes {
		cr, err := describeClient(r)
		if err != nil {
			return nil, err
		}
		methods = append(methods, newMethodView(cr))
	}

	// Up front, so their extra type imports can join the import block below.
	var sugars []sugarSurface
	for _, pkg := range withRoutes(methods) {
		if s, ok := describeSugarSurface(pkg, methods); ok {
			sugars = append(sugars, s)
		}
	}

	var b bytes.Buffer
	if err := execute(&b, "header", nil); err != nil {
		return nil, err
	}

	// Type-only imports, grouped by generated file.
	byFile := map[string]map[string]bool{}
	addRef := func(ref tsRef) {
		if byFile[ref.File] == nil {
			byFile[ref.File] = map[string]bool{}
		}
		byFile[ref.File][ref.Name] = true
	}
	for _, m := range methods {
		addRef(m.In)
		addRef(m.Out)
	}
	for _, s := range sugars {
		for _, ref := range s.refs {
			addRef(ref)
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
		if err := execute(&b, "import", fileImport{File: f, Names: names}); err != nil {
			return nil, err
		}
	}

	// The prefixes come from route-manifest.ts, their one home. A value import,
	// not a re-emitted literal; client.ts is re-exported by name (not `export
	// *`), so an imported binding it doesn't re-export can't collide there.
	var consts []string
	for _, pkg := range withRoutes(methods) {
		consts = append(consts, pkg.TSConst)
	}
	sort.Strings(consts)
	if err := execute(&b, "valueImport", fileImport{File: "./route-manifest.js", Names: consts}); err != nil {
		return nil, err
	}
	// param and query are emitted only where a route needs them, to avoid
	// dead code in a package that ships almost nothing.
	var needsParam, needsQuery bool
	for _, m := range methods {
		needsParam = needsParam || len(m.PathFields) > 0 || len(m.QueryFields) > 0
		needsQuery = needsQuery || len(m.QueryFields) > 0
	}
	if needsParam {
		if err := execute(&b, "param", nil); err != nil {
			return nil, err
		}
	}
	if needsQuery {
		if err := execute(&b, "query", nil); err != nil {
			return nil, err
		}
	}

	if err := execute(&b, "staticBody", nil); err != nil {
		return nil, err
	}

	// Iterating the table rather than the methods keeps class order stable.
	for _, pkg := range withRoutes(methods) {
		if err := execute(&b, "classOpen", pkg); err != nil {
			return nil, err
		}
		for _, m := range methods {
			if m.Pkg.Proto != pkg.Proto {
				continue
			}
			if err := tmpl.ExecuteTemplate(&b, "route", m); err != nil {
				return nil, fmt.Errorf("client.ts.tmpl: route %s.%s: %w", m.Service, m.RPC, err)
			}
		}
		if err := execute(&b, "classClose", nil); err != nil {
			return nil, err
		}
	}

	// Last: it wraps the raw classes above, so it reads best after them.
	for _, s := range sugars {
		if s.class.HasWrites {
			if err := execute(&b, "signer", nil); err != nil {
				return nil, err
			}
		}
		for _, vt := range s.views {
			if err := execute(&b, "viewType", vt); err != nil {
				return nil, err
			}
		}
		for _, vt := range s.views {
			if err := execute(&b, "flatten", vt); err != nil {
				return nil, err
			}
		}
		if err := execute(&b, "sugarOpen", s.class); err != nil {
			return nil, err
		}
		for _, m := range s.methods {
			if err := tmpl.ExecuteTemplate(&b, "sugarMethod", m); err != nil {
				return nil, fmt.Errorf("client.ts.tmpl: sugar %s.%s: %w", m.Service, m.RPC, err)
			}
		}
		if err := execute(&b, "classClose", nil); err != nil {
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

// execute runs one named block, naming the template and the block on failure.
func execute(b *bytes.Buffer, block string, data any) error {
	if err := tmpl.ExecuteTemplate(b, block, data); err != nil {
		return fmt.Errorf("client.ts.tmpl: %s: %w", block, err)
	}
	return nil
}
