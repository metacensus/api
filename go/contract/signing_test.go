package contract_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/metacensus/api/go/server/routes"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The schema half of the signing chain (see README.md); the runtime half is
// go/signing and ts/src/signing.ts.

// signatureType is the one message a signature may be; routegen names it too
// (model.SignatureType) and the two have to agree.
const (
	signatureType  = "metacensus.v1.UserSignature"
	contentField   = "content"
	signatureField = "userSignature"
)

// unsignedWrites lists routes that write but carry no signature, each with
// its reason; TestEveryWriteCarriesASignature fails if either side drifts.
var unsignedWrites = map[string]string{
	"POST /metacensus/api/v1/login":  "moves no content; the password is the credential and is never signed",
	"POST /metacensus/api/v1/logout": "moves no content; it surrenders a token",
}

// TestEveryWriteCarriesASignature: a login token says who is connected, not
// who authored a record, so every write carries content and a signature over
// it unless listed in unsignedWrites. The public surface is exempt — no
// identities, nobody to sign.
func TestEveryWriteCarriesASignature(t *testing.T) {
	got := map[string]bool{}
	for _, r := range routes.Routes {
		// Every method but GET, not every route with a body: Logout writes
		// entirely via the path and carries nothing to sign.
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

// Holds, at read time, the shape routegen's describeSigned refuses to generate.
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

// Keeps the schema to the numbers JCS (RFC 8785) can carry across Go and
// TypeScript identically — see go/signing/jcs.go's writeNumber and README.md
// ("The digest").
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

// A `*Content` message may hold no singular message field: protojson omits an
// absent one rather than defaulting it, which would be a digest ambiguity a
// signer and verifier could disagree about. Repeated/map fields are exempt
// (EmitDefaultValues always gives `[]`/`{}`). UserSignature.signingTime is the
// one such field allowed, made required instead to remove the ambiguity.
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

// Every stored signed record reserves the field just past its last, so the next
// signature layer is a pure field addition the wire already protects. See
// README.md ("The signing chain").
func TestSignedRecordsReserveTheNextField(t *testing.T) {
	forEachContractMessage(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		var highest protoreflect.FieldNumber
		signed := false
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if fd.Number() > highest {
				highest = fd.Number()
			}
			if fd.Message() != nil && fd.Message().FullName() == "metacensus.v1.UserSignature" {
				signed = true
			}
		}
		// Requests carry a signature too but are never stored.
		if !signed || strings.HasSuffix(string(md.Name()), "Request") {
			return
		}
		want := highest + 1
		ranges := md.ReservedRanges()
		for i := 0; i < ranges.Len(); i++ {
			if r := ranges.Get(i); want >= r[0] && want < r[1] {
				return
			}
		}
		t.Errorf("%s stores a signature but does not `reserved %d;`. That number is "+
			"the home the next signature layer will need, held open on the wire, and a "+
			"comment saying so is found by the next reader rather than by protoc.",
			md.FullName(), want)
	})
}
