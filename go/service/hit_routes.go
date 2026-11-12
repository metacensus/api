package service

import (
	"context"
	"strings"

	v1 "github.com/metacensus/api/go/metacensus/v1"
)

// Searcher searches the bibliographic provider (PubMed): the key, rate
// limits and caching are the backend's, as for Resolver. Search returns at
// most max works matching term, best match first, each as Resolve would
// return it; no match is an empty slice, not an error. An error means the
// provider could not be reached.
type Searcher interface {
	Search(ctx context.Context, term string, max int) ([]*v1.Publication, error)
}

// maxHits caps a search's answer; ListHits does not paginate.
const maxHits = 100

// maxTermBytes caps a search term, so a query fits a provider's GET URL.
const maxTermBytes = 2000

// ListHits searches the provider and marks each hit with what the topic
// already holds of it: the stored publication matched by DOI, then PMID, and
// the topic's paper for it. Nothing is stored.
func (h *Handlers) ListHits(ctx context.Context, req *v1.HitListRequest) (*v1.HitList, error) {
	term := strings.TrimSpace(req.GetTerm())
	switch {
	case term == "":
		return nil, badRequest("term_missing", "a search needs a term")
	case len(term) > maxTermBytes:
		return nil, badRequest("term_too_long", "the term is over 2000 bytes")
	}
	if _, err := h.store.GetTopic(ctx, req.GetTopicId()); err != nil {
		return nil, mapErr(err)
	}
	if h.searcher == nil {
		return nil, unavailable("no bibliographic provider is configured", nil)
	}
	found, err := h.searcher.Search(ctx, term, maxHits)
	if err != nil {
		return nil, unavailable("the bibliographic provider is unavailable", err)
	}

	papers, err := h.store.ListPapers(ctx, req.GetTopicId())
	if err != nil {
		return nil, mapErr(err)
	}
	paperOf := make(map[string]string, len(papers))
	for _, p := range papers {
		paperOf[p.GetContent().GetPublicationId()] = p.GetId()
	}

	hits := make([]*v1.Hit, 0, len(found))
	for _, pub := range found {
		stored, err := h.findPublication(ctx, pub.GetDoi(), pub.GetPmid())
		if err != nil {
			return nil, err
		}
		hit := &v1.Hit{Publication: pub}
		if stored != nil {
			hit.Publication, hit.PaperId = stored, paperOf[stored.GetId()]
		}
		hits = append(hits, hit)
	}
	return &v1.HitList{Items: hits}, nil
}
