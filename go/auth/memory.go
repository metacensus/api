package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
)

// MemorySessions is the development placeholder Sessions: a random opaque token
// mapped to a callerID in memory, dropped on revoke. No expiry, no persistence,
// no cryptographic binding — a restart forgets every session, it is not safe
// across processes, and it is not a security boundary. See the package doc for
// why authentication is deferred; replace it by implementing Sessions.
//
// The zero value is not ready; use NewMemorySessions.
type MemorySessions struct {
	mu      sync.Mutex
	byToken map[string]string // token -> callerID
}

// NewMemorySessions returns an empty MemorySessions.
func NewMemorySessions() *MemorySessions {
	return &MemorySessions{byToken: make(map[string]string)}
}

func (m *MemorySessions) Issue(callerID string) (string, error) {
	if callerID == "" {
		return "", errors.New("auth: cannot issue a token for an empty callerID")
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.byToken[token] = callerID
	m.mu.Unlock()
	return token, nil
}

func (m *MemorySessions) Resolve(token string) (string, error) {
	m.mu.Lock()
	callerID, ok := m.byToken[token]
	m.mu.Unlock()
	if !ok {
		return "", errors.New("auth: token not found")
	}
	return callerID, nil
}

// Revoke forgets a token. It is not an error to revoke one already gone, so a
// double logout is idempotent.
func (m *MemorySessions) Revoke(token string) error {
	m.mu.Lock()
	delete(m.byToken, token)
	m.mu.Unlock()
	return nil
}

// randomToken is 32 bytes of crypto/rand as base64url, unpadded — the same
// encoding the signing chain uses for everything else.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
