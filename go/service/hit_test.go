package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	v1 "github.com/metacensus/api/go/metacensus/v1"
)

// fakeSearcher answers every term with its works, and records what it was
// asked.
type fakeSearcher struct {
	works []*v1.Publication
	err   error
	terms []string
	max   int
}

func (f *fakeSearcher) Search(_ context.Context, term string, max int) ([]*v1.Publication, error) {
	f.terms, f.max = append(f.terms, term), max
	return f.works, f.err
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
