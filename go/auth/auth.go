// Package auth is the caller-resolution seam: how a request's session token
// becomes the callerID the service layer signs its work with. It is the second
// outbound port beside store — a backend plugs in a Sessions and the service
// depends only on this interface.
//
// # A placeholder, on purpose
//
// Authentication is not designed yet. The open questions — what a token is
// (opaque, JWT, key-bound), where session state lives, how tokens expire and
// revoke, how any of this improves under a real "authn" story — are all
// deferred behind Sessions. What this package ships today is the seam plus
// MemorySessions, a development placeholder that makes the whole request path
// resolve end to end without committing to any of those answers. Nothing here
// is a security boundary; swap MemorySessions before this is anything but a
// demo. The signature over each record, verified inside the store boundary,
// remains the real proof of authorship — see go/signing and go/store.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/metacensus/api/go/server"
)

// Sessions turns a callerID into a bearer token and back. Issue mints a token
// for an authenticated caller (login, sign-up); Resolve is the reverse, run on
// every authenticated request; Revoke ends one (logout). A real implementation
// decides token shape, expiry and persistence; the service never sees any of it.
type Sessions interface {
	Issue(callerID string) (token string, err error)
	Resolve(token string) (callerID string, err error)
	Revoke(token string) error
}

// ctxKey is unexported so only this package can put a caller or token on a
// context; a handler reads them back through CallerFrom and TokenFrom.
type ctxKey int

const (
	callerKey ctxKey = iota
	tokenKey
)

// CallerFrom returns the callerID the middleware resolved, or "" and false on a
// request that did not pass through it. Writes and GetSelf require it.
func CallerFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(callerKey).(string)
	return id, ok
}

// TokenFrom returns the raw bearer token the middleware carried onto the
// context, or "" and false. Logout needs it to revoke the session it arrived on.
func TokenFrom(ctx context.Context) (string, bool) {
	tok, ok := ctx.Value(tokenKey).(string)
	return tok, ok
}

// Middleware resolves the bearer token against s and, on success, carries the
// callerID and the token onto the request context for the handler beneath it. A
// missing or unresolvable token is a 401 in the contract's error envelope, and
// the handler never runs. The service layer wraps only the authenticated
// routes with this; login, sign-up and the health check are mounted without it.
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
			ctx = context.WithValue(ctx, tokenKey, token)
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

// tokenFromHeader pulls the token out of an "Authorization: Bearer <token>"
// header, case-insensitively on the scheme.
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
