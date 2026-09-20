// Package clientgen renders ts/src/client.ts: one batteries-included client
// per surface, one or two typed methods per rpc, over fetch.
//
// The client owns its HTTP: it holds the session token across login/logout and
// signs writes with a caller-injected signer. Reads return flat views
// ({id, recorded, ...content}); a signed-envelope read also gets a getXSigned
// method returning the raw envelope, for a caller verifying authorship. The
// shared ApiError is one declaration so `instanceof` does not depend on the
// import path.
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

var funcs = template.FuncMap{
	"lowerFirst": lowerFirst,
	"join":       func(sep string, items []string) string { return strings.Join(items, sep) },
}

var tmpl = template.Must(template.New("client.ts.tmpl").Funcs(funcs).Parse(tmplSrc))

// tsRef names a ts-proto type and the generated file that declares it.
// ts-proto names nested declarations Parent_Child.
type tsRef struct {
	Name string
	File string // e.g. "./metacensus/v1/topic.js"
}

func refOf(md protoreflect.MessageDescriptor) tsRef {
	var parts []string
	for d := protoreflect.Descriptor(md); d != nil; d = d.Parent() {
		if _, isFile := d.(protoreflect.FileDescriptor); isFile {
			break
		}
		parts = append([]string{string(d.Name())}, parts...)
	}
	path := "./" + strings.TrimSuffix(md.ParentFile().Path(), ".proto") + ".js"
	return tsRef{Name: strings.Join(parts, "_"), File: path}
}

// rejectBytes refuses a message tree with a bytes field: ts-proto types bytes
// as Uint8Array, which JSON.stringify/parse cannot round-trip as protojson's
// base64.
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

// pathExpr renders a route's path as a TypeScript expression: a template
// literal percent-encoding each parameter when the route has any, else the
// quoted literal. src prefixes each parameter read — "req." when the request
// object is read directly, "" when the parameters were destructured out first.
func pathExpr(path string, params []string, src string) string {
	if len(params) == 0 {
		return fmt.Sprintf("%q", path)
	}
	var b strings.Builder
	b.WriteString("`")
	for len(path) > 0 {
		open := strings.IndexByte(path, '{')
		if open < 0 {
			b.WriteString(path)
			break
		}
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

// viewType is a flat view alias and its flatten helper, emitted once per
// content type however many methods return it.
type viewType struct {
	View     string // "TopicView"
	Content  string // "Topic"
	Envelope string // "TopicSigned"
	HasId    bool
	Flatten  string // "flattenTopic"
}

// clientMethod is one method a client class emits. Kind selects the template
// that renders it; the other fields carry what that template needs.
type clientMethod struct {
	Name     string // "getTopic", "getTopicSigned", "createTopic", ...
	Service  string
	RPC      string
	HTTP     string
	Path     string
	Params   []string // path parameters, for a write's destructuring
	Req      string   // request type, or Omit<Req, "userSignature"> for a write
	Return   string
	Kind     string // raw|list|flatten|flattenList|post|login|logout|write|signup
	Envelope string // envelope type, for flatten and raw single
	List     string // list type, for flattenList and list
	Flatten  string // "flattenTopic"
	CType    string // contentType a write signs under
	PathExpr string // the rendered path expression
	BodyArg  string // the body argument for a raw method
}

// surface is one client class: the routes it serves, and the machinery those
// routes imply. HasSigner and HasToken gate the signer and token fields.
type surface struct {
	Class     string
	Options   string
	TSConst   string
	Summary   string
	HasSigner bool
	HasToken  bool
	Methods   []clientMethod
}

// describeSurface classifies one package's routes into the methods its client
// class emits, accumulating the view types and type imports the whole file
// needs. It returns ok=false for a surface with no routes.
func describeSurface(pkg model.Package, routes []model.Route, views *[]viewType, seen map[string]bool, refs *[]tsRef) (surface, bool, error) {
	s := surface{Class: pkg.TSClient, Options: pkg.TSClient + "Options", TSConst: pkg.TSConst, Summary: pkg.Summary}
	addView := func(vt viewType) {
		if !seen[vt.View] {
			seen[vt.View] = true
			*views = append(*views, vt)
		}
	}
	for _, r := range routes {
		if r.Pkg.Proto != pkg.Proto {
			continue
		}
		if len(r.Query) > 0 {
			return surface{}, false, fmt.Errorf("%s.%s: query parameters are not supported by the batteries-included client; add support when a route needs one", r.Service, r.RPC)
		}
		for _, tree := range []protoreflect.MessageDescriptor{r.Descriptor.Input(), r.Descriptor.Output()} {
			if err := rejectBytes(tree, map[protoreflect.FullName]bool{}); err != nil {
				return surface{}, false, fmt.Errorf("%s.%s: %w", r.Service, r.RPC, err)
			}
		}
		s.Methods = append(s.Methods, classify(r, addView, refs)...)
	}
	if len(s.Methods) == 0 {
		return surface{}, false, nil
	}
	for _, m := range s.Methods {
		s.HasSigner = s.HasSigner || m.Kind == "write" || m.Kind == "signup"
		s.HasToken = s.HasToken || m.Kind == "login" || m.Kind == "signup" || m.Kind == "logout"
	}
	return s, true, nil
}

// classify turns one route into the one or two methods its client emits. A
// signed-envelope read yields two: the flat getX and the raw getXSigned.
func classify(r model.Route, addView func(viewType), refs *[]tsRef) []clientMethod {
	in := refOf(r.Descriptor.Input())
	out := refOf(r.Descriptor.Output())
	elem, isList := listElem(r.Descriptor.Output())
	elemRef := refOf(elem)
	*refs = append(*refs, in, out, elemRef)

	base := clientMethod{
		Name: lowerFirst(r.RPC), Service: r.Service, RPC: r.RPC,
		HTTP: r.Method, Path: r.Path, Params: r.Params, Req: in.Name,
	}
	respHasToken := r.Descriptor.Output().Fields().ByName("token") != nil

	if r.Signed {
		reqContent := r.Descriptor.Input().Fields().ByName(model.ContentField).Message()
		*refs = append(*refs, refOf(r.Descriptor.Input().Fields().ByName(model.SignatureField).Message()))
		m := base
		m.Req = "Omit<" + in.Name + `, "userSignature">`
		m.CType = string(reqContent.FullName())
		m.PathExpr = pathExpr(r.Path, r.Params, "")
		if respHasToken { // sign-up: enrols the key and logs in
			m.Kind, m.Return = "signup", out.Name
			return []clientMethod{m}
		}
		content, hasID, _ := envelopeContent(elem)
		cRef := refOf(content)
		*refs = append(*refs, cRef)
		addView(viewType{View: cRef.Name + "View", Content: cRef.Name, Envelope: elemRef.Name, HasId: hasID, Flatten: "flatten" + cRef.Name})
		m.Kind, m.Return, m.Envelope, m.Flatten = "write", cRef.Name+"View", elemRef.Name, "flatten"+cRef.Name
		return []clientMethod{m}
	}

	base.PathExpr = pathExpr(r.Path, r.Params, "req.")

	if respHasToken { // login
		m := base
		m.Kind, m.Return = "login", out.Name
		return []clientMethod{m}
	}
	if r.RPC == "Logout" { // the one route whose success drops the token
		m := base
		m.Kind, m.Return = "logout", out.Name
		return []clientMethod{m}
	}

	if content, hasID, ok := envelopeContent(elem); ok {
		cRef := refOf(content)
		*refs = append(*refs, cRef)
		addView(viewType{View: cRef.Name + "View", Content: cRef.Name, Envelope: elemRef.Name, HasId: hasID, Flatten: "flatten" + cRef.Name})

		flat := base
		flat.Envelope, flat.List, flat.Flatten = elemRef.Name, out.Name, "flatten"+cRef.Name
		signed := base
		signed.Name, signed.Envelope, signed.List = base.Name+"Signed", elemRef.Name, out.Name
		if isList {
			flat.Kind, flat.Return = "flattenList", cRef.Name+"View[]"
			signed.Kind, signed.Return = "list", elemRef.Name+"[]"
		} else {
			flat.Kind, flat.Return = "flatten", cRef.Name+"View"
			signed.Kind, signed.Return, signed.BodyArg = "raw", elemRef.Name, "undefined"
		}
		return []clientMethod{flat, signed}
	}

	// An already-flat record (Member): returned as-is.
	if elem.Fields().ByName("id") != nil && elem.Fields().ByName(model.ContentField) == nil {
		m := base
		m.List = out.Name
		if isList {
			m.Kind, m.Return = "list", elemRef.Name+"[]"
		} else {
			m.Kind, m.Return, m.BodyArg = "raw", elemRef.Name, "undefined"
		}
		return []clientMethod{m}
	}

	// A plain response: a body-carrying POST (the public submission) or a bare
	// GET (the healthcheck).
	m := base
	m.Return = out.Name
	if r.Body == "*" {
		m.Kind = "post"
	} else {
		m.Kind, m.BodyArg = "raw", "undefined"
	}
	return []clientMethod{m}
}

// fileImport is one generated file and the type names pulled from it, sorted so
// re-running the generator is stable.
type fileImport struct {
	File  string
	Names []string
}

// Render writes ts/src/client.ts: one batteries-included client class per
// surface, the view types and flatten helpers its reads share, and the one
// ApiError both throw.
func Render(routes []model.Route) ([]byte, error) {
	var (
		surfaces []surface
		views    []viewType
		refs     []tsRef
	)
	seen := map[string]bool{}
	for _, pkg := range model.Packages {
		s, ok, err := describeSurface(pkg, routes, &views, seen, &refs)
		if err != nil {
			return nil, err
		}
		if ok {
			surfaces = append(surfaces, s)
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i].View < views[j].View })

	var b bytes.Buffer
	if err := execute(&b, "header", nil); err != nil {
		return nil, err
	}

	// Type-only imports, grouped by generated file.
	byFile := map[string]map[string]bool{}
	for _, ref := range refs {
		if byFile[ref.File] == nil {
			byFile[ref.File] = map[string]bool{}
		}
		byFile[ref.File][ref.Name] = true
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

	// The prefix constants come from route-manifest.ts, their one home.
	var consts []string
	for _, s := range surfaces {
		consts = append(consts, s.TSConst)
	}
	sort.Strings(consts)
	if err := execute(&b, "valueImport", fileImport{File: "./route-manifest.js", Names: consts}); err != nil {
		return nil, err
	}

	if err := execute(&b, "apiError", nil); err != nil {
		return nil, err
	}

	hasSigner := false
	for _, s := range surfaces {
		hasSigner = hasSigner || s.HasSigner
	}
	if hasSigner {
		if err := execute(&b, "signer", nil); err != nil {
			return nil, err
		}
	}

	if err := execute(&b, "requestHelper", nil); err != nil {
		return nil, err
	}

	for _, vt := range views {
		if err := execute(&b, "viewType", vt); err != nil {
			return nil, err
		}
	}
	if len(views) > 0 {
		b.WriteString("\n")
	}
	for _, vt := range views {
		if err := execute(&b, "flatten", vt); err != nil {
			return nil, err
		}
	}

	for _, s := range surfaces {
		if err := execute(&b, "classOpen", s); err != nil {
			return nil, err
		}
		for _, m := range s.Methods {
			if err := tmpl.ExecuteTemplate(&b, "m_"+m.Kind, m); err != nil {
				return nil, fmt.Errorf("client.ts.tmpl: %s.%s (%s): %w", m.Service, m.RPC, m.Kind, err)
			}
		}
		if err := execute(&b, "classClose", nil); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// execute runs one named block, naming the template and the block on failure.
func execute(b *bytes.Buffer, block string, data any) error {
	if err := tmpl.ExecuteTemplate(b, block, data); err != nil {
		return fmt.Errorf("client.ts.tmpl: %s: %w", block, err)
	}
	return nil
}
