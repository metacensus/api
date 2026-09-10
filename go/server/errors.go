package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Error is a failure an implementation has chosen to expose, named in HTTP
// terms rather than gRPC ones.
//
// It exists because grpc-gateway's native error model is gRPC status codes,
// and those are a second status vocabulary mapped onto HTTP lossily by
// runtime.HTTPStatusFromCode: codes.Aborted becomes 409 and
// codes.FailedPrecondition becomes 400, so 412 is unreachable, and there is no
// code that produces 413 at all. An implementation that wants a particular
// HTTP status says so here instead of picking the gRPC code that happens to
// map to it.
type Error struct {
	// Status is the HTTP status. These are net/http's constants.
	Status int

	// Code identifies this specific failure within its status, as a stable
	// PascalCase string. It is the part a client can branch on.
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

// errorHandler replaces grpc-gateway's default, which serialises
// google.rpc.Status as {"code": 5, "message": …, "details": []} — a numeric
// gRPC code, not the contract's Error, and a message it always exposes.
//
// Three cases, and the middle one is the one to keep an eye on:
//
//   - An *Error is written as it stands. Exposure is deliberate.
//   - A gRPC status error is mapped by runtime.HTTPStatusFromCode and its
//     message is exposed, because that is the gRPC convention and because
//     grpc-gateway's own binding failures arrive this way — a malformed body
//     is a codes.InvalidArgument whose message describes the caller's input.
//     It is also the leak: status.Error(codes.Internal, dbErr.Error()) is
//     idiomatic gRPC and puts a datastore's words on the wire.
//   - Anything else becomes 500 with a fixed body.
func errorHandler(log func(*http.Request, error)) runtime.ErrorHandlerFunc {
	return func(
		ctx context.Context,
		mux *runtime.ServeMux,
		marshaler runtime.Marshaler,
		w http.ResponseWriter,
		r *http.Request,
		err error,
	) {
		if log != nil {
			log(r, err)
		}

		httpStatus, body := http.StatusInternalServerError, &v1.Error{
			Code:  internalCode,
			Error: "internal error",
		}

		var e *Error
		switch {
		case errors.As(err, &e):
			httpStatus = e.Status
			body = &v1.Error{Code: e.Code, Error: e.Message}
		default:
			if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
				httpStatus = runtime.HTTPStatusFromCode(st.Code())
				body = &v1.Error{Code: codeName(st.Code()), Error: st.Message()}
			}
		}

		b, marshalErr := marshaler.Marshal(body)
		if marshalErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", marshaler.ContentType(body))
		w.WriteHeader(httpStatus)
		_, _ = w.Write(b)
	}
}

// codeName spells a gRPC code the way the contract spells codes: PascalCase,
// no underscores. codes.Code.String() gives "InvalidArgument" already for most,
// but "OK" and "Unknown" are the ones worth normalising.
func codeName(c codes.Code) string {
	switch c {
	case codes.OK, codes.Unknown:
		return internalCode
	default:
		return c.String()
	}
}
