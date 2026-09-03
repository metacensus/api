package server

import (
	"errors"
	"fmt"
	"net/http"

	contract "github.com/metacensus/api/go"
	v1 "github.com/metacensus/api/go/metacensus/v1"
)

// Error is a failure an implementation has chosen to expose.
//
// Exposure is opt-in, and that is the point: a handler returning any other
// error produces 500 with a fixed body, so a message from a datastore driver
// cannot reach a caller by default. Wrapping an error in this type is the
// deliberate act of saying it is safe to show.
type Error struct {
	// Status is the HTTP status. The contract does not re-spell the status
	// vocabulary — these are net/http's constants.
	Status int

	// Code identifies this specific failure within its status, as a stable
	// PascalCase string. It is the part a client can branch on.
	Code string

	// Message is human-readable detail. It is sent to the client, so it holds
	// nothing a caller should not see.
	Message string

	// Err is the underlying cause. It is passed to the logger and never
	// serialised.
	Err error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// Errorf builds an Error whose Message is formatted from format and a. The
// message reaches the client, so format nothing into it that should not.
func Errorf(status int, code, format string, a ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, a...)}
}

// Wrap builds an Error carrying cause, which is logged rather than sent.
func Wrap(status int, code, message string, cause error) *Error {
	return &Error{Status: status, Code: code, Message: message, Err: cause}
}

// ErrorHandler writes the response for an error returned by a handler.
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

// internalCode is the code for anything the implementation did not choose to
// expose. It is deliberately the only code this package invents.
const internalCode = "Internal"

// NewErrorHandler returns the ErrorHandler generated registration uses.
//
// An *Error is written with its own status, code and message. Anything else
// becomes 500 with code Internal and a fixed message, because an unclassified
// error is one nobody decided was safe to show. Both are passed to log, which
// may be nil.
func NewErrorHandler(log func(*http.Request, error)) ErrorHandler {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if log != nil {
			log(r, err)
		}

		status, body := http.StatusInternalServerError, &v1.Error{
			Code:  internalCode,
			Error: "internal error",
		}

		var e *Error
		if errors.As(err, &e) {
			status = e.Status
			body = &v1.Error{Code: e.Code, Error: e.Message}
		}

		b, marshalErr := contract.Marshal(body)
		if marshalErr != nil {
			// The body is two strings this package built, so this is
			// unreachable short of a protojson bug. Falling back to a bare
			// status beats writing a half-encoded body.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(b)
	}
}
