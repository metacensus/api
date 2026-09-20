package service

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
)

// ListUsers returns every user.
func (h *Handlers) ListUsers(ctx context.Context, _ *v1.UserListRequest) (*v1.UserList, error) {
	users, err := h.store.ListUsers(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return &v1.UserList{Items: users}, nil
}

// GetUser returns one user by id.
func (h *Handlers) GetUser(ctx context.Context, req *v1.UserGetRequest) (*v1.UserSigned, error) {
	user, err := h.store.GetUser(ctx, req.GetUserId())
	if err != nil {
		return nil, mapErr(err)
	}
	return user, nil
}

// GetSelf returns the session caller's own user record; it is GetUser bound to
// the caller rather than a path id.
func (h *Handlers) GetSelf(ctx context.Context, _ *v1.SelfGetRequest) (*v1.UserSigned, error) {
	id, cerr := h.caller(ctx)
	if cerr != nil {
		return nil, cerr
	}
	user, err := h.store.GetUser(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return user, nil
}
