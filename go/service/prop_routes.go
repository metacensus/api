package service

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *Handlers) ListProps(ctx context.Context, req *v1.PropListRequest) (*v1.PropList, error) {
	props, err := h.store.ListProps(ctx, req.GetTopicId())
	if err != nil {
		return nil, mapErr(err)
	}
	return &v1.PropList{Items: props}, nil
}

func (h *Handlers) GetProp(ctx context.Context, req *v1.PropGetRequest) (*v1.PropSigned, error) {
	prop, err := h.store.GetProp(ctx, req.GetTopicId(), req.GetPropId())
	if err != nil {
		return nil, mapErr(err)
	}
	return prop, nil
}

// CreateProp mints the prop's id and recorded time, wraps the client's content
// and signature, persists under content.topic_id, and echoes the record. The
// topic-id agreement between path and signed content is already enforced by the
// generated binding; the topic's existence is the store's to check.
func (h *Handlers) CreateProp(ctx context.Context, req *v1.PropCreateRequest) (*v1.PropSigned, error) {
	id, cerr := h.caller(ctx)
	if cerr != nil {
		return nil, cerr
	}
	record := &v1.PropSigned{
		Id:             h.newID(),
		Recorded:       timestamppb.New(h.now()),
		Content:        req.GetContent(),
		Interpretation: req.GetInterpretation(),
		UserSignature:  req.GetUserSignature(),
	}
	if err := h.store.CreateProp(ctx, id, record); err != nil {
		return nil, mapErr(err)
	}
	return record, nil
}

func (h *Handlers) ListVotes(ctx context.Context, req *v1.VoteListRequest) (*v1.VoteList, error) {
	votes, err := h.store.ListVotes(ctx, req.GetTopicId(), req.GetPropId())
	if err != nil {
		return nil, mapErr(err)
	}
	return &v1.VoteList{Items: votes}, nil
}

// SetVote records the caller's position on one prop. A vote carries no id — it
// is keyed by (prop, user) — so the server mints only recorded. The vote's
// user_id must be the session caller (re-checked in the store, which also binds
// the author key to the caller).
func (h *Handlers) SetVote(ctx context.Context, req *v1.VoteSetRequest) (*v1.VoteSigned, error) {
	id, cerr := h.caller(ctx)
	if cerr != nil {
		return nil, cerr
	}
	if req.GetContent().GetUserId() != id {
		return nil, unauthenticated("the vote's user_id is not the session caller")
	}
	record := &v1.VoteSigned{
		Recorded:       timestamppb.New(h.now()),
		Content:        req.GetContent(),
		Interpretation: req.GetInterpretation(),
		UserSignature:  req.GetUserSignature(),
	}
	if err := h.store.SetVote(ctx, id, record); err != nil {
		return nil, mapErr(err)
	}
	return record, nil
}
