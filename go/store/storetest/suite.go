// Package storetest is the conformance suite for store.Store.
//
// Two implementations of one interface drift behaviourally while agreeing
// structurally, and a suite every implementer runs is the only instrument that
// catches it. A compile-time assertion — var _ store.Store = (*x)(nil) — makes
// shape divergence impossible; this makes behavioural divergence detectable.
// They are different guarantees and neither substitutes for the other.
//
// Run it from the backend's own test package:
//
//	func TestConformance(t *testing.T) {
//		storetest.Run(t, storetest.Config{
//			New: func(t *testing.T) store.Store { return newEmptyBackend(t) },
//		})
//	}
//
// README.md next to this file states what the suite catches and, at greater
// length, what it does not.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacensus/api/go/store"
)

// Config is what an implementation supplies to run the suite.
type Config struct {
	// New returns an empty Store. It is called at least once per case, and
	// twice in the determinism case, so it must return genuinely independent
	// state each time — a shared package-level map fails the suite in ways
	// that look like a conformance bug.
	//
	// Cleanup belongs on t via t.Cleanup.
	New func(t *testing.T) store.Store

	// NewID mints an identifier of the given kind that this implementation
	// accepts. Optional: the default is a plain counter, which is fine for a
	// backend that treats ids as opaque. A backend that validates id shape —
	// infra requires a "prop:<uuid>" form — supplies its own.
	//
	// Minting ids in the suite rather than asking the store for them is the
	// point: the suite stands above the seam, which is the only place ids are
	// allowed to come from.
	NewID func(kind store.Kind) store.ID

	// Contend forces a concurrent-modification failure and returns the error
	// the store produced. Optional.
	//
	// It exists because store.CodeConflict is the one genuinely asymmetric
	// code — Fabric raises it structurally at MVCC validation, Postgres at
	// READ COMMITTED never does — and a code reachable from one backend and
	// not the other is the worst kind of failure mode, because callers write
	// handling that is untested everywhere it does not occur. Supplying this
	// is how an implementation claims the code is reachable.
	//
	// When it is nil the case skips, and the skip says what went unchecked.
	Contend func(t *testing.T, s store.Store) error
}

// Case is one conformance check.
type Case struct {
	// Name is the subtest name.
	Name string

	// Why is what a failure means, printed on skip and available to anyone
	// listing the suite.
	Why string

	// Fn runs the check.
	Fn func(t *testing.T, h *H)
}

// Run executes every case against a fresh store from cfg.New.
func Run(t *testing.T, cfg Config) {
	t.Helper()
	if cfg.New == nil {
		t.Fatal("storetest: Config.New is required")
	}
	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) {
			// A conformance failure is read by someone who did not write the
			// case, so print what it is for before the assertion's own message
			// scrolls past.
			t.Cleanup(func() {
				if t.Failed() && c.Why != "" {
					t.Logf("why this case exists: %s", c.Why)
				}
			})
			h := newH(t, cfg)
			c.Fn(t, h)
		})
	}
}

// H is the per-case harness: a store, a context, deterministic ids and clocks,
// and the assertions the cases share.
//
// Every value H mints is minted here, above the seam, which is the arrangement
// the interface requires of a real caller too.
type H struct {
	T   *testing.T
	S   store.Store
	Ctx context.Context

	cfg Config
	n   atomic.Int64
}

func newH(t *testing.T, cfg Config) *H {
	t.Helper()
	h := &H{T: t, cfg: cfg, Ctx: context.Background()}
	h.S = cfg.New(t)
	if h.S == nil {
		t.Fatal("storetest: Config.New returned nil")
	}
	return h
}

// New returns another independent store from the same Config.
func (h *H) New() store.Store {
	h.T.Helper()
	s := h.cfg.New(h.T)
	if s == nil {
		h.T.Fatal("storetest: Config.New returned nil")
	}
	return s
}

// base is the suite's clock. There is no time.Now anywhere in this package:
// every timestamp the suite sends is a value it chose, because every timestamp
// a real caller sends is too.
var base = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

// At returns a distinct, fixed timestamp.
func (h *H) At(i int) time.Time { return base.Add(time.Duration(i) * time.Minute) }

// ID mints an identifier the implementation accepts.
func (h *H) ID(kind store.Kind) store.ID {
	n := h.n.Add(1)
	if h.cfg.NewID != nil {
		id := h.cfg.NewID(kind)
		if id == "" {
			h.T.Fatal("storetest: Config.NewID returned an empty id")
		}
		return id
	}
	return store.ID(fmt.Sprintf("%s-%03d", kind, n))
}

// Digest returns a distinct fixed digest. The suite never computes a real one:
// what the digest is over is open decision 4, and the interface's requirement
// is only that an implementation stores what it is handed.
func (h *H) Digest(seed byte) store.Digest {
	var d store.Digest
	for i := range d {
		d[i] = seed + byte(i)
	}
	return d
}

// Sig returns a distinct fixed signature.
func (h *H) Sig(keyID, orgID string) *store.Signature {
	return &store.Signature{
		PersonalKeyID: keyID,
		OrgKeyID:      orgID,
		Alg:           "ES256",
		Bytes:         []byte(keyID + "/" + orgID + "/signature"),
	}
}

// --- assertions -----------------------------------------------------------

// OK fails the test if err is non-nil.
func (h *H) OK(err error, what string) {
	h.T.Helper()
	if err != nil {
		h.T.Fatalf("%s: unexpected error: %v", what, err)
	}
}

// WantCode fails the test unless err is a *store.Error carrying want.
//
// It rejects a bare error as firmly as a wrong code: an implementation that
// returns a driver error unwrapped forces the shared layer to string-match it,
// which is the thing the taxonomy exists to prevent.
func (h *H) WantCode(err error, want store.Code, what string) {
	h.T.Helper()
	if err == nil {
		h.T.Fatalf("%s: want %v, got no error", what, want)
	}
	got := store.CodeOf(err)
	if got == store.CodeUnspecified {
		h.T.Fatalf("%s: error is not a *store.Error, so the shared layer would have to "+
			"string-match it: %v", what, err)
	}
	if got != want {
		h.T.Fatalf("%s: want %v, got %v (%v)", what, want, got, err)
	}
	h.wantOp(err, what)
}

// AnyCode fails unless err is a *store.Error with a code from the vocabulary.
func (h *H) AnyCode(err error, what string) store.Code {
	h.T.Helper()
	if err == nil {
		h.T.Fatalf("%s: want an error, got none", what)
	}
	got := store.CodeOf(err)
	if got == store.CodeUnspecified {
		h.T.Fatalf("%s: error is not a *store.Error: %v", what, err)
	}
	h.wantOp(err, what)
	return got
}

func (h *H) wantOp(err error, what string) {
	h.T.Helper()
	var e *store.Error
	if !errors.As(err, &e) {
		return
	}
	if e.Op == "" {
		h.T.Errorf("%s: *store.Error has an empty Op, so a log line cannot say what failed: %v", what, err)
	}
}

// --- fixtures -------------------------------------------------------------

// User creates a user and returns the Caller that acts as them.
//
// UserCreate is the one method whose caller is the user being created: signup
// happens before there is a session. The suite passes Caller.UserID equal to
// the request's UserID everywhere, which is what the interface requires.
func (h *H) User(email string) (store.Caller, *store.User) {
	h.T.Helper()
	id := h.ID(store.KindUser)
	c := store.Caller{UserID: id}
	u, err := h.S.UserCreate(h.Ctx, c, &store.UserCreateRequest{
		UserID:       id,
		Created:      h.At(1),
		PasswordHash: []byte("$2a$08$notarealhashbutthecorrectshape"),
		Name:         "Name " + string(id),
		Email:        email,
		Country:      "US",
		Digest:       h.Digest(1),
	})
	h.OK(err, "UserCreate")
	if u == nil {
		h.T.Fatal("UserCreate returned a nil user and a nil error")
	}
	return c, u
}

// Topic creates a topic.
func (h *H) Topic(c store.Caller) *store.Topic {
	h.T.Helper()
	id := h.ID(store.KindTopic)
	tp, err := h.S.TopicCreate(h.Ctx, c, &store.TopicCreateRequest{
		TopicID:     id,
		Created:     h.At(2),
		Name:        "Topic " + string(id),
		Description: "described",
		Digest:      h.Digest(2),
	})
	h.OK(err, "TopicCreate")
	if tp == nil {
		h.T.Fatal("TopicCreate returned a nil topic and a nil error")
	}
	return tp
}

// Prop creates a proposition in a topic.
func (h *H) Prop(c store.Caller, topic store.ID) *store.Prop {
	h.T.Helper()
	id := h.ID(store.KindProp)
	p, err := h.S.PropCreate(h.Ctx, c, &store.PropCreateRequest{
		TopicID:     topic,
		PropID:      id,
		Created:     h.At(3),
		Type:        store.PropTypeStatement,
		Description: "a proposition",
		Digest:      h.Digest(3),
	})
	h.OK(err, "PropCreate")
	if p == nil {
		h.T.Fatal("PropCreate returned a nil prop and a nil error")
	}
	return p
}
