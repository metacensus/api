package server

import (
	"errors"
	"net/http"

	contract "github.com/metacensus/api/go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// respond writes resp as the contract's JSON, or err through the error model.
func (rt *Runtime) respond(w http.ResponseWriter, resp proto.Message, err error) {
	if err != nil {
		rt.writeError(w, err)
		return
	}
	if resp == nil || !resp.ProtoReflect().IsValid() {
		rt.writeError(w, errors.New("handler returned neither a response nor an error"))
		return
	}
	body, err := contract.Marshal(resp)
	if err != nil {
		rt.writeError(w, &Error{Status: http.StatusInternalServerError, Code: "encode_failed",
			Message: "could not encode response", Err: err})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// writeError writes err as {"error": message, "code": code}. An error that is
// not an *Error becomes a fixed-message 500 — its text stays server-side.
// The envelope is a google.protobuf.Struct, encoded via the contract's
// MarshalOptions, to keep encoding/json out of this module.
func (rt *Runtime) writeError(w http.ResponseWriter, err error) {
	var e *Error
	// A typed-nil *Error in a non-nil error interface reaches here as ok-and-nil.
	if !errors.As(err, &e) || e == nil {
		e = &Error{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error", Err: err}
	}
	status := e.status()
	s, serr := structpb.NewStruct(map[string]any{"error": e.Message, "code": e.Code})
	if serr != nil {
		http.Error(w, e.Message, status)
		return
	}
	body, serr := contract.Marshal(s)
	if serr != nil {
		http.Error(w, e.Message, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
