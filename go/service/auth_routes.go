package service

import (
	"context"
	"time"

	"github.com/metacensus/api/go/auth"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Healthcheck answers without touching the store; it reports that the API
// process is up, not that the backend is.
func (h *Handlers) Healthcheck(_ context.Context, _ *v1.HealthcheckRequest) (*v1.HealthcheckResponse, error) {
	return &v1.HealthcheckResponse{Status: "healthy"}, nil
}

// Login verifies an email/password pair and returns a fresh session. The store
// hands back the stored hash; the bcrypt comparison happens here, above the
// seam, so the plaintext is never an invocation argument. An unknown email and
// a wrong password return the identical 401, and cost the same bcrypt work, so
// neither the status nor the timing reveals which emails are enrolled.
func (h *Handlers) Login(ctx context.Context, req *v1.LoginRequest) (*v1.Session, error) {
	id, hash, err := h.store.Credential(ctx, req.GetEmail())
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(h.dummyHash, []byte(req.GetPassword()))
		return nil, unauthenticated("authentication required")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.GetPassword())) != nil {
		return nil, unauthenticated("authentication required")
	}
	return h.issue(id)
}

// SignUp enrolls a user and returns a session for the new id. The enrolling key
// travels inline in the signature (trust on first use); the signer_id is empty
// because the id is minted here, and setting it would change the bytes the
// client signed.
func (h *Handlers) SignUp(ctx context.Context, req *v1.SignUpRequest) (*v1.Session, error) {
	sig := req.GetUserSignature()
	if sig.GetPublicKey() == "" {
		return nil, badRequest("field_invalid", "sign-up must carry the enrolling public key in user_signature.public_key")
	}
	if sig.GetSignerId() != "" {
		return nil, badRequest("field_invalid", "sign-up signature must not name a signer; the id is minted by the server")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.GetPassword()), h.bcryptCost)
	if err != nil {
		return nil, internal(err)
	}

	record := &v1.UserSigned{
		Id:            h.newID(),
		Recorded:      timestamppb.New(h.now()),
		Content:       req.GetContent(),
		UserSignature: sig,
	}
	if err := h.store.EnrollUser(ctx, record, string(hash)); err != nil {
		return nil, mapErr(err)
	}
	return h.issue(record.Id)
}

// Refresh rotates the refresh token, minting a fresh session. It prefers the
// token the adapter read from the cookie over the request body. A missing,
// expired or already-rotated token is all one 401.
func (h *Handlers) Refresh(ctx context.Context, req *v1.RefreshRequest) (*v1.Session, error) {
	token := req.GetRefreshToken()
	if c, ok := auth.RefreshCookieFrom(ctx); ok && c != "" {
		token = c
	}
	if token == "" {
		return nil, unauthenticated("authentication required")
	}
	_, t, err := h.sessions.Refresh(token)
	if err != nil {
		return nil, unauthenticated("authentication required")
	}
	return h.session(t), nil
}

// Logout revokes the session its refresh token names — the token, its rotation
// lineage and its access tokens. It prefers the cookie the adapter carried over
// the body field. No token is a no-op success, and revoking one already gone is
// not an error, so a double logout is idempotent.
func (h *Handlers) Logout(ctx context.Context, req *v1.LogoutRequest) (*v1.LogoutResponse, error) {
	token := req.GetRefreshToken()
	if c, ok := auth.RefreshCookieFrom(ctx); ok && c != "" {
		token = c
	}
	if token != "" {
		if err := h.sessions.Revoke(token); err != nil {
			return nil, internal(err)
		}
	}
	return &v1.LogoutResponse{}, nil
}

// issue mints a fresh session for id, the shared tail of Login and SignUp.
func (h *Handlers) issue(id string) (*v1.Session, error) {
	t, err := h.sessions.Issue(id)
	if err != nil {
		return nil, internal(err)
	}
	return h.session(t), nil
}

// session maps a freshly minted token pair to the wire Session. expires_in is
// the access token's remaining lifetime in whole seconds; refresh_token rides
// the body until the cookie adapter lifts it into an HttpOnly cookie for a
// browser.
func (h *Handlers) session(t auth.Tokens) *v1.Session {
	return &v1.Session{
		Token:        t.Access,
		ExpiresIn:    int32(t.AccessExpiry.Sub(h.now()) / time.Second),
		RefreshToken: t.Refresh,
	}
}
