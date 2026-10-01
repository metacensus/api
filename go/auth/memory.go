package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// The access TTL bounds how long a leaked access token is good for; the refresh
// TTL, how long an idle session survives.
const (
	DefaultAccessTTL  = 15 * time.Minute
	DefaultRefreshTTL = 30 * 24 * time.Hour
)

// MemorySessions is the development-placeholder Sessions: opaque access and
// refresh tokens held in memory, indexed to a caller and a session lineage. It
// implements the whole token model — short access TTL, rotating single-use
// refresh tokens, logout that revokes a lineage — but a restart forgets every
// session and it is not safe across processes. It is not a security boundary;
// replace it with a persistent Sessions (see the package doc).
//
// The zero value is not ready; use NewMemorySessions.
type MemorySessions struct {
	mu         sync.Mutex
	now        func() time.Time
	accessTTL  time.Duration
	refreshTTL time.Duration
	access     map[string]record // by access token
	refresh    map[string]record // by refresh token
}

// record is one stored token: whose it is, which login lineage it belongs to
// (so Revoke can drop the lineage's every token at once), and when it expires.
type record struct {
	caller  string
	session string
	expiry  time.Time
}

// MemoryConfig tunes a MemorySessions. Every field has a default, so the zero
// value is production-shaped (if not production-ready); tests set a fake clock
// and short TTLs.
type MemoryConfig struct {
	Now        func() time.Time // defaults to time.Now
	AccessTTL  time.Duration    // defaults to DefaultAccessTTL
	RefreshTTL time.Duration    // defaults to DefaultRefreshTTL
}

// NewMemorySessions returns a ready MemorySessions with the default clock and
// lifetimes.
func NewMemorySessions() *MemorySessions {
	return NewMemorySessionsWith(MemoryConfig{})
}

// NewMemorySessionsWith returns a ready MemorySessions, filling defaults.
func NewMemorySessionsWith(cfg MemoryConfig) *MemorySessions {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = DefaultAccessTTL
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = DefaultRefreshTTL
	}
	return &MemorySessions{
		now:        cfg.Now,
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
		access:     map[string]record{},
		refresh:    map[string]record{},
	}
}

func (m *MemorySessions) Issue(_ context.Context, callerID string) (Tokens, error) {
	if callerID == "" {
		return Tokens{}, errors.New("auth: cannot issue tokens for an empty callerID")
	}
	session, err := randomToken()
	if err != nil {
		return Tokens{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mint(callerID, session)
}

func (m *MemorySessions) Resolve(_ context.Context, access string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.access[access]
	if !ok {
		return "", errors.New("auth: access token not found")
	}
	if !m.now().Before(rec.expiry) {
		delete(m.access, access)
		return "", errors.New("auth: access token expired")
	}
	return rec.caller, nil
}

func (m *MemorySessions) Refresh(_ context.Context, refresh string) (string, Tokens, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.refresh[refresh]
	if !ok {
		return "", Tokens{}, errors.New("auth: refresh token not found")
	}
	delete(m.refresh, refresh)
	if !m.now().Before(rec.expiry) {
		return "", Tokens{}, errors.New("auth: refresh token expired")
	}
	t, err := m.mint(rec.caller, rec.session)
	if err != nil {
		return "", Tokens{}, err
	}
	return rec.caller, t, nil
}

// Revoke cannot name a lineage from a refresh token already rotated away.
func (m *MemorySessions) Revoke(_ context.Context, refresh string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.refresh[refresh]
	if !ok {
		return nil
	}
	for tok, r := range m.access {
		if r.session == rec.session {
			delete(m.access, tok)
		}
	}
	for tok, r := range m.refresh {
		if r.session == rec.session {
			delete(m.refresh, tok)
		}
	}
	return nil
}

// mint issues one access+refresh pair on a lineage. The caller holds m.mu.
func (m *MemorySessions) mint(caller, session string) (Tokens, error) {
	access, err := randomToken()
	if err != nil {
		return Tokens{}, err
	}
	refresh, err := randomToken()
	if err != nil {
		return Tokens{}, err
	}
	now := m.now()
	accessExp := now.Add(m.accessTTL)
	refreshExp := now.Add(m.refreshTTL)
	m.access[access] = record{caller: caller, session: session, expiry: accessExp}
	m.refresh[refresh] = record{caller: caller, session: session, expiry: refreshExp}
	return Tokens{Access: access, AccessExpiry: accessExp, Refresh: refresh}, nil
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
