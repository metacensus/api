package store

import (
	"errors"
	"fmt"
)

// Code is the error vocabulary both backends must speak.
//
// It exists so the shared layer can map a failure to an HTTP status without
// string-matching a driver message. Today metacensus/infra turns every
// application error into a 500 carrying the raw backend string
// (core/api/server/server.go, writeError), so a not-found, a validation
// failure and an unreachable peer are indistinguishable to the SPA.
//
// The mapping to HTTP is the shared layer's, not an implementation's: nothing
// below this seam knows what a status code is. The comments below record the
// intended mapping so the two do not drift.
type Code int

const (
	// CodeUnspecified is never returned by a conforming implementation. A zero
	// Code means someone built an Error without one.
	CodeUnspecified Code = iota

	// CodeInvalid — 400. Malformed, missing or out of range. The
	// authoritative well-formedness check; the shared layer's identical check
	// above the seam is a courtesy that produces a better message.
	CodeInvalid

	// CodeUnauthenticated — 401. No valid caller. An implementation returns
	// this for a zero Caller.UserID rather than writing an unattributed record.
	CodeUnauthenticated

	// CodeForbidden — 403. Authenticated, and not permitted by a rule that
	// reads persisted state: topic membership, a credit balance, a
	// key-to-user binding. These checks live below the seam because
	// endorsement is what makes them non-bypassable.
	CodeForbidden

	// CodeNotFound — 404.
	CodeNotFound

	// CodeAlreadyExists — 409. This id or natural key is taken. Distinct from
	// CodeConflict: retrying will not help.
	CodeAlreadyExists

	// CodeConflict — 409. Concurrent modification. The operation did not
	// commit, and retrying the identical request is safe.
	//
	// This is the one genuinely asymmetric code, and the reason it is in the
	// vocabulary rather than hidden. Fabric produces it structurally: two
	// transactions in one block where the second reads a key the first wrote
	// are invalidated at validation time as an MVCC read conflict. Postgres at
	// READ COMMITTED doing an upsert produces it never — the last writer
	// simply wins.
	//
	// Hiding it would mean callers write handling that is exercised on one
	// backend and untested on the other, which is the asymmetric-leak failure
	// exactly. Two mitigations, in order: every write in this interface is an
	// upsert on a natural key or a create with a caller-supplied id, so a
	// bounded retry below the seam is safe and absorbs most conflicts; and the
	// Postgres backend should run at an isolation level that can actually
	// raise a serialization failure, so the code is reachable from both sides.
	CodeConflict

	// CodeUnavailable — 503. The store could not be reached. Distinguishable
	// from CodeInternal because it is retryable and is not a bug.
	CodeUnavailable

	// CodeDeadlineExceeded — 504. The context's deadline passed, or it was
	// cancelled. Every method takes a context precisely so this is reachable:
	// infra's DataSource takes none (core/api/server/server.go:69-94) and
	// Server.Run discards the one it is handed, so today a slow request cannot
	// be cancelled, only globally capped at gateway construction
	// (core/api/fabric/client/config.go:33-38,64-67).
	CodeDeadlineExceeded

	// CodeInternal — 500. A bug or an unclassified failure, and the only code
	// that maps to 500. An implementation returning this for a condition it
	// could have named has moved a caller's problem into a log line.
	CodeInternal
)

// String renders a Code for logs and test failures.
func (c Code) String() string {
	switch c {
	case CodeInvalid:
		return "Invalid"
	case CodeUnauthenticated:
		return "Unauthenticated"
	case CodeForbidden:
		return "Forbidden"
	case CodeNotFound:
		return "NotFound"
	case CodeAlreadyExists:
		return "AlreadyExists"
	case CodeConflict:
		return "Conflict"
	case CodeUnavailable:
		return "Unavailable"
	case CodeDeadlineExceeded:
		return "DeadlineExceeded"
	case CodeInternal:
		return "Internal"
	default:
		return fmt.Sprintf("Code(%d)", int(c))
	}
}

// Codes is every code a conforming implementation may return. The conformance
// suite uses it to reject anything outside the vocabulary.
func Codes() []Code {
	return []Code{
		CodeInvalid, CodeUnauthenticated, CodeForbidden, CodeNotFound,
		CodeAlreadyExists, CodeConflict, CodeUnavailable, CodeDeadlineExceeded,
		CodeInternal,
	}
}

// Error is the only error type an implementation returns.
//
// Callers match with errors.As, or with CodeOf. No string-matching on driver
// messages, in either direction: a backend translates SQLSTATE 23505 or a
// chaincode AssertExistsNot failure into CodeAlreadyExists at its own edge, and
// the shared layer never sees either.
type Error struct {
	// Code is required.
	Code Code

	// Op is the interface method that failed, e.g. "ExtractionUpsert".
	Op string

	// Ref is what the failure was about, when there is one.
	Ref Ref

	// Msg is safe to show a caller. It must not carry a driver message, a
	// query, a key layout or a peer address — those go in Err, which is
	// logged and never serialised.
	Msg string

	// Err is the wrapped cause. Logged, never returned to a client.
	Err error
}

func (e *Error) Error() string {
	s := e.Op + ": " + e.Code.String()
	if len(e.Ref.Key) > 0 {
		s += " " + e.Ref.String()
	}
	if e.Msg != "" {
		s += ": " + e.Msg
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

// Errorf builds an *Error with a formatted message.
func Errorf(code Code, op string, ref Ref, format string, args ...any) *Error {
	return &Error{Code: code, Op: op, Ref: ref, Msg: fmt.Sprintf(format, args...)}
}

// Wrap builds an *Error carrying a cause. The cause is for logs; Msg is for
// callers.
func Wrap(err error, code Code, op string, ref Ref, msg string) *Error {
	return &Error{Code: code, Op: op, Ref: ref, Msg: msg, Err: err}
}

// CodeOf reports the Code an error carries, or CodeUnspecified if it is not a
// *Error. A nil error reports CodeUnspecified.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeUnspecified
}

// IsCode reports whether err carries code.
func IsCode(err error, code Code) bool { return CodeOf(err) == code }
