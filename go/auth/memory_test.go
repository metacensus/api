package auth

import (
	"testing"
	"time"
)

// fixedClock is a hand-advanced clock so a test controls expiry exactly.
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time      { return c.t }
func (c *fixedClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestSessions(clk *fixedClock) *MemorySessions {
	return NewMemorySessionsWith(MemoryConfig{
		Now:        clk.now,
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 24 * time.Hour,
	})
}

// TestIssueAndResolve: a freshly issued access token resolves to its caller,
// and the reported expiries sit one TTL ahead of the issuing instant.
func TestIssueAndResolve(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	s := newTestSessions(clk)

	tok, err := s.Issue("ada")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if got := tok.AccessExpiry.Sub(clk.now()); got != 15*time.Minute {
		t.Errorf("access expiry %v ahead, want 15m", got)
	}
	if got := tok.RefreshExpiry.Sub(clk.now()); got != 24*time.Hour {
		t.Errorf("refresh expiry %v ahead, want 24h", got)
	}
	caller, err := s.Resolve(tok.Access)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if caller != "ada" {
		t.Errorf("caller = %q, want ada", caller)
	}
}

func TestIssueRejectsEmptyCaller(t *testing.T) {
	s := NewMemorySessions()
	if _, err := s.Issue(""); err == nil {
		t.Fatal("issue with empty caller should error")
	}
}

// TestAccessTokenExpires: past the access TTL, resolve fails.
func TestAccessTokenExpires(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	s := newTestSessions(clk)

	tok, _ := s.Issue("ada")
	clk.add(15 * time.Minute) // exactly at expiry: not before, so expired
	if _, err := s.Resolve(tok.Access); err == nil {
		t.Fatal("expired access token should not resolve")
	}
}

// TestRefreshRotates: refresh mints a new pair, the new access token resolves,
// and the presented refresh token is single-use — a replay is refused.
func TestRefreshRotates(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	s := newTestSessions(clk)

	first, _ := s.Issue("ada")
	caller, second, err := s.Refresh(first.Refresh)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if caller != "ada" {
		t.Errorf("caller = %q, want ada", caller)
	}
	if second.Access == first.Access || second.Refresh == first.Refresh {
		t.Error("refresh did not rotate the tokens")
	}
	if _, err := s.Resolve(second.Access); err != nil {
		t.Errorf("rotated access token should resolve: %v", err)
	}
	// The consumed refresh token is dead.
	if _, _, err := s.Refresh(first.Refresh); err == nil {
		t.Error("a replayed refresh token should be refused")
	}
}

// TestRefreshTokenExpires: past the refresh TTL, refresh fails.
func TestRefreshTokenExpires(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	s := newTestSessions(clk)

	tok, _ := s.Issue("ada")
	clk.add(24 * time.Hour)
	if _, _, err := s.Refresh(tok.Refresh); err == nil {
		t.Fatal("expired refresh token should not refresh")
	}
}

// TestRevokeEndsTheLineage: logout revokes the whole lineage — the current
// access and refresh tokens, and the tokens minted before a rotation.
func TestRevokeEndsTheLineage(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	s := newTestSessions(clk)

	first, _ := s.Issue("ada")
	_, second, _ := s.Refresh(first.Refresh) // rotate once within the lineage

	if err := s.Revoke(second.Refresh); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.Resolve(second.Access); err == nil {
		t.Error("access token survived revoke")
	}
	if _, err := s.Resolve(first.Access); err == nil {
		t.Error("pre-rotation access token survived revoke")
	}
	if _, _, err := s.Refresh(second.Refresh); err == nil {
		t.Error("refresh token survived revoke")
	}
}

// TestRevokeIsIdempotent: revoking an unknown token is not an error, so a
// double logout is safe.
func TestRevokeIsIdempotent(t *testing.T) {
	s := NewMemorySessions()
	if err := s.Revoke("never-issued"); err != nil {
		t.Errorf("revoking an unknown token: %v", err)
	}
}

// TestLineagesAreIndependent: revoking one session leaves another caller's
// session untouched.
func TestLineagesAreIndependent(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	s := newTestSessions(clk)

	ada, _ := s.Issue("ada")
	bob, _ := s.Issue("bob")
	if err := s.Revoke(ada.Refresh); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.Resolve(bob.Access); err != nil {
		t.Errorf("bob's session should survive ada's logout: %v", err)
	}
}
