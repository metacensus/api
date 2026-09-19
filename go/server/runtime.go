// Package server is the Go half of the contract: request binding, the error
// model, response encoding, the mux seam, and the Runtime every generated
// handler runs against.
package server

// DefaultMaxBodyBytes caps a request body when Runtime.MaxBodyBytes is zero.
const DefaultMaxBodyBytes int64 = 1 << 20

// Runtime is what every generated handler runs against. The zero value works
// on a StdMux; any other Mux must set PathValue. It persists nothing and
// validates only what binding requires — persistence and auth are the
// implementer's.
//
// There is no hook here that sees a raw request body: a signature is
// verified over the decoded message, not the octets (see UserSignature).
type Runtime struct {
	// Prefix is the path every route hangs off; "" means already mounted
	// there. Use routes.Prefix or routes.PublicPrefix for the origin root —
	// a process serving both surfaces needs one Runtime per surface.
	Prefix string

	// MaxBodyBytes bounds a request body; a larger one is a 413. Zero means
	// DefaultMaxBodyBytes.
	MaxBodyBytes int64

	// PathValue reads one path parameter out of a matched request; the
	// extraction convention differs across routers (see StdPathValue and
	// EscapedPathValue). Register<Service> panics if unset on any Mux but
	// StdMux.
	PathValue PathValueFunc
}

// checkPathValue panics at registration time if PathValue is unset on
// anything but a StdMux, whose convention can be inferred safely.
func (rt *Runtime) checkPathValue(mux Mux) {
	if rt.PathValue != nil {
		return
	}
	if _, ok := mux.(StdMux); ok {
		return
	}
	panic("server: Runtime.PathValue is unset and mux is not a StdMux. " +
		"Set it to the convention this router uses: a chi.Router needs " +
		"server.EscapedPathValue, a *http.ServeMux wrapped in server.StdMux " +
		"needs server.StdPathValue. Guessing binds a wrong id with a 200.")
}

// pathValue returns rt.PathValue, falling back to StdPathValue — safe
// because checkPathValue already ensures that only happens on a StdMux.
func (rt *Runtime) pathValue() PathValueFunc {
	if rt.PathValue != nil {
		return rt.PathValue
	}
	return StdPathValue
}

func (rt *Runtime) maxBody() int64 {
	if rt.MaxBodyBytes > 0 {
		return rt.MaxBodyBytes
	}
	return DefaultMaxBodyBytes
}
