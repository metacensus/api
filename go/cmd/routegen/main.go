// Command routegen writes the route manifest for Go and TypeScript from the
// google.api.http annotations on every service in every contract package. It
// reads the descriptors the generated Go packages register, so it runs after
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

	"github.com/metacensus/api/go/internal/protoscan"
	_ "github.com/metacensus/api/go/metacensus/public/v1"
	_ "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Paths are relative to the repository root, which is where the Makefile runs
// this. Everything else in the build — buf, the generator plugins — is rooted
// there too.
const (
	protoDir = "proto"
	goOut    = "go/routes/manifest.go"
	tsOut    = "ts/src/route-manifest.ts"
)

// packages is the only place either prefix is written down; the manifests take
// theirs from here so the constant and the paths it describes cannot drift.
// They are not in the .proto because google.api.http carries a path per route
// and protobuf has no string constant, so expressing them there would need a
// custom FileOptions extension in a non-resource file.
//
// Order is the manifest's order.
var packages = []contractPackage{
	{
		Proto:   "metacensus.v1",
		Prefix:  "/metacensus/api/v1",
		GoConst: "Prefix",
		TSConst: "apiPrefix",
		Summary: "the authenticated API",
	},
	{
		Proto: "metacensus.public.v1",
		// No version segment: see README.md, "Versioning the public surface".
		Prefix:  "/metacensus/public",
		GoConst: "PublicPrefix",
		TSConst: "publicPrefix",
		Summary: "the public, unauthenticated surface",
	},
}

type contractPackage struct {
	Proto   string
	Prefix  string
	GoConst string
	TSConst string
	Summary string
}

type route struct {
	// Carried per route: with two prefixes, a consumer holding one has no
	// other way to know which to join.
	Prefix   string
	Service  string
	RPC      string
	Method   string
	Path     string
	Params   []string
	Query    []string
	Body     string
	Request  string
	Response string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "routegen:", err)
		os.Exit(1)
	}
}

const modulePath = "module github.com/metacensus/api"

// checkRoot fails when routegen runs from anywhere but the repository root.
// The paths above are relative, so a wrong cwd writes the manifests elsewhere
// and leaves the committed ones stale — a drift the freshness check cannot see,
// because nothing in the tree changed.
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

	if err := checkPackages(); err != nil {
		return err
	}

	var routes []route

	for _, pkg := range packages {
		before := len(routes)
		for _, svc := range services(pkg.Proto) {
			for i := 0; i < svc.Methods().Len(); i++ {
				r, err := describe(pkg, svc.Methods().Get(i))
				if err != nil {
					return err
				}
				routes = append(routes, r)
			}
		}
		if len(routes) == before {
			return fmt.Errorf("no routes found in package %s", pkg.Proto)
		}
	}

	if err := write(goOut, renderGo(routes)); err != nil {
		return err
	}
	return write(tsOut, renderTS(routes))
}

// checkPackages fails when the table above does not name a package that exists
// on disk, or names one whose generated Go is not imported here. Either way its
// routes would be missing from the manifest with nothing to say so.
//
// On disk, not protoregistry: the registry holds only what this binary
// imported, so a package nobody imported is absent from both the manifest and
// the check that would have caught it.
func checkPackages() error {
	onDisk, err := protoscan.Packages(protoDir)
	if err != nil {
		return err
	}

	named := map[string]bool{}
	for _, pkg := range packages {
		named[pkg.Proto] = true
	}

	var stray []string
	for _, pkg := range onDisk {
		if !named[pkg] {
			stray = append(stray, pkg)
		}
	}
	if len(stray) > 0 {
		return fmt.Errorf("%s under %s/ but not in routegen's table, so their routes "+
			"would be missing from the manifest. Add an entry with the prefix each one's "+
			"routes hang off", strings.Join(stray, ", "), protoDir)
	}

	registered := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		registered[string(fd.Package())] = true
		return true
	})
	for _, pkg := range packages {
		if !registered[pkg.Proto] {
			return fmt.Errorf("no file registers %s; add a blank import of its "+
				"generated Go package here", pkg.Proto)
		}
	}
	return nil
}

// services returns every service in the package, ordered by file then by
// declaration, so the manifest is stable across runs.
func services(protoPkg string) []protoreflect.ServiceDescriptor {
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

func describe(pkg contractPackage, md protoreflect.MethodDescriptor) (route, error) {
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

	return route{
		Prefix:   pkg.Prefix,
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
	b.WriteString("// Package routes is every MetaCensus route on one screen, generated from the\n")
	b.WriteString("// google.api.http annotations on each service.\n")
	b.WriteString("//\n")
	b.WriteString("// Two surfaces are declared here, under two prefixes:\n")
	b.WriteString("//\n")
	for _, pkg := range packages {
		fmt.Fprintf(&b, "//\t%-12s %-20s %s\n", pkg.GoConst, pkg.Prefix, pkg.Summary)
	}
	b.WriteString("//\n")
	b.WriteString("// One manifest rather than one per surface: the client that consumes both is\n")
	b.WriteString("// the reason the contract lives in one repository at all, and a route table\n")
	b.WriteString("// split in two is a table no one reads whole.\n")
	b.WriteString("package routes\n\n")
	for _, pkg := range packages {
		fmt.Fprintf(&b, "// %s is the path %s routes hang off:\n", pkg.GoConst, pkg.Proto)
		fmt.Fprintf(&b, "// %s.\n//\n", pkg.Summary)
		b.WriteString("// Join it with a Route's Path to get the path a client actually requests;\n")
		b.WriteString("// the reverse proxy in front of both services is what makes that resolve.\n")
		b.WriteString("// A Route carries its own Prefix, so joining does not mean knowing which\n")
		b.WriteString("// surface a route came from.\n")
		fmt.Fprintf(&b, "const %s = %q\n\n", pkg.GoConst, pkg.Prefix)
	}
	b.WriteString("// Route is one declared route. Path is relative to Prefix and\n")
	b.WriteString("// spells its parameters {lowerCamelCase}, as the wire does. Params, Query and\n")
	b.WriteString("// Body between them account for every field of Request: Params bind path\n")
	b.WriteString("// segments, Query travels in the query string, and Body is \"*\" when the rest\n")
	b.WriteString("// travels in the body and empty when none does.\n")
	b.WriteString("type Route struct {\n")
	b.WriteString("\tPrefix   string\n")
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
	b.WriteString("// Routes is every route across every surface, ordered by package, then by\n")
	b.WriteString("// file, then by declaration.\n")
	b.WriteString("var Routes = []Route{\n")

	for _, r := range routes {
		fmt.Fprintf(&b, "\t{Prefix: %s, Service: %q, RPC: %q, Method: %q, Path: %q, Params: %s, Query: %s, Body: %q, Request: %q, Response: %q},\n",
			goConstFor(r.Prefix), r.Service, r.RPC, r.Method, r.Path, goSlice(r.Params), goSlice(r.Query), r.Body, r.Request, r.Response)
	}

	b.WriteString("}\n")

	src, err := format.Source(b.Bytes())
	if err != nil {
		panic(err)
	}
	return src
}

// goConstFor renders a route's Prefix as the constant rather than the literal,
// so the two cannot be edited apart in the generated file either.
func goConstFor(prefix string) string {
	for _, pkg := range packages {
		if pkg.Prefix == prefix {
			return pkg.GoConst
		}
	}
	panic("no constant for prefix " + prefix)
}

func tsConstFor(prefix string) string {
	for _, pkg := range packages {
		if pkg.Prefix == prefix {
			return pkg.TSConst
		}
	}
	panic("no constant for prefix " + prefix)
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
	b.WriteString("// Every MetaCensus route on one screen, generated from the google.api.http\n")
	b.WriteString("// annotations on each service. Two surfaces, under two prefixes:\n")
	b.WriteString("//\n")
	for _, pkg := range packages {
		fmt.Fprintf(&b, "//   %-12s %-20s %s\n", pkg.TSConst, pkg.Prefix, pkg.Summary)
	}
	b.WriteString("//\n")
	b.WriteString("// One manifest rather than one per surface: the client that consumes both is\n")
	b.WriteString("// the reason the contract lives in one repository at all, and a route table\n")
	b.WriteString("// split in two is a table no one reads whole.\n\n")
	for _, pkg := range packages {
		fmt.Fprintf(&b, "// The path %s routes hang off:\n", pkg.Proto)
		fmt.Fprintf(&b, "// %s. Join it with a\n", pkg.Summary)
		b.WriteString("// route's `path` to get the path a client actually requests; the reverse proxy\n")
		b.WriteString("// in front of both services is what makes that resolve. A route carries its\n")
		b.WriteString("// own `prefix`, so joining does not mean knowing which surface it came from.\n")
		fmt.Fprintf(&b, "export const %s = %q;\n\n", pkg.TSConst, pkg.Prefix)
	}
	b.WriteString("// `path` is relative to `prefix` and spells its parameters\n")
	b.WriteString("// {lowerCamelCase}, as the wire does. `params`, `query` and `body` between them\n")
	b.WriteString("// account for every field of `request`: `params` bind path segments, `query`\n")
	b.WriteString("// travels in the query string, and `body` is \"*\" when the rest travels in the\n")
	b.WriteString("// body and \"\" when none does.\n")
	b.WriteString("export interface Route {\n")
	b.WriteString("  readonly prefix: string;\n")
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
		fmt.Fprintf(&b, "  { prefix: %s, service: %q, rpc: %q, method: %q, path: %q, params: [%s], query: [%s], body: %q, request: %q, response: %q },\n",
			tsConstFor(r.Prefix), r.Service, r.RPC, r.Method, r.Path,
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
