package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/metacensus/api/go/routes"
)

// Except sends the named rpcs to alt and every other route to mux, so one
// service can be registered across two routers.
//
// Register<Service> registers a whole service, and a service's rpcs do not
// always sit behind the same middleware. metacensus/infra mounts
// AuthRoutes.Login and AuthRoutes.SignUp publicly and AuthRoutes.Logout
// behind its JWT check:
//
//	v1Routes.Group(func(authed chi.Router) {
//		authed.Use(s.auth.Middleware)
//		server.RegisterAuthRoutes(
//			server.Except(authed, v1Routes, rt, "AuthRoutes.Login", "AuthRoutes.SignUp"),
//			rt, authImpl{})
//	})
//
// The alternative is a Mux of the caller's own that matches on the pattern
// string, which puts the route table back at the callsite — the one thing
// generating it was for. Naming rpcs instead keeps the patterns here: they
// come from routes.Routes, and a name that is not in the manifest panics at
// construction, so a typo or a renamed rpc fails at startup rather than
// mounting a route on the wrong side of an auth boundary.
//
// Each name is "Service.Rpc", as routes.Route spells it.
func Except(mux, alt Mux, rt *Runtime, rpcs ...string) Mux {
	if len(rpcs) == 0 {
		panic("server: Except needs at least one rpc; without one it is just mux")
	}
	byName := map[string]routes.Route{}
	for _, r := range routes.Routes {
		byName[r.Service+"."+r.RPC] = r
	}

	moved := map[string]bool{}
	for _, name := range rpcs {
		r, ok := byName[name]
		if !ok {
			panic(fmt.Sprintf("server: Except: %q is not an rpc in the route manifest (%s)",
				name, strings.Join(rpcNames(), ", ")))
		}
		moved[r.Method+" "+rt.Prefix+r.Path] = true
	}
	return exceptMux{mux: mux, alt: alt, moved: moved}
}

func rpcNames() []string {
	out := make([]string, 0, len(routes.Routes))
	for _, r := range routes.Routes {
		out = append(out, r.Service+"."+r.RPC)
	}
	return out
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
