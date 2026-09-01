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

	_ "github.com/metacensus/ui/contract/go/metacensus/v1"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const (
	protoPkg = "metacensus.v1"
	goOut    = "routes/manifest.go"
	tsOut    = "../ts/src/route-manifest.ts"
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
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "routegen:", err)
		os.Exit(1)
	}
}

func run() error {
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
	return write(tsOut, renderTS(routes))
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
	b.WriteString("// Package routes is the whole route table of /metacensus/api/v1 on one\n")
	b.WriteString("// screen, generated from the google.api.http annotations on each resource's\n")
	b.WriteString("// service in metacensus/v1.\n")
	b.WriteString("package routes\n\n")
	b.WriteString("// Route is one declared route. Path is relative to /metacensus/api/v1 and\n")
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
	b.WriteString("// The whole route table of /metacensus/api/v1 on one screen, generated from\n")
	b.WriteString("// the google.api.http annotations on each resource's service in metacensus/v1.\n\n")
	b.WriteString("// `path` is relative to /metacensus/api/v1 and spells its parameters\n")
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
