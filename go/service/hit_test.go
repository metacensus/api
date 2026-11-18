package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	v1 "github.com/metacensus/api/go/metacensus/v1"
)

// fakeSearcher answers every term with its works' PMIDs, fetches them by
// PMID, and records what it was asked.
type fakeSearcher struct {
	works   []*v1.Publication
	err     error
	terms   []string
	max     int
	fetched [][]string
}

func (f *fakeSearcher) Name() string { return "fake" }

func (f *fakeSearcher) Search(_ context.Context, term string, max int) ([]string, error) {
	f.terms, f.max = append(f.terms, term), max
	ids := make([]string, len(f.works))
	for i, w := range f.works {
		ids[i] = "pmid:" + w.GetPmid()
	}
	return ids, f.err
}

func (f *fakeSearcher) Fetch(_ context.Context, ids []string) ([]*v1.Publication, error) {
	f.fetched = append(f.fetched, ids)
	var pubs []*v1.Publication
	for _, w := range f.works {
		if slices.Contains(ids, "pmid:"+w.GetPmid()) {
			pubs = append(pubs, w)
		}
	}
	return pubs, f.err
}

// searching mounts a service over res and s and returns a client signed up
// on it.
func searching(t *testing.T, res Resolver, s Searcher) *client {
	t.Helper()
	return signedUpWith(t, Config{Resolver: res, Searcher: s})
}

func (c *client) hits(topicID, term string) *v1.HitList {
	c.t.Helper()
	var list v1.HitList
	decode(c.t, c.do("GET", "/topic/"+topicID+"/hit?term="+url.QueryEscape(term), nil), &list)
	return &list
}

// TestHitsMarkWhatTheTopicHolds searches for three works: one the topic
// includes, one another topic added (a publication, no paper here), and one
// nobody has.
func TestHitsMarkWhatTheTopicHolds(t *testing.T) {
	other := &v1.Publication{Doi: "10.1000/new", Pmid: "22222222", Title: "New"}
	// The provider's title has changed since dpyd was stored; a held work is
	// answered as stored.
	fresh := &v1.Publication{Doi: dpyd.GetDoi(), Pmid: dpyd.GetPmid(), Title: "DPYD genotyping, as PubMed has it now"}
	s := &fakeSearcher{works: []*v1.Publication{fresh, ugt, other}}
	c := searching(t, &fakeResolver{works: []*v1.Publication{dpyd, ugt}}, s)

	topicID := c.topic()
	included := c.publication(dpyd.GetDoi(), "")
	paper := c.paper(topicID, included.GetId())
	elsewhere := c.publication("", ugt.GetPmid())

	got := c.hits(topicID, "  dpyd AND toxicity ")
	if len(s.terms) != 1 || s.terms[0] != "dpyd AND toxicity" || s.max != maxHits {
		t.Errorf("searched %q for %d, want %q for %d", s.terms, s.max, "dpyd AND toxicity", maxHits)
	}
	items := got.GetItems()
	if len(items) != 3 {
		t.Fatalf("%d hits, want 3: %v", len(items), items)
	}
	if p := items[0].GetPublication(); p.GetId() != included.GetId() || p.GetTitle() != dpyd.GetTitle() || items[0].GetPaperId() != paper.GetId() {
		t.Errorf("included hit = %v, want publication %s (stored) and paper %s", items[0], included.GetId(), paper.GetId())
	}
	if items[1].GetPublication().GetId() != elsewhere.GetId() || items[1].GetPaperId() != "" {
		t.Errorf("hit held elsewhere = %v, want publication %s and no paper", items[1], elsewhere.GetId())
	}
	if p := items[2].GetPublication(); p.GetId() != "" || p.GetRecorded() != nil || p.GetPmid() != other.GetPmid() || items[2].GetPaperId() != "" {
		t.Errorf("new hit = %v, want the provider's record, unstored", items[2])
	}
	if want := [][]string{{"pmid:" + other.GetPmid()}}; !slices.EqualFunc(s.fetched, want, slices.Equal) {
		t.Errorf("fetched %v, want only the unstored work: %v", s.fetched, want)
	}
}

func answering(t *testing.T, s Searcher, a Answers) *client {
	t.Helper()
	return signedUpWith(t, Config{Resolver: &fakeResolver{works: []*v1.Publication{dpyd}}, Searcher: s, Answers: a})
}

func TestWithoutAnswersEverySearchAsksTheProvider(t *testing.T) {
	s := &fakeSearcher{works: []*v1.Publication{ugt}}
	c := searching(t, nil, s)
	topicID := c.topic()
	c.hits(topicID, "ugt")
	got := c.hits(topicID, "ugt")
	if len(s.terms) != 2 || len(s.fetched) != 2 {
		t.Errorf("searched %d and fetched %d times, want 2 and 2", len(s.terms), len(s.fetched))
	}
	if !got.GetSearched().AsTime().Equal(testNow) {
		t.Errorf("searched %v, want %v", got.GetSearched().AsTime(), testNow)
	}
}

func TestRepeatedTermAsksTheProviderNothing(t *testing.T) {
	s := &fakeSearcher{works: []*v1.Publication{ugt}}
	c := answering(t, s, NewMemoryAnswers())
	topicID := c.topic()
	c.hits(topicID, "ugt AND  toxicity")
	got := c.hits(topicID, " ugt AND toxicity ")
	if len(s.terms) != 1 || len(s.fetched) != 1 {
		t.Errorf("searched %d and fetched %d times, want 1 and 1", len(s.terms), len(s.fetched))
	}
	if items := got.GetItems(); len(items) != 1 || items[0].GetPublication().GetPmid() != ugt.GetPmid() {
		t.Errorf("repeated hits = %v, want %s", items, ugt.GetPmid())
	}
}

// TestNewTermOfStoredWorksOnlySearches searches for a stored publication and
// a work whose record is held.
func TestNewTermOfStoredWorksOnlySearches(t *testing.T) {
	ctx := context.Background()
	a := NewMemoryAnswers()
	a.PutRecords(ctx, "fake", map[string]*ProviderRecord{"pmid:" + ugt.GetPmid(): {Publication: ugt, Fetched: testNow}})
	s := &fakeSearcher{works: []*v1.Publication{dpyd, ugt}}
	c := answering(t, s, a)
	topicID := c.topic()
	stored := c.publication(dpyd.GetDoi(), "")

	items := c.hits(topicID, "dpyd OR ugt").GetItems()
	if len(s.terms) != 1 || len(s.fetched) != 0 {
		t.Errorf("searched %d and fetched %v, want 1 search and no fetch", len(s.terms), s.fetched)
	}
	if len(items) != 2 || items[0].GetPublication().GetId() != stored.GetId() || items[1].GetPublication().GetPmid() != ugt.GetPmid() {
		t.Errorf("hits = %v, want %s then %s", items, stored.GetId(), ugt.GetPmid())
	}
}

// TestExpiredAnswersAreAskedAgain holds a term's answer and a work's record,
// each fresh or expired, and checks the provider is asked for the expired
// ones and its answer replaces them.
func TestExpiredAnswersAreAskedAgain(t *testing.T) {
	const term = "ugt"
	id := "pmid:" + ugt.GetPmid()
	revised := &v1.Publication{Doi: ugt.GetDoi(), Pmid: ugt.GetPmid(), Title: "UGT1A1, revised"}
	fresh, termExpired, recordExpired := testNow.Add(-time.Hour), testNow.Add(-DefaultTermTTL), testNow.Add(-DefaultRecordTTL)
	for _, tt := range []struct {
		name                    string
		searched, fetched       time.Time
		wantSearches, wantFetch int
		wantSearched            time.Time
		wantTitle               string
	}{
		{name: "both fresh", searched: fresh, fetched: fresh, wantSearched: fresh, wantTitle: ugt.GetTitle()},
		{name: "term expired", searched: termExpired, fetched: fresh, wantSearches: 1, wantSearched: testNow, wantTitle: ugt.GetTitle()},
		{name: "record expired", searched: fresh, fetched: recordExpired, wantFetch: 1, wantSearched: fresh, wantTitle: revised.GetTitle()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			a := NewMemoryAnswers()
			a.PutTerm(ctx, "fake", term, &TermAnswer{IDs: []string{id}, Searched: tt.searched})
			a.PutRecords(ctx, "fake", map[string]*ProviderRecord{id: {Publication: ugt, Fetched: tt.fetched}})
			s := &fakeSearcher{works: []*v1.Publication{revised}}
			c := answering(t, s, a)

			got := c.hits(c.topic(), term)
			if len(s.terms) != tt.wantSearches || len(s.fetched) != tt.wantFetch {
				t.Errorf("searched %d and fetched %d times, want %d and %d", len(s.terms), len(s.fetched), tt.wantSearches, tt.wantFetch)
			}
			if !got.GetSearched().AsTime().Equal(tt.wantSearched) {
				t.Errorf("searched %v, want when the provider answered, %v", got.GetSearched().AsTime(), tt.wantSearched)
			}
			if items := got.GetItems(); len(items) != 1 || items[0].GetPublication().GetTitle() != tt.wantTitle {
				t.Errorf("hits = %v, want %q", items, tt.wantTitle)
			}
			if held, _ := a.Term(ctx, "fake", term); !held.Searched.Equal(tt.wantSearched) {
				t.Errorf("held term searched %v, want %v", held.Searched, tt.wantSearched)
			}
			if held, _ := a.Records(ctx, "fake", []string{id}); held[id].Publication.GetTitle() != tt.wantTitle {
				t.Errorf("held record %v, want %q", held[id].Publication, tt.wantTitle)
			}
		})
	}
}

// TestPublicationFromAHeldRecord adds a hit to MetaCensus without asking the
// resolver: the search held its record.
func TestPublicationFromAHeldRecord(t *testing.T) {
	res := &fakeResolver{}
	c := signedUpWith(t, Config{Resolver: res, Searcher: &fakeSearcher{works: []*v1.Publication{ugt}}, Answers: NewMemoryAnswers()})
	c.hits(c.topic(), "ugt")
	if pub := c.publication("", ugt.GetPmid()); pub.GetTitle() != ugt.GetTitle() || res.asked != 0 {
		t.Errorf("publication %v after %d resolves, want %q and none", pub, res.asked, ugt.GetTitle())
	}
}

func TestListHitsRefuses(t *testing.T) {
	for _, tt := range []struct {
		name       string
		searcher   Searcher
		topic      bool
		term       string
		wantStatus int
		wantCode   string
	}{
		{name: "no term", searcher: &fakeSearcher{}, topic: true, term: " ", wantStatus: http.StatusBadRequest, wantCode: "term_missing"},
		{name: "a term too long", searcher: &fakeSearcher{}, topic: true, term: strings.Repeat("a", maxTermBytes+1), wantStatus: http.StatusBadRequest, wantCode: "term_too_long"},
		{name: "an unknown topic", searcher: &fakeSearcher{}, term: "x", wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "provider unreachable", searcher: &fakeSearcher{err: errors.New("dial tcp: timeout")}, topic: true, term: "x", wantStatus: http.StatusServiceUnavailable, wantCode: "unavailable"},
		{name: "no provider configured", topic: true, term: "x", wantStatus: http.StatusServiceUnavailable, wantCode: "unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := searching(t, nil, tt.searcher)
			topicID := "topic:0192f3e4-0000-7000-8000-000000000000"
			if tt.topic {
				topicID = c.topic()
			}
			rec := c.do("GET", "/topic/"+topicID+"/hit?term="+url.QueryEscape(tt.term), nil)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if code := errorCode(t, rec); code != tt.wantCode {
				t.Errorf("code %q, want %q", code, tt.wantCode)
			}
		})
	}
}
