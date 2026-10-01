// Package sessiontest is the conformance suite for auth.Sessions: every
// implementation runs it to prove it honours the Sessions doc comment.
//
//	func TestConformance(t *testing.T) {
//		sessiontest.Run(t, sessiontest.Harness{
//			Open:       func(t *testing.T, now func() time.Time) auth.Sessions { return mySessions(t, now) },
//			RefreshTTL: myRefreshTTL,
//		})
//	}
//
// It asserts only what that comment promises: each case quotes its clause, and
// TestEveryPromiseIsQuoted fails on a quote go/auth does not contain. Where it is
// silent, the case is omitted and marked where it would sit.
//
// Time is the suite's: it starts at the wall clock and only moves forward, so
// expiry is tested without sleeping.
//
// Not covered, as not provokable through the interface: a backend failure.
package sessiontest

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/metacensus/api/go/auth"
)

type Harness struct {
	// Open returns the Sessions one case runs against, which must read the time
	// from now. It is called once per case, with that case's t; every call in a
	// Run gets the same now, so it may return a fresh Sessions or a shared one.
	Open func(t *testing.T, now func() time.Time) auth.Sessions

	// RefreshTTL is the refresh lifetime the Sessions Open returns is
	// configured with.
	RefreshTTL time.Duration
}

func (h Harness) validate() error {
	if h.Open == nil {
		return errors.New("sessiontest: Harness.Open is nil")
	}
	if h.RefreshTTL < time.Second {
		return fmt.Errorf("sessiontest: Harness.RefreshTTL is %v; declare the backend's refresh lifetime, at least 1s", h.RefreshTTL)
	}
	return nil
}

// Run fails t on any case whose promise h's Sessions breaks.
func Run(t *testing.T, h Harness) {
	t.Helper()
	if err := h.validate(); err != nil {
		t.Fatal(err)
	}
	// Whole seconds, so a backend storing coarser timestamps still lands every
	// instant a case steps to exactly.
	now := time.Now().UTC().Truncate(time.Second)
	for _, method := range sessionsMethods() {
		t.Run(method, func(t *testing.T) {
			for _, c := range suite[method] {
				t.Run(c.name, func(t *testing.T) {
					c.run(&scene{
						t:          t,
						s:          h.Open(t, func() time.Time { return now }),
						now:        &now,
						refreshTTL: h.RefreshTTL,
						promise:    c.promise,
					})
				})
			}
		})
	}
}

var suite = map[string][]sessionsCase{
	"Issue":   issueCases,
	"Resolve": resolveCases,
	"Refresh": refreshCases,
	"Revoke":  revokeCases,
}

func sessionsMethods() []string {
	iface := reflect.TypeFor[auth.Sessions]()
	out := make([]string, iface.NumMethod())
	for i := range out {
		out[i] = iface.Method(i).Name
	}
	return out
}
