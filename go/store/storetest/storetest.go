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
// Every case mints its own ids (store.NewID) and emails, never deletes, and checks a global
// list only for its own records, so a store may be fresh or shared.
//
// Not covered, as not provokable through the interface: a failure partway
// through a commit, Unavailable, agreement between endorsing peers, and
// context cancellation.
package storetest

import (
	"crypto/rand"
	"errors"
	"fmt"
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

// Run runs every conformance case against the store h opens.
func Run(t *testing.T, h Harness) {
	t.Helper()
	if err := h.validate(); err != nil {
		t.Fatal(err)
	}
	emails := newEmails()
	for _, method := range storeMethods() {
		t.Run(method, func(t *testing.T) {
			for _, c := range suite[method] {
				t.Run(c.title(), func(t *testing.T) { c.run(t, h, emails) })
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

func storeMethods() []string {
	iface := reflect.TypeFor[store.Store]()
	out := make([]string, iface.NumMethod())
	for i := range out {
		out[i] = iface.Method(i).Name
	}
	return out
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
