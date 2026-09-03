package storetest_test

import (
	"context"
	"testing"
	"time"

	"github.com/metacensus/api/go/store"
	"github.com/metacensus/api/go/store/storetest"
	"github.com/metacensus/api/go/store/storetest/internal/memstore"
)

// The suite run against the in-memory double.
//
// This is the suite testing itself, not a backend being certified. It answers
// the one question a conformance suite cannot answer about itself — are these
// cases satisfiable together, or does the suite encode a contradiction — and
// it is why this repository's `go test ./...` exercises the cases at all.
//
// It says nothing about Postgres or Fabric. See README.md.
func TestSuiteIsSatisfiable(t *testing.T) {
	storetest.Run(t, storetest.Config{
		New: func(t *testing.T) store.Store { return memstore.New() },
	})
}

// Every case has a name and a reason. A case whose failure message cannot say
// what it is for gets ignored the first time it goes red on someone else's PR.
func TestEveryCaseIsNamedAndExplained(t *testing.T) {
	seen := map[string]bool{}
	cases := storetest.Cases()
	if len(cases) == 0 {
		t.Fatal("the suite is empty")
	}
	for _, c := range cases {
		if c.Name == "" {
			t.Error("a case has no name")
			continue
		}
		if seen[c.Name] {
			t.Errorf("%s: duplicate case name", c.Name)
		}
		seen[c.Name] = true
		if c.Why == "" {
			t.Errorf("%s: no Why. A conformance failure is read by someone who did not "+
				"write the case", c.Name)
		}
		if c.Fn == nil {
			t.Errorf("%s: no Fn", c.Name)
		}
	}
}

// Config.New must hand back independent state, and the suite depends on it in
// the determinism case. A double that quietly shares a package-level map fails
// the suite in ways that read as conformance bugs.
func TestTheDoubleIsIndependentPerCall(t *testing.T) {
	a, b := memstore.New(), memstore.New()
	c := store.Caller{UserID: "u"}
	req := &store.UserCreateRequest{
		UserID: "u", Created: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		PasswordHash: []byte("h"),
		Name:         "N", Email: "e@example.test", Country: "US",
	}
	if _, err := a.UserCreate(context.Background(), c, req); err != nil {
		t.Fatalf("first store: %v", err)
	}
	if _, err := b.UserCreate(context.Background(), c, req); err != nil {
		t.Fatalf("second store rejected the same request, so the two share state: %v", err)
	}
}
