// Package servergen renders go/server/routes_gen.go: one handler interface
// per service, an Unimplemented* per service, and one Register* per service
// that binds path/query/body and dispatches to it. The hand-written runtime
// it calls into is package server's own files (binding.go, errors.go,
// mux.go, response.go, runtime.go).
//
// Unimplemented<Service> is kept at a known cost: an implementer who embeds
// it and later renames an rpc still compiles, and the renamed route answers
// 501 at runtime instead of failing the build. The alternative — no
// embedding, so a rename is a compile error — trades that for a service that
// can never be implemented one route at a time, which is the shape every
// consumer of this contract is actually in.
//
// server.go.tmpl is embedded and parsed at init so a broken template fails
// `make gen` immediately. Its named blocks are executed by a Go loop that
// mirrors the original imperative writer, one service and one route at a
// time, so an execution error carries the block, the service and — for the
// per-route blocks — the rpc that was being rendered.
package servergen

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"text/template"

	"github.com/metacensus/api/go/cmd/routegen/internal/model"
)

//go:embed server.go.tmpl
var tmplSrc string

var tmpl = template.Must(template.New("server.go.tmpl").Funcs(template.FuncMap{
	"goSlice": model.GoSlice,
}).Parse(tmplSrc))

// routeView adds the derived values servergen's own templates need beyond
// model.Route: whether the handler declares `var err error` up front (it
// does unless a body="*" block already declares err via :=), and the
// service-qualified rpc name errNotImplemented reports. Both are decisions
// about the route, computed once here rather than re-derived inside the
// template on every reference.
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
func Render(routes []model.Route) []byte {
	var order []string
	byService := map[string][]routeView{}
	for _, r := range routes {
		if _, ok := byService[r.Service]; !ok {
			order = append(order, r.Service)
		}
		byService[r.Service] = append(byService[r.Service], newRouteView(r))
	}

	var b bytes.Buffer
	execute(&b, "header", struct{ V1Import string }{model.V1Import}, "", "")

	for _, svc := range order {
		rs := byService[svc]
		svcData := struct{ Name string }{svc}

		execute(&b, "interfaceOpen", svcData, svc, "")
		for _, r := range rs {
			execute(&b, "interfaceMethod", r, svc, r.RPC)
		}
		execute(&b, "interfaceClose", nil, svc, "")

		execute(&b, "unimplementedOpen", svcData, svc, "")
		for _, r := range rs {
			execute(&b, "unimplementedMethod", r, svc, r.RPC)
		}

		execute(&b, "registerOpen", svcData, svc, "")
		for _, r := range rs {
			execute(&b, "registerRoute", r, svc, r.RPC)
		}
		execute(&b, "registerClose", nil, svc, "")
	}

	src, err := format.Source(b.Bytes())
	if err != nil {
		// Show the unformatted source: the error's line numbers refer to it.
		panic(fmt.Sprintf("%v\n%s", err, b.Bytes()))
	}
	return src
}

// execute runs one named block of server.go.tmpl and panics with the block,
// the service and — when there is one — the rpc on failure. The routes
// executed here already passed model.Walk, so a failure means a broken
// template, the same class of error format.Source's own panic above
// reports loudly rather than swallowing into partial output.
func execute(b *bytes.Buffer, block string, data any, service, rpc string) {
	if err := tmpl.ExecuteTemplate(b, block, data); err != nil {
		if rpc != "" {
			panic(fmt.Sprintf("server.go.tmpl: %s: route %s.%s: %v", block, service, rpc, err))
		}
		panic(fmt.Sprintf("server.go.tmpl: %s: service %s: %v", block, service, err))
	}
}
