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
// contract message reaches today; ts/test/wire.test.mjs checks ts/signing.ts
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
	k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// attrs is a valid TopicContent signature so each test below varies one thing.
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

// The signed attributes are inside the digest, so none can change after the
// fact — a signature can't be re-attributed or re-dated.
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

// contentType is what stops a signature over one message type verifying
// another whose canonical form happens to coincide.
func TestVerifyRejectsSubstitutedContentType(t *testing.T) {
	priv := key(t)
	sig := attrs(t, priv)
	if err := Sign(priv, &v1.TopicContent{Name: "n", Description: "d"}, sig); err != nil {
		t.Fatal(err)
	}

	// UserContent's first two fields are also two strings.
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

// signingTime is required rather than optional; see SigningInput.
func TestDigestRefusesAnUnsetSigningTime(t *testing.T) {
	priv := key(t)
	sig := attrs(t, priv)
	sig.SigningTime = nil
	if _, err := Digest(&v1.TopicContent{}, sig); err == nil {
		t.Fatal("a signature with no signing time was digested")
	}
}

// Pins the shape of SigningInput: content then signature, value emptied
// rather than dropped.
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

// The key offered must be the one keyId names, or a participant could enrol a
// public key that is not theirs.
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

// Es384 names P-384; another curve is a signature this package won't make.
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
