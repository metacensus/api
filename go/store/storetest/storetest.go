// Package storetest is the conformance suite for store.Store: every
// implementation of the persistence seam runs it to prove it honours the
// contract store.go's doc comments state.
//
//	func TestConformance(t *testing.T) {
//		storetest.Run(t, storetest.Harness{
//			Open:       func(t *testing.T) store.Store { return myStore(t) },
//			Signatures: storetest.Hard,
//		})
//	}
//
// # What it asserts
//
// Only what store.go, errors.go and doc.go promise. Each case quotes the clause
// it enforces as its promise, and a self-test fails if any promise is not
// found in those sources — an assertion the doc comments do not make would
// otherwise become contract by being tested. Where the comments are silent or
// disagree (list order, concurrency, which Kind an unresolvable key_id earns),
// the suite asserts nothing, and the omission is marked where the case would
// sit.
//
// # Isolation
//
// Harness.Open is called once per case, and may return a fresh store or the
// same shared one: the suite assumes neither. Every case mints its own ids and
// emails under a per-Run random prefix, never deletes, and checks a global list
// (ListUsers, ListTopics) only for its own records. A list scoped to a parent
// the case created (ListProps, ListVotes) is the case's alone, and is checked
// exactly. How a backend isolates or tears down is the backend's.
//
// # Signatures
//
// Every fixture is correctly signed, under Origin and RPID. A store declares
// how hard it verifies: Soft (the Postgres backend: an assertion must be
// present, not stand) or Hard (inside the Fabric boundary). Cases that need a
// cryptographic failure refused run only under Hard and are skipped, by name,
// under Soft; they never assert that a Soft store accepts a bad signature. A
// Hard store's test verifier must accept a participant assertion from Origin.
//
// # Where this sits
//
// go/service tests the API over an in-memory store, and is only as true as
// that store; this suite is what makes it true, and the only layer that
// reaches states the API never produces — id collisions, a key enrolled to
// someone else, a tampered record. The third layer, the API over a real store,
// is each backend's own: whatever it catches that these two miss is behaviour
// the doc comments do not state, and belongs back in them.
//
// # Not covered
//
// Atomicity is checked only as "a refused write leaves no trace". Not covered:
// MVCC conflicts and Unavailable (not provokable generically), concurrent use,
// a failure partway through a commit, agreement between endorsing peers,
// context cancellation, key rotation, and the institutional signature's
// verification (undesigned; the suite checks only that it is persisted).
package storetest

import (
	"crypto/rand"
	"encoding/hex"
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

	// Soft: an assertion must be present, not stand — the Postgres backend,
	// which raises SignatureInvalid only for malformed input.
	Soft

	// Hard: an assertion must stand against the enrolled key — inside the
	// Fabric boundary.
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

// Harness is what an implementation hands the suite.
type Harness struct {
	// Open returns the store one case runs against. It is called once per
	// case, with that case's t, so a backend can register cleanup; it may
	// return a fresh store or a shared one.
	Open func(t *testing.T) store.Store

	// Signatures declares how hard the store verifies.
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

// Run runs every conformance case against the store h opens, one subtest per
// store.Store method and one per case beneath it.
func Run(t *testing.T, h Harness) {
	t.Helper()
	if err := h.validate(); err != nil {
		t.Fatal(err)
	}
	ids := newMinter(t)
	for _, method := range storeMethods() {
		cases, ok := suite[method]
		if !ok {
			t.Errorf("storetest: no conformance cases for store.Store.%s", method)
			continue
		}
		t.Run(method, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) { c.run(t, h, ids) })
			}
		})
	}
}

// suite is every case, keyed by the store.Store method it covers;
// TestSuiteCoversEveryStoreMethod holds the keys to the interface.
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

// storeMethods names the methods of store.Store.
func storeMethods() []string {
	iface := reflect.TypeFor[store.Store]()
	out := make([]string, iface.NumMethod())
	for i := range out {
		out[i] = iface.Method(i).Name
	}
	return out
}

// minter issues ids and emails unique to one Run, so cases never collide with
// each other or with whatever a shared store already holds.
type minter struct {
	prefix string
	n      atomic.Int64
}

func newMinter(t *testing.T) *minter {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("storetest: mint a run prefix: %v", err)
	}
	return &minter{prefix: "conformance-" + hex.EncodeToString(b)}
}

func (m *minter) id(kind string) string {
	return fmt.Sprintf("%s-%s-%d", m.prefix, kind, m.n.Add(1))
}

func (m *minter) email() string {
	return fmt.Sprintf("%s-%d@%s", m.prefix, m.n.Add(1), RPID)
}
