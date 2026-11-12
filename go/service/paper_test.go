package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"github.com/metacensus/api/internal/memstore"
)

// fakeResolver knows works by either id, and counts how often it was asked.
type fakeResolver struct {
	works []*v1.Publication
	err   error
	asked int
}

func (f *fakeResolver) Resolve(_ context.Context, doi, pmid string) (*v1.Publication, error) {
	f.asked++
	if f.err != nil {
		return nil, f.err
	}
	for _, w := range f.works {
		if doi != "" && w.GetDoi() == doi || pmid != "" && w.GetPmid() == pmid {
			return w, nil
		}
	}
	return nil, ErrUnresolved
}

var (
	dpyd = &v1.Publication{Doi: "10.1200/jco.2018.79.5780", Pmid: "30348537", Title: "DPYD genotyping", Authors: []*v1.Author{{Family: "Henricks", Given: "LM"}}}
	ugt  = &v1.Publication{Doi: "10.1000/ugt1a1", Pmid: "11111111", Title: "UGT1A1"}
)

// signedUp mounts a service over res and returns a client signed up on it.
func signedUp(t *testing.T, res Resolver) *client {
	t.Helper()
	h := New(Config{
		Store:      memstore.New(nil),
		Resolver:   res,
		Now:        func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		BcryptCost: 4,
	})
	c := newClient(t, mount(h))
	user := &v1.User{Name: "Ada", Email: "ada@example.com"}
	interp, sig := c.sign(user)
	var session v1.Session
	decode(t, c.do("POST", "/signup", &v1.SignUpRequest{
		Content: user, Password: "pw", Interpretation: interp, PublicKey: c.publicKey(), UserSignature: sig,
	}), &session)
	c.token = session.GetToken()
	return c
}

func (c *client) publication(doi, pmid string) *v1.Publication {
	c.t.Helper()
	var pub v1.Publication
	decode(c.t, c.do("POST", "/publication", &v1.PublicationCreateRequest{Doi: doi, Pmid: pmid}), &pub)
	return &pub
}

func (c *client) topic() string {
	c.t.Helper()
	content := &v1.Topic{Name: "DPYD"}
	interp, sig := c.sign(content)
	var rec v1.TopicSigned
	decode(c.t, c.do("POST", "/topic", &v1.TopicCreateRequest{Content: content, Interpretation: interp, UserSignature: sig}), &rec)
	return rec.GetId()
}

func (c *client) paper(topicID, publicationID string) *v1.PaperSigned {
	c.t.Helper()
	content := &v1.Paper{TopicId: topicID, PublicationId: publicationID}
	interp, sig := c.sign(content)
	var rec v1.PaperSigned
	decode(c.t, c.do("POST", "/topic/"+topicID+"/paper", &v1.PaperCreateRequest{
		TopicId: topicID, Content: content, Interpretation: interp, UserSignature: sig,
	}), &rec)
	return &rec
}

// TestPaperJourney adds a work to a topic by its DOI, then by its PMID and
// again by a resolver URL: one publication and one paper throughout.
func TestPaperJourney(t *testing.T) {
	res := &fakeResolver{works: []*v1.Publication{dpyd, ugt}}
	c := signedUp(t, res)

	pub := c.publication("", dpyd.GetPmid())
	if _, err := store.ParseID(store.PublicationID, pub.GetId()); err != nil {
		t.Fatalf("minted publication id: %v", err)
	}
	if pub.GetDoi() != dpyd.GetDoi() || pub.GetTitle() != dpyd.GetTitle() || pub.GetRecorded() == nil {
		t.Fatalf("publication not filled in from the provider: %+v", pub)
	}

	byDOI := c.publication("https://doi.org/10.1200/JCO.2018.79.5780", "")
	if byDOI.GetId() != pub.GetId() {
		t.Fatalf("the DOI of a work added by its PMID made a second publication: %s, want %s", byDOI.GetId(), pub.GetId())
	}
	if res.asked != 1 {
		t.Errorf("provider asked %d times, want 1: a held id needs no lookup", res.asked)
	}

	var got v1.Publication
	decode(t, c.do("GET", "/publication/"+pub.GetId(), nil), &got)
	if got.GetId() != pub.GetId() {
		t.Fatalf("GET /publication/%s = %s", pub.GetId(), got.GetId())
	}

	topicID := c.topic()
	paper := c.paper(topicID, pub.GetId())
	if _, err := store.ParseID(store.PaperID, paper.GetId()); err != nil {
		t.Fatalf("minted paper id: %v", err)
	}
	if again := c.paper(topicID, pub.GetId()); again.GetId() != paper.GetId() {
		t.Fatalf("adding the paper twice made a second one: %s, want %s", again.GetId(), paper.GetId())
	}

	var list v1.PaperList
	decode(t, c.do("GET", "/topic/"+topicID+"/paper", nil), &list)
	if len(list.GetItems()) != 1 || list.GetItems()[0].GetId() != paper.GetId() {
		t.Fatalf("GET /topic/%s/paper = %v, want only %s", topicID, list.GetItems(), paper.GetId())
	}
	var gotPaper v1.PaperSigned
	decode(t, c.do("GET", "/topic/"+topicID+"/paper/"+paper.GetId(), nil), &gotPaper)
	if gotPaper.GetId() != paper.GetId() {
		t.Fatalf("GET /topic/%s/paper/%s = %s", topicID, paper.GetId(), gotPaper.GetId())
	}
}

func TestCreatePublicationRefuses(t *testing.T) {
	for _, tt := range []struct {
		name       string
		res        Resolver
		doi, pmid  string
		wantStatus int
		wantCode   string
	}{
		{name: "neither id", res: &fakeResolver{}, wantStatus: http.StatusBadRequest, wantCode: "external_id_missing"},
		{name: "not a DOI", res: &fakeResolver{}, doi: "jco.2018", wantStatus: http.StatusBadRequest, wantCode: "doi_invalid"},
		{name: "not a PMID", res: &fakeResolver{}, pmid: "PMC123", wantStatus: http.StatusBadRequest, wantCode: "pmid_invalid"},
		{name: "unknown to the provider", res: &fakeResolver{}, pmid: "42", wantStatus: http.StatusUnprocessableEntity, wantCode: "invalid_content"},
		{name: "DOI and PMID of different works", res: &fakeResolver{works: []*v1.Publication{dpyd, ugt}}, doi: dpyd.GetDoi(), pmid: ugt.GetPmid(), wantStatus: http.StatusUnprocessableEntity, wantCode: "invalid_content"},
		{name: "provider unreachable", res: &fakeResolver{err: errors.New("dial tcp: timeout")}, pmid: "42", wantStatus: http.StatusServiceUnavailable, wantCode: "unavailable"},
		{name: "no provider configured", pmid: "42", wantStatus: http.StatusServiceUnavailable, wantCode: "unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := signedUp(t, tt.res)
			rec := c.do("POST", "/publication", &v1.PublicationCreateRequest{Doi: tt.doi, Pmid: tt.pmid})
			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if code := errorCode(t, rec); code != tt.wantCode {
				t.Errorf("code %q, want %q", code, tt.wantCode)
			}
		})
	}
}

// errorCode is the `code` of an error envelope.
func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not the envelope: %v: %s", err, rec.Body.String())
	}
	return body.Code
}
