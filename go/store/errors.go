package store

import (
	"errors"
	"fmt"
)

// Kind is the closed vocabulary a Store speaks, independent of any backend's
// own error strings. It exists so the server maps a Kind to an HTTP status
// (that mapping is the server PR's) rather than string-matching a driver
// message — infra today returns HTTP 500 with the raw backend string for every
// application error, which this replaces.
//
// A Kind both names the failure and is itself an error, so a backend can return
// a bare Kind for the common case and callers can test with errors.Is:
//
//	if errors.Is(err, store.NotFound) { ... }
//
// Wrap a Kind with Errf when the failure carries context worth logging; the
// wrapped value still satisfies errors.Is(err, kind).
type Kind string

const (
	// NotFound: no record with the given id (or tuple) exists.
	NotFound Kind = "not_found"

	// AlreadyExists: a uniqueness rule the store owns is violated — an email
	// already enrolled, or a server-minted id that collides with a stored one.
	AlreadyExists Kind = "already_exists"

	// InvalidContent: the request is self-inconsistent or unsatisfiable against
	// state in a way no retry fixes — a required id absent from signed content,
	// a signed id disagreeing with its addressing tuple, or a parent (topic for
	// a prop, prop for a vote) that does not exist. Distinct from
	// SignatureInvalid: the bytes may be perfectly signed and still name a
	// parent that isn't there.
	InvalidContent Kind = "invalid_content"

	// SignatureInvalid: the user (or institutional) signature does not stand —
	// the digest fails, the key_id resolves to no enrolled key, or an inline
	// key's thumbprint mismatches its key_id. Raised where signatures are
	// verified hard (inside the Fabric boundary); the Postgres backend verifies
	// softly and so raises this only for malformed input, not cryptographic
	// failure. See README.md, "The signing chain".
	SignatureInvalid Kind = "signature_invalid"

	// Unauthenticated: the caller could not be established, or the caller
	// established by the session is not the author named by the signature. A
	// login token says who is connected; only the signature says who authored
	// the record, and a write whose two disagree is refused here.
	Unauthenticated Kind = "unauthenticated"

	// Unavailable: the backend could not answer and the same request might
	// succeed later — a connection lost or timed out, or a Fabric MVCC read
	// conflict. It is the one Kind whose meaning is "retry may work"; see the
	// note on MVCC in doc.go for why a conflict lands here rather than in a
	// vocabulary of its own this pass.
	Unavailable Kind = "unavailable"
)

func (k Kind) Error() string { return string(k) }

// Error carries a Kind together with the operation and cause, for logs. The
// Kind is what the server switches on; Op and Err are never shown to a client.
type Error struct {
	Kind Kind
	Op   string // the Store method, e.g. "CreateProp"
	Err  error  // the backend cause, for logs only; may be nil
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Kind, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Kind)
}

// Unwrap exposes both the Kind and the cause, so errors.Is(err, store.NotFound)
// and errors.Is(err, someDriverErr) both hold.
func (e *Error) Unwrap() []error {
	if e.Err == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Err}
}

// Errf builds an *Error of the given Kind. cause may be nil.
func Errf(kind Kind, op string, cause error) *Error {
	return &Error{Kind: kind, Op: op, Err: cause}
}

// KindOf reports the Kind an error carries, or "" if it carries none — the
// server's single point of translation to a status. A bare Kind, a wrapped
// one, and an *Error all satisfy this, since each holds a Kind errors.As
// reaches.
func KindOf(err error) Kind {
	var k Kind
	if errors.As(err, &k) {
		return k
	}
	return ""
}
