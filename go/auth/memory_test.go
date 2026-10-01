package auth

import (
	"testing"
	"time"
)

func TestMemoryConfigAccessTTL(t *testing.T) {
	ctx := t.Context()
	now := time.Unix(1_700_000_000, 0).UTC()
	s := NewMemorySessionsWith(MemoryConfig{
		Now:       func() time.Time { return now },
		AccessTTL: time.Minute,
	})

	tok, err := s.Issue(ctx, "ada")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if got := tok.AccessExpiry.Sub(now); got != time.Minute {
		t.Errorf("access expiry %v ahead, want the configured 1m", got)
	}
}
