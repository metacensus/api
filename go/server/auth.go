package server

import (
	"fmt"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/metacensus/api/go/routes"
)

// Authenticator decides whether a request may proceed. Returning an error
// stops it, and the error travels the same path as one from a handler — so
// a *Error controls the status and body, and anything else is a 500.
type Authenticator interface {
	Authenticate(*http.Request) (*http.Request, error)
}

// AuthenticatorFunc adapts a function to Authenticator.
type AuthenticatorFunc func(*http.Request) (*http.Request, error)

func (f AuthenticatorFunc) Authenticate(r *http.Request) (*http.Request, error) { return f(r) }

// routeKey is how a route is named in Options.Public.
func routeKey(method, path string) string { return method + " " + path }

// guard builds the middleware that authenticates every route except those
// named public.
//
// This is where grpc-gateway costs something specific. The in-process
// registration mode does not run gRPC interceptors — grpc-gateway's own
// documentation says so and offers WithMiddlewares instead — and that
// middleware is installed once for every route, so it is not told which route
// matched. It sees a method and a path and must decide.
//
// That works here only because every public route is parameterless, so an
// exact match is enough and no path-pattern matching has to be reimplemented.
// The check below is what keeps that true: naming a parameterised route public
// fails at construction rather than silently authenticating it.
func guard(a Authenticator, public map[string]bool, log func(*http.Request, error)) (runtime.Middleware, error) {
	declared := map[string]routes.Route{}
	for _, r := range routes.Routes {
		declared[routeKey(r.Method, r.Path)] = r
	}

	for key := range public {
		r, ok := declared[key]
		if !ok {
			return nil, fmt.Errorf("server: %q is named public but the manifest does not declare it", key)
		}
		if len(r.Params) > 0 {
			return nil, fmt.Errorf("server: %q is named public but carries path parameters %v; "+
				"the gateway's middleware matches a public route exactly and cannot match a pattern",
				key, r.Params)
		}
	}

	writeErr := errorHandler(log)

	return func(next runtime.HandlerFunc) runtime.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
			if public[routeKey(r.Method, r.URL.Path)] {
				next(w, r, pathParams)
				return
			}

			authed, err := a.Authenticate(r)
			if err != nil {
				writeErr(r.Context(), nil, defaultMarshaler(), w, r, err)
				return
			}
			next(w, authed, pathParams)
		}
	}, nil
}
