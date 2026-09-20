package service

import (
	"context"

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
// travels on the request (trust on first use), not on the signature — the store
// binds it under the signature's key_id, which must thumbprint it. The signature
// carries no signer id: the id is minted here, and the author is the enrolled
// key's owner, never a client claim.
func (h *Handlers) SignUp(ctx context.Context, req *v1.SignUpRequest) (*v1.Session, error) {
	if req.GetPublicKey() == "" {
		return nil, badRequest("field_invalid", "sign-up must carry the enrolling public key in public_key")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.GetPassword()), h.bcryptCost)
	if err != nil {
		return nil, internal(err)
	}

	record := &v1.UserSigned{
		Id:             h.newID(),
		Recorded:       timestamppb.New(h.now()),
		Content:        req.GetContent(),
		Interpretation: req.GetInterpretation(),
		UserSignature:  req.GetUserSignature(),
	}
	if err := h.store.EnrollUser(ctx, record, req.GetPublicKey(), string(hash)); err != nil {
		return nil, mapErr(err)
	}
	return h.issue(record.Id)
}

// Logout revokes the session the request arrived on. The token is the one the
// middleware carried onto the context; revoking one already gone is not an
// error.
func (h *Handlers) Logout(ctx context.Context, _ *v1.LogoutRequest) (*v1.LogoutResponse, error) {
	token, ok := auth.TokenFrom(ctx)
	if !ok {
		return nil, unauthenticated("authentication required")
	}
	if err := h.sessions.Revoke(token); err != nil {
		return nil, internal(err)
	}
	return &v1.LogoutResponse{}, nil
}

// issue mints a session for id, the shared tail of Login and SignUp.
func (h *Handlers) issue(id string) (*v1.Session, error) {
	token, err := h.sessions.Issue(id)
	if err != nil {
		return nil, internal(err)
	}
	return &v1.Session{Token: token}, nil
}
