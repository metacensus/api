package sessiontest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/metacensus/api/go/auth"
)

// The suite proves itself on MemorySessions both ways: fresh and shared show
// no case leans on an empty Sessions or on a clock no other case has moved.
func TestRun_MemorySessions(t *testing.T) {
	for _, tt := range []struct {
		name   string
		shared bool
	}{
		{name: "fresh per case"},
		{name: "one shared Sessions", shared: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var s auth.Sessions
			Run(t, Harness{
				Open: func(_ *testing.T, now func() time.Time) auth.Sessions {
					if s == nil || !tt.shared {
						s = auth.NewMemorySessionsWith(auth.MemoryConfig{Now: now, RefreshTTL: time.Hour})
					}
					return s
				},
				RefreshTTL: time.Hour,
			})
		})
	}
}

func TestHarness_Validate(t *testing.T) {
	open := func(*testing.T, func() time.Time) auth.Sessions { return auth.NewMemorySessions() }
	for _, tt := range []struct {
		name    string
		harness Harness
		wantErr bool
	}{
		{name: "error - no Open", harness: Harness{RefreshTTL: time.Hour}, wantErr: true},
		{name: "error - RefreshTTL undeclared", harness: Harness{Open: open}, wantErr: true},
		{name: "error - RefreshTTL negative", harness: Harness{Open: open, RefreshTTL: -time.Hour}, wantErr: true},
		{name: "success", harness: Harness{Open: open, RefreshTTL: time.Hour}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.harness.validate(); (err != nil) != tt.wantErr {
				t.Errorf("validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// An auth.Sessions method with no cases would land unchecked; a table keyed by
// a method that no longer exists would never run.
func TestSuiteCoversEverySessionsMethod(t *testing.T) {
	methods := sessionsMethods()
	for _, m := range methods {
		if len(suite[m]) == 0 {
			t.Errorf("auth.Sessions.%s has no conformance cases", m)
		}
	}
	for m := range suite {
		if !slices.Contains(methods, m) {
			t.Errorf("suite has cases for %s, which auth.Sessions does not declare", m)
		}
	}
}

func TestEveryPromiseIsQuoted(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "auth.go"))
	if err != nil {
		t.Fatal(err)
	}
	var doc strings.Builder
	for _, line := range strings.Split(string(b), "\n") {
		if text, ok := strings.CutPrefix(strings.TrimSpace(line), "//"); ok {
			doc.WriteString(text + " ")
		}
	}
	quoted := normalize(doc.String())

	for _, cases := range suite {
		for _, c := range cases {
			if c.promise == "" || !strings.Contains(quoted, normalize(c.promise)) {
				t.Errorf("promise %q is not quoted from go/auth's doc comments", c.promise)
			}
		}
	}
}

func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }
