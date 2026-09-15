// The hand-written half of package server. See doc.go for what this package
// owns, what it refuses, and what it leaves to the router.
package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	contract "github.com/metacensus/api/go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
)

// DefaultMaxBodyBytes caps a request body when Runtime.MaxBodyBytes is zero.
const DefaultMaxBodyBytes int64 = 1 << 20

// Mux is the registration surface a generated Register<Service> function
// needs: bind one HTTP method and pattern to a handler. Both a bare stdlib
// mux and chi already have this method under a compatible name and signature,
// so this package picks no router and imports none.
type Mux interface {
	Method(method, pattern string, handler http.Handler)
}

// StdMux adapts a *http.ServeMux to Mux using the "METHOD pattern" syntax
// net/http's own mux has accepted since Go 1.22. It is the only place this
// package knows that syntax exists; a Mux implementation for a different
// router does not need to.
type StdMux struct {
	*http.ServeMux
}

func (m StdMux) Method(method, pattern string, handler http.Handler) {
	m.ServeMux.Handle(method+" "+pattern, handler)
}

// Runtime is what every generated handler runs against. The zero value is
// usable: no prefix, DefaultMaxBodyBytes, StdPathValue, no body verification.
type Runtime struct {
	// Prefix is the path every route hangs off, and "" means exactly that:
	// no prefix. That is what a router already mounted at the contract's
	// prefix needs — chi's Route, http.StripPrefix — and it has to be
	// expressible, so it is the zero value rather than a sentinel. A router
	// mounted at the origin root wants routes.Prefix.
	Prefix string

	// MaxBodyBytes bounds a request body; a larger one is a 413. Zero means
	// DefaultMaxBodyBytes.
	MaxBodyBytes int64

	// PathValue reads one path parameter out of a matched request. It is the
	// one thing Mux cannot carry: registration is identical across routers
	// and extraction is not. Zero means StdPathValue; a chi.Router needs
	// EscapedPathValue.
	PathValue PathValueFunc

	// VerifyBody, when set, sees the raw request body — the octets as
	// received, before they are decoded — for every route that carries a
	// body. This is where an X-Signature check belongs: protojson output is
	// not byte-stable, so a signature over the body can only be checked
	// against the bytes that arrived, never against a re-encoding. Return an
	// *Error to choose the status; any other error is a 401.
	VerifyBody func(r *http.Request, raw []byte) error
}

// Error is what a handler returns to choose the response status. Message is
// what the client sees; Err, if set, is for the server's own logs and never
// crosses the wire.
type Error struct {
	Status  int
	Code    string
	Message string
	Err     error
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

func (rt *Runtime) prefix() string { return rt.Prefix }

// PathValueFunc reads the path parameter named name out of a request the
// router has already matched. An error is a 400.
type PathValueFunc func(r *http.Request, name string) (string, error)

// StdPathValue returns r.PathValue verbatim. net/http's ServeMux percent-
// decodes a segment before storing it, so there is nothing left to do. This
// is Runtime's default.
func StdPathValue(r *http.Request, name string) (string, error) {
	return r.PathValue(name), nil
}

// EscapedPathValue percent-decodes r.PathValue. chi v5 stores the raw segment,
// so a chi.Router needs this:
//
//	server.Runtime{PathValue: server.EscapedPathValue}
//
// Choosing wrong is silent for an id that contains nothing worth escaping,
// which is why internal/chitest asserts both halves against real chi rather
// than leaving this to a comment.
//
// The error is unreachable behind net/http, which rejects a request URI with
// a bad escape before routing; it exists so that a router feeding this
// something else cannot make a mangled id look like a valid one.
func EscapedPathValue(r *http.Request, name string) (string, error) {
	raw := r.PathValue(name)
	v, err := url.PathUnescape(raw)
	if err != nil {
		return "", &Error{Status: http.StatusBadRequest, Code: "path_param_invalid",
			Message: fmt.Sprintf("path parameter %q is not valid percent-encoding", name), Err: err}
	}
	return v, nil
}

func (rt *Runtime) pathValue() PathValueFunc {
	if rt.PathValue != nil {
		return rt.PathValue
	}
	return StdPathValue
}

func (rt *Runtime) maxBody() int64 {
	if rt.MaxBodyBytes > 0 {
		return rt.MaxBodyBytes
	}
	return DefaultMaxBodyBytes
}

// readBody reads the whole body under the cap. It is the only place the body
// is read, so what it returns is what VerifyBody sees and what decodeBody
// parses: one set of octets for both.
func (rt *Runtime) readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, rt.maxBody()))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, Errorf(http.StatusRequestEntityTooLarge, "body_too_large",
				"request body exceeds %d bytes", tooLarge.Limit)
		}
		return nil, &Error{Status: http.StatusBadRequest, Code: "body_unreadable",
			Message: "could not read request body", Err: err}
	}
	return raw, nil
}

func (rt *Runtime) verifyBody(r *http.Request, raw []byte) error {
	if rt.VerifyBody == nil {
		return nil
	}
	if err := rt.VerifyBody(r, raw); err != nil {
		var e *Error
		if errors.As(err, &e) {
			return e
		}
		return &Error{Status: http.StatusUnauthorized, Code: "body_verification_failed",
			Message: "request body failed verification", Err: err}
	}
	return nil
}

// decodeBody parses the body with the contract's UnmarshalOptions: unknown
// fields are an error, which surfaces here as a 400.
//
// This is the one place a decoder's own text crosses the wire — everywhere
// else an error that is not an *Error becomes an opaque 500. A parse failure
// is the caller's to fix and unreadable without the offending token, so the
// exception is deliberate; it does echo caller-supplied field names back.
func (rt *Runtime) decodeBody(raw []byte, m proto.Message) error {
	if err := contract.UnmarshalOptions.Unmarshal(raw, m); err != nil {
		return &Error{Status: http.StatusBadRequest, Code: "body_invalid",
			Message: "request body is not valid " + string(m.ProtoReflect().Descriptor().Name()) + ": " + err.Error(),
			Err:     err}
	}
	return nil
}

// pathParam returns the path parameter named name. fromBody is the value the
// body decode left in the same field, if any: a body may repeat a path-bound
// field, since the request message models the whole request, but it may not
// disagree with the path.
func (rt *Runtime) pathParam(r *http.Request, name, fromBody string) (string, error) {
	v, err := rt.pathValue()(r, name)
	if err != nil {
		return "", err
	}
	if v == "" {
		// ServeMux never routes here without the segment; this guards a
		// pattern registered by hand.
		return "", Errorf(http.StatusBadRequest, "path_param_missing", "path parameter %q is missing", name)
	}
	if fromBody != "" && fromBody != v {
		return "", Errorf(http.StatusBadRequest, "path_body_conflict",
			"%q is %q in the path and %q in the body", name, v, fromBody)
	}
	return v, nil
}

// bindQuery sets the named fields of m from the query string. Only names in
// allowed are accepted, each at most once; anything else is a 400, matching
// the body's rejection of unknown fields. Values are parsed by the field's
// kind: strings verbatim, bools as strconv.ParseBool, numbers in base 10,
// enums by value name.
//
// Every route calls this, including the ones declaring no query field at all,
// where allowed is nil and the whole query string is therefore a 400. A route
// that quietly ignored ?page=2 would defeat TestNoPaginationFields at the one
// layer a caller can observe.
func (rt *Runtime) bindQuery(r *http.Request, m proto.Message, allowed []string) error {
	msg := m.ProtoReflect()
	fields := msg.Descriptor().Fields()
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	for key, values := range r.URL.Query() {
		if !ok[key] {
			return Errorf(http.StatusBadRequest, "query_unknown", "unknown query parameter %q", key)
		}
		fd := fields.ByJSONName(key)
		if fd == nil {
			return Errorf(http.StatusBadRequest, "query_unknown", "unknown query parameter %q", key)
		}
		if fd.IsList() {
			list := msg.Mutable(fd).List()
			for _, s := range values {
				v, err := parseScalar(fd, s)
				if err != nil {
					return Errorf(http.StatusBadRequest, "query_invalid", "query parameter %q: %v", key, err)
				}
				list.Append(v)
			}
			continue
		}
		if len(values) != 1 {
			return Errorf(http.StatusBadRequest, "query_repeated", "query parameter %q given %d times", key, len(values))
		}
		v, err := parseScalar(fd, values[0])
		if err != nil {
			return Errorf(http.StatusBadRequest, "query_invalid", "query parameter %q: %v", key, err)
		}
		msg.Set(fd, v)
	}
	return nil
}

func parseScalar(fd protoreflect.FieldDescriptor, s string) (protoreflect.Value, error) {
	switch fd.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(s), nil
	case protoreflect.BoolKind:
		b, err := strconv.ParseBool(s)
		return protoreflect.ValueOfBool(b), err
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := strconv.ParseInt(s, 10, 32)
		return protoreflect.ValueOfInt32(int32(n)), err
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := strconv.ParseInt(s, 10, 64)
		return protoreflect.ValueOfInt64(n), err
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := strconv.ParseUint(s, 10, 32)
		return protoreflect.ValueOfUint32(uint32(n)), err
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := strconv.ParseUint(s, 10, 64)
		return protoreflect.ValueOfUint64(n), err
	case protoreflect.FloatKind:
		f, err := strconv.ParseFloat(s, 32)
		return protoreflect.ValueOfFloat32(float32(f)), err
	case protoreflect.DoubleKind:
		f, err := strconv.ParseFloat(s, 64)
		return protoreflect.ValueOfFloat64(f), err
	case protoreflect.EnumKind:
		ev := fd.Enum().Values().ByName(protoreflect.Name(s))
		if ev == nil {
			return protoreflect.Value{}, fmt.Errorf("%q is not a value of %s", s, fd.Enum().FullName())
		}
		return protoreflect.ValueOfEnum(ev.Number()), nil
	default:
		return protoreflect.Value{}, fmt.Errorf("%s cannot travel in a query string", fd.Kind())
	}
}

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
// not an *Error is a 500 with a fixed message: its text is the server's, not
// the client's. The envelope is a google.protobuf.Struct encoded with the
// contract's MarshalOptions, so encoding/json stays out of the module; the
// contract itself defines no error message.
func (rt *Runtime) writeError(w http.ResponseWriter, err error) {
	e, ok := err.(*Error)
	if !ok && !errors.As(err, &e) {
		e = &Error{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error", Err: err}
	}
	s, serr := structpb.NewStruct(map[string]any{"error": e.Message, "code": e.Code})
	if serr != nil {
		http.Error(w, e.Message, e.Status)
		return
	}
	body, serr := contract.Marshal(s)
	if serr != nil {
		http.Error(w, e.Message, e.Status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	_, _ = w.Write(body)
}
