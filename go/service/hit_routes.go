package service

import (
	"context"
	"strings"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Searcher searches the bibliographic provider (PubMed) in two steps, so the
// service asks only for the records it does not hold; the key and rate limits
// are the backend's, as for Resolver. An error means the provider could not be
// reached.
type Searcher interface {
	// Name keys what Answers holds of this provider's answers.
	Name() string

	// Search returns the ids ("pmid:<pmid>", "doi:<doi>") of at most max works
	// matching term, best match first; no match is an empty slice.
	Search(ctx context.Context, term string, max int) ([]string, error)

	// Fetch returns the records of the works ids name, each as Resolve would
	// return it, in any order; a work the provider has no record of is left
	// out.
	Fetch(ctx context.Context, ids []string) ([]*v1.Publication, error)
}

// maxHits caps a search's answer; ListHits does not paginate.
const maxHits = 100

// maxTermBytes caps a search term, so a query fits a provider's GET URL.
const maxTermBytes = 2000

// ListHits answers a search from the answer Answers holds for the term while
// fresh, else the provider; hitPublications answers each hit. Each hit carries
// the topic's paper for it. Nothing is recorded on the topic.
func (h *Handlers) ListHits(ctx context.Context, req *v1.HitListRequest) (*v1.HitList, error) {
	// Collapsed, not folded: PubMed's AND, OR and NOT are case-sensitive.
	term := strings.Join(strings.Fields(req.GetTerm()), " ")
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
	provider := h.searcher.Name()

	answer, fresh, err := h.heldTerm(ctx, provider, term)
	if err != nil {
		return nil, err
	}
	if !fresh {
		ids, err := h.searcher.Search(ctx, term, maxHits)
		if err != nil {
			return nil, unavailable("the bibliographic provider is unavailable", err)
		}
		answer = &TermAnswer{IDs: ids, Searched: h.now()}
	}

	answered, pubs, err := h.hitPublications(ctx, provider, answer.IDs)
	if err != nil {
		return nil, err
	}
	if !fresh && h.answers != nil {
		// Only the ids that answered, so a work with no record (a PubMed
		// book) is not fetched again on every repeat.
		answer.IDs = answered
		if err := h.answers.PutTerm(ctx, provider, term, answer); err != nil {
			return nil, unavailable("stored provider answers are unavailable", err)
		}
	}

	papers, err := h.store.ListPapers(ctx, req.GetTopicId())
	if err != nil {
		return nil, mapErr(err)
	}
	paperOf := make(map[string]string, len(papers))
	for _, p := range papers {
		paperOf[p.GetContent().GetPublicationId()] = p.GetId()
	}
	hits := make([]*v1.Hit, 0, len(pubs))
	for _, pub := range pubs {
		hits = append(hits, &v1.Hit{Publication: pub, PaperId: paperOf[pub.GetId()]})
	}
	return &v1.HitList{Items: hits, Searched: timestamppb.New(answer.Searched)}, nil
}

func (h *Handlers) heldTerm(ctx context.Context, provider, term string) (held *TermAnswer, fresh bool, _ error) {
	if h.answers == nil {
		return nil, false, nil
	}
	a, err := h.answers.Term(ctx, provider, term)
	if err != nil {
		return nil, false, unavailable("stored provider answers are unavailable", err)
	}
	return a, a != nil && h.now().Sub(a.Searched) < h.termTTL, nil
}

// hitPublications answers ids in order, each from the first that holds it:
// the stored publication, a fresh held record, one fetch of the rest. A
// fetched or held record is answered as the stored publication holding its
// other id, if one does. An id with no record is left out of both answered
// and pubs.
func (h *Handlers) hitPublications(ctx context.Context, provider string, ids []string) (answered []string, pubs []*v1.Publication, _ error) {
	byID := make(map[string]*v1.Publication, len(ids))
	var unstored []string
	for _, id := range ids {
		pub, err := h.storedUnder(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if pub != nil {
			byID[id] = pub
		} else {
			unstored = append(unstored, id)
		}
	}

	held, err := h.heldRecords(ctx, provider, unstored)
	if err != nil {
		return nil, nil, err
	}
	var unheld []string
	for _, id := range unstored {
		if held[id] == nil {
			unheld = append(unheld, id)
		}
	}
	if len(unheld) > 0 {
		fetched, err := h.searcher.Fetch(ctx, unheld)
		if err != nil {
			return nil, nil, unavailable("the bibliographic provider is unavailable", err)
		}
		if err := h.holdRecords(ctx, provider, fetched); err != nil {
			return nil, nil, err
		}
		for _, pub := range fetched {
			for _, id := range recordIDs(pub) {
				held[id] = pub
			}
		}
	}
	for _, id := range unstored {
		rec := held[id]
		if rec == nil {
			continue
		}
		stored, err := h.findPublication(ctx, rec.GetDoi(), rec.GetPmid())
		if err != nil {
			return nil, nil, err
		}
		if stored != nil {
			rec = stored
		}
		byID[id] = rec
	}

	for _, id := range ids {
		if pub := byID[id]; pub != nil {
			answered, pubs = append(answered, id), append(pubs, pub)
		}
	}
	return answered, pubs, nil
}

// storedUnder is the stored publication holding id ("doi:…", "pmid:…"), or nil.
func (h *Handlers) storedUnder(ctx context.Context, id string) (*v1.Publication, error) {
	if doi, ok := strings.CutPrefix(id, "doi:"); ok {
		return h.findPublication(ctx, doi, "")
	}
	if pmid, ok := strings.CutPrefix(id, "pmid:"); ok {
		return h.findPublication(ctx, "", pmid)
	}
	return nil, nil
}

// heldRecords returns the fresh records Answers holds under ids, by id.
func (h *Handlers) heldRecords(ctx context.Context, provider string, ids []string) (map[string]*v1.Publication, error) {
	pubs := make(map[string]*v1.Publication, len(ids))
	if h.answers == nil || len(ids) == 0 {
		return pubs, nil
	}
	held, err := h.answers.Records(ctx, provider, ids)
	if err != nil {
		return nil, unavailable("stored provider answers are unavailable", err)
	}
	for id, r := range held {
		if h.now().Sub(r.Fetched) < h.recordTTL {
			pubs[id] = r.Publication
		}
	}
	return pubs, nil
}

// holdRecords puts pubs in Answers under every id each carries.
func (h *Handlers) holdRecords(ctx context.Context, provider string, pubs []*v1.Publication) error {
	if h.answers == nil || len(pubs) == 0 {
		return nil
	}
	now := h.now()
	records := make(map[string]*ProviderRecord, 2*len(pubs))
	for _, pub := range pubs {
		for _, id := range recordIDs(pub) {
			records[id] = &ProviderRecord{Publication: pub, Fetched: now}
		}
	}
	if err := h.answers.PutRecords(ctx, provider, records); err != nil {
		return unavailable("stored provider answers are unavailable", err)
	}
	return nil
}
