package pubmed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/service"
	"google.golang.org/protobuf/proto"
)

// A record in EFetch's shape, trimmed to what a Publication reads plus enough
// around it to show the rest is ignored.
const record = `<?xml version="1.0" ?>
<!DOCTYPE PubmedArticleSet PUBLIC "-//NLM//DTD PubMedArticle, 1st January 2025//EN" "https://dtd.nlm.nih.gov/ncbi/pubmed/out/pubmed_250101.dtd">
<PubmedArticleSet>
<PubmedArticle>
  <MedlineCitation Status="MEDLINE" Owner="NLM">
    <PMID Version="1">31452104</PMID>
    <Article PubModel="Print-Electronic">
      <Journal>
        <JournalIssue CitedMedium="Internet">
          <PubDate><Year>2021</Year><Month>Mar</Month><Day>4</Day></PubDate>
        </JournalIssue>
        <Title>Nature</Title>
      </Journal>
      <ArticleTitle>Effects of <i>Cannabis sativa</i> on
        sleep.</ArticleTitle>
      <ELocationID EIdType="doi" ValidYN="Y">10.1038/S41586-021-0001-X</ELocationID>
      <Abstract>
        <AbstractText Label="BACKGROUND">Sleep is <sup>1</sup>hard.</AbstractText>
        <AbstractText Label="RESULTS">It helped.</AbstractText>
      </Abstract>
      <AuthorList CompleteYN="Y">
        <Author ValidYN="Y"><LastName>Smith</LastName><ForeName>Jane A</ForeName><Initials>JA</Initials><Identifier Source="ORCID">https://orcid.org/0000-0002-1825-0097</Identifier></Author>
        <Author ValidYN="Y"><LastName>Lee</LastName><Initials>K</Initials><Identifier Source="ORCID">000000021825009x</Identifier></Author>
        <Author ValidYN="Y"><CollectiveName>Sleep Study Group</CollectiveName></Author>
      </AuthorList>
    </Article>
  </MedlineCitation>
  <PubmedData>
    <ArticleIdList>
      <ArticleId IdType="pubmed">31452104</ArticleId>
      <ArticleId IdType="doi">10.1038/s41586-021-0001-x</ArticleId>
    </ArticleIdList>
  </PubmedData>
</PubmedArticle>
</PubmedArticleSet>`

// An erratum indexed under its parent's DOI: no DOI of its own.
const erratumArticle = `<PubmedArticle>
  <MedlineCitation><PMID>39999999</PMID><Article>
    <Journal><JournalIssue><PubDate><MedlineDate>2021 Mar-Apr</MedlineDate></PubDate></JournalIssue><Title>Nature</Title></Journal>
    <ArticleTitle>Erratum.</ArticleTitle>
  </Article></MedlineCitation>
  <PubmedData><ArticleIdList><ArticleId IdType="pubmed">39999999</ArticleId></ArticleIdList></PubmedData>
</PubmedArticle>`

const erratum = "<PubmedArticleSet>" + erratumArticle + "</PubmedArticleSet>"

// Both, in one set, the erratum first.
var both = strings.Replace(record, "<PubmedArticleSet>", "<PubmedArticleSet>"+erratumArticle, 1)

const empty = `<?xml version="1.0" ?><PubmedArticleSet></PubmedArticleSet>`

var resolved = &v1.Publication{
	Doi:   "10.1038/s41586-021-0001-x",
	Pmid:  "31452104",
	Title: "Effects of Cannabis sativa on sleep.",
	Authors: []*v1.Author{
		{Family: "Smith", Given: "Jane A", Orcid: "0000-0002-1825-0097"},
		{Family: "Lee", Given: "K", Orcid: "0000-0002-1825-009X"},
		{Literal: "Sleep Study Group"},
	},
	Journal:   "Nature",
	Published: "2021 Mar 4",
	Abstract:  "BACKGROUND: Sleep is 1hard.\n\nRESULTS: It helped.",
}

// ncbi answers esearch with search and efetch with fetch, keyed by the id
// parameter, and records every query it is sent.
type ncbi struct {
	search string
	fetch  map[string]string
	status int
	got    []string
}

func (n *ncbi) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.got = append(n.got, r.URL.Path+"?"+r.URL.RawQuery)
	if n.status != 0 {
		w.WriteHeader(n.status)
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/esearch.fcgi"):
		w.Write([]byte(n.search))
	case strings.HasSuffix(r.URL.Path, "/efetch.fcgi"):
		body, ok := n.fetch[r.URL.Query().Get("id")]
		if !ok {
			body = empty
		}
		w.Write([]byte(body))
	default:
		http.NotFound(w, r)
	}
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ncbi      ncbi
		doi, pmid string
		want      *v1.Publication
		wantErr   error // ErrUnresolved, or nil for any other error when want is nil
	}{
		{
			name: "a PMID is fetched",
			ncbi: ncbi{fetch: map[string]string{"31452104": record}},
			pmid: "31452104",
			want: resolved,
		},
		{
			name: "a DOI is searched, then fetched",
			ncbi: ncbi{
				search: `{"esearchresult":{"count":"1","idlist":["31452104"]}}`,
				fetch:  map[string]string{"31452104": record},
			},
			doi:  "https://doi.org/10.1038/S41586-021-0001-X",
			want: resolved,
		},
		{
			name: "a DOI search keeps the record carrying that DOI",
			ncbi: ncbi{
				search: `{"esearchresult":{"count":"2","idlist":["39999999","31452104"]}}`,
				fetch:  map[string]string{"39999999,31452104": both},
			},
			doi:  "10.1038/s41586-021-0001-x",
			want: resolved,
		},
		{
			name: "with both ids the PMID decides",
			ncbi: ncbi{fetch: map[string]string{"31452104": record}},
			doi:  "10.1000/other",
			pmid: "31452104",
			want: resolved,
		},
		{
			name:    "an unknown PMID is unresolved",
			ncbi:    ncbi{},
			pmid:    "1",
			wantErr: service.ErrUnresolved,
		},
		{
			name:    "a DOI nothing carries is unresolved",
			ncbi:    ncbi{search: `{"esearchresult":{"count":"0","idlist":[]}}`},
			doi:     "10.1000/nothing",
			wantErr: service.ErrUnresolved,
		},
		{
			name: "a DOI found only on an erratum is unresolved",
			ncbi: ncbi{
				search: `{"esearchresult":{"count":"1","idlist":["39999999"]}}`,
				fetch:  map[string]string{"39999999": erratum},
			},
			doi:     "10.1038/s41586-021-0001-x",
			wantErr: service.ErrUnresolved,
		},
		{
			name: "a search error is not unresolved",
			ncbi: ncbi{search: `{"esearchresult":{"ERROR":"Invalid query"}}`},
			doi:  "10.1000/x",
		},
		{
			name: "a rate-limited request is not unresolved",
			ncbi: ncbi{status: http.StatusTooManyRequests},
			pmid: "31452104",
		},
		{
			name: "a malformed record is not unresolved",
			ncbi: ncbi{fetch: map[string]string{"31452104": "<PubmedArticleSet><PubmedArticle>"}},
			pmid: "31452104",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&tc.ncbi)
			defer srv.Close()
			got, err := New(Config{BaseURL: srv.URL}).Resolve(context.Background(), tc.doi, tc.pmid)
			switch {
			case tc.want != nil:
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				if !proto.Equal(got, tc.want) {
					t.Errorf("Resolve = %v, want %v", got, tc.want)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("Resolve error = %v, want %v", err, tc.wantErr)
				}
			default:
				if err == nil || errors.Is(err, service.ErrUnresolved) {
					t.Errorf("Resolve error = %v, want an error other than ErrUnresolved", err)
				}
			}
		})
	}
}

func TestConfigReachesNCBI(t *testing.T) {
	n := &ncbi{fetch: map[string]string{"31452104": record}}
	srv := httptest.NewServer(n)
	defer srv.Close()
	waits := 0
	c := New(Config{
		BaseURL: srv.URL + "/entrez/eutils",
		APIKey:  "k3y",
		Tool:    "metacensus",
		Email:   "ops@example.org",
		Wait:    func(context.Context) error { waits++; return nil },
	})
	if _, err := c.Resolve(context.Background(), "", "31452104"); err != nil {
		t.Fatal(err)
	}
	if waits != 1 || len(n.got) != 1 {
		t.Fatalf("waits = %d, requests = %v; want one of each", waits, n.got)
	}
	for _, want := range []string{"/entrez/eutils/efetch.fcgi?", "api_key=k3y", "tool=metacensus", "email=ops%40example.org", "db=pubmed", "retmode=xml"} {
		if !strings.Contains(n.got[0], want) {
			t.Errorf("request %q lacks %q", n.got[0], want)
		}
	}

	limited := errors.New("limited")
	c = New(Config{BaseURL: srv.URL, Wait: func(context.Context) error { return limited }})
	if _, err := c.Resolve(context.Background(), "", "31452104"); !errors.Is(err, limited) {
		t.Errorf("Resolve error = %v, want the Wait error", err)
	}
	if len(n.got) != 1 {
		t.Errorf("a failed Wait still sent a request: %v", n.got)
	}
}

type mapCache map[string]*v1.Publication

func (m mapCache) Get(_ context.Context, key string) (*v1.Publication, bool) {
	p, ok := m[key]
	return p, ok
}

func (m mapCache) Put(_ context.Context, key string, pub *v1.Publication) { m[key] = pub }

func TestCacheIsFilledUnderBothIds(t *testing.T) {
	n := &ncbi{fetch: map[string]string{"31452104": record}}
	srv := httptest.NewServer(n)
	defer srv.Close()
	cache := mapCache{}
	c := New(Config{BaseURL: srv.URL, Cache: cache})
	ctx := context.Background()
	if _, err := c.Resolve(ctx, "", "31452104"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ doi, pmid string }{
		{pmid: "31452104"},
		{doi: "doi:10.1038/S41586-021-0001-X"},
	} {
		got, err := c.Resolve(ctx, tc.doi, tc.pmid)
		if err != nil || !proto.Equal(got, resolved) {
			t.Errorf("Resolve(%q, %q) = %v, %v; want the cached publication", tc.doi, tc.pmid, got, err)
		}
	}
	if len(n.got) != 1 {
		t.Errorf("cached lookups reached NCBI: %v", n.got)
	}

	if _, err := c.Resolve(ctx, "", "1"); !errors.Is(err, service.ErrUnresolved) {
		t.Fatalf("Resolve error = %v, want ErrUnresolved", err)
	}
	if _, ok := cache["pmid:1"]; ok {
		t.Error("a miss was cached")
	}
}
