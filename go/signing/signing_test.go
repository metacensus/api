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

// The canonicaliser on its own, over documents rather than over messages: the
// cases here are the ones RFC 8785 is *for*, and none of them can be reached
// through a contract message today. ts/src/signing.ts has to produce the same
// strings, and ts/test/wire.test.mjs is where that is checked across the two
// languages rather than asserted separately in each.
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
			// Sorting is by code unit, not by a locale and not case-folded.
			name: "sorting is by code unit",
			in:   `{"a":1,"A":2,"_":3}`,
			want: `{"A":2,"_":3,"a":1}`,
		},
		{
			name: "array order is data and is preserved",
			in:   `{"x":[3,1,2]}`,
			want: `{"x":[3,1,2]}`,
		},
		{
			// The two mandatory escapes and the short forms, and nothing
			// else: a literal < stays literal, which is exactly where
			// encoding/json would have diverged from the TypeScript half.
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
			// Non-ASCII is literal UTF-8, not escaped. U+2028 in particular:
			// Go's encoding/json escapes it even with SetEscapeHTML(false),
			// and JavaScript's JSON.stringify does not.
			name: "non-ascii stays literal",
			in:   "{\"s\":\"h\\u00e9llo \\u2028 \\ud83d\\ude00\"}",
			want: "{\"s\":\"h\u00e9llo \u2028 \U0001f600\"}",
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

// The number rule, which is the half of RFC 8785 the contract avoids rather
// than implements. TestNoWideNumbersCrossJCS keeps these out of the schema;
// this keeps them out of a digest even if one arrives another way.
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
	k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// attrs is the conventional set for a TopicContent signature, so that each
// test below varies exactly one thing.
func attrs(t *testing.T, priv *ecdsa.PrivateKey) *v1.UserSignature {
	t.Helper()
	id, err := KeyID(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return &v1.UserSignature{
		SignerId:    "u1",
		KeyId:       id,
		Alg:         v1.UserSignature_Es384,
		SigningTime: timestamppb.New(time.Unix(1700000000, 0).UTC()),
		Spec:        Spec,
		ContentType: "metacensus.v1.TopicContent",
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	priv := key(t)
	content := &v1.TopicContent{Name: "n", Description: "d"}
	sig := attrs(t, priv)

	if err := Sign(priv, content, sig); err != nil {
		t.Fatal(err)
	}
	if sig.Value == "" {
		t.Fatal("Sign left value empty")
	}
	if err := Verify(&priv.PublicKey, content, sig); err != nil {
		t.Fatal(err)
	}
}

// The content is what is signed, so a change to it is what must be caught —
// including one that leaves the document the same length.
func TestVerifyRejectsAlteredContent(t *testing.T) {
	priv := key(t)
	content := &v1.TopicContent{Name: "n", Description: "d"}
	sig := attrs(t, priv)
	if err := Sign(priv, content, sig); err != nil {
		t.Fatal(err)
	}

	content.Description = "e"
	if err := Verify(&priv.PublicKey, content, sig); err == nil {
		t.Error("an altered description verified")
	}
}

// The signed attributes are inside the digest, so none of them can be changed
// after the fact — which is what stops a signature being re-attributed to
// another signer or re-dated.
func TestVerifyRejectsAlteredAttributes(t *testing.T) {
	priv := key(t)
	content := &v1.TopicContent{Name: "n", Description: "d"}

	for _, tc := range []struct {
		name  string
		alter func(*v1.UserSignature)
	}{
		{"signer", func(s *v1.UserSignature) { s.SignerId = "u2" }},
		{"signing time", func(s *v1.UserSignature) { s.SigningTime = timestamppb.New(time.Unix(1, 0).UTC()) }},
		{"key id", func(s *v1.UserSignature) { s.KeyId = "not-the-thumbprint" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sig := attrs(t, priv)
			if err := Sign(priv, content, sig); err != nil {
				t.Fatal(err)
			}
			tc.alter(sig)
			if err := Verify(&priv.PublicKey, content, sig); err == nil {
				t.Errorf("an altered %s verified", tc.name)
			}
		})
	}
}

// A signature sound over the wrong content type is not a signature over this
// record: two messages of two strings are one document once they are
// canonical, and contentType is what tells them apart.
func TestVerifyRejectsSubstitutedContentType(t *testing.T) {
	priv := key(t)
	sig := attrs(t, priv)
	if err := Sign(priv, &v1.TopicContent{Name: "n", Description: "d"}, sig); err != nil {
		t.Fatal(err)
	}

	// UserContent's first two fields are also two strings; without
	// contentType this would be very nearly the same document.
	err := Verify(&priv.PublicKey, &v1.UserContent{Name: "n", Email: "d"}, sig)
	if err == nil {
		t.Fatal("a TopicContent signature verified over a UserContent")
	}
	if !strings.Contains(err.Error(), "contentType") {
		t.Errorf("error %q does not name contentType", err)
	}
}

func TestSignRefusesAnUnknownSpec(t *testing.T) {
	priv := key(t)
	sig := attrs(t, priv)
	sig.Spec = "metacensus.sig/99"
	if err := Sign(priv, &v1.TopicContent{}, sig); err == nil {
		t.Fatal("an unknown spec was signed")
	}
}

// signingTime is the one message-typed field inside the digest, so its absence
// would be a presence case no reader could see. It is required instead.
func TestDigestRefusesAnUnsetSigningTime(t *testing.T) {
	priv := key(t)
	sig := attrs(t, priv)
	sig.SigningTime = nil
	if _, err := Digest(&v1.TopicContent{}, sig); err == nil {
		t.Fatal("a signature with no signing time was digested")
	}
}

// The signing input is a document, not a hash, so a disagreement between the
// two languages reads as a diff. This pins its shape: the two keys in order,
// with value emptied rather than dropped.
func TestSigningInputIsTheDocumentBothLanguagesBuild(t *testing.T) {
	priv := key(t)
	sig := attrs(t, priv)
	sig.Value = "this must not appear"

	in, err := SigningInput(&v1.TopicContent{Name: "n", Description: "d"}, sig)
	if err != nil {
		t.Fatal(err)
	}
	got := string(in)
	if !strings.HasPrefix(got, `{"content":{"description":"d","name":"n"},"signature":{`) {
		t.Errorf("unexpected shape: %s", got)
	}
	if strings.Contains(got, "this must not appear") {
		t.Errorf("value reached the digest: %s", got)
	}
	if !strings.Contains(got, `"value":""`) {
		t.Errorf("value is dropped rather than emptied: %s", got)
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

// Enrolment is trust on first use, and the binding is what stops it being
// trust in anybody: the key offered has to be the one keyId names, or a
// participant could enrol a public key that is not theirs and later claim the
// signatures made with it.
func TestPublicKeyOfBindsKeyIdToTheKeyOffered(t *testing.T) {
	priv := key(t)
	sig := attrs(t, priv)

	if _, err := PublicKeyOf(sig); err == nil {
		t.Error("a signature with no inline key resolved")
	}

	encoded, err := EncodePublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sig.PublicKey = encoded
	if _, err := PublicKeyOf(sig); err != nil {
		t.Fatal(err)
	}

	sig.KeyId = "someone-elses-thumbprint"
	if _, err := PublicKeyOf(sig); err == nil {
		t.Error("a key that does not thumbprint to keyId was accepted")
	}
}

// P-384 is the curve Es384 names; another curve is a signature this package
// cannot speak for, whatever the attributes claim.
func TestCurveIsHeld(t *testing.T) {
	wrong, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig := attrs(t, key(t))
	if err := Sign(wrong, &v1.TopicContent{}, sig); err == nil {
		t.Error("a P-256 key signed an Es384 signature")
	}
}
