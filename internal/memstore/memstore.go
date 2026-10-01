// Package memstore is an in-memory store.Store: the double go/service's tests
// run against, and the implementation go/store/storetest validates itself on.
// It enforces the invariants the seam promises — uniqueness, parent existence,
// and that the author key resolves to the caller — and verifies signatures
// softly by default, as the Postgres backend does: an assertion is required to
// be present, not to stand. Config.Verify switches it to hard verification, as
// inside the Fabric boundary, so the conformance suite's Hard-only cases have
// an implementation in this repository to run against.
//
// It is internal deliberately: the shipped backends are service-api-standard
// and service-api-chain, and exporting a store from the published module is a
// decision to take when a consumer asks, not one an import would take for it.
package memstore

import (
	"context"
	"crypto/ecdsa"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/signing"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
)

// Config selects how hard the store verifies. A nil Verify verifies softly;
// a non-nil one checks every user signature against the enrolled key under
// that acceptance policy.
type Config struct {
	Verify *signing.Policy
}

// Store is the in-memory store.Store. It is not safe for concurrent use.
type Store struct {
	verify    *signing.Policy
	usersByID map[string]*v1.UserSigned
	idByEmail map[string]string
	hashByID  map[string]string
	keys      map[string]enrolledKey // the key history: key_id -> its owner and key
	topics    map[string]*v1.TopicSigned
	props     map[string]map[string]*v1.PropSigned            // topicID -> propID -> prop
	votes     map[string]map[string]map[string]*v1.VoteSigned // topicID -> propID -> userID -> vote
}

type enrolledKey struct {
	owner string
	pub   *ecdsa.PublicKey
}

// New returns an empty Store.
func New(cfg Config) *Store {
	return &Store{
		verify:    cfg.Verify,
		usersByID: map[string]*v1.UserSigned{},
		idByEmail: map[string]string{},
		hashByID:  map[string]string{},
		keys:      map[string]enrolledKey{},
		topics:    map[string]*v1.TopicSigned{},
		props:     map[string]map[string]*v1.PropSigned{},
		votes:     map[string]map[string]map[string]*v1.VoteSigned{},
	}
}

var _ store.Store = (*Store)(nil)

// authored resolves sig's key_id through the key history and reports whether
// it belongs to callerID — the binding that replaces a self-claimed signer id.
func (s *Store) authored(sig *v1.Signature, callerID string) (enrolledKey, bool) {
	k, ok := s.keys[sig.GetKeyId()]
	return k, ok && k.owner == callerID
}

// stands reports whether a user signature stands, under hard verification;
// soft verification accepts it unchecked.
func (s *Store) stands(pub *ecdsa.PublicKey, content proto.Message, interp *v1.Interpretation, sig *v1.Signature) bool {
	if s.verify == nil {
		return true
	}
	return signing.VerifyUser(pub, content, interp, sig, *s.verify) == nil
}

// clone keeps a stored record from aliasing the caller's, as a real store's
// would not: a write after the call must not reach what was persisted.
func clone[M proto.Message](m M) M { return proto.Clone(m).(M) }

func (s *Store) EnrollUser(_ context.Context, record *v1.UserSigned, publicKey, passwordHash string) error {
	sig := record.GetUserSignature()
	// Trust on first use: bind the enrolling key, but only if key_id
	// thumbprints the key offered.
	pub, err := signing.EnrolledKey(publicKey, sig.GetKeyId())
	if err != nil {
		return store.InvalidContent
	}
	if !s.stands(pub, record.GetContent(), record.GetInterpretation(), sig) {
		return store.SignatureInvalid
	}
	if _, taken := s.idByEmail[record.GetContent().GetEmail()]; taken {
		return store.AlreadyExists
	}
	if _, taken := s.usersByID[record.GetId()]; taken {
		return store.AlreadyExists
	}
	s.usersByID[record.GetId()] = clone(record)
	s.idByEmail[record.GetContent().GetEmail()] = record.GetId()
	s.hashByID[record.GetId()] = passwordHash
	s.keys[sig.GetKeyId()] = enrolledKey{owner: record.GetId(), pub: pub}
	return nil
}

func (s *Store) Credential(_ context.Context, email string) (string, string, error) {
	id, ok := s.idByEmail[email]
	if !ok {
		return "", "", store.Unauthenticated
	}
	return id, s.hashByID[id], nil
}

func (s *Store) GetUser(_ context.Context, id string) (*v1.UserSigned, error) {
	u, ok := s.usersByID[id]
	if !ok {
		return nil, store.NotFound
	}
	return clone(u), nil
}

func (s *Store) ListUsers(_ context.Context) ([]*v1.UserSigned, error) {
	out := make([]*v1.UserSigned, 0, len(s.usersByID))
	for _, u := range s.usersByID {
		out = append(out, clone(u))
	}
	return out, nil
}

func (s *Store) CreateTopic(_ context.Context, callerID string, record *v1.TopicSigned) error {
	k, ok := s.authored(record.GetUserSignature(), callerID)
	if !ok {
		return store.Unauthenticated
	}
	if !s.stands(k.pub, record.GetContent(), record.GetInterpretation(), record.GetUserSignature()) {
		return store.SignatureInvalid
	}
	if _, taken := s.topics[record.GetId()]; taken {
		return store.AlreadyExists
	}
	s.topics[record.GetId()] = clone(record)
	return nil
}

func (s *Store) GetTopic(_ context.Context, id string) (*v1.TopicSigned, error) {
	t, ok := s.topics[id]
	if !ok {
		return nil, store.NotFound
	}
	return clone(t), nil
}

func (s *Store) ListTopics(_ context.Context) ([]*v1.TopicSigned, error) {
	out := make([]*v1.TopicSigned, 0, len(s.topics))
	for _, t := range s.topics {
		out = append(out, clone(t))
	}
	return out, nil
}

func (s *Store) CreateProp(_ context.Context, callerID string, record *v1.PropSigned) error {
	k, ok := s.authored(record.GetUserSignature(), callerID)
	if !ok {
		return store.Unauthenticated
	}
	if !s.stands(k.pub, record.GetContent(), record.GetInterpretation(), record.GetUserSignature()) {
		return store.SignatureInvalid
	}
	topicID := record.GetContent().GetTopicId()
	if _, ok := s.topics[topicID]; !ok {
		return store.InvalidContent
	}
	if s.props[topicID] == nil {
		s.props[topicID] = map[string]*v1.PropSigned{}
	}
	if _, taken := s.props[topicID][record.GetId()]; taken {
		return store.AlreadyExists
	}
	s.props[topicID][record.GetId()] = clone(record)
	return nil
}

func (s *Store) GetProp(_ context.Context, topicID, propID string) (*v1.PropSigned, error) {
	p, ok := s.props[topicID][propID]
	if !ok {
		return nil, store.NotFound
	}
	return clone(p), nil
}

func (s *Store) ListProps(_ context.Context, topicID string) ([]*v1.PropSigned, error) {
	out := make([]*v1.PropSigned, 0, len(s.props[topicID]))
	for _, p := range s.props[topicID] {
		out = append(out, clone(p))
	}
	return out, nil
}

func (s *Store) SetVote(_ context.Context, callerID string, record *v1.VoteSigned) error {
	c := record.GetContent()
	k, ok := s.authored(record.GetUserSignature(), callerID)
	if !ok {
		return store.Unauthenticated
	}
	if !s.stands(k.pub, c, record.GetInterpretation(), record.GetUserSignature()) {
		return store.SignatureInvalid
	}
	// The author is the caller from here, so a user_id that disagrees with one
	// disagrees with the other.
	if c.GetTopicId() == "" || c.GetPropId() == "" || c.GetUserId() == "" || c.GetUserId() != callerID {
		return store.InvalidContent
	}
	if _, ok := s.props[c.GetTopicId()][c.GetPropId()]; !ok {
		return store.InvalidContent
	}
	if s.votes[c.GetTopicId()] == nil {
		s.votes[c.GetTopicId()] = map[string]map[string]*v1.VoteSigned{}
	}
	if s.votes[c.GetTopicId()][c.GetPropId()] == nil {
		s.votes[c.GetTopicId()][c.GetPropId()] = map[string]*v1.VoteSigned{}
	}
	s.votes[c.GetTopicId()][c.GetPropId()][c.GetUserId()] = clone(record)
	return nil
}

func (s *Store) ListVotes(_ context.Context, topicID, propID string) ([]*v1.VoteSigned, error) {
	m := s.votes[topicID][propID]
	out := make([]*v1.VoteSigned, 0, len(m))
	for _, v := range m {
		out = append(out, clone(v))
	}
	return out, nil
}
