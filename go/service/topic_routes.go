package service

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *Handlers) ListTopics(ctx context.Context, _ *v1.TopicListRequest) (*v1.TopicList, error) {
	topics, err := h.store.ListTopics(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return &v1.TopicList{Items: topics}, nil
}

func (h *Handlers) GetTopic(ctx context.Context, req *v1.TopicGetRequest) (*v1.TopicSigned, error) {
	topic, err := h.store.GetTopic(ctx, req.GetTopicId())
	if err != nil {
		return nil, mapErr(err)
	}
	return topic, nil
}

// CreateTopic mints the id and recorded time, wraps the client's content and
// signature into the record, persists it, and echoes it back.
func (h *Handlers) CreateTopic(ctx context.Context, req *v1.TopicCreateRequest) (*v1.TopicSigned, error) {
	id, cerr := h.caller(ctx)
	if cerr != nil {
		return nil, cerr
	}
	record := &v1.TopicSigned{
		Id:             h.newID(),
		Recorded:       timestamppb.New(h.now()),
		Content:        req.GetContent(),
		Interpretation: req.GetInterpretation(),
		UserSignature:  req.GetUserSignature(),
	}
	if err := h.store.CreateTopic(ctx, id, record); err != nil {
		return nil, mapErr(err)
	}
	return record, nil
}
