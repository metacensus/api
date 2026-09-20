// Package auth is the caller-resolution seam: how a request's access token
// becomes the callerID the service layer signs its work with. It is the second
// outbound port beside store — a backend plugs in a Sessions and the service
// depends only on this interface.
//
// # The token model
//
// Two tokens, the browser-auth standard. An access token is short-lived and
// opaque, sent as `Authorization: Bearer <token>`; Resolve turns it back into a
// caller on every authenticated request, and it expires on its own. A refresh
// token is long-lived, single-use and rotated: Refresh consumes one and mints a
// fresh pair, Revoke ends the whole lineage at logout. Both are opaque and
// server-stored — the point of storing them is that logout can drop them now,
// which a self-describing token could not promise before its own expiry.
//
// The CookieAdapter (see cookie.go) is what keeps the refresh token out of a
// browser's JavaScript: it moves the token between the JSON contract and an
// HttpOnly cookie, and it is the one piece designed to relocate to a reverse
// proxy without the handlers or this port changing.
//
// # MemorySessions is still a placeholder
//
// The model is designed; the storage is not. MemorySessions holds it all in
// memory: a restart forgets every session and it is not safe across processes.
// Swap it for a persistent Sessions before this is anything but a demo. The
// signature over each record, verified inside the store boundary, remains the
// real proof of authorship — a session token only says who is connected. See
// go/contract and go/store.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/metacensus/api/go/server"
)

// Tokens is one freshly minted access+refresh pair and the expiries that go
// with them. The service maps AccessExpiry to the wire's expires_in.
// RefreshExpiry is the refresh token's own server-side horizon; the cookie's
// Max-Age is set from CookieConfig.MaxAge instead (the adapter never sees a
// Tokens), so a deployment must keep that config in step with the Sessions
// refresh TTL — see service.Config.
type Tokens struct {
	Access        string
	AccessExpiry  time.Time
	Refresh       string
	RefreshExpiry time.Time
}

// Sessions is the session port. Issue mints a pair for a freshly authenticated
// caller (login, sign-up); Resolve turns an access token back into its caller
// on every authenticated request, rejecting an expired or unknown one; Refresh
// consumes a refresh token and mints a new pair for the same caller, rotating
// it; Revoke ends the lineage a refresh token belongs to (logout). A real
// implementation decides storage and expiry; the service sees only this port.
type Sessions interface {
	Issue(callerID string) (Tokens, error)
	Resolve(access string) (callerID string, err error)
	Refresh(refresh string) (callerID string, t Tokens, err error)
	Revoke(refresh string) error
}

// ctxKey is unexported so only this package can put a value on a context; a
// handler reads them back through CallerFrom and RefreshCookieFrom.
type ctxKey int

const (
	callerKey ctxKey = iota
	refreshCookieKey
)

// CallerFrom returns the callerID the middleware resolved, or "" and false on a
// request that did not pass through it. Writes and GetSelf require it.
func CallerFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(callerKey).(string)
	return id, ok
}

// RefreshCookieFrom returns the refresh token the CookieAdapter read off the
// request's HttpOnly cookie, or "" and false when the request carried none. The
// Refresh and Logout handlers prefer it over the body field, so a browser can
// leave the field empty and let the cookie speak.
func RefreshCookieFrom(ctx context.Context) (string, bool) {
	tok, ok := ctx.Value(refreshCookieKey).(string)
	return tok, ok
}

// Middleware resolves the bearer access token against s and, on success,
// carries the callerID onto the request context for the handler beneath it. A
// missing or unresolvable token is a 401 in the contract's error envelope, and
// the handler never runs. The service layer wraps the authenticated content
// routes with this; the auth routes (login, sign-up, refresh, logout) and the
// health check are mounted without it — each establishes or ends a session, so
// none can require a live one.
func Middleware(s Sessions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := tokenFromHeader(r)
			if err != nil {
				server.WriteError(w, unauthorized("no bearer token"))
				return
			}
			callerID, err := s.Resolve(token)
			if err != nil {
				server.WriteError(w, unauthorized("token does not resolve to a caller"))
				return
			}
			ctx := context.WithValue(r.Context(), callerKey, callerID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// unauthorized is the one 401 shape this package emits; its message never says
// whether the token was absent, malformed or unknown, to keep it from probing.
func unauthorized(logDetail string) *server.Error {
	return &server.Error{
		Status:  http.StatusUnauthorized,
		Code:    "unauthenticated",
		Message: "authentication required",
		Err:     errors.New(logDetail),
	}
}

func tokenFromHeader(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", errors.New("authorization header missing")
	}
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "bearer") || token == "" {
		return "", errors.New("authorization header is not a bearer token")
	}
	return token, nil
}
