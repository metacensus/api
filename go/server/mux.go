package server

import (
	"fmt"
	"net/http"
	"net/url"
)

// Mux is the registration surface a generated Register<Service> function
// needs. A chi.Router satisfies it directly; a *http.ServeMux needs StdMux
// (it has no method-specific registration call).
//
// Everything else a router decides before a handler runs — path-parameter
// escaping, HEAD-on-GET, 404/405 bodies, path cleaning — is left to it and
// differs between net/http's ServeMux and chi (routegen/chitest exercises
// both). Escaping specifically is not left to chance: see Runtime.PathValue.
type Mux interface {
	Method(method, pattern string, handler http.Handler)
}

// StdMux adapts a *http.ServeMux to Mux using net/http's "METHOD pattern"
// registration syntax.
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

// EscapedPathValue percent-decodes r.PathValue, for a chi.Router (which
// stores the raw segment):
//
//	server.Runtime{PathValue: server.EscapedPathValue}
//
// Do not use this with a StdMux — net/http already decodes once, so a
// second decode would mangle an id like "50%2Fx" into "50/x". The error
// branch guards against a router less strict than net/http about escapes.
func EscapedPathValue(r *http.Request, name string) (string, error) {
	raw := r.PathValue(name)
	v, err := url.PathUnescape(raw)
	if err != nil {
		return "", &Error{Status: http.StatusBadRequest, Code: "path_param_invalid",
			Message: fmt.Sprintf("path parameter %q is not valid percent-encoding", name), Err: err}
	}
	return v, nil
}
