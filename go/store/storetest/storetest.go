// Package storetest is the conformance suite for store.Store: every
// implementation runs it to prove it honours go/store's doc comments.
//
//	func TestConformance(t *testing.T) {
//		storetest.Run(t, storetest.Harness{
//			Open:       func(t *testing.T) store.Store { return myStore(t) },
//			Signatures: storetest.Hard,
//		})
//	}
//
// It asserts only what those comments promise: each case quotes its clause,
// and TestEveryPromiseIsQuoted fails on a quote they do not contain. Where they
// are silent or disagree, the case is omitted and marked where it would sit.
//
// Each case is Stable or Draft (Stage), and STORETEST_STAGE picks which run:
// unset or "stable" runs only Stable cases, "draft" only Draft ones, "all"
// both. A Draft case covers a promise still joining the contract — usually a
// method declared in go/store/draft.go, whose cases skip as not implemented
// against a store that lacks it.
//
// Every case mints its own ids (store.NewID) and emails, never deletes, and checks a global
// list only for its own records, so a store may be fresh or shared.
//
// Not covered, as not provokable through the interface: a failure partway
// through a commit, Unavailable, agreement between replicas, and
// context cancellation.
package storetest

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/metacensus/api/go/store"
)

// Origin and RPID are what every fixture's participant assertion carries: the
// clientDataJSON origin and the authenticatorData rpId. A Hard store's test
// verifier accepts Origin (signing.ParticipantPolicy(storetest.Origin)).
const (
	Origin = "https://conformance.invalid"
	RPID   = "conformance.invalid"
)

// Verification is how hard a store checks a signature at its write boundary.
// The zero value is deliberately not a choice: a Harness must declare one.
type Verification int

const (
	_ Verification = iota

	// Soft: an assertion must be present, not stand.
	Soft

	// Hard: an assertion must stand against the enrolled key. Cases that need
	// a forged signature refused run only against a Hard store, and are
	// skipped by name against a Soft one.
	Hard
)

func (v Verification) String() string {
	switch v {
	case Soft:
		return "Soft"
	case Hard:
		return "Hard"
	}
	return fmt.Sprintf("Verification(%d)", int(v))
}

// Stage is how far a case's promise has come. The zero value is Stable, so a
// case that declares no stage is one every implementation is held to.
type Stage int

const (
	// Stable: in the released contract; every implementation passes it.
	Stable Stage = iota

	// Draft: joining the contract; an implementation adopting it runs these,
	// and a release carries none (make release-check).
	Draft
)

func (s Stage) String() string {
	switch s {
	case Stable:
		return "Stable"
	case Draft:
		return "Draft"
	}
	return fmt.Sprintf("Stage(%d)", int(s))
}

// stages reads STORETEST_STAGE into the set of stages a Run selects.
func stages(v string) (map[Stage]bool, error) {
	switch v {
	case "", "stable":
		return map[Stage]bool{Stable: true}, nil
	case "draft":
		return map[Stage]bool{Draft: true}, nil
	case "all":
		return map[Stage]bool{Stable: true, Draft: true}, nil
	}
	return nil, fmt.Errorf("storetest: STORETEST_STAGE is %q; want stable, draft or all", v)
}

type Harness struct {
	// Open returns the store one case runs against. It is called once per
	// case, with that case's t, so a backend can register cleanup; it may
	// return a fresh store or a shared one.
	Open func(t *testing.T) store.Store

	Signatures Verification
}

func (h Harness) validate() error {
	if h.Open == nil {
		return errors.New("storetest: Harness.Open is nil")
	}
	if h.Signatures != Soft && h.Signatures != Hard {
		return fmt.Errorf("storetest: Harness.Signatures is %v; declare Soft or Hard", h.Signatures)
	}
	return nil
}

// Run runs every conformance case STORETEST_STAGE selects against the store h
// opens.
func Run(t *testing.T, h Harness) {
	t.Helper()
	if err := h.validate(); err != nil {
		t.Fatal(err)
	}
	selected, err := stages(os.Getenv("STORETEST_STAGE"))
	if err != nil {
		t.Fatal(err)
	}
	emails := newEmails()
	for _, method := range storeMethods() {
		t.Run(method, func(t *testing.T) {
			for _, c := range suite[method] {
				if selected[c.stage] {
					t.Run(c.title(), func(t *testing.T) { c.run(t, h, emails, method) })
				}
			}
		})
	}
}

// suite is every case, keyed by the store.Store method it covers.
var suite = map[string][]storeCase{
	"EnrollUser":  enrollUserCases,
	"Credential":  credentialCases,
	"GetUser":     getUserCases,
	"ListUsers":   listUsersCases,
	"CreateTopic": createTopicCases,
	"GetTopic":    getTopicCases,
	"ListTopics":  listTopicsCases,
	"CreateProp":  createPropCases,
	"GetProp":     getPropCases,
	"ListProps":   listPropsCases,
	"SetVote":     setVoteCases,
	"ListVotes":   listVotesCases,
}

// drafts is store.Drafts, a variable so the suite's own tests can declare one.
var drafts = store.Drafts

// storeMethods is every method the suite covers: Store's, then each Draft's.
func storeMethods() []string {
	iface := reflect.TypeFor[store.Store]()
	out := make([]string, iface.NumMethod())
	for i := range out {
		out[i] = iface.Method(i).Name
	}
	for _, d := range drafts {
		out = append(out, d.Method(0).Name)
	}
	return out
}

// draftOf returns the Draft interface declaring method, if one does.
func draftOf(method string) (reflect.Type, bool) {
	for _, d := range drafts {
		if d.Method(0).Name == method {
			return d, true
		}
	}
	return nil, false
}

// emails issues addresses unique to one Run, so cases never collide with each
// other or with whatever a shared store already holds. Ids need no such help:
// store.NewID is unique on its own.
type emails struct {
	prefix string
	n      atomic.Int64
}

func newEmails() *emails { return &emails{prefix: "conformance-" + rand.Text()} }

func (e *emails) next() string {
	return fmt.Sprintf("%s-%d@%s", e.prefix, e.n.Add(1), RPID)
}
