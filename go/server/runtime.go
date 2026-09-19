// Package server is the Go server half of the contract; routes_gen.go is
// generated, every other file hand-written. See README.md, "The generated
// server".
package server

// DefaultMaxBodyBytes caps a request body when Runtime.MaxBodyBytes is zero.
const DefaultMaxBodyBytes int64 = 1 << 20

// Runtime is what every generated handler runs against. The zero value is
// usable on a StdMux; any other Mux must set PathValue. It persists nothing and
// validates only what binding requires.
//
// There is deliberately no raw-body hook: a signature is verified behind
// persistence over the decoded message, not at the edge. See README.md, "The
// signing chain".
type Runtime struct {
	// Prefix is the path every route hangs off; "" means none, which is what a
	// router already mounted at the prefix needs (chi's Route, http.StripPrefix).
	// A router at the origin root wants routes.Prefix or routes.PublicPrefix.
	// This is the one per-surface field: sharing a Runtime mounts one surface
	// under the other's prefix, silently.
	Prefix string

	// MaxBodyBytes bounds a request body; a larger one is a 413. Zero means
	// DefaultMaxBodyBytes.
	MaxBodyBytes int64

	// PathValue reads one path parameter out of a matched request — the one
	// thing Mux cannot carry, since extraction differs by router and both wrong
	// answers bind a wrong id with a 200 (see StdPathValue, EscapedPathValue).
	// StdMux is the only router this package can identify; Register<Service>
	// panics for any other until this is set.
	PathValue PathValueFunc
}

// checkPathValue fails registration, once per Register<Service>, when the router
// is not a StdMux and PathValue is unset — so the mistake surfaces at startup
// rather than as a wrong id on the first request carrying an escaped character.
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

// pathValue reads a segment through PathValue, falling back to StdPathValue —
// which checkPathValue has already guaranteed is right for the only Mux that
// reaches the fallback.
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
