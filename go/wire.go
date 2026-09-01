// Package contract carries the wire encoding for the MetaCensus API contract.
//
// The generated types are in ./metacensus/v1. Marshal them with these options,
// never with encoding/json: see contract/README.md.
package contract

import (
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// MarshalOptions is the encoder every producer of this contract must use.
// EmitDefaultValues, not EmitUnpopulated: absent messages are omitted rather
// than emitted as null.
var MarshalOptions = protojson.MarshalOptions{
	EmitDefaultValues: true,
}

// UnmarshalOptions is the decoder every consumer must use. Unknown fields are
// an error.
var UnmarshalOptions = protojson.UnmarshalOptions{}

// Marshal encodes m as the contract's JSON.
//
// The output is not byte-stable: protojson varies whitespace between members
// deliberately. Compare parsed documents, not bytes.
func Marshal(m proto.Message) ([]byte, error) {
	return MarshalOptions.Marshal(m)
}

// Unmarshal decodes the contract's JSON into m.
func Unmarshal(b []byte, m proto.Message) error {
	return UnmarshalOptions.Unmarshal(b, m)
}
