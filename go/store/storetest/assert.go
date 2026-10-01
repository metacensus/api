package storetest

import (
	"context"
	"slices"
	"testing"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
)

// Clauses more than one case or step quotes.
const (
	atomicity      = "One method is one atomic unit of work"
	readsAsWritten = "a read returns the record exactly as it was written"
)

type storeCase struct {
	name    string
	promise string // the clause of store.go/errors.go/doc.go this enforces, verbatim
	hard    bool   // runs only against a Hard store

	given []step             // state built through the interface; each must succeed
	call  func(*scene) error // the one call under test
	want  store.Kind         // "" for success
	then  []step
}

func (c storeCase) title() string {
	if c.want == "" {
		return "success - " + c.name
	}
	return "error - " + string(c.want) + ": " + c.name
}

// A step builds or observes state, failing the case on its own.
type step func(*scene)

type scene struct {
	t   *testing.T
	ctx context.Context
	s   store.Store
	ids *minter
	c   *storeCase

	ada, bob *participant

	// The draft the call under test submits, and what earlier steps stored.
	topic, storedTopic, otherTopic *v1.TopicSigned
	prop, storedProp, secondProp   *v1.PropSigned
	vote, standingVote, otherVote  *v1.VoteSigned

	got            proto.Message
	gotList        []proto.Message
	gotID, gotHash string
}

func (c *storeCase) run(t *testing.T, h Harness, ids *minter) {
	if c.hard && h.Signatures != Hard {
		t.Skipf("runs only against a Hard store; this one declares %v", h.Signatures)
	}
	sc := &scene{t: t, ctx: t.Context(), s: h.Open(t), ids: ids, c: c}
	for _, given := range c.given {
		given(sc)
	}
	assertKind(sc, c.call(sc), c.want)
	for _, then := range c.then {
		then(sc)
	}
}

func (sc *scene) must(err error, what string) {
	sc.t.Helper()
	if err != nil {
		sc.t.Fatalf("setup: %s: %v", what, err)
	}
}

// assertKind holds an error to the Kind a promise names, by identity; "" means
// the call must succeed.
func assertKind(sc *scene, err error, want store.Kind) {
	sc.t.Helper()
	got := store.KindOf(err)
	if want == "" && err == nil || want != "" && got == want {
		return
	}
	if want == "" {
		sc.t.Fatalf("want success, got %v (Kind %q)\n  promise: %s", err, got, sc.c.promise)
	}
	sc.t.Fatalf("want Kind %q, got %v (Kind %q)\n  promise: %s", want, err, got, sc.c.promise)
}

func assertRecord(sc *scene, got, want proto.Message) {
	sc.t.Helper()
	if !proto.Equal(got, want) {
		sc.t.Fatalf("read back a different record\n  got:  %v\n  want: %v\n  promise: %s", got, want, sc.c.promise)
	}
}

// assertOnceIn holds a global list to containing want exactly once, by id; the
// rest of the list is whatever a shared store holds.
func assertOnceIn[M interface {
	proto.Message
	GetId() string
}](sc *scene, list []proto.Message, want M) {
	sc.t.Helper()
	var found []proto.Message
	for _, m := range list {
		if r, ok := m.(M); ok && r.GetId() == want.GetId() {
			found = append(found, m)
		}
	}
	if len(found) != 1 {
		sc.t.Fatalf("want %s listed once, found %d times\n  promise: %s", want.GetId(), len(found), sc.c.promise)
	}
	assertRecord(sc, found[0], want)
}

// assertExactly holds a list the case owns entirely to exactly want, in any
// order: no order is promised.
func assertExactly(sc *scene, list []proto.Message, want ...proto.Message) {
	sc.t.Helper()
	rest := slices.Clone(list)
	for _, w := range want {
		i := slices.IndexFunc(rest, func(m proto.Message) bool { return proto.Equal(m, w) })
		if i < 0 {
			sc.t.Fatalf("want listed, not found: %v\n  in: %v\n  promise: %s", w, list, sc.c.promise)
		}
		rest = slices.Delete(rest, i, i+1)
	}
	if len(rest) > 0 {
		sc.t.Fatalf("listed %d record(s) beyond the %d expected: %v\n  promise: %s", len(rest), len(want), rest, sc.c.promise)
	}
}

func messages[M proto.Message](list []M) []proto.Message {
	out := make([]proto.Message, len(list))
	for i, m := range list {
		out[i] = m
	}
	return out
}
