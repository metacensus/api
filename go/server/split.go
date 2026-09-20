package server

import (
	"fmt"
	"net/http"

	"github.com/metacensus/api/go/server/routes"
)

// Except sends the named rpcs to alt and every other route to mux, so one
// service can be registered across two routers — e.g. mounting some rpcs of
// a service behind auth middleware and others not:
//
//	v1Routes.Group(func(authed chi.Router) {
//		authed.Use(s.auth.Middleware)
//		server.RegisterAuthRoutes(
//			server.Except(authed, v1Routes, rt, "AuthRoutes.Login", "AuthRoutes.SignUp"),
//			rt, authImpl{})
//	})
//
// rpcs are "Service.Rpc" names resolved through routes.Lookup; a name not in
// the manifest panics at construction rather than mounting a route on the wrong
// side of an auth boundary.
func Except(mux, alt Mux, rt *Runtime, rpcs ...string) Mux {
	if len(rpcs) == 0 {
		panic("server: Except needs at least one rpc; without one it is just mux")
	}
	moved := map[string]bool{}
	for _, name := range rpcs {
		r, ok := routes.Lookup(name)
		if !ok {
			panic(fmt.Sprintf("server: Except: %q is not an rpc in the route manifest", name))
		}
		moved[r.Method+" "+rt.Prefix+r.Path] = true
	}
	return exceptMux{mux: mux, alt: alt, moved: moved}
}

type exceptMux struct {
	mux, alt Mux
	moved    map[string]bool // "METHOD full-pattern"
}

func (e exceptMux) Method(method, pattern string, handler http.Handler) {
	if e.moved[method+" "+pattern] {
		e.alt.Method(method, pattern, handler)
		return
	}
	e.mux.Method(method, pattern, handler)
}
