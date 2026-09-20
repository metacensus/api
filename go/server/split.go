package server

import (
	"net/http"

	"github.com/metacensus/api/go/server/routes"
)

// Except sends the named routes to alt and every other route to mux, so one
// service can be registered across two routers — e.g. mounting some rpcs of
// a service behind auth middleware and others not:
//
//	v1Routes.Group(func(authed chi.Router) {
//		authed.Use(s.auth.Middleware)
//		server.RegisterAuthRoutes(
//			server.Except(authed, v1Routes, rt, routes.AuthLogin, routes.AuthSignUp),
//			rt, authImpl{})
//	})
//
// The routes are the manifest's named values, so a route on the wrong side of
// an auth boundary is a typo the compiler catches, not a runtime panic.
func Except(mux, alt Mux, rt *Runtime, rs ...routes.Route) Mux {
	if len(rs) == 0 {
		panic("server: Except needs at least one route; without one it is just mux")
	}
	moved := map[string]bool{}
	for _, r := range rs {
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
