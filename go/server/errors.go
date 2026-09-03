package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Error is a failure an implementation has chosen to expose.
//
// Exposure is opt-in: a handler returning any other error produces 500 with a
// fixed body, so a message from a datastore driver cannot reach a caller by
// default.
type Error struct {
	// Status is the HTTP status. These are net/http's constants.
	Status int

	// Code identifies this specific failure within its status, as a stable
	// PascalCase string.
	Code string

	// Message is human-readable detail. It is sent to the client.
	Message string

	// Err is the underlying cause. It is logged, never serialised.
	Err error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// Errorf builds an Error whose Message is formatted from format and a.
func Errorf(status int, code, format string, a ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, a...)}
}

// Wrap builds an Error carrying cause, which is logged rather than sent.
func Wrap(status int, code, message string, cause error) *Error {
	return &Error{Status: status, Code: code, Message: message, Err: cause}
}

const internalCode = "Internal"

// errorHandler is given to oapi-codegen for both the request and the response
// error paths.
//
// It encodes with encoding/json rather than protojson, and not by choice: the
// error body has to match what the generated code emits for every successful
// response, which is json.NewEncoder over oapi-codegen's own types, hard-coded
// in server.gen.go. Using protojson here would make the error shape the one
// artifact on this server encoded to the contract's rules.
func errorHandler(log func(*http.Request, error)) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if log != nil {
			log(r, err)
		}

		status, body := http.StatusInternalServerError, errorBody{
			Code:  internalCode,
			Error: "internal error",
		}

		var e *Error
		if errors.As(err, &e) {
			status = e.Status
			body = errorBody{Code: e.Code, Error: e.Message}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

// errorBody is the wire shape of a failure. It is hand-written because the
// contract does not model errors, so no generated type describes it -- on this
// branch there is no v1.Error to reach for, and adding one would be described
// by the OpenAPI document without being used by it.
type errorBody struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}
