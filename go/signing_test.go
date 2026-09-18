package contract_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/metacensus/api/go/routes"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The schema half of the signing chain: what the contract's shapes have to be
// for a digest to mean the same thing in Go and in TypeScript, and for "a
// login token does not by itself permit a write" to be a fact rather than a
// paragraph.
//
// The runtime half is go/signing and ts/src/signing.ts; ts/test/wire.test.mjs
// drives one against the other.

// signatureType is the one message a signature may be, named here because
// routegen names it too (model.SignatureType) and the two have to agree.
const (
	signatureType  = "metacensus.v1.UserSignature"
	contentField   = "content"
	signatureField = "userSignature"
)

// Routes that change something and carry no signature, with the reason each is
// allowed to. Like TestNonConformingRoutes, the exceptions are the point: a write that
// starts arriving unsigned fails here, and one that stops needing the
// exception fails here too.
//
// The two auth routes move no content. Login hands over a password and gets a
// token; Logout gives the token back. Neither writes anything a participant
// could vouch for, and neither can be signed by a key the service has not seen
// yet — which is exactly why SignUp, the third auth route, *is* signed: it is
// where the key arrives.
var unsignedWrites = map[string]string{
	"POST /metacensus/api/v1/login":  "moves no content; the password is the credential and is never signed",
	"POST /metacensus/api/v1/logout": "moves no content; it surrenders a token",
}

// TestEveryWriteCarriesASignature is what makes the two identities real.
//
// A login token grants access to the API. It does not grant the right to write
// a record, because a record says who authored it and a token says only who is
// connected. Every route that writes therefore carries content and a signature
// over it, and the ones that do not are listed above with their reason.
//
// The public surface is exempt and not by omission: it has no identities at
// all, so there is nobody to sign and no key to verify against.
func TestEveryWriteCarriesASignature(t *testing.T) {
	got := map[string]bool{}
	for _, r := range routes.Routes {
		// Every method but GET, not every route with a body: a write moved
		// entirely into the path would otherwise slip the rule by carrying
		// nothing to sign. Logout is exactly that shape, and is listed.
		if r.Method == "GET" || r.Prefix != routes.Prefix {
			continue
		}
		if !r.Signed {
			got[r.Method+" "+fullPath(r)] = true
		}
	}

	for _, key := range slices.Sorted(maps.Keys(got)) {
		if _, ok := unsignedWrites[key]; !ok {
			t.Errorf("%s writes without a signature. A session token says who is "+
				"connected; only a signature says who authored the record. Give the "+
				"request a `content` message and a `%s` over it — or add it to "+
				"unsignedWrites with the reason it moves no content.", key, signatureType)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(unsignedWrites)) {
		if !got[key] {
			t.Errorf("%s is listed as an unsigned write and is not one any more; "+
				"strike it off unsignedWrites.", key)
		}
	}
}

// TestSignedRoutesPairContentAndSignature holds the shape routegen's
// describeSigned refuses at generation time, so that the contract's own suite
// says it too rather than deferring to a generator nobody runs on a read.
func TestSignedRoutesPairContentAndSignature(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		content := fields.ByJSONName(contentField)
		sig := fields.ByJSONName(signatureField)
		switch {
		case content == nil && sig == nil:
			return
		case content == nil:
			t.Errorf("%s carries %s and no %s: a signature over nothing attests to nothing",
				md.FullName(), signatureField, contentField)
			return
		case sig == nil:
			t.Errorf("%s carries %s and no %s: content nobody signed is what the "+
				"signing chain exists to remove", md.FullName(), contentField, signatureField)
			return
		}
		if got := string(sig.Message().FullName()); got != signatureType {
			t.Errorf("%s.%s is a %s, want %s", md.FullName(), signatureField, got, signatureType)
		}
	})
}

// TestNoWideNumbersCrossJCS is what lets RFC 8785 be implemented twice.
//
// JCS serialises a number as ECMAScript would, and reproducing ECMAScript's
// shortest-round-trip printing of an arbitrary double in Go is the one part of
// the spec that does not port cleanly — get it wrong and two implementations
// disagree about a digest for a reason neither can see. The contract sidesteps
// it rather than solving it: no float, no double, no 64-bit integer, which
// leaves int32 and uint32, whose protojson output is an integer both languages
// print identically.
//
// protojson already quotes a 64-bit integer, so one would in fact survive as a
// string — but it would survive by an accident of protojson's defaults rather
// than by anything stated, and `forceLong=string` on the TypeScript side is the
// other half of that accident. The rule here is the one worth holding: the
// digest sees no number a reader has to think about.
func TestNoWideNumbersCrossJCS(t *testing.T) {
	wide := map[protoreflect.Kind]bool{
		protoreflect.Int64Kind: true, protoreflect.Sint64Kind: true, protoreflect.Sfixed64Kind: true,
		protoreflect.Uint64Kind: true, protoreflect.Fixed64Kind: true,
		protoreflect.FloatKind: true, protoreflect.DoubleKind: true,
	}
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if wide[fd.Kind()] {
				t.Errorf("%s.%s is %s. The signing digest is JCS (RFC 8785) over this "+
					"schema, and JCS numbers are ECMAScript numbers; a 64-bit integer or "+
					"a float is where a Go and a TypeScript canonicaliser stop agreeing. "+
					"Use int32/uint32, or a string.", md.FullName(), fd.Name(), fd.Kind())
			}
		}
	})
}

// A `*Content` message may hold no singular message field, so that its
// canonical form has no presence case.
//
// protojson omits an absent message field and emits nothing in its place, so
// `{}` and `{"x": …}` are two documents that differ by a key — which is fine
// on the wire and awkward inside a digest, because a signer and a verifier can
// disagree about whether the field was there without either being wrong.
// Repeated and map fields are exempt: `EmitDefaultValues` gives them `[]` and
// `{}`, so they are always present.
//
// UserSignature itself is the one place the contract accepts such a field —
// signingTime — and it is required rather than optional; go/signing refuses to
// digest a signature without it, which is what removes the ambiguity in
// practice instead of in the schema.
func TestContentHasNoSingularMessageFields(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		if !strings.HasSuffix(string(md.Name()), "Content") {
			return
		}
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if fd.IsList() || fd.IsMap() {
				continue
			}
			if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
				t.Errorf("%s.%s is a singular message. Inside signed content that is a "+
					"presence case the canonical form carries and no reader can see: an "+
					"absent message is a missing key, so a signer and a verifier can "+
					"disagree about the document without either being wrong.",
					md.FullName(), fd.Name())
			}
		}
	})
}
