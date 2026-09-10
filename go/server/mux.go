// Package server is the server side of the MetaCensus API contract: one
// interface per resource that an implementation must satisfy to compile, and
// the route registration and request binding that drive them.
//
// It names no router. Router and PathParamFunc are the whole of what a router
// must supply, and chi satisfies both as it stands, so the published module
// keeps its two requirements:
//
//	mux := server.Mux{
//		Router:    chiRouter,
//		PathParam: chi.URLParam,
//		OnError:   server.NewErrorHandler(logErr),
//	}
//	mux.RegisterTopicRoutes(store)
//	mux.RegisterUserRoutes(store)
//
// Responses are written with protojson and nothing else. encoding/json is not
// reachable from here, which is the property this package exists to hold: the
// generated types marshal through encoding/json into a second, wrong-looking
// encoding, and a comment saying not to is not enforcement.
package server

import (
	"errors"
	"io"
	"net/http"

	contract "github.com/metacensus/api/go"
	"github.com/metacensus/api/go/routes"
	"google.golang.org/protobuf/proto"
)

// Router is the part of a router the generated registration uses.
// chi.Router satisfies it without an adapter.
type Router interface {
	Method(method, pattern string, h http.Handler)
}

// PathParamFunc reads a path parameter by name. chi.URLParam satisfies it.
type PathParamFunc func(r *http.Request, name string) string

// Mux carries what registration needs. Router and PathParam are required.
type Mux struct {
	Router    Router
	PathParam PathParamFunc

	// OnError writes the response for an error a handler returns. Nil means
	// NewErrorHandler(nil), which logs nothing and exposes nothing beyond what
	// an *Error carries.
	OnError ErrorHandler

	// Wrap, when set, is called for every route before it is registered and
	// may return a wrapped handler.
	//
	// This is where authentication goes. The contract does not model which
	// routes are public, so that decision stays with the server — and putting
	// it here makes it a per-route answer rather than a consequence of which
	// router a service happened to be registered on. AuthRoutes is why:
	// Login and SignUp are unauthenticated and Logout is not, so a service
	// does not divide cleanly by middleware.
	Wrap func(route routes.Route, h http.Handler) http.Handler

	// DiscardUnknownFields relaxes body decoding to ignore fields the contract
	// does not declare, rather than rejecting the request.
	//
	// The contract's own decoder rejects them, which is right for policing
	// conformance and wrong while a system that is ahead of the contract
	// migrates onto it. Set it deliberately, per side, and turn it off once
	// the sending side has caught up.
	DiscardUnknownFields bool

	// MaxBodyBytes caps a request body. Zero means DefaultMaxBodyBytes; a
	// negative value means no cap, which hands an anonymous caller an
	// unbounded read.
	MaxBodyBytes int64
}

// DefaultMaxBodyBytes is the body cap a zero-valued Mux applies. No route in
// the contract carries bulk content — the largest is a DataExtraction of
// per-page highlight rectangles — so this is generous rather than tuned.
const DefaultMaxBodyBytes = 1 << 20

// prefixedPath is where a route is served: paths in the manifest are declared
// relative to routes.Prefix.
func prefixedPath(route routes.Route) string { return routes.Prefix + route.Path }

// routeOf is the manifest entry for a service and rpc, and is how the generated
// registration gets a path at all.
//
// Reading it from the manifest rather than emitting it again is what stops the
// two generated artifacts drifting: they come out of the same run over the same
// descriptors, so a miss cannot happen, and if one ever did it is a wiring
// failure worth hearing about at startup rather than a route quietly served at
// the empty path.
func routeOf(service, rpc string) routes.Route {
	for _, r := range routes.Routes {
		if r.Service == service && r.RPC == rpc {
			return r
		}
	}
	panic("server: no route in the manifest for " + service + "." + rpc)
}

// handle registers h for route, applying Wrap.
//
// The two panics are for a Mux that cannot serve the route at all. Both are
// mistakes in wiring rather than in a request, so failing at startup beats
// binding every id to "" and failing per request in a way that looks like bad
// input.
func (m Mux) handle(route routes.Route, h http.HandlerFunc) {
	if m.Router == nil {
		panic("server: Mux.Router is nil")
	}
	if len(route.Params) > 0 && m.PathParam == nil {
		panic("server: Mux.PathParam is nil, required by " + route.Method + " " + route.Path)
	}

	var handler http.Handler = h
	if m.Wrap != nil {
		handler = m.Wrap(route, handler)
	}
	m.Router.Method(route.Method, prefixedPath(route), handler)
}

// errorHandler is OnError, or the default when it is unset.
func (m Mux) errorHandler() ErrorHandler {
	if m.OnError != nil {
		return m.OnError
	}
	return NewErrorHandler(nil)
}

// respond writes msg, or hands err to the error handler. Generated code writes
// responses through here and nowhere else.
func (m Mux) respond(w http.ResponseWriter, r *http.Request, msg proto.Message, err error) {
	if err != nil {
		m.errorHandler()(w, r, err)
		return
	}
	// A handler that returns neither a response nor an error. Nothing sensible
	// can be written, and an empty 200 would read as success.
	if msg == nil || !msg.ProtoReflect().IsValid() {
		m.errorHandler()(w, r, errors.New("server: handler returned no response and no error"))
		return
	}

	b, err := contract.Marshal(msg)
	if err != nil {
		m.errorHandler()(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// badRequest is the error a request that could not be bound produces. The
// caller sent it, so the detail is theirs to see.
func badRequest(cause error) *Error {
	return Wrap(http.StatusBadRequest, "InvalidRequest", "request could not be decoded", cause)
}

// bindBody decodes the request body into msg.
func (m Mux) bindBody(w http.ResponseWriter, r *http.Request, msg proto.Message) error {
	body := r.Body
	if m.MaxBodyBytes >= 0 {
		limit := m.MaxBodyBytes
		if limit == 0 {
			limit = DefaultMaxBodyBytes
		}
		body = http.MaxBytesReader(w, body, limit)
	}
	defer func() { _ = body.Close() }()

	opts := contract.UnmarshalOptions
	opts.DiscardUnknown = m.DiscardUnknownFields

	b, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return Wrap(http.StatusRequestEntityTooLarge, "RequestTooLarge",
				"request body is too large", err)
		}
		return badRequest(err)
	}
	// An absent body leaves the message at its zero value: routes declaring
	// `body: "*"` with every field optional are legitimately callable empty.
	if len(b) == 0 {
		return nil
	}
	if err := opts.Unmarshal(b, msg); err != nil {
		return badRequest(err)
	}
	return nil
}

// bindPath reads a path parameter. handle has already refused to register a
// parameterised route on a Mux with no PathParam.
func (m Mux) bindPath(r *http.Request, name string) string {
	return m.PathParam(r, name)
}

// bindQuery reads a query parameter. Absent and empty are the same thing here:
// the contract has no optional scalars.
func bindQuery(r *http.Request, name string) string {
	return r.URL.Query().Get(name)
}
