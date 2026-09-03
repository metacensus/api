// Package memstore is an in-memory store.Store used to test the conformance
// suite itself.
//
// It is NOT a third backend and must never become one. It is under internal/
// so nothing outside ./storetest can import it, and it exists to answer one
// question a suite cannot answer about itself: are these cases satisfiable at
// all, or does the suite encode a contradiction? A suite that has never been
// run green against anything is a proposal.
//
// What it proves: the cases are mutually consistent and an implementation can
// pass them. What it does not prove: anything about Postgres or Fabric.
// Passing here is necessary and nowhere near sufficient — see
// ../../README.md, "What the suite does not catch".
package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/metacensus/api/go/store"
)

// New returns an empty store.
func New() store.Store { return &mem{} }

// The compile-time assertion every implementation should carry.
var _ store.Store = (*mem)(nil)

type propKey struct{ topic, prop store.ID }
type voteKey struct{ topic, prop, user store.ID }
type extKey struct{ topic, paper, protocol, reviewer store.ID }

type mem struct {
	mu     sync.Mutex
	users  map[store.ID]store.User
	emails map[string]store.ID
	creds  map[store.ID][]byte
	topics map[store.ID]store.Topic
	props  map[propKey]store.Prop
	votes  map[voteKey]store.Vote
	exts   map[extKey]*ext
	atts   map[string]store.Attestation
}

type ext struct {
	rec  store.Extraction
	data map[store.ID]store.Datum
}

func (m *mem) init() {
	if m.users == nil {
		m.users = map[store.ID]store.User{}
		m.emails = map[string]store.ID{}
		m.creds = map[store.ID][]byte{}
		m.topics = map[store.ID]store.Topic{}
		m.props = map[propKey]store.Prop{}
		m.votes = map[voteKey]store.Vote{}
		m.exts = map[extKey]*ext{}
		m.atts = map[string]store.Attestation{}
	}
}

// --- shared preconditions -------------------------------------------------

func ctxErr(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return store.Wrap(err, store.CodeDeadlineExceeded, op, store.Ref{}, "context is done")
	}
	return nil
}

func caller(c store.Caller, op string) error {
	if c.UserID == "" {
		return store.Errorf(store.CodeUnauthenticated, op, store.Ref{}, "no caller")
	}
	return nil
}

func required(op string, ref store.Ref, fields map[string]store.ID) error {
	for name, v := range fields {
		if v == "" {
			return store.Errorf(store.CodeInvalid, op, ref, "%s is required", name)
		}
	}
	return nil
}

func copySig(s *store.Signature) *store.Signature {
	if s == nil {
		return nil
	}
	out := *s
	out.Bytes = append([]byte(nil), s.Bytes...)
	return &out
}

// --- Users ----------------------------------------------------------------

func (m *mem) UserCreate(ctx context.Context, c store.Caller, r *store.UserCreateRequest) (*store.User, error) {
	const op = "UserCreate"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, store.Errorf(store.CodeInvalid, op, store.Ref{}, "nil request")
	}
	ref := store.UserRef(r.UserID)
	if err := required(op, ref, map[string]store.ID{"UserID": r.UserID}); err != nil {
		return nil, err
	}
	if r.Email == "" {
		return nil, store.Errorf(store.CodeInvalid, op, ref, "Email is required")
	}
	if r.Created.IsZero() {
		return nil, store.Errorf(store.CodeInvalid, op, ref, "Created is minted above the seam and is required")
	}
	if c.UserID != r.UserID {
		return nil, store.Errorf(store.CodeInvalid, op, ref,
			"caller %q is not the user being created", c.UserID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	if _, taken := m.users[r.UserID]; taken {
		return nil, store.Errorf(store.CodeAlreadyExists, op, ref, "user id is taken")
	}
	if _, taken := m.emails[r.Email]; taken {
		return nil, store.Errorf(store.CodeAlreadyExists, op, ref, "email is taken")
	}

	u := store.User{ID: r.UserID, Name: r.Name, Email: r.Email, Country: r.Country, Created: r.Created}
	m.users[u.ID] = u
	m.emails[r.Email] = u.ID
	m.creds[u.ID] = append([]byte(nil), r.PasswordHash...)
	m.record(ref, c, r.Created)
	return &u, nil
}

func (m *mem) UserGet(ctx context.Context, c store.Caller, r *store.UserGetRequest) (*store.User, error) {
	const op = "UserGet"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.UserRef(r.UserID)
	if err := required(op, ref, map[string]store.ID{"UserID": r.UserID}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	u, ok := m.users[r.UserID]
	if !ok {
		return nil, store.Errorf(store.CodeNotFound, op, ref, "no such user")
	}
	return &u, nil
}

func (m *mem) UserList(ctx context.Context, c store.Caller, _ *store.UserListRequest) (*store.UserList, error) {
	const op = "UserList"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	out := &store.UserList{}
	for _, u := range m.users {
		out.Items = append(out.Items, u)
	}
	return out, nil
}

func (m *mem) CredentialGet(ctx context.Context, r *store.CredentialGetRequest) (*store.Credential, error) {
	const op = "CredentialGet"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if r == nil || r.Email == "" {
		return nil, store.Errorf(store.CodeInvalid, op, store.Ref{}, "Email is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	id, ok := m.emails[r.Email]
	if !ok {
		return nil, store.Errorf(store.CodeNotFound, op, store.Ref{}, "no user with that email")
	}
	return &store.Credential{UserID: id, PasswordHash: append([]byte(nil), m.creds[id]...)}, nil
}

// --- Topics ---------------------------------------------------------------

func (m *mem) TopicCreate(ctx context.Context, c store.Caller, r *store.TopicCreateRequest) (*store.Topic, error) {
	const op = "TopicCreate"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.TopicRef(r.TopicID)
	if err := required(op, ref, map[string]store.ID{"TopicID": r.TopicID}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	if _, taken := m.topics[r.TopicID]; taken {
		return nil, store.Errorf(store.CodeAlreadyExists, op, ref, "topic id is taken")
	}
	tp := store.Topic{ID: r.TopicID, Name: r.Name, Description: r.Description, Created: r.Created}
	m.topics[tp.ID] = tp
	m.record(ref, c, r.Created)
	return &tp, nil
}

func (m *mem) TopicGet(ctx context.Context, c store.Caller, r *store.TopicGetRequest) (*store.Topic, error) {
	const op = "TopicGet"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.TopicRef(r.TopicID)
	if err := required(op, ref, map[string]store.ID{"TopicID": r.TopicID}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	tp, ok := m.topics[r.TopicID]
	if !ok {
		return nil, store.Errorf(store.CodeNotFound, op, ref, "no such topic")
	}
	return &tp, nil
}

func (m *mem) TopicList(ctx context.Context, c store.Caller, _ *store.TopicListRequest) (*store.TopicList, error) {
	const op = "TopicList"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	out := &store.TopicList{}
	for _, tp := range m.topics {
		out.Items = append(out.Items, tp)
	}
	return out, nil
}

// --- Props ----------------------------------------------------------------

func (m *mem) PropCreate(ctx context.Context, c store.Caller, r *store.PropCreateRequest) (*store.Prop, error) {
	const op = "PropCreate"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.PropRef(r.TopicID, r.PropID)
	if err := required(op, ref, map[string]store.ID{"TopicID": r.TopicID, "PropID": r.PropID}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	if _, ok := m.topics[r.TopicID]; !ok {
		return nil, store.Errorf(store.CodeNotFound, op, store.TopicRef(r.TopicID), "no such topic")
	}
	k := propKey{r.TopicID, r.PropID}
	if _, taken := m.props[k]; taken {
		return nil, store.Errorf(store.CodeAlreadyExists, op, ref, "prop id is taken")
	}
	p := store.Prop{
		TopicID: r.TopicID, ID: r.PropID, AuthorID: c.UserID,
		Type: r.Type, Description: r.Description, Created: r.Created,
	}
	m.props[k] = p
	m.record(ref, c, r.Created)
	return &p, nil
}

func (m *mem) PropGet(ctx context.Context, c store.Caller, r *store.PropGetRequest) (*store.Prop, error) {
	const op = "PropGet"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.PropRef(r.TopicID, r.PropID)
	if err := required(op, ref, map[string]store.ID{"TopicID": r.TopicID, "PropID": r.PropID}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	p, ok := m.props[propKey{r.TopicID, r.PropID}]
	if !ok {
		return nil, store.Errorf(store.CodeNotFound, op, ref, "no such prop")
	}
	return &p, nil
}

func (m *mem) PropList(ctx context.Context, c store.Caller, r *store.PropListRequest) (*store.PropList, error) {
	const op = "PropList"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.TopicRef(r.TopicID)
	if err := required(op, ref, map[string]store.ID{"TopicID": r.TopicID}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	out := &store.PropList{}
	for k, p := range m.props {
		if k.topic == r.TopicID {
			out.Items = append(out.Items, p)
		}
	}
	return out, nil
}

func (m *mem) VoteSet(ctx context.Context, c store.Caller, r *store.VoteSetRequest) (*store.Vote, error) {
	const op = "VoteSet"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.VoteRef(r.TopicID, r.PropID, c.UserID)
	if err := required(op, ref, map[string]store.ID{"TopicID": r.TopicID, "PropID": r.PropID}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	if _, ok := m.props[propKey{r.TopicID, r.PropID}]; !ok {
		return nil, store.Errorf(store.CodeNotFound, op, store.PropRef(r.TopicID, r.PropID), "no such prop")
	}
	v := store.Vote{
		TopicID: r.TopicID, PropID: r.PropID, UserID: c.UserID,
		Position: r.Position, Explanation: r.Explanation,
		Citations: append([]store.Citation(nil), r.Citations...),
		Cast:      r.Cast,
	}
	m.votes[voteKey{r.TopicID, r.PropID, c.UserID}] = v
	m.record(ref, c, r.Cast)
	return &v, nil
}

func (m *mem) VoteList(ctx context.Context, c store.Caller, r *store.VoteListRequest) (*store.VoteList, error) {
	const op = "VoteList"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.PropRef(r.TopicID, r.PropID)
	if err := required(op, ref, map[string]store.ID{"TopicID": r.TopicID, "PropID": r.PropID}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	out := &store.VoteList{}
	for k, v := range m.votes {
		if k.topic == r.TopicID && k.prop == r.PropID {
			out.Items = append(out.Items, v)
		}
	}
	return out, nil
}

// --- Extractions ----------------------------------------------------------

func (m *mem) ExtractionUpsert(ctx context.Context, c store.Caller, r *store.ExtractionUpsertRequest) (*store.Extraction, error) {
	const op = "ExtractionUpsert"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.ExtractionRef(r.TopicID, r.PaperID, r.ProtocolID, c.UserID)
	if err := required(op, ref, map[string]store.ID{
		"TopicID": r.TopicID, "PaperID": r.PaperID, "ProtocolID": r.ProtocolID,
	}); err != nil {
		return nil, err
	}

	// Validate the whole fan-out before touching anything. One method is one
	// atomic unit of work, so a malformed element at position 3 must leave
	// positions 1 and 2 unwritten.
	seen := map[store.ID]bool{}
	for i, d := range r.Data {
		if d.ProtocolElementID == "" {
			return nil, store.Errorf(store.CodeInvalid, op, ref,
				"data[%d]: ProtocolElementID is required", i)
		}
		if seen[d.ProtocolElementID] {
			return nil, store.Errorf(store.CodeInvalid, op, ref,
				"data[%d]: %q appears twice in one call", i, d.ProtocolElementID)
		}
		seen[d.ProtocolElementID] = true
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	if _, ok := m.topics[r.TopicID]; !ok {
		return nil, store.Errorf(store.CodeNotFound, op, store.TopicRef(r.TopicID), "no such topic")
	}

	k := extKey{r.TopicID, r.PaperID, r.ProtocolID, c.UserID}
	e, ok := m.exts[k]
	if !ok {
		e = &ext{
			rec: store.Extraction{
				TopicID: r.TopicID, PaperID: r.PaperID,
				ProtocolID: r.ProtocolID, ReviewerID: c.UserID,
			},
			data: map[store.ID]store.Datum{},
		}
		m.exts[k] = e
	}
	for _, d := range r.Data {
		if d.Retract {
			delete(e.data, d.ProtocolElementID)
			continue
		}
		stored := d
		stored.Source = append([]store.SourcePage(nil), d.Source...)
		e.data[d.ProtocolElementID] = stored
	}
	e.rec.Recorded = r.Recorded
	m.record(ref, c, r.Recorded)
	return e.snapshot(), nil
}

func (e *ext) snapshot() *store.Extraction {
	out := e.rec
	out.Data = nil
	for _, d := range e.data {
		out.Data = append(out.Data, d)
	}
	return &out
}

func (m *mem) ExtractionGet(ctx context.Context, c store.Caller, r *store.ExtractionGetRequest) (*store.Extraction, error) {
	const op = "ExtractionGet"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.ExtractionRef(r.TopicID, r.PaperID, r.ProtocolID, r.ReviewerID)
	if err := required(op, ref, map[string]store.ID{
		"TopicID": r.TopicID, "PaperID": r.PaperID,
		"ProtocolID": r.ProtocolID, "ReviewerID": r.ReviewerID,
	}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	e, ok := m.exts[extKey{r.TopicID, r.PaperID, r.ProtocolID, r.ReviewerID}]
	if !ok {
		return nil, store.Errorf(store.CodeNotFound, op, ref, "that reviewer has no pass over that paper")
	}
	return e.snapshot(), nil
}

func (m *mem) ExtractionList(ctx context.Context, c store.Caller, r *store.ExtractionListRequest) (*store.ExtractionList, error) {
	const op = "ExtractionList"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}
	ref := store.ExtractionRef(r.TopicID, r.PaperID, r.ProtocolID, "")
	if err := required(op, ref, map[string]store.ID{
		"TopicID": r.TopicID, "PaperID": r.PaperID, "ProtocolID": r.ProtocolID,
	}); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	out := &store.ExtractionList{}
	for k, e := range m.exts {
		if k.topic == r.TopicID && k.paper == r.PaperID && k.protocol == r.ProtocolID {
			out.Items = append(out.Items, *e.snapshot())
		}
	}
	return out, nil
}

// --- Attestations ---------------------------------------------------------

// record writes what this store can say about a record it just wrote. It is
// called with m.mu held.
func (m *mem) record(ref store.Ref, c store.Caller, recorded time.Time) {
	m.atts[ref.String()] = store.Attestation{
		Level:    store.LevelRecorded,
		Subject:  ref,
		Author:   c.UserID,
		Recorded: recorded,
		Signed:   copySig(c.Signed),
	}
}

func (m *mem) Attestation(ctx context.Context, c store.Caller, r *store.AttestationRequest) (*store.Attestation, error) {
	const op = "Attestation"
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := caller(c, op); err != nil {
		return nil, err
	}

	want := map[store.Kind]int{
		store.KindUser: 1, store.KindTopic: 1, store.KindProp: 2,
		store.KindVote: 3, store.KindExtraction: 4,
	}
	n, known := want[r.Subject.Kind]
	if !known || len(r.Subject.Key) != n {
		return nil, store.Errorf(store.CodeInvalid, op, r.Subject,
			"a %q ref has %d key segments, got %d", r.Subject.Kind, n, len(r.Subject.Key))
	}
	for _, k := range r.Subject.Key {
		if k == "" {
			return nil, store.Errorf(store.CodeInvalid, op, r.Subject, "empty key segment")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()

	att, ok := m.atts[r.Subject.String()]
	if !ok {
		return nil, store.Errorf(store.CodeNotFound, op, r.Subject, "no such record")
	}
	out := att
	out.Signed = copySig(att.Signed)
	return &out, nil
}
