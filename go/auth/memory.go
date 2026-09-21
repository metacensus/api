package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Default token lifetimes: a short access token, a long refresh token. The
// access TTL bounds how long a revoked-but-unexpired token lingers; the refresh
// TTL is how long a session survives without re-authenticating.
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

// Issue opens a new session lineage for callerID and mints its first token
// pair.
func (m *MemorySessions) Issue(callerID string) (Tokens, error) {
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

// Resolve returns the caller a live access token belongs to. An unknown or
// expired token is an error; an expired one is dropped on the way out.
func (m *MemorySessions) Resolve(access string) (string, error) {
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

// Refresh consumes a refresh token and mints a fresh pair on the same lineage.
// The presented token is single-use: it is deleted here, so a replay finds
// nothing and is refused. An unknown or expired token is an error.
func (m *MemorySessions) Refresh(refresh string) (string, Tokens, error) {
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

// Revoke ends the lineage a refresh token names — every access and refresh
// token on it. Revoking one already gone is not an error, so a double logout is
// idempotent; the trade-off is that a stale, already-rotated token can no
// longer name its lineage, so logout is driven by the current refresh token
// (the one the cookie holds).
func (m *MemorySessions) Revoke(refresh string) error {
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
	return Tokens{Access: access, AccessExpiry: accessExp, Refresh: refresh, RefreshExpiry: refreshExp}, nil
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
