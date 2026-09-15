// Command routegen writes the route manifest, a Go server and a TypeScript
// client for metacensus.v1 from the google.api.http annotations on each
// service. One walk of the compiled descriptors feeds four renderers:
//   - go/routes/manifest.go and ts/src/route-manifest.ts, the data-only
//     manifest (renderGo, renderTS);
//   - go/server/routes_gen.go, a handler interface, an Unimplemented* and a
//     Register* per service that binds path/query/body and dispatches to it
//     (renderServer) — the hand-written runtime beside it is
//     go/server/runtime.go;
//   - ts/src/client.ts, a typed method per rpc over a caller-supplied
//     transport (renderClient).
//
// It reads the descriptors the generated Go package registers, so it runs
// after `buf generate`. None of this changes the route table: routegen only
// deepens what is generated from the 19 routes already declared, and refuses
// to generate at all for a route shape it cannot bind (see describe and
// describeClient).
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	_ "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Output paths are relative to the repository root, which is where the
// Makefile runs this. Everything else in the build — buf, the generator
// plugins — is rooted there too.
const (
	protoPkg = "metacensus.v1"
	goOut    = "go/routes/manifest.go"
	tsOut    = "ts/src/route-manifest.ts"
	srvOut   = "go/server/routes_gen.go"

	// v1Import is the generated package the server code binds against. The
	// generator itself imports it (above) to register the descriptors, so the
	// two cannot name different packages.
	v1Import = "github.com/metacensus/api/go/metacensus/v1"

	// apiPrefix is the path every route in the manifest is relative to, and the
	// only place this string is written down. Both generated manifests take it
	// from here, including their prose, so the constant and the paths it
	// describes cannot drift apart.
	//
	// It is not in the .proto: google.api.http annotations carry a path each and
	// protobuf has no notion of a string constant, so expressing it there would
	// mean a custom FileOptions extension and a non-resource .proto file inside
	// a schema whose tests assert every file is a resource. Not worth it for one
	// string that the route generator is already the authority on.
	apiPrefix = "/metacensus/api/v1"
)

type route struct {
	Service  string
	RPC      string
	Method   string
	Path     string
	Params   []string
	Query    []string
	Body     string
	Request  string
	Response string

	// The server rendering needs the Go side of the same facts: the Go type
	// names protoc-gen-go gave the messages and, per path parameter, the Go
	// struct field it binds. None of these are derived by re-implementing
	// protoc-gen-go's naming; see goNames.
	GoRequest  string
	GoResponse string
	PathFields []pathField
}

// pathField pairs a path parameter's wire spelling with the generated Go
// struct field that carries it.
type pathField struct {
	JSONName string
	GoField  string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "routegen:", err)
		os.Exit(1)
	}
}

// modulePath anchors the cwd check below.
const modulePath = "module github.com/metacensus/api"

// checkRoot fails loudly when routegen is run from anywhere but the repository
// root. goOut and tsOut are relative, so a wrong cwd does not error — it writes
// the manifests somewhere else and leaves the committed ones stale, which the
// freshness check cannot see because nothing in the tree changed.
func checkRoot() error {
	b, err := os.ReadFile("go.mod")
	if err != nil || !strings.Contains(string(b), modulePath) {
		wd, _ := os.Getwd()
		return fmt.Errorf("run from the repository root (cwd is %s); use `make gen`", wd)
	}
	return nil
}

func run() error {
	if err := checkRoot(); err != nil {
		return err
	}

	var routes []route
	var clientRoutes []clientRoute

	for _, svc := range services() {
		for i := 0; i < svc.Methods().Len(); i++ {
			md := svc.Methods().Get(i)
			r, err := describe(md)
			if err != nil {
				return err
			}
			routes = append(routes, r)

			cr, err := describeClient(md, r)
			if err != nil {
				return err
			}
			clientRoutes = append(clientRoutes, cr)
		}
	}
	if len(routes) == 0 {
		return fmt.Errorf("no routes found in package %s", protoPkg)
	}

	if err := write(goOut, renderGo(routes)); err != nil {
		return err
	}
	if err := write(tsOut, renderTS(routes)); err != nil {
		return err
	}
	if err := write(srvOut, renderServer(routes)); err != nil {
		return err
	}
	return write(clientOut, renderClient(clientRoutes))
}

// services returns every service in the package, ordered by file then by
// declaration, so the manifest is stable across runs.
func services() []protoreflect.ServiceDescriptor {
	var files []protoreflect.FileDescriptor
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) == protoPkg {
			files = append(files, fd)
		}
		return true
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path() < files[j].Path() })

	var out []protoreflect.ServiceDescriptor
	for _, fd := range files {
		for i := 0; i < fd.Services().Len(); i++ {
			out = append(out, fd.Services().Get(i))
		}
	}
	return out
}

func describe(md protoreflect.MethodDescriptor) (route, error) {
	rule, ok := proto.GetExtension(md.Options(), annotations.E_Http).(*annotations.HttpRule)
	if !ok || rule == nil {
		return route{}, fmt.Errorf("%s: no google.api.http option", md.Name())
	}
	if n := len(rule.GetAdditionalBindings()); n > 0 {
		return route{}, fmt.Errorf("%s: %d additional_bindings; the manifest holds one route per rpc", md.Name(), n)
	}

	var method, path string
	switch p := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		method, path = "GET", p.Get
	case *annotations.HttpRule_Post:
		method, path = "POST", p.Post
	case *annotations.HttpRule_Put:
		method, path = "PUT", p.Put
	case *annotations.HttpRule_Patch:
		method, path = "PATCH", p.Patch
	case *annotations.HttpRule_Delete:
		method, path = "DELETE", p.Delete
	default:
		return route{}, fmt.Errorf("%s: unsupported http pattern %T", md.Name(), p)
	}

	body := rule.GetBody()
	path, params, bound, err := rewriteParams(path, md.Input())
	if err != nil {
		return route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}
	query, err := queryParams(md.Input(), bound, body)
	if err != nil {
		return route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}

	// The server rendering binds the request statically, so it needs facts
	// the manifest does not, and it refuses shapes it could not bind.
	if body != "" && body != "*" {
		// A named body decodes the body into one sub-message and leaves the
		// rest to the query string. Nothing in the contract does this and the
		// binding it needs (a message-typed field, a second decode target) is
		// not worth carrying unused.
		return route{}, fmt.Errorf("%s: body %q names a field; only body: \"*\" or no body is supported", md.Name(), body)
	}
	req, resp, err := goNames(md.Input(), md.Output())
	if err != nil {
		return route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}
	var pathFields []pathField
	for _, p := range params {
		fd := fieldByJSONName(md.Input(), p)
		if fd.Kind() != protoreflect.StringKind || fd.IsList() {
			// Path segments are strings on the wire. Binding a non-string
			// would mean parsing, with a 400 on failure and a Go-side
			// conversion per kind; ids are strings here (TestIdsAreStrings),
			// so this is a shape the contract does not have.
			return route{}, fmt.Errorf("%s: path parameter %q is %s, not a singular string", md.Name(), p, fd.Kind())
		}
		if fd.ContainingOneof() != nil {
			return route{}, fmt.Errorf("%s: path parameter %q is a oneof member; the generated binding sets struct fields directly", md.Name(), p)
		}
		goField, ok := req.fields[fd.Name()]
		if !ok {
			return route{}, fmt.Errorf("%s: no Go struct field carries %s", md.Name(), fd.FullName())
		}
		pathFields = append(pathFields, pathField{JSONName: p, GoField: goField})
	}
	for _, q := range query {
		fd := fieldByJSONName(md.Input(), q)
		if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind || fd.IsMap() {
			// Query strings carry scalars. google.api.http allows nested
			// `a.b=1`, which needs a path walker; none of the routes need it.
			return route{}, fmt.Errorf("%s: query parameter %q is %s; only scalar query fields are supported", md.Name(), q, fd.Kind())
		}
	}

	return route{
		Service:    string(md.Parent().Name()),
		RPC:        string(md.Name()),
		Method:     method,
		Path:       path,
		Params:     params,
		Query:      query,
		Body:       body,
		Request:    string(md.Input().Name()),
		Response:   string(md.Output().Name()),
		GoRequest:  req.typeName,
		GoResponse: resp.typeName,
		PathFields: pathFields,
	}, nil
}

func fieldByJSONName(md protoreflect.MessageDescriptor, jsonName string) protoreflect.FieldDescriptor {
	return md.Fields().ByJSONName(jsonName)
}

// goType is what the server rendering knows about one generated Go message
// type: its name and the Go struct field for each proto field.
type goType struct {
	typeName string
	fields   map[protoreflect.Name]string
}

// goNames reads the Go names protoc-gen-go emitted, off the generated code
// itself, rather than re-deriving them. protoc-gen-go's camel-casing and its
// collision suffixing live in internal packages that cannot be imported, and
// a copy would drift from them silently. The generated struct carries a
// `protobuf:"...,name=<proto name>,..."` tag on every field, so the mapping
// from proto field to Go field is read from the type that the descriptors
// registered — whatever protoc-gen-go produced is by construction what is
// returned. TestGoNamesAgreeWithProtogen cross-checks this against
// compiler/protogen, the public package protoc-gen-go is built on.
func goNames(mds ...protoreflect.MessageDescriptor) (goType, goType, error) {
	var out [2]goType
	for i, md := range mds {
		mt, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
		if err != nil {
			return goType{}, goType{}, fmt.Errorf("%s: not registered as a Go type: %w", md.FullName(), err)
		}
		t := reflect.TypeOf(mt.New().Interface())
		if t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
			return goType{}, goType{}, fmt.Errorf("%s: Go type %s is not a pointer to struct", md.FullName(), t)
		}
		if t.Elem().PkgPath() != v1Import {
			return goType{}, goType{}, fmt.Errorf("%s: Go type %s is not in %s", md.FullName(), t, v1Import)
		}
		out[i] = goType{typeName: t.Elem().Name(), fields: map[protoreflect.Name]string{}}
		for j := 0; j < t.Elem().NumField(); j++ {
			sf := t.Elem().Field(j)
			for _, part := range strings.Split(sf.Tag.Get("protobuf"), ",") {
				if name, ok := strings.CutPrefix(part, "name="); ok {
					out[i].fields[protoreflect.Name(name)] = sf.Name
				}
			}
		}
		// Every field the descriptor declares must have been found on the
		// struct, or the tag format has changed under us.
		for j := 0; j < md.Fields().Len(); j++ {
			fd := md.Fields().Get(j)
			if fd.ContainingOneof() != nil {
				continue // oneof members sit behind an interface field
			}
			if _, ok := out[i].fields[fd.Name()]; !ok {
				return goType{}, goType{}, fmt.Errorf("%s: no struct field tagged name=%s on %s", md.FullName(), fd.Name(), t)
			}
		}
	}
	return out[0], out[1], nil
}

// rewriteParams replaces each {field} segment with {jsonName} and returns the
// parameters in path order alongside the proto names they bound. A segment
// naming no field of the request is an error: it would be a route no
// implementation could bind.
func rewriteParams(path string, req protoreflect.MessageDescriptor) (string, []string, map[protoreflect.Name]bool, error) {
	var out strings.Builder
	var params []string
	bound := map[protoreflect.Name]bool{}

	for len(path) > 0 {
		open := strings.IndexByte(path, '{')
		if open < 0 {
			out.WriteString(path)
			break
		}
		shut := strings.IndexByte(path[open:], '}')
		if shut < 0 {
			return "", nil, nil, fmt.Errorf("unterminated path parameter in %q", path)
		}
		shut += open

		name, _, _ := strings.Cut(path[open+1:shut], "=")
		fd := req.Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			return "", nil, nil, fmt.Errorf("path parameter %q is not a field of %s", name, req.FullName())
		}

		out.WriteString(path[:open])
		out.WriteString("{" + fd.JSONName() + "}")
		params = append(params, fd.JSONName())
		bound[fd.Name()] = true
		path = path[shut+1:]
	}

	return out.String(), params, bound, nil
}

// queryParams returns the request fields that travel in the query string: every
// field the path did not bind and the body does not carry. `body: "*"` carries
// all of them, a named body carries that one field, and an absent body carries
// none.
func queryParams(req protoreflect.MessageDescriptor, bound map[protoreflect.Name]bool, body string) ([]string, error) {
	if body == "*" {
		return nil, nil
	}
	if body != "" && req.Fields().ByName(protoreflect.Name(body)) == nil {
		return nil, fmt.Errorf("body %q is not a field of %s", body, req.FullName())
	}

	var out []string
	fields := req.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if bound[fd.Name()] || string(fd.Name()) == body {
			continue
		}
		out = append(out, fd.JSONName())
	}
	return out, nil
}

func renderGo(routes []route) []byte {
	var b bytes.Buffer

	b.WriteString("// Code generated by cmd/routegen. DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "// Package routes is the whole route table of %s on one\n", apiPrefix)
	b.WriteString("// screen, generated from the google.api.http annotations on each resource's\n")
	b.WriteString("// service in metacensus/v1.\n")
	b.WriteString("package routes\n\n")
	b.WriteString("// Prefix is the path every route below hangs off. Join it with a Route's\n")
	b.WriteString("// Path to get the path a client actually requests; the reverse proxy in\n")
	b.WriteString("// front of the API is what makes that resolve.\n")
	fmt.Fprintf(&b, "const Prefix = %q\n\n", apiPrefix)
	fmt.Fprintf(&b, "// Route is one declared route. Path is relative to %s and\n", apiPrefix)
	b.WriteString("// spells its parameters {lowerCamelCase}, as the wire does. Params, Query and\n")
	b.WriteString("// Body between them account for every field of Request: Params bind path\n")
	b.WriteString("// segments, Query travels in the query string, and Body is \"*\" when the rest\n")
	b.WriteString("// travels in the body and empty when none does.\n")
	b.WriteString("type Route struct {\n")
	b.WriteString("\tService  string\n")
	b.WriteString("\tRPC      string\n")
	b.WriteString("\tMethod   string\n")
	b.WriteString("\tPath     string\n")
	b.WriteString("\tParams   []string\n")
	b.WriteString("\tQuery    []string\n")
	b.WriteString("\tBody     string\n")
	b.WriteString("\tRequest  string\n")
	b.WriteString("\tResponse string\n")
	b.WriteString("}\n\n")
	b.WriteString("// Routes is every route across every service, ordered by file then by\n")
	b.WriteString("// declaration.\n")
	b.WriteString("var Routes = []Route{\n")

	for _, r := range routes {
		fmt.Fprintf(&b, "\t{Service: %q, RPC: %q, Method: %q, Path: %q, Params: %s, Query: %s, Body: %q, Request: %q, Response: %q},\n",
			r.Service, r.RPC, r.Method, r.Path, goSlice(r.Params), goSlice(r.Query), r.Body, r.Request, r.Response)
	}

	b.WriteString("}\n")

	src, err := format.Source(b.Bytes())
	if err != nil {
		panic(err)
	}
	return src
}

func goSlice(items []string) string {
	if len(items) == 0 {
		return "nil"
	}
	return "[]string{" + strings.Join(quoteAll(items), ", ") + "}"
}

func quoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

func renderTS(routes []route) []byte {
	var b bytes.Buffer

	b.WriteString("// Code generated by cmd/routegen. DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "// The whole route table of %s on one screen, generated from\n", apiPrefix)
	b.WriteString("// the google.api.http annotations on each resource's service in metacensus/v1.\n\n")
	b.WriteString("// The path every route below hangs off. Join it with a route's `path` to get\n")
	b.WriteString("// the path a client actually requests; the reverse proxy in front of the API\n")
	b.WriteString("// is what makes that resolve.\n")
	fmt.Fprintf(&b, "export const apiPrefix = %q;\n\n", apiPrefix)
	fmt.Fprintf(&b, "// `path` is relative to %s and spells its parameters\n", apiPrefix)
	b.WriteString("// {lowerCamelCase}, as the wire does. `params`, `query` and `body` between them\n")
	b.WriteString("// account for every field of `request`: `params` bind path segments, `query`\n")
	b.WriteString("// travels in the query string, and `body` is \"*\" when the rest travels in the\n")
	b.WriteString("// body and \"\" when none does.\n")
	b.WriteString("export interface Route {\n")
	b.WriteString("  readonly service: string;\n")
	b.WriteString("  readonly rpc: string;\n")
	b.WriteString("  readonly method: string;\n")
	b.WriteString("  readonly path: string;\n")
	b.WriteString("  readonly params: readonly string[];\n")
	b.WriteString("  readonly query: readonly string[];\n")
	b.WriteString("  readonly body: string;\n")
	b.WriteString("  readonly request: string;\n")
	b.WriteString("  readonly response: string;\n")
	b.WriteString("}\n\n")
	b.WriteString("export const routes: readonly Route[] = [\n")

	for _, r := range routes {
		fmt.Fprintf(&b, "  { service: %q, rpc: %q, method: %q, path: %q, params: [%s], query: [%s], body: %q, request: %q, response: %q },\n",
			r.Service, r.RPC, r.Method, r.Path,
			strings.Join(quoteAll(r.Params), ", "), strings.Join(quoteAll(r.Query), ", "),
			r.Body, r.Request, r.Response)
	}

	b.WriteString("];\n")
	return b.Bytes()
}

func write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// renderServer writes the Go server half: one handler interface per service,
// an Unimplemented* per service, and one Register* per service that binds
// each route's request and dispatches it. The hand-written runtime it calls
// into is go/server/runtime.go.
func renderServer(routes []route) []byte {
	var b bytes.Buffer

	b.WriteString("// Code generated by cmd/routegen. DO NOT EDIT.\n\n")
	b.WriteString("package server\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	fmt.Fprintf(&b, "\tv1 %q\n", v1Import)
	b.WriteString(")\n\n")

	// Group by service, preserving order.
	var order []string
	byService := map[string][]route{}
	for _, r := range routes {
		if _, ok := byService[r.Service]; !ok {
			order = append(order, r.Service)
		}
		byService[r.Service] = append(byService[r.Service], r)
	}

	for _, svc := range order {
		rs := byService[svc]

		fmt.Fprintf(&b, "// %s is what an implementation of the %s service provides.\n", svc, svc)
		b.WriteString("// One method per route; the request carries path, query and body fields\n")
		b.WriteString("// already bound.\n")
		fmt.Fprintf(&b, "type %s interface {\n", svc)
		for _, r := range rs {
			fmt.Fprintf(&b, "\t// %s %s\n", r.Method, r.Path)
			fmt.Fprintf(&b, "\t%s(context.Context, *v1.%s) (*v1.%s, error)\n", r.RPC, r.GoRequest, r.GoResponse)
		}
		b.WriteString("}\n\n")

		fmt.Fprintf(&b, "// Unimplemented%s answers every %s route with 501. Embed it to\n", svc, svc)
		b.WriteString("// implement a service one route at a time.\n")
		fmt.Fprintf(&b, "type Unimplemented%s struct{}\n\n", svc)
		for _, r := range rs {
			fmt.Fprintf(&b, "func (Unimplemented%s) %s(context.Context, *v1.%s) (*v1.%s, error) {\n", svc, r.RPC, r.GoRequest, r.GoResponse)
			fmt.Fprintf(&b, "\treturn nil, errNotImplemented(%q)\n", svc+"."+r.RPC)
			b.WriteString("}\n\n")
		}

		fmt.Fprintf(&b, "// Register%s registers every %s route on mux, under rt's prefix. mux\n", svc, svc)
		b.WriteString("// needs only Method(method, pattern string, http.Handler): a *http.ServeMux\n")
		b.WriteString("// wrapped in StdMux, or a chi.Router, both satisfy it.\n")
		fmt.Fprintf(&b, "func Register%s(mux Mux, rt *Runtime, impl %s) {\n", svc, svc)
		for _, r := range rs {
			fmt.Fprintf(&b, "\tmux.Method(%q, rt.prefix()+%q, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n", r.Method, r.Path)
			fmt.Fprintf(&b, "\t\treq := new(v1.%s)\n", r.GoRequest)
			if r.Body != "*" && len(r.PathFields) > 0 {
				b.WriteString("\t\tvar err error\n")
			}
			if r.Body == "*" {
				b.WriteString("\t\traw, err := rt.readBody(w, r)\n")
				b.WriteString("\t\tif err != nil {\n\t\t\trt.writeError(w, err)\n\t\t\treturn\n\t\t}\n")
				b.WriteString("\t\tif err := rt.verifyBody(r, raw); err != nil {\n\t\t\trt.writeError(w, err)\n\t\t\treturn\n\t\t}\n")
				b.WriteString("\t\tif err := rt.decodeBody(raw, req); err != nil {\n\t\t\trt.writeError(w, err)\n\t\t\treturn\n\t\t}\n")
			}
			if len(r.Query) > 0 {
				fmt.Fprintf(&b, "\t\tif err := rt.bindQuery(r, req, %s); err != nil {\n\t\t\trt.writeError(w, err)\n\t\t\treturn\n\t\t}\n", goSlice(r.Query))
			}
			for _, p := range r.PathFields {
				// The body may legitimately repeat a path-bound field (a
				// request message models the whole request). The path is
				// authoritative; a body value that disagrees is a 400.
				fmt.Fprintf(&b, "\t\tif req.%s, err = rt.pathParam(r, %q, req.%s); err != nil {\n\t\t\trt.writeError(w, err)\n\t\t\treturn\n\t\t}\n", p.GoField, p.JSONName, p.GoField)
			}
			fmt.Fprintf(&b, "\t\tresp, err := impl.%s(r.Context(), req)\n", r.RPC)
			b.WriteString("\t\trt.respond(w, resp, err)\n")
			b.WriteString("\t}))\n")
		}
		b.WriteString("}\n\n")
	}

	src, err := format.Source(b.Bytes())
	if err != nil {
		// Show the unformatted source: the error's line numbers refer to it.
		panic(fmt.Sprintf("%v\n%s", err, b.Bytes()))
	}
	return src
}
