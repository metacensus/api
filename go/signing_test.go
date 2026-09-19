package contract_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/metacensus/api/go/routes"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Schema tests for the signing chain: the shape properties a digest's
// cross-language agreement, and "a token alone permits no write", rest on. The
// runtime half is go/signing and ts/src/signing.ts. See README.md.

// signatureType is the one message a signature may be, named here because
// routegen names it too (model.SignatureType) and the two have to agree.
const (
	signatureType  = "metacensus.v1.UserSignature"
	contentField   = "content"
	signatureField = "userSignature"
)

// Writes that carry no signature, with the reason each is exempt: Login and
// Logout move no content a participant could vouch for. The list is the point —
// a write that starts arriving unsigned, or stops needing its exception, fails
// here.
var unsignedWrites = map[string]string{
	"POST /metacensus/api/v1/login":  "moves no content; the password is the credential and is never signed",
	"POST /metacensus/api/v1/logout": "moves no content; it surrenders a token",
}

// TestEveryWriteCarriesASignature makes the two identities real: every write
// carries content and a signature over it, except the routes listed above and
// the public surface, which has no identities at all. See README.md, "The
// signing chain".
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

// TestNoWideNumbersCrossJCS lets RFC 8785 be implemented twice: it refuses every
// float, double and 64-bit integer, leaving only int32/uint32, which both
// languages print identically. Why that is the hard part of JCS: go/signing/jcs.go.
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
// absent one, so `{}` and `{"x":…}` would digest differently over a field a
// signer and verifier can disagree was there. Repeated and map fields are exempt
// (EmitDefaultValues makes them always present). UserSignature.signingTime is
// the one such field, made safe by being required — go/signing won't digest
// without it.
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

// TestSignedRecordsReserveTheNextField holds the room the institutional
// signature lands in: every stored record reserves the field after its last (5,
// or 4 on Vote, which mints no id), so protoc refuses the number until it is
// spent. See User in user.proto.
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
		// A request carries a signature too, but nothing stores it, so there
		// is nothing for an institution to endorse later.
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
			"where the institutional signature over user_signature.value lands, and a "+
			"comment saying so is found by the next reader rather than by protoc.",
			md.FullName(), want)
	})
}
