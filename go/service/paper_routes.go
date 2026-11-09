package service

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CreatePaper mints the paper's id and recorded time, wraps the client's
// content and signature, and persists it under content.topic_id. A topic that
// already includes the publication answers with its existing paper; the store
// refuses the duplicate only after the signature stands, so a forged request
// learns nothing it could not read.
func (h *Handlers) CreatePaper(ctx context.Context, req *v1.PaperCreateRequest) (*v1.PaperSigned, error) {
	id, cerr := h.caller(ctx)
	if cerr != nil {
		return nil, cerr
	}
	record := &v1.PaperSigned{
		Id:             store.NewID(store.PaperID),
		Recorded:       timestamppb.New(h.now()),
		Content:        req.GetContent(),
		Interpretation: req.GetInterpretation(),
		UserSignature:  req.GetUserSignature(),
	}
	err := h.store.CreatePaper(ctx, id, record)
	if store.KindOf(err) == store.AlreadyExists {
		c := req.GetContent()
		if existing, ferr := h.store.FindPaper(ctx, c.GetTopicId(), c.GetPublicationId()); ferr == nil {
			return existing, nil
		}
	}
	if err != nil {
		return nil, mapErr(err)
	}
	return record, nil
}
