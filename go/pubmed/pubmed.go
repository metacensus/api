// Package pubmed is a service.Resolver over NCBI E-utilities: a DOI is
// searched with ESearch, a PMID is fetched with EFetch, and the PubMed record
// becomes a v1.Publication. Standard library only, since every consumer of the
// module would carry a dependency here.
//
// The API key, rate limit and cache are the caller's, passed in Config: NCBI
// allows 3 requests a second without a key and 10 with one, per key, so the
// limiter must be shared by everything that uses that key — something this
// package cannot see.
package pubmed

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/service"
)

// DefaultBaseURL is NCBI's E-utilities endpoint.
const DefaultBaseURL = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/"

// maxBody caps a response read; an EFetch of a few records is well under it.
const maxBody = 4 << 20

// maxMatches caps the PMIDs a DOI search fetches. A DOI names one work, but
// PubMed indexes errata and comments under their parent's DOI.
const maxMatches = 5

// Config is everything the backend owns. Only BaseURL has a default.
type Config struct {
	// BaseURL is DefaultBaseURL if empty; tests point it at a local server.
	BaseURL string

	// APIKey is sent as api_key when set.
	APIKey string

	// Tool and Email identify the caller to NCBI, which asks for both.
	Tool  string
	Email string

	// HTTPClient is http.DefaultClient if nil. Timeouts are the caller's, or
	// the context's.
	HTTPClient *http.Client

	// Wait is called before every request to NCBI and fails it on error —
	// rate.Limiter.Wait fits. Nil means no limit.
	Wait func(context.Context) error

	// Cache, if set, is consulted before NCBI and filled after it.
	Cache Cache
}

// Cache holds resolved publications by "doi:<doi>" and "pmid:<pmid>". A
// publication is stored under each id it carries. Misses are not cached.
type Cache interface {
	Get(ctx context.Context, key string) (*v1.Publication, bool)
	Put(ctx context.Context, key string, pub *v1.Publication)
}

// Client resolves DOIs and PMIDs at PubMed.
type Client struct {
	cfg  Config
	base string
}

var _ service.Resolver = (*Client)(nil)

// New returns a Client over cfg.
func New(cfg Config) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	return &Client{cfg: cfg, base: strings.TrimSuffix(base, "/") + "/"}
}

// Resolve implements service.Resolver. With a PMID it fetches that record and
// ignores doi (the service checks the two agree); with only a DOI it searches
// for it and keeps the record carrying exactly that DOI.
func (c *Client) Resolve(ctx context.Context, doi, pmid string) (*v1.Publication, error) {
	doi = normalizeDOI(doi)
	pmid = strings.TrimSpace(pmid)
	key := "pmid:" + pmid
	if pmid == "" {
		key = "doi:" + doi
	}
	if c.cfg.Cache != nil {
		if pub, ok := c.cfg.Cache.Get(ctx, key); ok {
			return pub, nil
		}
	}

	var pub *v1.Publication
	var err error
	switch {
	case pmid != "":
		pub, err = c.fetch(ctx, []string{pmid}, func(p *v1.Publication) bool { return p.GetPmid() == pmid })
	case doi != "":
		var ids []string
		if ids, err = c.search(ctx, doi); err == nil {
			pub, err = c.fetch(ctx, ids, func(p *v1.Publication) bool { return p.GetDoi() == doi })
		}
	default:
		return nil, fmt.Errorf("pubmed: no DOI or PMID to resolve")
	}
	if err != nil {
		return nil, err
	}

	if c.cfg.Cache != nil {
		if pub.GetDoi() != "" {
			c.cfg.Cache.Put(ctx, "doi:"+pub.GetDoi(), pub)
		}
		c.cfg.Cache.Put(ctx, "pmid:"+pub.GetPmid(), pub)
	}
	return pub, nil
}

// search returns the PMIDs whose record carries doi, or ErrUnresolved.
func (c *Client) search(ctx context.Context, doi string) ([]string, error) {
	q := url.Values{
		"term":    {`"` + doi + `"[doi]`},
		"retmode": {"json"},
		"retmax":  {fmt.Sprint(maxMatches)},
	}
	body, err := c.get(ctx, "esearch.fcgi", q)
	if err != nil {
		return nil, err
	}
	var res struct {
		Result struct {
			IDList []string `json:"idlist"`
			Error  string   `json:"ERROR"`
		} `json:"esearchresult"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("pubmed: esearch: %w", err)
	}
	if res.Result.Error != "" {
		return nil, fmt.Errorf("pubmed: esearch: %s", res.Result.Error)
	}
	if len(res.Result.IDList) == 0 {
		return nil, service.ErrUnresolved
	}
	return res.Result.IDList, nil
}

// fetch returns the first of ids' records that keep accepts, or ErrUnresolved.
func (c *Client) fetch(ctx context.Context, ids []string, keep func(*v1.Publication) bool) (*v1.Publication, error) {
	q := url.Values{"id": {strings.Join(ids, ",")}, "retmode": {"xml"}}
	body, err := c.get(ctx, "efetch.fcgi", q)
	if err != nil {
		return nil, err
	}
	var set articleSet
	if err := xml.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("pubmed: efetch: %w", err)
	}
	for _, a := range set.Articles {
		if pub := a.publication(); keep(pub) {
			return pub, nil
		}
	}
	return nil, service.ErrUnresolved
}

func (c *Client) get(ctx context.Context, endpoint string, q url.Values) ([]byte, error) {
	if c.cfg.Wait != nil {
		if err := c.cfg.Wait(ctx); err != nil {
			return nil, err
		}
	}
	q.Set("db", "pubmed")
	for k, v := range map[string]string{"api_key": c.cfg.APIKey, "tool": c.cfg.Tool, "email": c.cfg.Email} {
		if v != "" {
			q.Set(k, v)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pubmed: %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pubmed: %s: %s", endpoint, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("pubmed: %s: %w", endpoint, err)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("pubmed: %s: response over %d bytes", endpoint, maxBody)
	}
	return body, nil
}

// normalizeDOI lowercases a DOI and strips a resolver or `doi:` prefix, as
// the service normalizes a posted one.
func normalizeDOI(doi string) string {
	doi = strings.ToLower(strings.TrimSpace(doi))
	for _, p := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
		doi = strings.TrimPrefix(doi, p)
	}
	return doi
}

// normalizeORCID gives an ORCID iD bare and hyphenated, from any of the forms
// PubMed carries ("https://orcid.org/…", unhyphenated); "" if it isn't one.
func normalizeORCID(id string) string {
	var d []byte
	for _, c := range strings.ToUpper(id) {
		if c >= '0' && c <= '9' || c == 'X' {
			d = append(d, byte(c))
		}
	}
	if len(d) != 16 {
		return ""
	}
	return string(d[0:4]) + "-" + string(d[4:8]) + "-" + string(d[8:12]) + "-" + string(d[12:16])
}

// The EFetch PubmedArticleSet, as much of it as a Publication reads. Book
// records (PubmedBookArticle) are skipped, so resolve as unknown.
type articleSet struct {
	Articles []article `xml:"PubmedArticle"`
}

type article struct {
	PMID    string `xml:"MedlineCitation>PMID"`
	Title   text   `xml:"MedlineCitation>Article>ArticleTitle"`
	Journal string `xml:"MedlineCitation>Article>Journal>Title"`
	PubDate struct {
		Year        string `xml:"Year"`
		Month       string `xml:"Month"`
		Day         string `xml:"Day"`
		MedlineDate string `xml:"MedlineDate"`
	} `xml:"MedlineCitation>Article>Journal>JournalIssue>PubDate"`
	Abstract []abstractText `xml:"MedlineCitation>Article>Abstract>AbstractText"`
	Authors  []struct {
		LastName    string `xml:"LastName"`
		ForeName    string `xml:"ForeName"`
		Initials    string `xml:"Initials"`
		Collective  text   `xml:"CollectiveName"`
		Identifiers []struct {
			Source string `xml:"Source,attr"`
			Value  string `xml:",chardata"`
		} `xml:"Identifier"`
	} `xml:"MedlineCitation>Article>AuthorList>Author"`
	ELocationIDs []struct {
		Type  string `xml:"EIdType,attr"`
		Valid string `xml:"ValidYN,attr"`
		Value string `xml:",chardata"`
	} `xml:"MedlineCitation>Article>ELocationID"`
	ArticleIDs []struct {
		Type  string `xml:"IdType,attr"`
		Value string `xml:",chardata"`
	} `xml:"PubmedData>ArticleIdList>ArticleId"`
}

// publication is the record as a Publication: authors as their parts (the
// forename, or initials when PubMed has none), the date as the issue gives it, and a
// structured abstract's sections as "LABEL: text" paragraphs.
func (a article) publication() *v1.Publication {
	pub := &v1.Publication{
		Pmid:    strings.TrimSpace(a.PMID),
		Title:   string(a.Title),
		Journal: strings.TrimSpace(a.Journal),
	}
	for _, id := range a.ArticleIDs {
		if id.Type == "doi" {
			pub.Doi = normalizeDOI(id.Value)
			break
		}
	}
	if pub.Doi == "" {
		for _, id := range a.ELocationIDs {
			if id.Type == "doi" && id.Valid != "N" {
				pub.Doi = normalizeDOI(id.Value)
				break
			}
		}
	}
	for _, au := range a.Authors {
		name := &v1.Author{Family: strings.TrimSpace(au.LastName)}
		if name.Family != "" {
			if name.Given = strings.TrimSpace(au.ForeName); name.Given == "" {
				name.Given = strings.TrimSpace(au.Initials)
			}
		} else if name.Literal = string(au.Collective); name.Literal == "" {
			continue
		}
		for _, id := range au.Identifiers {
			if id.Source == "ORCID" {
				name.Orcid = normalizeORCID(id.Value)
				break
			}
		}
		pub.Authors = append(pub.Authors, name)
	}
	d := a.PubDate
	if pub.Published = strings.Join(strings.Fields(d.Year+" "+d.Month+" "+d.Day), " "); pub.Published == "" {
		pub.Published = strings.TrimSpace(d.MedlineDate)
	}
	var paras []string
	for _, p := range a.Abstract {
		if p.Text == "" {
			continue
		}
		if p.Label != "" {
			paras = append(paras, p.Label+": "+p.Text)
		} else {
			paras = append(paras, p.Text)
		}
	}
	pub.Abstract = strings.Join(paras, "\n\n")
	return pub
}

// text is an element's character data with its inline markup (<i>, <sup>)
// dropped and its whitespace collapsed.
type text string

func (t *text) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var b strings.Builder
	for depth := 1; depth > 0; {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			b.Write(tok)
		}
	}
	*t = text(strings.Join(strings.Fields(b.String()), " "))
	return nil
}

// abstractText is one AbstractText: a structured abstract labels each section.
type abstractText struct {
	Label string
	Text  string
}

func (a *abstractText) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, attr := range start.Attr {
		if attr.Name.Local == "Label" {
			a.Label = attr.Value
		}
	}
	var t text
	if err := t.UnmarshalXML(d, start); err != nil {
		return err
	}
	a.Text = string(t)
	return nil
}
