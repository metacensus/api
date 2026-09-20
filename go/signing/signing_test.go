package signing

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Exercises the canonicaliser directly over documents, including shapes no
// contract message reaches today; ts/test/wire.test.mjs checks ts/src/signing.ts
// produces the same strings.
func TestCanonicalOrdersAndEscapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "keys are sorted and whitespace is dropped",
			in:   `{ "b": 1,  "a": 2, "C": 3 }`,
			want: `{"C":3,"a":2,"b":1}`,
		},
		{
			name: "sorting is by code unit, not locale or case-folded",
			in:   `{"a":1,"A":2,"_":3}`,
			want: `{"A":2,"_":3,"a":1}`,
		},
		{
			name: "array order is data and is preserved",
			in:   `{"x":[3,1,2]}`,
			want: `{"x":[3,1,2]}`,
		},
		{
			// A literal < stays literal — where encoding/json would diverge.
			name: "only the required escapes",
			in:   `{"s":"a\"b\\c\nd\te<f&g"}`,
			want: `{"s":"a\"b\\c\nd\te<f&g"}`,
		},
		{
			name: "other control characters are lowercase \\u00xx",
			in:   "{\"s\":\"\\u0000\\u001f\"}",
			want: "{\"s\":\"\\u0000\\u001f\"}",
		},
		{
			// U+2028: encoding/json escapes it even with SetEscapeHTML(false).
			name: "non-ascii stays literal",
			in:   "{\"s\":\"h\\u00e9llo \\u2028 \\ud83d\\ude00\"}",
			want: "{\"s\":\"héllo   \U0001f600\"}",
		},
		{
			name: "literals",
			in:   `{"t":true,"f":false,"n":null}`,
			want: `{"f":false,"n":null,"t":true}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := canonicalize(&b, decode(t, tc.in)); err != nil {
				t.Fatal(err)
			}
			if got := b.String(); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// Backstops TestNoWideNumbersCrossJCS: these are refused even if one reaches
// the canonicaliser some other way.
func TestCanonicalRefusesNumbersTheTwoLanguagesWouldPrintDifferently(t *testing.T) {
	for _, in := range []string{`{"x":1.5}`, `{"x":1e3}`, `{"x":9007199254740993}`} {
		var b bytes.Buffer
		if err := canonicalize(&b, decode(t, in)); err == nil {
			t.Errorf("%s was accepted as %s", in, b.String())
		}
	}
}

func decode(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	return v
}

// --- the chain -------------------------------------------------------------

func key(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

var when = timestamppb.New(time.Unix(1700000000, 0).UTC())

// signUser produces a participant Signature over content the way a passkey
// would: a user-verified webauthn.get from origin. The key is software (a test
// can't drive an authenticator), but the object is the shape a passkey emits.
func signUser(t *testing.T, priv *ecdsa.PrivateKey, content *v1.Topic, interp *v1.Interpretation, origin string) *v1.Signature {
	t.Helper()
	keyID, err := KeyID(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := UserChallenge(content, interp, keyID, when)
	if err != nil {
		t.Fatal(err)
	}
	authData := AuthenticatorData("localhost", FlagUP|FlagUV)
	a, err := Assert(priv, challenge, authData, ClientData{Type: TypeGet, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	return &v1.Signature{KeyId: keyID, Time: when, Assertion: a}
}

const origin = "https://app.metacensus.example"

func TestUserSignVerifyRoundTrip(t *testing.T) {
	priv := key(t)
	content := &v1.Topic{Name: "n", Description: "d"}
	interp := Interpretation(content)

	sig := signUser(t, priv, content, interp, origin)
	if err := VerifyUser(&priv.PublicKey, content, interp, sig, ParticipantPolicy(origin)); err != nil {
		t.Fatal(err)
	}
}

// The content is what the challenge binds, so a change to it is what must be
// caught — including one that leaves the document the same length.
func TestVerifyRejectsAlteredContent(t *testing.T) {
	priv := key(t)
	content := &v1.Topic{Name: "n", Description: "d"}
	interp := Interpretation(content)
	sig := signUser(t, priv, content, interp, origin)

	content.Description = "e"
	if err := VerifyUser(&priv.PublicKey, content, interp, sig, ParticipantPolicy(origin)); err == nil {
		t.Error("an altered description verified")
	}
}

// key_id and time are inside the challenge, so neither can change after the
// fact — a signature can't be re-attributed or re-dated. (Interpretation is
// covered by TestVerifyRejectsSubstitutedContentType and
// TestVerifyRefusesAnUnknownSpec.)
func TestVerifyRejectsAlteredBoundFields(t *testing.T) {
	priv := key(t)
	content := &v1.Topic{Name: "n", Description: "d"}

	for _, tc := range []struct {
		name  string
		alter func(*v1.Signature)
	}{
		{"time", func(s *v1.Signature) { s.Time = timestamppb.New(time.Unix(1, 0).UTC()) }},
		{"key id", func(s *v1.Signature) { s.KeyId = "not-the-thumbprint" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			interp := Interpretation(content)
			sig := signUser(t, priv, content, interp, origin)
			tc.alter(sig) // the assertion committed to the original; verify must fail
			if err := VerifyUser(&priv.PublicKey, content, interp, sig, ParticipantPolicy(origin)); err == nil {
				t.Errorf("an altered %s verified", tc.name)
			}
		})
	}
}

// contentType is what stops a signature over one message type verifying
// another whose canonical form happens to coincide.
func TestVerifyRejectsSubstitutedContentType(t *testing.T) {
	priv := key(t)
	content := &v1.Topic{Name: "n", Description: "d"}
	interp := Interpretation(content)
	sig := signUser(t, priv, content, interp, origin)

	// User's first two fields are also two strings; the interpretation still
	// names a Topic, so verifying it as a User must fail on contentType.
	err := VerifyUser(&priv.PublicKey, &v1.User{Name: "n", Email: "d"}, interp, sig, ParticipantPolicy(origin))
	if err == nil {
		t.Fatal("a Topic signature verified over a User")
	}
	if !strings.Contains(err.Error(), "contentType") {
		t.Errorf("error %q does not name contentType", err)
	}
}

func TestVerifyRefusesAnUnknownSpec(t *testing.T) {
	priv := key(t)
	content := &v1.Topic{Name: "n"}
	interp := Interpretation(content)
	sig := signUser(t, priv, content, interp, origin)
	interp.Spec = "metacensus.sig/99"
	if err := VerifyUser(&priv.PublicKey, content, interp, sig, ParticipantPolicy(origin)); err == nil {
		t.Fatal("an unknown spec verified")
	}
}

// time is required rather than optional; see baseFields.
func TestChallengeRefusesAnUnsetTime(t *testing.T) {
	content := &v1.Topic{Name: "n"}
	if _, err := UserChallenge(content, Interpretation(content), "k", nil); err == nil {
		t.Fatal("a signature with no time was digested")
	}
}

// --- acceptance policy -----------------------------------------------------

// The acceptance policy is the one honest difference between the layers, and it
// is a per-layer check, not a second verify path.
func TestPolicyHoldsTheLayerDifference(t *testing.T) {
	priv := key(t)
	content := &v1.Topic{Name: "n"}
	interp := Interpretation(content)

	// A participant assertion (webauthn.get, UV, an origin) fails the
	// institution's policy (which wants the countersign type and no origin).
	part := signUser(t, priv, content, interp, origin)
	if err := VerifyUser(&priv.PublicKey, content, interp, part, InstitutionPolicy()); err == nil {
		t.Error("a passkey assertion satisfied the institution policy")
	}

	// An assertion from an unknown origin fails the participant policy.
	if err := VerifyUser(&priv.PublicKey, content, interp, part, ParticipantPolicy("https://evil.example")); err == nil {
		t.Error("an assertion from an unaccepted origin verified")
	}

	// An assertion with no user-verified flag fails a policy that requires it.
	keyID, _ := KeyID(&priv.PublicKey)
	challenge, _ := UserChallenge(content, interp, keyID, when)
	noUV := AuthenticatorData("localhost", FlagUP) // present, not verified
	a, err := Assert(priv, challenge, noUV, ClientData{Type: TypeGet, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	sig := &v1.Signature{KeyId: keyID, Time: when, Assertion: a}
	if err := VerifyUser(&priv.PublicKey, content, interp, sig, ParticipantPolicy(origin)); err == nil {
		t.Error("an assertion with no UV flag satisfied a UV-required policy")
	}
}

// --- the institutional layer -----------------------------------------------

// The institution countersigns over the whole participant signature, in the
// identical format, checked by the identical VerifyAssertion — one decoder, one
// primitive, both layers.
func TestCountersignRoundTripAndNesting(t *testing.T) {
	userKey, instKey := key(t), key(t)
	content := &v1.Topic{Name: "n", Description: "d"}
	interp := Interpretation(content)
	userSig := signUser(t, userKey, content, interp, origin)

	instKeyID, err := KeyID(&instKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := CountersignChallenge(userSig, interp, instKeyID, when)
	if err != nil {
		t.Fatal(err)
	}
	// Headless: user-present but not user-verified, the institutional type, no
	// browser origin.
	authData := AuthenticatorData("metacensus.example", FlagUP)
	a, err := Assert(instKey, challenge, authData, ClientData{Type: TypeCountersign})
	if err != nil {
		t.Fatal(err)
	}
	inst := &v1.Signature{KeyId: instKeyID, Time: when, Assertion: a}

	if err := VerifyCountersign(&instKey.PublicKey, userSig, interp, inst, InstitutionPolicy()); err != nil {
		t.Fatalf("countersignature did not verify: %v", err)
	}

	// It nests: altering the participant signature invalidates the
	// countersignature, without touching the countersignature itself.
	userSig.Time = timestamppb.New(time.Unix(1, 0).UTC())
	if err := VerifyCountersign(&instKey.PublicKey, userSig, interp, inst, InstitutionPolicy()); err == nil {
		t.Error("the countersignature survived an altered user signature")
	}
}

// --- keys ------------------------------------------------------------------

func TestPublicKeyRoundTripAndThumbprint(t *testing.T) {
	priv := key(t)
	encoded, err := EncodePublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("%q is not base64url, unpadded", encoded)
	}
	back, err := DecodePublicKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equal(&priv.PublicKey) {
		t.Error("the key did not survive the round trip")
	}

	id, err := KeyID(back)
	if err != nil {
		t.Fatal(err)
	}
	same, err := KeyID(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if id != same {
		t.Errorf("thumbprint %q != %q", id, same)
	}
}

// The key offered at sign-up must be the one key_id names, or a participant
// could enrol a public key that is not theirs.
func TestEnrolledKeyBindsKeyIdToTheKeyOffered(t *testing.T) {
	priv := key(t)
	keyID, err := KeyID(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodePublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := EnrolledKey("", keyID); err == nil {
		t.Error("sign-up with no enrolling key resolved")
	}
	if _, err := EnrolledKey(encoded, keyID); err != nil {
		t.Fatalf("the enrolling key did not resolve: %v", err)
	}
	if _, err := EnrolledKey(encoded, "someone-elses-thumbprint"); err == nil {
		t.Error("a key that does not thumbprint to keyId was accepted")
	}
}

// The scheme is ES256; a key on another curve is a signature this package won't
// make.
func TestCurveIsHeld(t *testing.T) {
	wrong, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Assert(wrong, make([]byte, 32), AuthenticatorData("localhost", FlagUP), ClientData{Type: TypeGet}); err == nil {
		t.Error("a P-384 key produced an ES256 assertion")
	}
}

// --- the digest input ------------------------------------------------------

// Pins the shape of the participant signing input: the four bound fields, and
// the interpretation and time rendered as the contract's JSON.
func TestUserSigningInputIsTheDocumentBothLanguagesBuild(t *testing.T) {
	content := &v1.Topic{Name: "n", Description: "d"}
	interp := Interpretation(content)
	in, err := UserSigningInput(content, interp, "KID", when)
	if err != nil {
		t.Fatal(err)
	}
	got := string(in)
	want := `{"content":{"description":"d","name":"n"},"interpretation":{"contentType":"metacensus.v1.Topic","spec":"` + Spec + `"},"keyId":"KID","time":"2023-11-14T22:13:20Z"}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
