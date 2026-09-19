// Package servergen renders go/server/routes_gen.go: per service, a handler
// interface, an Unimplemented*, and a Register* that binds and dispatches.
// The runtime it calls into is package server's hand-written files.
//
// Unimplemented<Service> lets a service be implemented one route at a time: a
// renamed rpc still compiles and answers 501 at runtime instead of failing
// the build.
package servergen

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"

	"github.com/metacensus/api/routegen/internal/model"
)

//go:embed server.go.tmpl
var tmplSrc string

var tmpl = template.Must(template.New("server.go.tmpl").Funcs(template.FuncMap{
	"goSlice": model.GoSlice,
}).Parse(tmplSrc))

// routeView is model.Route plus what the template cannot work out for
// itself. NeedsErrVar: a handler declares `var err error` up front unless a
// body="*" block already declared it with :=. Alias is the import alias for
// the generated package this route's messages live in.
type routeView struct {
	model.Route
	ServiceRPC  string
	Alias       string
	NeedsErrVar bool
}

func newRouteView(r model.Route) routeView {
	return routeView{
		Route:       r,
		ServiceRPC:  r.Service + "." + r.RPC,
		Alias:       r.Pkg.GoAlias,
		NeedsErrVar: r.Body != "*" && len(r.PathFields) > 0,
	}
}

// serviceView is what the per-service blocks need: the Go identifier they
// build names from, and the prefix constant its routes hang off.
type serviceView struct {
	Name        string
	PrefixConst string
}

// Render writes go/server/routes_gen.go, gofmt'd.
func Render(routes []model.Route) ([]byte, error) {
	var order []string
	byService := map[string][]routeView{}
	for _, r := range routes {
		if _, ok := byService[r.Service]; !ok {
			order = append(order, r.Service)
		}
		byService[r.Service] = append(byService[r.Service], newRouteView(r))
	}

	// Refused here, with a clear message: two services sharing a name across
	// surfaces would otherwise collide on one interface and Register func.
	seen := map[string]string{}
	for _, r := range routes {
		if first, ok := seen[r.Service]; ok && first != r.Pkg.Proto {
			return nil, fmt.Errorf("service %s is declared by both %s and %s; the "+
				"generated server is one package, so the two would collide on "+
				"%[1]s, Unimplemented%[1]s and Register%[1]s", r.Service, first, r.Pkg.Proto)
		}
		seen[r.Service] = r.Pkg.Proto
	}

	// Only surfaces that actually declare a route are imported: an unused
	// import does not compile.
	var imports []model.Package
	for _, pkg := range model.Packages {
		for _, r := range routes {
			if r.Pkg.Proto == pkg.Proto {
				imports = append(imports, pkg)
				break
			}
		}
	}

	// emit stops at the first failure and keeps it, so Render below reads as
	// the sequence of blocks it writes.
	var b bytes.Buffer
	var err error
	emit := func(block string, data any, service, rpc string) {
		if err != nil {
			return
		}
		err = execute(&b, block, data, service, rpc)
	}

	emit("header", struct{ Imports []model.Package }{imports}, "", "")

	for _, svc := range order {
		rs := byService[svc]
		svcData := serviceView{Name: svc, PrefixConst: rs[0].Pkg.GoConst}

		emit("interfaceOpen", svcData, svc, "")
		for _, r := range rs {
			emit("interfaceMethod", r, svc, r.RPC)
		}
		emit("interfaceClose", nil, svc, "")

		emit("unimplementedOpen", svcData, svc, "")
		for _, r := range rs {
			emit("unimplementedMethod", r, svc, r.RPC)
		}

		emit("registerOpen", svcData, svc, "")
		for _, r := range rs {
			emit("registerRoute", r, svc, r.RPC)
		}
		emit("registerClose", nil, svc, "")
	}
	if err != nil {
		return nil, err
	}
	return model.GoFormat("go/server/routes_gen.go", b.Bytes())
}

// execute runs one named block, naming the template, the block, the service
// and the route on failure.
func execute(b *bytes.Buffer, block string, data any, service, rpc string) error {
	if err := tmpl.ExecuteTemplate(b, block, data); err != nil {
		if rpc != "" {
			return fmt.Errorf("server.go.tmpl: %s: route %s.%s: %w", block, service, rpc, err)
		}
		return fmt.Errorf("server.go.tmpl: %s: service %s: %w", block, service, err)
	}
	return nil
}
