package service

import (
	"context"
	"errors"
	"regexp"
	"strings"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Resolver looks a work up at the bibliographic provider (PubMed): the key,
// rate limits and caching are the backend's. Resolve returns the publication
// that doi or pmid (either may be empty, never both) names, with every id the
// provider knows normalized as the service normalizes posted ones, and id and
// recorded unset.
//
// ErrUnresolved if the provider knows no such work. Any other error means it
// could not be reached, and the request creates nothing.
type Resolver interface {
	Resolve(ctx context.Context, doi, pmid string) (*v1.Publication, error)
}

// ErrUnresolved is the Resolver's answer for an id the provider does not know.
var ErrUnresolved = errors.New("service: the provider knows no such work")

func (h *Handlers) GetPublication(ctx context.Context, req *v1.PublicationGetRequest) (*v1.Publication, error) {
	pub, err := h.store.GetPublication(ctx, req.GetPublicationId())
	if err != nil {
		return nil, mapErr(err)
	}
	return pub, nil
}

// CreatePublication answers with the publication a posted DOI or PMID names.
// A publication already holding a posted id is returned without asking the
// provider; otherwise the provider resolves the work, and a publication
// holding one of the ids it resolved to (a DOI posted for a work first added
// by its PMID) is returned instead of a second one.
func (h *Handlers) CreatePublication(ctx context.Context, req *v1.PublicationCreateRequest) (*v1.Publication, error) {
	if _, cerr := h.caller(ctx); cerr != nil {
		return nil, cerr
	}
	doi, pmid, berr := externalIDs(req)
	if berr != nil {
		return nil, berr
	}
	if pub, err := h.findPublication(ctx, doi, pmid); pub != nil || err != nil {
		return pub, err
	}
	if h.resolver == nil {
		return nil, unavailable("no bibliographic provider is configured", nil)
	}
	resolved, err := h.resolver.Resolve(ctx, doi, pmid)
	if errors.Is(err, ErrUnresolved) {
		return nil, invalidContent("the provider knows no such work")
	}
	if err != nil {
		return nil, unavailable("the bibliographic provider is unavailable", err)
	}
	if doi != "" && resolved.GetDoi() != doi || pmid != "" && resolved.GetPmid() != pmid {
		return nil, invalidContent("the DOI and the PMID name different works")
	}
	if pub, err := h.findPublication(ctx, resolved.GetDoi(), resolved.GetPmid()); pub != nil || err != nil {
		return pub, err
	}
	record := proto.Clone(resolved).(*v1.Publication)
	record.Id = store.NewID(store.PublicationID)
	record.Recorded = timestamppb.New(h.now())
	if err := h.store.CreatePublication(ctx, record); err != nil {
		if store.KindOf(err) == store.AlreadyExists {
			// Another request added it first.
			if pub, ferr := h.findPublication(ctx, record.GetDoi(), record.GetPmid()); pub != nil || ferr != nil {
				return pub, ferr
			}
		}
		return nil, mapErr(err)
	}
	return record, nil
}

// findPublication is FindPublication with NotFound as (nil, nil).
func (h *Handlers) findPublication(ctx context.Context, doi, pmid string) (*v1.Publication, error) {
	pub, err := h.store.FindPublication(ctx, doi, pmid)
	if store.KindOf(err) == store.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, mapErr(err)
	}
	return pub, nil
}

var (
	doiPrefixes = []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"}
	doiShape    = regexp.MustCompile(`^10\.[0-9]{4,9}/\S+$`)
	pmidShape   = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
)

// externalIDs normalizes the posted ids — a DOI lowercased and stripped of a
// resolver or `doi:` prefix, a PMID as bare digits — or answers 400.
func externalIDs(req *v1.PublicationCreateRequest) (doi, pmid string, _ error) {
	doi = strings.ToLower(strings.TrimSpace(req.GetDoi()))
	for _, p := range doiPrefixes {
		doi = strings.TrimPrefix(doi, p)
	}
	pmid = strings.TrimSpace(req.GetPmid())
	switch {
	case doi == "" && pmid == "":
		return "", "", badRequest("external_id_missing", "a publication is created from a DOI or a PMID")
	case doi != "" && !doiShape.MatchString(doi):
		return "", "", badRequest("doi_invalid", "the DOI is not of the form 10.<registrant>/<suffix>")
	case pmid != "" && !pmidShape.MatchString(pmid):
		return "", "", badRequest("pmid_invalid", "the PMID is not a positive integer")
	}
	return doi, pmid, nil
}
