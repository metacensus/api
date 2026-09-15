// Package servergen renders go/server/routes_gen.go: per service, a handler
// interface, an Unimplemented*, and a Register* that binds and dispatches.
// The runtime it calls into is package server's hand-written files.
//
// Unimplemented<Service> is kept at a known cost: an implementer who embeds
// it and later renames an rpc still compiles, and the renamed route answers
// 501 at runtime instead of failing the build. The alternative — no
// embedding, so a rename is a compile error — trades that for a service that
// can never be implemented one route at a time, which is the shape every
// consumer of this contract is actually in.
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
// body="*" block already declared it with :=. Both are decided here rather
// than with nested {{if}} in the template.
type routeView struct {
	model.Route
	ServiceRPC  string
	NeedsErrVar bool
}

func newRouteView(r model.Route) routeView {
	return routeView{
		Route:       r,
		ServiceRPC:  r.Service + "." + r.RPC,
		NeedsErrVar: r.Body != "*" && len(r.PathFields) > 0,
	}
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

	// emit stops at the first failure and keeps it, so Render below reads as
	// the sequence of blocks it writes. Partial output never reaches a file:
	// main writes nothing when Render returns an error.
	var b bytes.Buffer
	var err error
	emit := func(block string, data any, service, rpc string) {
		if err != nil {
			return
		}
		err = execute(&b, block, data, service, rpc)
	}

	emit("header", struct{ V1Import string }{model.V1Import}, "", "")

	for _, svc := range order {
		rs := byService[svc]
		svcData := struct{ Name string }{svc}

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
