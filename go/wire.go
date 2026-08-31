// Package contract carries the wire encoding for the MetaCensus API contract.
//
// The generated types live in ./metacensus/v1. This package exists so the one
// decision that is not expressible in a .proto file — how those types are
// turned into JSON — is code rather than a paragraph someone has to remember.
//
// Marshal these types with protojson, never with encoding/json. The types have
// no usable `json:` struct tags, and even if they did, encoding/json would get
// enums wrong (integers instead of value names), timestamps wrong
// (`{"seconds":…,"nanos":…}` instead of RFC 3339) and 64-bit integers wrong
// (unquoted numbers). protojson is not an optimisation here; it is the only
// encoder that produces the wire format this contract describes.
package contract

import (
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// MarshalOptions is the encoder every producer of this contract must use.
//
// EmitDefaultValues, not EmitUnpopulated. The two differ on fields that support
// presence — message-typed fields, proto3 `optional` scalars and oneofs:
//
//	EmitDefaultValues:  {"page":0, "limit":0}
//	EmitUnpopulated:    {"page":0, "limit":0, "total":null}
//
// Both emit zero-valued scalars, empty lists and `Unspecified` enums. Only
// EmitUnpopulated invents `null` for an absent message.
//
// That difference is what makes the choice, because it has to agree with
// ts-proto's `useOptionals=messages` on the TypeScript side. Under that setting
// a message-typed field is generated as `created?: string | undefined` — and
// `null` is not assignable to `string | undefined`. EmitUnpopulated would emit
// documents that the generated TypeScript rejects, in both directions: the
// cross-language golden check in this package's tests fails on the very first
// absent timestamp.
//
// The pairing is therefore:
//
//   - scalars, enums and repeated fields are ALWAYS present in the JSON, so
//     ts-proto types them as required and a consumer never has to check;
//   - message-typed fields are present or absent, never null, so ts-proto types
//     them optional and a consumer checks once.
//
// A consequence worth knowing: because presence is the only thing that
// distinguishes "absent" from "zero", and only message fields have presence,
// this contract expresses optionality exclusively through message-typed fields.
// `google.protobuf.Timestamp` covers the timestamps; `google.protobuf.Int32Value`
// covers `ListMetadata.total`. There are no proto3 `optional` scalars, because
// ts-proto's handling of them under `useOptionals=messages` is a third set of
// rules nobody needs to learn.
var MarshalOptions = protojson.MarshalOptions{
	EmitDefaultValues: true,
}

// UnmarshalOptions is the decoder every consumer of this contract must use.
//
// DiscardUnknown is deliberately off. A field the receiver does not recognise
// is either a backend running ahead of its contract or a client talking to the
// wrong service, and both are worth a loud error while this contract is young.
// Turn it on when there are enough independent implementations that tolerating
// forward-compatible additions matters more than catching mistakes.
var UnmarshalOptions = protojson.UnmarshalOptions{}

// Marshal encodes m as the contract's JSON.
//
// The output is not byte-stable: protojson deliberately varies the whitespace
// between JSON members so that nobody comes to depend on its exact framing.
// Compare parsed documents, not bytes. (The golden files in this package are
// re-indented through encoding/json for exactly this reason.)
func Marshal(m proto.Message) ([]byte, error) {
	return MarshalOptions.Marshal(m)
}

// Unmarshal decodes the contract's JSON into m.
func Unmarshal(b []byte, m proto.Message) error {
	return UnmarshalOptions.Unmarshal(b, m)
}
