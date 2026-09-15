package server

import (
	"fmt"
	"net/http"
	"net/url"
)

// Mux is the registration surface a generated Register<Service> function
// needs: bind one HTTP method and pattern to a handler. Both a bare stdlib
// mux and chi already have this method under a compatible name and
// signature, so this package picks no router and imports none. A chi.Router
// satisfies Mux directly, with no adapter; a *http.ServeMux needs StdMux,
// because it has no method-specific registration call.
//
// Method(method, pattern string, handler http.Handler) is everything this
// package asks of a router, so everything a router decides before a handler
// runs is the router's answer, not this package's. Measured against
// net/http's ServeMux and github.com/go-chi/chi/v5 v5.1.0 — the two routers
// go/server/chitest exercises against real chi:
//
//   - Path-parameter escaping. ServeMux percent-decodes a segment before
//     r.PathValue returns it; chi does not. This one is not left to the
//     router, because the generated TypeScript client percent-encodes every
//     segment and the two halves would then disagree about what an id is:
//     Runtime.PathValue owns it — see StdPathValue and EscapedPathValue.
//   - HEAD on a GET route. ServeMux matches it (Go 1.22+ pattern semantics)
//     and answers 200 with the body suppressed; chi answers 405.
//   - 404 and 405 bodies. Neither is the {"error","code"} envelope: they come
//     from the router before any generated handler runs. ServeMux writes
//     text/plain; chi writes an empty body with an Allow header. A client
//     must not assume a non-2xx response parses as JSON.
//   - Path cleaning. ServeMux redirects "/a//b" to "/a/b" with a 301; chi
//     does not.
type Mux interface {
	Method(method, pattern string, handler http.Handler)
}

// StdMux adapts a *http.ServeMux to Mux using the "METHOD pattern" syntax
// net/http's own mux has accepted since Go 1.22. It is the only place this
// package knows that syntax exists; a Mux implementation for a different
// router does not need to.
type StdMux struct {
	*http.ServeMux
}

func (m StdMux) Method(method, pattern string, handler http.Handler) {
	m.ServeMux.Handle(method+" "+pattern, handler)
}

// PathValueFunc reads the path parameter named name out of a request the
// router has already matched. An error is a 400.
type PathValueFunc func(r *http.Request, name string) (string, error)

// StdPathValue returns r.PathValue verbatim. net/http's ServeMux percent-
// decodes a segment before storing it, so there is nothing left to do. This
// is Runtime's default.
func StdPathValue(r *http.Request, name string) (string, error) {
	return r.PathValue(name), nil
}

// EscapedPathValue percent-decodes r.PathValue. chi v5 stores the raw segment,
// so a chi.Router needs this:
//
//	server.Runtime{PathValue: server.EscapedPathValue}
//
// A StdMux must not have it. net/http has already decoded once, so a second
// decode turns the id "50%2Fx" into "50/x" with a 200 and rejects "100%" with
// a 400. Choosing wrong in either direction is silent for an id containing
// nothing worth escaping, which is why go/server/chitest pins both directions
// against real routers rather than leaving this to a comment.
//
// The error is unreachable behind net/http, which rejects a request URI with
// a bad escape before routing; it exists so that a router feeding this
// something else cannot make a mangled id look like a valid one.
func EscapedPathValue(r *http.Request, name string) (string, error) {
	raw := r.PathValue(name)
	v, err := url.PathUnescape(raw)
	if err != nil {
		return "", &Error{Status: http.StatusBadRequest, Code: "path_param_invalid",
			Message: fmt.Sprintf("path parameter %q is not valid percent-encoding", name), Err: err}
	}
	return v, nil
}
