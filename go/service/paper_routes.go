package service

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *Handlers) ListPapers(ctx context.Context, req *v1.PaperListRequest) (*v1.PaperList, error) {
	papers, err := h.store.ListPapers(ctx, req.GetTopicId())
	if err != nil {
		return nil, mapErr(err)
	}
	return &v1.PaperList{Items: papers}, nil
}

func (h *Handlers) GetPaper(ctx context.Context, req *v1.PaperGetRequest) (*v1.PaperSigned, error) {
	paper, err := h.store.GetPaper(ctx, req.GetTopicId(), req.GetPaperId())
	if err != nil {
		return nil, mapErr(err)
	}
	return paper, nil
}

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
