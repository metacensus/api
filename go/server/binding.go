package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	contract "github.com/metacensus/api/go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// readBody reads the whole body under the cap. It is the only place the body
// is read. Nothing inspects the octets it returns — decodeBody parses them and
// they are then done with, because a participant's signature is checked against
// the decoded message rather than against the bytes; see runtime.go. Request
// Content-Type is not inspected, and a body sent to a route that declares none
// is never read — binding is driven by the route's declaration, not by the
// request.
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
// disagree with the path. Path parameters are bound last, after the body, and
// win.
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

// requireField turns a missing half of a signed request into a 400 naming the
// field, rather than a message that reaches persistence and fails there with
// less to say. Shape only: whether the signature is *good* is never asked here.
func requireField(present bool, jsonName string) error {
	if present {
		return nil
	}
	return Errorf(http.StatusBadRequest, "field_missing",
		"%q is required: this route carries signed content", jsonName)
}

// contentParam fails when a path-bound id and the copy inside the signed
// content disagree.
//
// **It is a better error message, not a control.** Persistence keys the record
// off `content`, the signed copy, so a server that skipped this would write the
// record the signature describes rather than the one the URL asked for — wrong,
// but not forgeable. Do not build anything on it that assumes otherwise.
//
// Path parameters are bound before this runs and win over the body, so
// fromPath is what the URL actually said.
func contentParam(jsonName, fromPath, fromContent string) error {
	if fromPath == fromContent {
		return nil
	}
	return Errorf(http.StatusBadRequest, "path_content_conflict",
		"%q is %q in the path and %q in the signed content", jsonName, fromPath, fromContent)
}

// bindQuery sets the named fields of m from the query string. Only fields in
// allowed are accepted, each at most once; anything else is a 400, matching
// the body's rejection of unknown fields. Values are parsed by the field's
// kind: strings verbatim, bools as strconv.ParseBool, numbers in base 10,
// enums by value name.
//
// A parameter may be spelled either way round — topicId or topic_id — because
// protojson accepts both in a body (TestPostDecodesBodyWithContractOptions
// pins that) and a request message is one message however its fields travel.
// Accepting one spelling in the body and rejecting it in the query string
// would make the wire format depend on which half of the request a field
// happened to land in.
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
	seen := map[protoreflect.FieldNumber]string{}
	for key, values := range r.URL.Query() {
		fd := fields.ByJSONName(key)
		if fd == nil {
			fd = fields.ByTextName(key)
		}
		if fd == nil || !ok[fd.JSONName()] {
			return Errorf(http.StatusBadRequest, "query_unknown", "unknown query parameter %q", key)
		}
		// Two spellings of one field would otherwise both bind, and the
		// winner would be map iteration order.
		if prev, dup := seen[fd.Number()]; dup {
			return Errorf(http.StatusBadRequest, "query_repeated",
				"query parameter %q is the same field as %q", key, prev)
		}
		seen[fd.Number()] = key
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
