// Package model walks the compiled descriptors for every contract package
// and turns each rpc into a Route, by reading its google.api.http
// annotation. Renderers import model, never each other (imports_test.go
// enforces this); rejections common to every renderer live here, and
// describe_test.go enumerates them. A rejection specific to one renderer
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

	// Registers each contract package's descriptors into protoregistry, which
	// every walk below reads; CheckPackages fails when one registers nothing.
	_ "github.com/metacensus/api/go/metacensus/public/v1"
	_ "github.com/metacensus/api/go/metacensus/v1"
)

// protoDir holds the .proto sources, relative to the repository root.
const protoDir = "proto"

// Package is one contract package: a proto package, the path its routes hang
// off, the generated Go it binds against, and the names each rendering gives
// its prefix. Adding a surface is an entry here and nothing else; this is
// the only place a prefix is written down, since google.api.http has no way
// to express one in the .proto itself.
type Package struct {
	// Proto is the proto package, as declared in the .proto sources.
	Proto string

	// Prefix is the path this package's routes are relative to.
	Prefix string

	// GoImport is the generated Go package the server rendering binds
	// against; GoAlias is what it is imported as in the generated file.
	GoImport string
	GoAlias  string

	// GoConst and TSConst name Prefix's constant in each manifest.
	GoConst string
	TSConst string

	// TSClient is the generated client class for this surface.
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
		TSClient: "ClientSigned",
		Summary:  "the authenticated API",
	},
	{
		Proto: "metacensus.public.v1",
		// No version segment: AGENTS.md, "Compatibility and versioning".
		Prefix:   "/metacensus/public",
		GoImport: "github.com/metacensus/api/go/metacensus/public/v1",
		GoAlias:  "publicv1",
		GoConst:  "PublicPrefix",
		TSConst:  "publicPrefix",
		TSClient: "PublicClient",
		Summary:  "the public, unauthenticated surface",
	},
}

// CheckPackages fails when a proto package on disk is missing from Packages,
// or a package in Packages has no registered Go import — either way its
// routes would silently be missing from everything generated here.
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
// relative to Pkg.Prefix and spells parameters {lowerCamelCase}. Params,
// Query and Body between them account for every field of Request: Params
// bind path segments, Query travels in the query string, and Body is "*"
// when the rest travels in the body and empty when none does.
type Route struct {
	// Pkg is the contract package this route was read from.
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

	// Signed is true when this route carries a Content field and a
	// UserSignature over it; TestEveryWriteCarriesASignature holds which
	// writes are exempt.
	Signed bool

	// GoRequest, GoResponse and PathFields are the Go names protoc-gen-go
	// gave the messages and, per path parameter, the struct field it binds.
	// See goNames.
	GoRequest  string
	GoResponse string
	PathFields []PathField

	// ContentGoField and SignatureGoField are the Go struct fields carrying
	// the signed content and the signature, empty on an unsigned route.
	ContentGoField   string
	SignatureGoField string

	// ContentParams pairs each path parameter's top-level field with the
	// field inside content that repeats it.
	ContentParams []ContentParam

	// Descriptor is the rpc this route was read from; clientgen uses it for
	// detail no other renderer needs.
	Descriptor protoreflect.MethodDescriptor
}

// PathField pairs a path parameter's wire spelling with the generated Go
// struct field that carries it.
type PathField struct {
	JSONName string
	GoField  string
}

// ContentParam pairs a path parameter's top-level field with the field
// inside signed content that repeats it. See server.contentParam.
type ContentParam struct {
	JSONName       string
	PathGoField    string
	ContentGoField string
}

// The two fields a signed request carries, by proto name, and the required
// type of the signature field.
const (
	ContentField   = "content"
	SignatureField = "user_signature"
	SignatureType  = "metacensus.v1.UserSignature"
)

// Walk reads every service in every contract package and returns one Route
// per rpc, in package-then-file-then-declaration order for stable output. It
// fails on the first route it cannot describe, and per package (so one
// package's routes can't vouch for another's absence) when a package
// declares none at all.
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

	if body != "" && body != "*" {
		// A named body would decode into one sub-message; no route needs that.
		return Route{}, fmt.Errorf("%s: body %q names a field; only body: \"*\" or no body is supported", md.Name(), body)
	}
	req, resp, err := goNames(pkg, md.Input(), md.Output())
	if err != nil {
		return Route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}
	var pathFields []PathField
	for _, p := range params {
		fd := md.Input().Fields().ByJSONName(p)
		if fd.Kind() != protoreflect.StringKind || fd.IsList() {
			// Path segments are strings on the wire; ids are strings here too
			// (TestIdsAreStrings), so a non-string path field never occurs.
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
		fd := md.Input().Fields().ByJSONName(q)
		if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind || fd.IsMap() {
			// Query strings carry scalars only.
			return Route{}, fmt.Errorf("%s: query parameter %q is %s; only scalar query fields are supported", md.Name(), q, fd.Kind())
		}
	}

	signed, contentField, sigField, contentParams, err := describeSigned(pkg, md.Input(), req, params)
	if err != nil {
		return Route{}, fmt.Errorf("%s: %w", md.Name(), err)
	}
	if signed && body != "*" {
		// Unreachable today (queryParams already refuses a message-typed
		// field), but the two checks are independent so this stays explicit.
		return Route{}, fmt.Errorf("%s: carries signed content but no body; it cannot travel", md.Name())
	}

	return Route{
		Pkg:              pkg,
		Service:          string(md.Parent().Name()),
		RPC:              string(md.Name()),
		Method:           method,
		Path:             path,
		Params:           params,
		Query:            query,
		Body:             body,
		Request:          string(md.Input().Name()),
		Response:         string(md.Output().Name()),
		Signed:           signed,
		GoRequest:        req.typeName,
		GoResponse:       resp.typeName,
		PathFields:       pathFields,
		ContentGoField:   contentField,
		SignatureGoField: sigField,
		ContentParams:    contentParams,
		Descriptor:       md,
	}, nil
}

// goType is what the server rendering knows about one generated Go message
// type: its name and the Go struct field for each proto field.
type goType struct {
	typeName string
	fields   map[protoreflect.Name]string
}

// goNames reads the Go names protoc-gen-go emitted, off the generated
// struct's `protobuf:"...,name=<proto name>,..."` tags rather than
// re-deriving them. TestGoNamesAgreeWithProtogen cross-checks against
// compiler/protogen.
func goNames(pkg Package, mds ...protoreflect.MessageDescriptor) (goType, goType, error) {
	var out [2]goType
	for i, md := range mds {
		gt, err := goTypeOf(pkg, md)
		if err != nil {
			return goType{}, goType{}, err
		}
		out[i] = gt
	}
	return out[0], out[1], nil
}

// goTypeOf reads one message's generated Go names. Split out of goNames so a
// signed route can also read the content message's fields, needed on both
// sides of the generated path/content comparison.
func goTypeOf(pkg Package, md protoreflect.MessageDescriptor) (goType, error) {
	mt, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
	if err != nil {
		return goType{}, fmt.Errorf("%s: not registered as a Go type: %w", md.FullName(), err)
	}
	t := reflect.TypeOf(mt.New().Interface())
	if t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return goType{}, fmt.Errorf("%s: Go type %s is not a pointer to struct", md.FullName(), t)
	}
	// Must resolve into the route's own package; a cross-surface field is
	// refused outright by TestNoMessageFieldCrossesResourceFiles.
	if t.Elem().PkgPath() != pkg.GoImport {
		return goType{}, fmt.Errorf("%s: Go type %s is not in %s", md.FullName(), t, pkg.GoImport)
	}
	out := goType{typeName: t.Elem().Name(), fields: map[protoreflect.Name]string{}}
	for j := 0; j < t.Elem().NumField(); j++ {
		sf := t.Elem().Field(j)
		for _, part := range strings.Split(sf.Tag.Get("protobuf"), ",") {
			if name, ok := strings.CutPrefix(part, "name="); ok {
				out.fields[protoreflect.Name(name)] = sf.Name
			}
		}
	}
	for j := 0; j < md.Fields().Len(); j++ {
		fd := md.Fields().Get(j)
		if fd.ContainingOneof() != nil {
			continue // oneof members sit behind an interface field
		}
		if _, ok := out.fields[fd.Name()]; !ok {
			return goType{}, fmt.Errorf("%s: no struct field tagged name=%s on %s", md.FullName(), fd.Name(), t)
		}
	}
	return out, nil
}

// describeSigned reads the signed half of a request: the content field, the
// signature over it, and the path parameters content has to repeat. It
// rejects a misshapen signed route here rather than at a verifier: content
// and signature must travel together, the signature must be a
// UserSignature, and every path-bound id must be repeated inside content.
func describeSigned(pkg Package, md protoreflect.MessageDescriptor, req goType, params []string) (bool, string, string, []ContentParam, error) {
	fields := md.Fields()
	content := fields.ByName(ContentField)
	sig := fields.ByName(SignatureField)

	switch {
	case content == nil && sig == nil:
		return false, "", "", nil, nil
	case content == nil:
		return false, "", "", nil, fmt.Errorf("%s carries %s but no %s; a signature over nothing attests to nothing",
			md.FullName(), SignatureField, ContentField)
	case sig == nil:
		return false, "", "", nil, fmt.Errorf("%s carries %s but no %s; content nobody signed is what the signing chain exists to remove",
			md.FullName(), ContentField, SignatureField)
	}

	for _, fd := range []protoreflect.FieldDescriptor{content, sig} {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return false, "", "", nil, fmt.Errorf("%s.%s is %s; it must be a singular message", md.FullName(), fd.Name(), fd.Kind())
		}
	}
	if got := string(sig.Message().FullName()); got != SignatureType {
		return false, "", "", nil, fmt.Errorf("%s.%s is a %s, not a %s", md.FullName(), SignatureField, got, SignatureType)
	}

	contentType, err := goTypeOf(pkg, content.Message())
	if err != nil {
		return false, "", "", nil, err
	}

	var out []ContentParam
	for _, p := range params {
		cfd := content.Message().Fields().ByJSONName(p)
		if cfd == nil {
			return false, "", "", nil, fmt.Errorf(
				"%s binds path parameter %q but %s does not carry it. Every id the path "+
					"binds is part of what the content means and must be inside the "+
					"signature, or a record can be filed under one address while "+
					"attesting to another", md.FullName(), p, content.Message().FullName())
		}
		if cfd.Kind() != protoreflect.StringKind || cfd.IsList() {
			return false, "", "", nil, fmt.Errorf("%s.%s is %s, not a singular string", content.Message().FullName(), cfd.Name(), cfd.Kind())
		}
		out = append(out, ContentParam{
			JSONName:       p,
			PathGoField:    req.fields[fields.ByJSONName(p).Name()],
			ContentGoField: contentType.fields[cfd.Name()],
		})
	}
	return true, req.fields[content.Name()], req.fields[sig.Name()], out, nil
}

// rewriteParams replaces each {field} segment with {jsonName} and returns the
// parameters in path order alongside the proto names they bound. It refuses,
// rather than silently narrows, two shapes google.api.http allows but no
// renderer supports: a segment pattern after "=" (every renderer emits a
// bare {name}), and the same parameter appearing twice (net/http's ServeMux
// panics on a duplicate wildcard name).
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

// queryParams returns the request fields that travel in the query string:
// every field the path did not bind and the body does not carry.
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

// GoSlice renders items as a Go []string literal, or nil for an empty slice.
func GoSlice(items []string) string {
	if len(items) == 0 {
		return "nil"
	}
	return "[]string{" + strings.Join(QuoteAll(items), ", ") + "}"
}

// GoFormat gofmts generated Go, returning the unformatted source alongside
// the error, since that is what the error's line numbers refer to.
func GoFormat(what string, src []byte) ([]byte, error) {
	out, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("%s: gofmt: %w\n%s", what, err, src)
	}
	return out, nil
}

// QuoteAll applies %q to each item.
func QuoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}
