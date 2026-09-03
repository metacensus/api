// Command routegen writes the route manifest for Go and TypeScript from the
// google.api.http annotations on every service in metacensus.v1. It reads the
// descriptors the generated Go package registers, so it runs after
// `buf generate`.
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
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
	srvOut   = "go/server/routes.gen.go"

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

// fieldRef is one request field a route binds. JSON is the name on the wire and
// in the manifests; Go is the field protoc-gen-go generates for it, which the
// server renderer assigns to and the manifests never mention.
type fieldRef struct {
	JSON string
	Go   string
}

type route struct {
	Service  string
	RPC      string
	Method   string
	Path     string
	Params   []fieldRef
	Query    []fieldRef
	Body     string
	Request  string
	Response string
}

// jsonNames is the wire names of refs, which is all either manifest carries.
func jsonNames(refs []fieldRef) []string {
	out := make([]string, len(refs))
	for i, f := range refs {
		out[i] = f.JSON
	}
	return out
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

	for _, svc := range services() {
		for i := 0; i < svc.Methods().Len(); i++ {
			r, err := describe(svc.Methods().Get(i))
			if err != nil {
				return err
			}
			routes = append(routes, r)
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
	return write(srvOut, renderServer(routes))
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
	if body != "" && body != "*" {
		return route{}, fmt.Errorf("%s: body %q names a single field; generated binding decodes "+
			"the whole request message or none of it", md.Name(), body)
	}
	path, params, bound, err := rewriteParams(path, md.Input())
	if err != nil {
		return route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}
	query, err := queryParams(md.Input(), bound, body)
	if err != nil {
		return route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}

	return route{
		Service:  string(md.Parent().Name()),
		RPC:      string(md.Name()),
		Method:   method,
		Path:     path,
		Params:   params,
		Query:    query,
		Body:     body,
		Request:  string(md.Input().Name()),
		Response: string(md.Output().Name()),
	}, nil
}

// rewriteParams replaces each {field} segment with {jsonName} and returns the
// parameters in path order alongside the proto names they bound. A segment
// naming no field of the request is an error: it would be a route no
// implementation could bind.
func rewriteParams(path string, req protoreflect.MessageDescriptor) (string, []fieldRef, map[protoreflect.Name]bool, error) {
	var out strings.Builder
	var params []fieldRef
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

		if fd.Kind() != protoreflect.StringKind || fd.IsList() || fd.IsMap() {
			return "", nil, nil, fmt.Errorf("path parameter %q is not a singular string on %s; "+
				"generated binding cannot parse it", name, req.FullName())
		}

		out.WriteString(path[:open])
		out.WriteString("{" + fd.JSONName() + "}")
		params = append(params, refOf(fd))
		bound[fd.Name()] = true
		path = path[shut+1:]
	}

	return out.String(), params, bound, nil
}

// queryParams returns the request fields that travel in the query string: every
// field the path did not bind and the body does not carry. `body: "*"` carries
// all of them, a named body carries that one field, and an absent body carries
// none.
func queryParams(req protoreflect.MessageDescriptor, bound map[protoreflect.Name]bool, body string) ([]fieldRef, error) {
	if body == "*" {
		return nil, nil
	}
	if body != "" && req.Fields().ByName(protoreflect.Name(body)) == nil {
		return nil, fmt.Errorf("body %q is not a field of %s", body, req.FullName())
	}

	var out []fieldRef
	fields := req.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if bound[fd.Name()] || string(fd.Name()) == body {
			continue
		}
		// Generated binding assigns a string. A non-string query field would
		// need parsing this does not do, so it is an error rather than a
		// silently dropped parameter.
		if fd.Kind() != protoreflect.StringKind || fd.IsList() || fd.IsMap() {
			return nil, fmt.Errorf("%s.%s travels in the query string but is not a singular string; "+
				"generated binding cannot parse it", req.FullName(), fd.Name())
		}
		out = append(out, refOf(fd))
	}
	return out, nil
}

// refOf names a field on the wire and in generated Go.
func refOf(fd protoreflect.FieldDescriptor) fieldRef {
	return fieldRef{JSON: fd.JSONName(), Go: goCamelCase(string(fd.Name()))}
}

// goCamelCase is protoc-gen-go's field naming, reimplemented because the
// upstream copy lives in an internal package. It has to agree exactly: the
// generated server assigns to the fields protoc-gen-go emitted, so a
// disagreement is a compile error in generated code.
func goCamelCase(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '.' && i+1 < len(s) && isASCIILower(s[i+1]):
			// Skip over '.' in ".{{lowercase}}".
		case c == '.':
			b = append(b, '_')
		case c == '_' && (i == 0 || s[i-1] == '.'):
			b = append(b, 'X')
		case c == '_' && i+1 < len(s) && isASCIILower(s[i+1]):
			// Skip the underscore; the next iteration capitalises.
		case isASCIIDigit(c):
			b = append(b, c)
		default:
			// A letter starts a word, so it is upper case; the lower-case run
			// that follows is copied as it stands.
			if isASCIILower(c) {
				c -= 'a' - 'A'
			}
			b = append(b, c)
			for ; i+1 < len(s) && isASCIILower(s[i+1]); i++ {
				b = append(b, s[i+1])
			}
		}
	}
	return string(b)
}

func isASCIILower(c byte) bool { return 'a' <= c && c <= 'z' }
func isASCIIDigit(c byte) bool { return '0' <= c && c <= '9' }

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
			r.Service, r.RPC, r.Method, r.Path, goSlice(jsonNames(r.Params)), goSlice(jsonNames(r.Query)), r.Body, r.Request, r.Response)
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
			strings.Join(quoteAll(jsonNames(r.Params)), ", "), strings.Join(quoteAll(jsonNames(r.Query)), ", "),
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

// renderServer writes the server package's generated half: one interface per
// service, and the registration and binding that drive it.
//
// The interfaces are the point. An implementation missing a route, or carrying
// the wrong request or response type, does not build — which is a stronger
// guarantee than the manifest, since the manifest is a definition nothing
// compares an implementation to.
func renderServer(routes []route) []byte {
	var b bytes.Buffer

	b.WriteString("// Code generated by cmd/routegen. DO NOT EDIT.\n\n")
	b.WriteString("package server\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString("\tv1 \"github.com/metacensus/api/go/metacensus/v1\"\n")
	b.WriteString(")\n\n")

	for _, svc := range groupByService(routes) {
		renderService(&b, svc)
	}

	src, err := format.Source(b.Bytes())
	if err != nil {
		panic(err)
	}
	return src
}

// service is one service's routes, in declaration order.
type service struct {
	Name   string
	Routes []route
}

// groupByService keeps the manifest's order: routes arrive grouped by service
// already, so this only draws the boundaries.
func groupByService(routes []route) []service {
	var out []service
	for _, r := range routes {
		if len(out) == 0 || out[len(out)-1].Name != r.Service {
			out = append(out, service{Name: r.Service})
		}
		out[len(out)-1].Routes = append(out[len(out)-1].Routes, r)
	}
	return out
}

func renderService(b *bytes.Buffer, svc service) {
	fmt.Fprintf(b, "// %s is the %s side of every route declared by the %s\n", svc.Name, "server", svc.Name)
	fmt.Fprintf(b, "// service. An implementation satisfies it or does not compile.\n")
	fmt.Fprintf(b, "type %s interface {\n", svc.Name)
	for _, r := range svc.Routes {
		fmt.Fprintf(b, "\t// %s serves %s %s.\n", r.RPC, r.Method, apiPrefix+r.Path)
		fmt.Fprintf(b, "\t%s(context.Context, *v1.%s) (*v1.%s, error)\n", r.RPC, r.Request, r.Response)
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// Register%s registers every %s route on m.\n", svc.Name, svc.Name)
	fmt.Fprintf(b, "func (m Mux) Register%s(h %s) {\n", svc.Name, svc.Name)
	for _, r := range svc.Routes {
		renderRoute(b, r)
	}
	b.WriteString("}\n\n")
}

func renderRoute(b *bytes.Buffer, r route) {
	fmt.Fprintf(b, "\tm.handle(routeOf(%q, %q), func(w http.ResponseWriter, r *http.Request) {\n", r.Service, r.RPC)
	fmt.Fprintf(b, "\t\treq := &v1.%s{}\n", r.Request)

	if r.Body == "*" {
		b.WriteString("\t\tif err := m.bindBody(w, r, req); err != nil {\n")
		b.WriteString("\t\t\tm.respond(w, r, nil, err)\n")
		b.WriteString("\t\t\treturn\n")
		b.WriteString("\t\t}\n")
	}
	// Path wins over the body: the path is what routed the request, so a body
	// field of the same name is the caller disagreeing with the URL it called.
	for _, f := range r.Params {
		fmt.Fprintf(b, "\t\treq.%s = m.bindPath(r, %q)\n", f.Go, f.JSON)
	}
	for _, f := range r.Query {
		fmt.Fprintf(b, "\t\treq.%s = bindQuery(r, %q)\n", f.Go, f.JSON)
	}

	fmt.Fprintf(b, "\t\tresp, err := h.%s(r.Context(), req)\n", r.RPC)
	b.WriteString("\t\tm.respond(w, r, resp, err)\n")
	b.WriteString("\t})\n")
}
