package storetest

import (
	"os"
	"path/filepath"
	"reflect"
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
	for _, f := range []string{"doc.go", "store.go", "errors.go", "draft.go"} {
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

func TestStages(t *testing.T) {
	for _, tt := range []struct {
		value   string
		want    []Stage
		wantErr bool
	}{
		{value: "", want: []Stage{Stable}},
		{value: "stable", want: []Stage{Stable}},
		{value: "draft", want: []Stage{Draft}},
		{value: "all", want: []Stage{Stable, Draft}},
		{value: "Stable", wantErr: true},
		{value: "beta", wantErr: true},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := stages(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("stages(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("stages(%q) = %v, want %v", tt.value, got, tt.want)
			}
			for _, s := range tt.want {
				if !got[s] {
					t.Errorf("stages(%q) = %v, want %v", tt.value, got, tt.want)
				}
			}
		})
	}
}

// go test -json reports each case by name, so a Draft case's name is the only
// way its output tells the stages apart.
func TestDraftCasesAreNamedDraft(t *testing.T) {
	for _, c := range []storeCase{{name: "n"}, {name: "n", want: store.NotFound}} {
		if strings.HasPrefix(c.title(), "draft") {
			t.Errorf("Stable case titled %q", c.title())
		}
		c.stage = Draft
		if !strings.HasPrefix(c.title(), "draft - ") {
			t.Errorf("Draft case titled %q, want a draft prefix", c.title())
		}
	}
}

type fakeDraft interface{ FakeDraft() }

type withFakeDraft struct{ store.Store }

func (withFakeDraft) FakeDraft() {}

func TestDraftMethodCasesSkipAgainstAStoreThatLacksIt(t *testing.T) {
	t.Cleanup(func(old []reflect.Type) func() { return func() { drafts = old } }(drafts))
	drafts = []reflect.Type{reflect.TypeFor[fakeDraft]()}

	c := storeCase{name: "n", stage: Draft, call: func(*scene) error { return nil }}
	for _, tt := range []struct {
		name    string
		s       store.Store
		wantRun bool
	}{
		{name: "lacks it", s: memstore.New(nil)},
		{name: "has it", s: withFakeDraft{memstore.New(nil)}, wantRun: true},
	} {
		var ran bool
		t.Run(tt.name, func(t *testing.T) {
			c.run(t, Harness{Open: func(*testing.T) store.Store { return tt.s }, Signatures: Soft}, newEmails(), "FakeDraft")
			ran = true
		})
		if ran != tt.wantRun {
			t.Errorf("%s: ran = %v, want %v", tt.name, ran, tt.wantRun)
		}
	}
	if !slices.Contains(storeMethods(), "FakeDraft") {
		t.Errorf("storeMethods() = %v, want it to include the Draft method", storeMethods())
	}
}

// A Draft method is not yet one every implementation has, so no Stable case
// may hold a store to it.
func TestDraftMethodsHaveOnlyDraftCases(t *testing.T) {
	for _, d := range drafts {
		m := d.Method(0).Name
		for _, c := range suite[m] {
			if c.stage != Draft {
				t.Errorf("%s is a Draft method; case %q is %v", m, c.name, c.stage)
			}
		}
	}
}

// TestReleaseCheck holds a release to carrying nothing Draft. Only make
// release-check runs it, so Draft work can sit on main between releases.
func TestReleaseCheck(t *testing.T) {
	if os.Getenv("STORETEST_RELEASE_CHECK") == "" {
		t.Skip("make release-check runs this")
	}
	for _, d := range drafts {
		t.Errorf("go/store/draft.go declares %s; move it into store.Store or out of the release", d.Name())
	}
	for m, cases := range suite {
		for _, c := range cases {
			if c.stage == Draft {
				t.Errorf("%s case %q is Draft; make it Stable or drop it", m, c.name)
			}
		}
	}
}
