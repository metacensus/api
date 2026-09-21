// Package service is the middle of the MetaCensus API: it implements the
// generated handler interfaces (go/server) in terms of the persistence port
// (go/store) and the session port (go/auth). It is what metacensus/demo and
// metacensus/infra import; each supplies only its own store (and, later, its
// own real auth), and mounts the handlers on its own mux.
//
// Its job is everything that is neither routing nor persistence:
//
//   - resolve the caller from the session to a callerID (go/auth);
//   - mint the non-determinism a replicated executor cannot — ids and the
//     recorded timestamp;
//   - assemble the signed record from the client's content and signature plus
//     the minted fields. It wraps; it never modifies. id and recorded are
//     record-level, outside the {content, signature} the client hashed, so
//     nothing the server adds can invalidate a signature;
//   - call the store once per request — one handler, one atomic unit of work;
//   - map a store.Kind to an HTTP status through *server.Error;
//   - echo the assembled record back, since store writes return only an error.
//
// It never verifies a signature — that happens inside the store boundary (see
// go/store). It fails fast only on checks that are O(1), state-independent, and
// re-enforced authoritatively below: that a caller is present, and that a vote's
// user_id equals the session caller. Binding the author to the caller — the key
// the author signature's key_id resolves to must be the caller's — needs the key
// history, so it is the store's, below the seam; no record carries a signer id
// or a verification key for the edge to shortcut with.
package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/metacensus/api/go/auth"
	"github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/store"
	"golang.org/x/crypto/bcrypt"
)

// defaultBcryptCost is a step above bcrypt's own default (10); it is what
// Config.BcryptCost falls back to.
const defaultBcryptCost = 12

// Config is the composition root's input. Only Store is required.
type Config struct {
	// Store is the persistence backend. Required.
	Store store.Store

	// Sessions resolves a token to a caller. Defaults to
	// auth.NewMemorySessions — a development placeholder, not a security
	// boundary (see go/auth).
	Sessions auth.Sessions

	// RefreshTTL is how long a refresh token — and the browser cookie that
	// carries it — lives. Defaults to auth.DefaultRefreshTTL. It sets the cookie
	// Max-Age and, when Sessions is left to default, the store's refresh lifetime,
	// so the two cannot drift. A caller supplying its own Sessions must set this
	// to that backend's refresh TTL, or the cookie and the server-side token will
	// disagree on how long the session lasts.
	RefreshTTL time.Duration

	// Now mints the recorded timestamp. Defaults to time.Now. Injectable so a
	// test controls the one clock the server stamps records with.
	Now func() time.Time

	// NewID mints a record id. Defaults to 16 random bytes as base64url.
	// Injectable for deterministic tests.
	NewID func() string

	// BcryptCost is the cost the password hash is minted at. Defaults to
	// defaultBcryptCost. Tests set it low so hashing is cheap.
	BcryptCost int

	// RefreshCookieName is the name of the HttpOnly refresh cookie. Defaults to
	// "mc_refresh".
	RefreshCookieName string

	// InsecureCookies drops the Secure attribute from the refresh cookie so it
	// round-trips over plain http. For tests only; never set in production.
	InsecureCookies bool
}

// Handlers implements every generated server route interface. It embeds each
// Unimplemented set, so a route with no store behind it (the topic Member
// routes, which store deliberately omits) answers 501 rather than failing to
// compile.
type Handlers struct {
	server.UnimplementedAuthRoutes
	server.UnimplementedHealthRoutes
	server.UnimplementedUserRoutes
	server.UnimplementedTopicRoutes
	server.UnimplementedPropRoutes

	store        store.Store
	sessions     auth.Sessions
	now          func() time.Time
	newID        func() string
	bcryptCost   int
	cookieName   string
	cookieSecure bool
	refreshTTL   time.Duration

	// dummyHash is compared against on a login for an unknown email, so a miss
	// costs the same bcrypt work as a wrong password and cannot be timed apart.
	dummyHash []byte
}

// New builds the handlers, filling defaults. It panics if Store is nil, because
// a server with no persistence is a programming error, not a runtime condition.
func New(cfg Config) *Handlers {
	if cfg.Store == nil {
		panic("service: Config.Store is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	newID := cfg.NewID
	if newID == nil {
		newID = randomID
	}
	refreshTTL := cfg.RefreshTTL
	if refreshTTL == 0 {
		refreshTTL = auth.DefaultRefreshTTL
	}
	sessions := cfg.Sessions
	if sessions == nil {
		// Share the service's clock and refresh TTL: expires_in is the access
		// token's expiry (minted by Sessions) minus now (read here), so the two
		// must agree, and the cookie Max-Age is this same refreshTTL. A caller
		// supplying its own Sessions must align it with Config.Now and
		// Config.RefreshTTL.
		sessions = auth.NewMemorySessionsWith(auth.MemoryConfig{Now: now, RefreshTTL: refreshTTL})
	}
	cost := cfg.BcryptCost
	if cost == 0 {
		cost = defaultBcryptCost
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("metacensus-timing-equalizer"), cost)
	if err != nil {
		panic("service: bcrypt rejected the configured cost: " + err.Error())
	}
	cookieName := cfg.RefreshCookieName
	if cookieName == "" {
		cookieName = "mc_refresh"
	}
	return &Handlers{
		store:        cfg.Store,
		sessions:     sessions,
		now:          now,
		newID:        newID,
		bcryptCost:   cost,
		cookieName:   cookieName,
		cookieSecure: !cfg.InsecureCookies,
		refreshTTL:   refreshTTL,
		dummyHash:    dummy,
	}
}

// Register mounts every authenticated-surface service on mux under rt. Health,
// Login and SignUp mount open; every other route is wrapped in the session
// middleware. The public surface (PartnerRoutes) has no store behind it and is
// a backend's own to register, on its own runtime with routes.PublicPrefix.
func (h *Handlers) Register(mux server.Mux, rt *server.Runtime) {
	// Wrapping the mux (for the middleware, and for the auth split) hides a
	// StdMux from the runtime's own path-value inference, which would then
	// panic. Resolve the convention once, here, from the real mux, so a StdMux
	// caller still needs no PathValue and a chi caller still gets the guiding
	// panic if they forgot server.EscapedPathValue.
	rt = resolvePathValue(mux, rt)
	authed := middlewareMux{inner: mux, mw: auth.Middleware(h.sessions)}
	cookie := middlewareMux{inner: mux, mw: auth.CookieAdapter(h.cookieConfig(rt))}

	server.RegisterHealthRoutes(mux, rt, h)
	server.RegisterUserRoutes(authed, rt, h)
	server.RegisterTopicRoutes(authed, rt, h)
	server.RegisterPropRoutes(authed, rt, h)
	// Every auth route establishes or ends a session, so none can require a
	// live access token: all four mount behind the cookie adapter (which reads
	// and writes the refresh cookie) and none behind the bearer middleware.
	// Logout in particular must work with an expired access token.
	server.RegisterAuthRoutes(cookie, rt, h)
}

// cookieConfig is the refresh-cookie policy the adapter enforces. The cookie is
// scoped to the API prefix — both routes that consume it, /refresh and /logout,
// hang off that prefix as siblings, and an RFC 6265 path only matches a request
// under it, so a cookie pinned to /refresh alone would never reach /logout and
// browser logout could not revoke the lineage. Only the auth routes are wrapped
// by the adapter, so the cookie riding the other prefixed routes is inert there.
func (h *Handlers) cookieConfig(rt *server.Runtime) auth.CookieConfig {
	path := rt.Prefix
	if path == "" {
		path = "/"
	}
	return auth.CookieConfig{
		Name:     h.cookieName,
		Path:     path,
		MaxAge:   h.refreshTTL,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
}

// middlewareMux wraps every handler registered through it before handing it to
// the real mux, so a whole service is mounted behind the session middleware
// with one decorator rather than a wrapper per route.
type middlewareMux struct {
	inner server.Mux
	mw    func(http.Handler) http.Handler
}

func (m middlewareMux) Method(method, pattern string, handler http.Handler) {
	m.inner.Method(method, pattern, m.mw(handler))
}

// resolvePathValue returns rt unchanged if it already carries a PathValue, or a
// copy defaulted to StdPathValue when the real mux is a StdMux. For any other
// router with no PathValue it leaves rt alone, so server's registration panics
// with the message naming the convention that router needs.
func resolvePathValue(mux server.Mux, rt *server.Runtime) *server.Runtime {
	if rt.PathValue != nil {
		return rt
	}
	if _, ok := mux.(server.StdMux); !ok {
		return rt
	}
	cp := *rt
	cp.PathValue = server.StdPathValue
	return &cp
}

// caller returns the session caller, or a 401 if the request carried none —
// which only happens on a route the middleware did not wrap, i.e. a bug.
func (h *Handlers) caller(ctx context.Context) (string, *server.Error) {
	id, ok := auth.CallerFrom(ctx)
	if !ok || id == "" {
		return "", unauthenticated("authentication required")
	}
	return id, nil
}

// mapErr turns a store error into a client response. The store's Kind chooses
// the status; the backend's own message stays in Err, for logs, never the wire.
func mapErr(err error) *server.Error {
	switch store.KindOf(err) {
	case store.NotFound:
		return &server.Error{Status: http.StatusNotFound, Code: "not_found", Message: "not found", Err: err}
	case store.AlreadyExists:
		return &server.Error{Status: http.StatusConflict, Code: "already_exists", Message: "already exists", Err: err}
	case store.InvalidContent:
		return &server.Error{Status: http.StatusUnprocessableEntity, Code: "invalid_content", Message: "the request cannot be satisfied against current state", Err: err}
	case store.SignatureInvalid:
		return &server.Error{Status: http.StatusBadRequest, Code: "signature_invalid", Message: "the signature does not verify", Err: err}
	case store.Unauthenticated:
		return &server.Error{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: "authentication required", Err: err}
	case store.Unavailable:
		return &server.Error{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "the backend is temporarily unavailable", Err: err}
	default:
		return &server.Error{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error", Err: err}
	}
}

func unauthenticated(msg string) *server.Error {
	return &server.Error{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: msg}
}

func internal(err error) *server.Error {
	return &server.Error{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error", Err: err}
}

func badRequest(code, msg string) *server.Error {
	return &server.Error{Status: http.StatusBadRequest, Code: code, Message: msg}
}

// randomID is the default id minter: 16 bytes of crypto/rand as base64url,
// unpadded — the encoding the rest of the contract uses.
func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read does not fail on the supported platforms; if it
		// ever does, a panic is better than minting a predictable id.
		panic("service: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
