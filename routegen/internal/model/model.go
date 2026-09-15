// Package model owns the descriptor walk and the Route every renderer
// consumes: one walk of the compiled descriptors for every contract package,
// turned into a route per rpc by reading its google.api.http annotation. A
// renderer package imports model and nothing else in this module; model
// imports no renderer, so the boundary that keeps a renderer from reaching
// into another's internals is the import graph, not a convention (see
// imports_test.go beside main.go).
//
// The rejections here are properties of a route, not of any one renderer.
// describe_test.go is the enumeration of them, fed synthetic descriptors; a
// rejection specific to one renderer — clientgen's bytes-in-the-tree check —
// lives in that renderer instead.
package model

import (
	"fmt"
	"go/format"
	"reflect"
	"sort"
	"strings"

	"github.com/metacensus/api/internal/protoscan"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Registration. Every walk below reads protoregistry, which holds only
	// what this binary imported, so a contract package absent here is absent
	// from the manifest and from the checks that would have caught it.
	// Packages names them; CheckPackages fails when one of them registers
	// nothing.
	_ "github.com/metacensus/api/go/metacensus/public/v1"
	_ "github.com/metacensus/api/go/metacensus/v1"
)

// protoDir holds the .proto sources, relative to the repository root, which
// is where the generator runs. CheckPackages reads it.
const protoDir = "proto"

// Package is one contract package: a proto package, the path its routes hang
// off, the generated Go it binds against, and the names each rendering gives
// its prefix. Adding a surface is an entry here and nothing else.
//
// Packages below is the only place either prefix is written down. Every
// renderer takes its prefixes from there, prose included, so a constant and
// the paths it describes cannot drift apart.
//
// A prefix is not in the .proto: google.api.http annotations carry a path
// each and protobuf has no notion of a string constant, so expressing one
// there would mean a custom FileOptions extension and a non-resource .proto
// file inside a schema whose tests assert every file is a resource. Not worth
// it for two strings this package is already the authority on.
type Package struct {
	// Proto is the proto package, as declared in the .proto sources.
	Proto string

	// Prefix is the path this package's routes are relative to.
	Prefix string

	// GoImport is the generated Go package the server rendering binds
	// against; GoAlias is what it is imported as in the generated file.
	GoImport string
	GoAlias  string

	// GoConst and TSConst name Prefix's constant in each manifest. The
	// generated rows spell a route's prefix as the constant rather than the
	// literal, so the two cannot be edited apart in the generated file
	// either.
	GoConst string
	TSConst string

	// TSClient is the generated client class for this surface. One class per
	// surface, not one for all of them: the entry points exist so a consumer
	// of only the public surface does not acquire the authenticated types,
	// and a single class carrying every route would hand them over.
	TSClient string

	// Summary is the one-line description the generated prose uses.
	Summary string
}

// Packages is every contract package, in manifest order.
var Packages = []Package{
	{
		Proto:    "metacensus.v1",
		Prefix:   "/metacensus/api/v1",
		GoImport: "github.com/metacensus/api/go/metacensus/v1",
		GoAlias:  "v1",
		GoConst:  "Prefix",
		TSConst:  "apiPrefix",
		TSClient: "Client",
		Summary:  "the authenticated API",
	},
	{
		Proto: "metacensus.public.v1",
		// No version segment: see README.md, "Versioning the public surface".
		Prefix:   "/metacensus/public",
		GoImport: "github.com/metacensus/api/go/metacensus/public/v1",
		GoAlias:  "publicv1",
		GoConst:  "PublicPrefix",
		TSConst:  "publicPrefix",
		TSClient: "PublicClient",
		Summary:  "the public, unauthenticated surface",
	},
}

// CheckPackages fails when Packages does not name a proto package that exists
// on disk, or names one whose generated Go this package does not import.
// Either way that package's routes would be missing from everything generated
// here, with nothing to say so — the failure is not that a surface is exempt
// but that it is invisible.
//
// On disk, not protoregistry: the registry holds only what this binary
// imported, so a package nobody imported is absent from the output and from
// the check that would have caught it.
func CheckPackages() error {
	onDisk, err := protoscan.Packages(protoDir)
	if err != nil {
		return err
	}

	named := map[string]bool{}
	for _, pkg := range Packages {
		named[pkg.Proto] = true
	}

	var stray []string
	for _, pkg := range onDisk {
		if !named[pkg] {
			stray = append(stray, pkg)
		}
	}
	if len(stray) > 0 {
		return fmt.Errorf("%s declared under %s/ but not in model.Packages, so their "+
			"routes would be missing from the manifest, the server and the client. Add "+
			"an entry naming the prefix each one's routes hang off",
			strings.Join(stray, ", "), protoDir)
	}

	registered := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		registered[string(fd.Package())] = true
		return true
	})
	for _, pkg := range Packages {
		if !registered[pkg.Proto] {
			return fmt.Errorf("no file registers %s; add a blank import of %s to "+
				"internal/model", pkg.Proto, pkg.GoImport)
		}
	}
	return nil
}

// Route is one route, in the shape every renderer consumes. Path is
// relative to Pkg.Prefix and spells its parameters {lowerCamelCase}, as the
// wire does. Params, Query and Body between them account for every field of
// Request: Params bind path segments, Query travels in the query string,
// and Body is "*" when the rest travels in the body and empty when none
// does.
type Route struct {
	// Pkg is the contract package this route was read from, and so the
	// prefix it hangs off. Carried per route because with more than one
	// prefix a consumer holding a Route has no other way to know which to
	// join, and inferring it from the service name is exactly the
	// hand-mirroring this repository exists to stop.
	Pkg Package

	Service  string
	RPC      string
	Method   string
	Path     string
	Params   []string
	Query    []string
	Body     string
	Request  string
	Response string

	// The server and client renderings need the Go side of the same facts:
	// the Go type names protoc-gen-go gave the messages and, per path
	// parameter, the Go struct field it binds. None of these are derived by
	// re-implementing protoc-gen-go's naming; see goNames.
	GoRequest  string
	GoResponse string
	PathFields []PathField

	// Descriptor is the rpc this route was read from. Every field above is
	// derived from it; clientgen keeps it because its own rejections and its
	// TypeScript type references need descriptor detail no renderer that
	// only emits the manifest or the Go server requires.
	Descriptor protoreflect.MethodDescriptor
}

// PathField pairs a path parameter's wire spelling with the generated Go
// struct field that carries it.
type PathField struct {
	JSONName string
	GoField  string
}

// Walk reads every service in every contract package off the descriptors
// this package's blank imports registered, and returns one Route per rpc, in
// package-then-file-then-declaration order so the output is stable across
// runs. It fails on the first route it cannot describe, and when a package
// declares no routes at all — an empty manifest is never the generator's own
// decision to make.
//
// Per package, not in total: one package's routes must not vouch for
// another's absence. Without that, adding a surface and misspelling its proto
// package generates a manifest silently missing it, over a route table
// smaller than the one that ships.
func Walk() ([]Route, error) {
	var routes []Route
	for _, pkg := range Packages {
		before := len(routes)
		for _, svc := range services(pkg.Proto) {
			for i := 0; i < svc.Methods().Len(); i++ {
				md := svc.Methods().Get(i)
				r, err := describe(pkg, md)
				if err != nil {
					return nil, err
				}
				routes = append(routes, r)
			}
		}
		if len(routes) == before {
			return nil, fmt.Errorf("no routes found in package %s", pkg.Proto)
		}
	}
	return routes, nil
}

// services returns every service in the package, ordered by file then by
// declaration, so Walk's output is stable across runs.
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

func describe(pkg Package, md protoreflect.MethodDescriptor) (Route, error) {
	rule, ok := proto.GetExtension(md.Options(), annotations.E_Http).(*annotations.HttpRule)
	if !ok || rule == nil {
		return Route{}, fmt.Errorf("%s: no google.api.http option", md.Name())
	}
	if n := len(rule.GetAdditionalBindings()); n > 0 {
		return Route{}, fmt.Errorf("%s: %d additional_bindings; the manifest holds one route per rpc", md.Name(), n)
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
		return Route{}, fmt.Errorf("%s: unsupported http pattern %T", md.Name(), p)
	}

	body := rule.GetBody()
	path, params, bound, err := rewriteParams(path, md.Input())
	if err != nil {
		return Route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}
	query, err := queryParams(md.Input(), bound, body)
	if err != nil {
		return Route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}

	// The server rendering binds the request statically, so it needs facts
	// the manifest does not, and it refuses shapes it could not bind.
	if body != "" && body != "*" {
		// A named body decodes the body into one sub-message and leaves the
		// rest to the query string. Nothing in the contract does this and the
		// binding it needs (a message-typed field, a second decode target) is
		// not worth carrying unused.
		return Route{}, fmt.Errorf("%s: body %q names a field; only body: \"*\" or no body is supported", md.Name(), body)
	}
	req, resp, err := goNames(pkg, md.Input(), md.Output())
	if err != nil {
		return Route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}
	var pathFields []PathField
	for _, p := range params {
		fd := fieldByJSONName(md.Input(), p)
		if fd.Kind() != protoreflect.StringKind || fd.IsList() {
			// Path segments are strings on the wire. Binding a non-string
			// would mean parsing, with a 400 on failure and a Go-side
			// conversion per kind; ids are strings here (TestIdsAreStrings),
			// so this is a shape the contract does not have.
			return Route{}, fmt.Errorf("%s: path parameter %q is %s, not a singular string", md.Name(), p, fd.Kind())
		}
		if fd.ContainingOneof() != nil {
			return Route{}, fmt.Errorf("%s: path parameter %q is a oneof member; the generated binding sets struct fields directly", md.Name(), p)
		}
		goField, ok := req.fields[fd.Name()]
		if !ok {
			return Route{}, fmt.Errorf("%s: no Go struct field carries %s", md.Name(), fd.FullName())
		}
		pathFields = append(pathFields, PathField{JSONName: p, GoField: goField})
	}
	for _, q := range query {
		fd := fieldByJSONName(md.Input(), q)
		if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind || fd.IsMap() {
			// Query strings carry scalars. google.api.http allows nested
			// `a.b=1`, which needs a path walker; none of the routes need it.
			return Route{}, fmt.Errorf("%s: query parameter %q is %s; only scalar query fields are supported", md.Name(), q, fd.Kind())
		}
	}

	return Route{
		Pkg:        pkg,
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
		Descriptor: md,
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
func goNames(pkg Package, mds ...protoreflect.MessageDescriptor) (goType, goType, error) {
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
		// The route's own package, not "some generated package": a message
		// resolving elsewhere is a shape the server rendering has no import
		// alias for, and a cross-surface field besides — which
		// TestNoMessageFieldCrossesResourceFiles refuses outright.
		if t.Elem().PkgPath() != pkg.GoImport {
			return goType{}, goType{}, fmt.Errorf("%s: Go type %s is not in %s", md.FullName(), t, pkg.GoImport)
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
// parameters in path order alongside the proto names they bound.
//
// Two shapes google.api.http allows are refused here rather than narrowed,
// because narrowing them is silent:
//
//   - A segment pattern after "=". Every renderer emits a single-segment
//     {name}, so accepting {id=**} would claim one segment for a template
//     that means the rest of the path, and accepting {id=a/*/b} would drop
//     the shape entirely. Both produce a route that generates, compiles, and
//     answers 404 for exactly the ids the pattern was written for.
//   - The same parameter twice. net/http's ServeMux panics on a duplicate
//     wildcard name, so this reached a reader as a stack trace out of an
//     unrelated test rather than as a rejection naming the route.
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

		name, pattern, hasPattern := strings.Cut(path[open+1:shut], "=")
		if hasPattern {
			return "", nil, nil, fmt.Errorf(
				"path parameter %q carries the segment pattern %q; only a bare {%s} is supported",
				name, pattern, name)
		}
		fd := req.Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			return "", nil, nil, fmt.Errorf("path parameter %q is not a field of %s", name, req.FullName())
		}
		if bound[fd.Name()] {
			return "", nil, nil, fmt.Errorf("path parameter %q appears twice in %q", name, path)
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

// GoSlice spells items as a Go []string literal, or the bare identifier nil
// for an empty slice — shared by every renderer that writes a Params or
// Query field as Go source.
func GoSlice(items []string) string {
	if len(items) == 0 {
		return "nil"
	}
	return "[]string{" + strings.Join(QuoteAll(items), ", ") + "}"
}

// GoFormat gofmts generated Go, returning the unformatted source alongside
// the error: that is what the error's line numbers refer to. what names the
// output for the message.
func GoFormat(what string, src []byte) ([]byte, error) {
	out, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("%s: gofmt: %w\n%s", what, err, src)
	}
	return out, nil
}

// QuoteAll applies %q to each item, for a renderer building a slice or
// array literal one quoted element at a time.
func QuoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}
