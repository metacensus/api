package server

import (
	"fmt"
	"net/http"
)

// Error is what a handler returns to choose the response status. Message is
// what the client sees; Err, if set, is for the server's own logs and never
// crosses the wire.
//
// Status is the one field a composite literal can leave out and still
// compile, so anything outside 100..599 is written as a 500: a handler that
// mis-fills an error should answer badly, not take the connection down.
type Error struct {
	Status  int
	Code    string
	Message string
	Err     error
}

func (e *Error) status() int {
	if e.Status < 100 || e.Status > 599 {
		return http.StatusInternalServerError
	}
	return e.Status
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%d %s: %s: %v", e.Status, e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// Errorf builds an *Error with a formatted message.
func Errorf(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func errNotImplemented(rpc string) error {
	return Errorf(http.StatusNotImplemented, "unimplemented", "%s is not implemented", rpc)
}
