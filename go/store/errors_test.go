package store_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/metacensus/api/go/store"
)

// A bare Kind is an error, and a wrapped one still answers errors.Is for both
// its Kind and its cause — this is what lets the server switch on KindOf while
// the logs keep the backend reason.
func TestErrorCarriesKindAndCause(t *testing.T) {
	cause := errors.New("pq: duplicate key value violates unique constraint")
	err := store.Errf(store.AlreadyExists, "EnrollUser", cause)

	if !errors.Is(err, store.AlreadyExists) {
		t.Errorf("errors.Is(err, AlreadyExists) = false; want true")
	}
	if errors.Is(err, store.NotFound) {
		t.Errorf("errors.Is(err, NotFound) = true; want false")
	}
	if !errors.Is(err, cause) {
		t.Errorf("the backend cause did not survive wrapping")
	}
	if got := store.KindOf(err); got != store.AlreadyExists {
		t.Errorf("KindOf = %q; want %q", got, store.AlreadyExists)
	}
}

// A backend may also return a bare Kind, and KindOf must read it.
func TestKindOfBareKind(t *testing.T) {
	if got := store.KindOf(store.NotFound); got != store.NotFound {
		t.Errorf("KindOf(NotFound) = %q; want %q", got, store.NotFound)
	}
	if got := store.KindOf(fmt.Errorf("wrapped: %w", store.Unavailable)); got != store.Unavailable {
		t.Errorf("KindOf(wrapped Unavailable) = %q; want %q", got, store.Unavailable)
	}
}

// An error with no Kind reports the empty Kind, the server's signal to answer
// 500 rather than guess.
func TestKindOfUnclassified(t *testing.T) {
	if got := store.KindOf(errors.New("something else")); got != "" {
		t.Errorf("KindOf(plain error) = %q; want \"\"", got)
	}
	if got := store.KindOf(nil); got != "" {
		t.Errorf("KindOf(nil) = %q; want \"\"", got)
	}
}

// Compile-time proof the interface is nameable and implementable; the server PR
// supplies real backends. A struct with no methods would not satisfy it, so
// this also fails loudly if a method signature is mistyped against a stub.
var _ store.Store = (*stubStore)(nil)

type stubStore struct{ store.Store }
