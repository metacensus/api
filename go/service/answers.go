package service

import (
	"context"
	"sync"
	"time"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/protobuf/proto"
)

// Answers holds what a bibliographic provider answered, so a repeated search
// or lookup asks it nothing. An entry is not a record: unsigned, unminted,
// naming no user or topic, and overwritten when the provider is asked again.
// So it sits beside store.Store, not in it. Freshness is the service's
// (Config.TermTTL, Config.RecordTTL); a backend may evict an entry at any time.
type Answers interface {
	// Term returns provider's last answer for term, or nil if none is held.
	Term(ctx context.Context, provider, term string) (*TermAnswer, error)

	// PutTerm holds a as provider's answer for term, replacing any before it.
	PutTerm(ctx context.Context, provider, term string, a *TermAnswer) error

	// Records returns provider's records held under ids, by id; an id with none
	// is absent.
	Records(ctx context.Context, provider string, ids []string) (map[string]*ProviderRecord, error)

	// PutRecords holds each record under its id, replacing any before it.
	PutRecords(ctx context.Context, provider string, records map[string]*ProviderRecord) error
}

// TermAnswer is a provider's answer for a term: the ids of the works it
// matched, best match first, possibly none.
type TermAnswer struct {
	IDs      []string
	Searched time.Time
}

// ProviderRecord is a provider's record of one work, as Resolve returns it.
type ProviderRecord struct {
	Publication *v1.Publication
	Fetched     time.Time
}

// Default freshness of what Answers holds: a term's answer changes as the
// provider indexes new works, a work's record rarely.
const (
	DefaultTermTTL   = 24 * time.Hour
	DefaultRecordTTL = 30 * 24 * time.Hour
)

func recordIDs(pub *v1.Publication) []string {
	var ids []string
	if pub.GetDoi() != "" {
		ids = append(ids, "doi:"+pub.GetDoi())
	}
	if pub.GetPmid() != "" {
		ids = append(ids, "pmid:"+pub.GetPmid())
	}
	return ids
}

// MemoryAnswers is Answers in a map: nothing is evicted, nothing survives a
// restart, and replicas do not share it. A development placeholder, and a
// backend's until it keeps answers elsewhere.
type MemoryAnswers struct {
	mu      sync.Mutex
	terms   map[[2]string]TermAnswer
	records map[[2]string]ProviderRecord
}

var _ Answers = (*MemoryAnswers)(nil)

func NewMemoryAnswers() *MemoryAnswers {
	return &MemoryAnswers{terms: map[[2]string]TermAnswer{}, records: map[[2]string]ProviderRecord{}}
}

func (m *MemoryAnswers) Term(_ context.Context, provider, term string) (*TermAnswer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.terms[[2]string{provider, term}]
	if !ok {
		return nil, nil
	}
	a.IDs = append([]string(nil), a.IDs...)
	return &a, nil
}

func (m *MemoryAnswers) PutTerm(_ context.Context, provider, term string, a *TermAnswer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.terms[[2]string{provider, term}] = TermAnswer{IDs: append([]string(nil), a.IDs...), Searched: a.Searched}
	return nil
}

func (m *MemoryAnswers) Records(_ context.Context, provider string, ids []string) (map[string]*ProviderRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := make(map[string]*ProviderRecord, len(ids))
	for _, id := range ids {
		if r, ok := m.records[[2]string{provider, id}]; ok {
			found[id] = &ProviderRecord{Publication: proto.Clone(r.Publication).(*v1.Publication), Fetched: r.Fetched}
		}
	}
	return found, nil
}

func (m *MemoryAnswers) PutRecords(_ context.Context, provider string, records map[string]*ProviderRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range records {
		m.records[[2]string{provider, id}] = ProviderRecord{Publication: proto.Clone(r.Publication).(*v1.Publication), Fetched: r.Fetched}
	}
	return nil
}
