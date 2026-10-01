package storetest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/metacensus/api/go/signing"
	"github.com/metacensus/api/go/store"
	"github.com/metacensus/api/internal/memstore"
)

// The suite proves itself on the in-memory store four ways. Fresh and shared
// show no case leans on an empty store; Soft and Hard show the Hard-only cases
// are sound where they run, so a Soft store's skip hides nothing untested.
func TestRun_Memstore(t *testing.T) {
	hard := signing.ParticipantPolicy(Origin)
	for _, tt := range []struct {
		name   string
		policy *signing.Policy
		verify Verification
		shared bool
	}{
		{name: "soft, fresh per case", verify: Soft},
		{name: "soft, one shared store", verify: Soft, shared: true},
		{name: "hard, fresh per case", policy: &hard, verify: Hard},
		{name: "hard, one shared store", policy: &hard, verify: Hard, shared: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			shared := memstore.New(tt.policy)
			Run(t, Harness{
				Open: func(*testing.T) store.Store {
					if tt.shared {
						return shared
					}
					return memstore.New(tt.policy)
				},
				Signatures: tt.verify,
			})
		})
	}
}

func TestHarness_Validate(t *testing.T) {
	open := func(*testing.T) store.Store { return memstore.New(nil) }
	for _, tt := range []struct {
		name    string
		harness Harness
		wantErr bool
	}{
		{name: "error - no Open", harness: Harness{Signatures: Soft}, wantErr: true},
		{name: "error - Signatures undeclared", harness: Harness{Open: open}, wantErr: true},
		{name: "error - Signatures out of range", harness: Harness{Open: open, Signatures: Hard + 1}, wantErr: true},
		{name: "success - Soft", harness: Harness{Open: open, Signatures: Soft}},
		{name: "success - Hard", harness: Harness{Open: open, Signatures: Hard}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.harness.validate(); (err != nil) != tt.wantErr {
				t.Errorf("validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// A store.Store method with no cases would land unchecked; a table keyed by a
// method that no longer exists would never run.
func TestSuiteCoversEveryStoreMethod(t *testing.T) {
	methods := storeMethods()
	for _, m := range methods {
		if len(suite[m]) == 0 {
			t.Errorf("store.Store.%s has no conformance cases", m)
		}
	}
	for m := range suite {
		if !slices.Contains(methods, m) {
			t.Errorf("suite has cases for %s, which store.Store does not declare", m)
		}
	}
}

func TestEveryPromiseIsQuoted(t *testing.T) {
	var doc strings.Builder
	for _, f := range []string{"doc.go", "store.go", "errors.go"} {
		b, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if text, ok := strings.CutPrefix(strings.TrimSpace(line), "//"); ok {
				doc.WriteString(text + " ")
			}
		}
	}
	quoted := normalize(doc.String())

	var promises []string
	for _, cases := range suite {
		for _, c := range cases {
			promises = append(promises, c.promise)
		}
	}
	for _, p := range promises {
		if p == "" || !strings.Contains(quoted, normalize(p)) {
			t.Errorf("promise %q is not quoted from go/store's doc comments", p)
		}
	}
}

func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }
