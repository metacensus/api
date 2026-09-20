package service

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/signing"
	"github.com/metacensus/api/go/store"
)

// fakeStore is an in-memory store.Store for the service tests. It enforces the
// invariants the seam promises — uniqueness, parent existence, and that the
// author key resolves to the caller — but verifies signatures softly, as the
// Postgres backend does: an assertion is required to be present, not to stand.
// It is deliberately test-only; the shipped backends are metacensus/demo and
// metacensus/infra.
type fakeStore struct {
	usersByID    map[string]*v1.UserSigned
	idByEmail    map[string]string
	hashByID     map[string]string
	ownerByKeyID map[string]string // the key history: key_id -> the user who enrolled it
	topics       map[string]*v1.TopicSigned
	props        map[string]map[string]*v1.PropSigned            // topicID -> propID -> prop
	votes        map[string]map[string]map[string]*v1.VoteSigned // topicID -> propID -> userID -> vote
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		usersByID:    map[string]*v1.UserSigned{},
		idByEmail:    map[string]string{},
		hashByID:     map[string]string{},
		ownerByKeyID: map[string]string{},
		topics:       map[string]*v1.TopicSigned{},
		props:        map[string]map[string]*v1.PropSigned{},
		votes:        map[string]map[string]map[string]*v1.VoteSigned{},
	}
}

var _ store.Store = (*fakeStore)(nil)

// authored reports whether sig's key_id resolves to callerID — the key-history
// binding that replaces a self-claimed signer id.
func (s *fakeStore) authored(sig *v1.Signature, callerID string) bool {
	owner, ok := s.ownerByKeyID[sig.GetKeyId()]
	return ok && owner == callerID
}

func (s *fakeStore) EnrollUser(_ context.Context, record *v1.UserSigned, publicKey, passwordHash string) error {
	sig := record.GetUserSignature()
	// Trust on first use: bind the enrolling key, but only if key_id thumbprints
	// the key offered (a real store also checks the assertion stands).
	if _, err := signing.EnrolledKey(publicKey, sig.GetKeyId()); err != nil {
		return store.InvalidContent
	}
	if _, taken := s.idByEmail[record.GetContent().GetEmail()]; taken {
		return store.AlreadyExists
	}
	if _, taken := s.usersByID[record.GetId()]; taken {
		return store.AlreadyExists
	}
	s.usersByID[record.GetId()] = record
	s.idByEmail[record.GetContent().GetEmail()] = record.GetId()
	s.hashByID[record.GetId()] = passwordHash
	s.ownerByKeyID[sig.GetKeyId()] = record.GetId()
	return nil
}

func (s *fakeStore) Credential(_ context.Context, email string) (string, string, error) {
	id, ok := s.idByEmail[email]
	if !ok {
		return "", "", store.Unauthenticated
	}
	return id, s.hashByID[id], nil
}

func (s *fakeStore) GetUser(_ context.Context, id string) (*v1.UserSigned, error) {
	u, ok := s.usersByID[id]
	if !ok {
		return nil, store.NotFound
	}
	return u, nil
}

func (s *fakeStore) ListUsers(_ context.Context) ([]*v1.UserSigned, error) {
	out := make([]*v1.UserSigned, 0, len(s.usersByID))
	for _, u := range s.usersByID {
		out = append(out, u)
	}
	return out, nil
}

func (s *fakeStore) CreateTopic(_ context.Context, callerID string, record *v1.TopicSigned) error {
	if !s.authored(record.GetUserSignature(), callerID) {
		return store.Unauthenticated
	}
	if _, taken := s.topics[record.GetId()]; taken {
		return store.AlreadyExists
	}
	s.topics[record.GetId()] = record
	return nil
}

func (s *fakeStore) GetTopic(_ context.Context, id string) (*v1.TopicSigned, error) {
	t, ok := s.topics[id]
	if !ok {
		return nil, store.NotFound
	}
	return t, nil
}

func (s *fakeStore) ListTopics(_ context.Context) ([]*v1.TopicSigned, error) {
	out := make([]*v1.TopicSigned, 0, len(s.topics))
	for _, t := range s.topics {
		out = append(out, t)
	}
	return out, nil
}

func (s *fakeStore) CreateProp(_ context.Context, callerID string, record *v1.PropSigned) error {
	if !s.authored(record.GetUserSignature(), callerID) {
		return store.Unauthenticated
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
	s.props[topicID][record.GetId()] = record
	return nil
}

func (s *fakeStore) GetProp(_ context.Context, topicID, propID string) (*v1.PropSigned, error) {
	p, ok := s.props[topicID][propID]
	if !ok {
		return nil, store.NotFound
	}
	return p, nil
}

func (s *fakeStore) ListProps(_ context.Context, topicID string) ([]*v1.PropSigned, error) {
	out := make([]*v1.PropSigned, 0, len(s.props[topicID]))
	for _, p := range s.props[topicID] {
		out = append(out, p)
	}
	return out, nil
}

func (s *fakeStore) SetVote(_ context.Context, callerID string, record *v1.VoteSigned) error {
	c := record.GetContent()
	if !s.authored(record.GetUserSignature(), callerID) || c.GetUserId() != callerID {
		return store.Unauthenticated
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
	s.votes[c.GetTopicId()][c.GetPropId()][c.GetUserId()] = record
	return nil
}

func (s *fakeStore) ListVotes(_ context.Context, topicID, propID string) ([]*v1.VoteSigned, error) {
	m := s.votes[topicID][propID]
	out := make([]*v1.VoteSigned, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out, nil
}
