// Package signing is the signing chain of the MetaCensus contract, and the Go
// half of a digest ts/src/signing.ts must reproduce exactly. See README.md,
// "The signing chain".
//
// One format serves both layers. A participant signs with a passkey (WebAuthn);
// an institution countersigns headless (server/HSM). Both produce the same
// object — a WebAuthn assertion whose challenge is the record's digest — so
// there is one decoder and one crypto primitive (ECDSA P-256 / ES256) the whole
// chain down. The digest is
//
//	challenge = SHA-256( JCS( D ) )
//
// where D is {content, interpretation, keyId, time} for the participant and
// {signature, interpretation, keyId, time} for the institution (which nests
// over the participant's whole signature). The assertion carries that challenge
// inside clientDataJSON and signs authenticatorData ‖ SHA-256(clientDataJSON):
// WebAuthn does not sign arbitrary bytes, so the content digest rides in the
// challenge and is recomputed — never reverse-engineered — at verify time.
//
// Non-obvious choices this file is the source for:
//
//   - Canonical JSON of the decoded message, not the octets received: protojson
//     is not byte-stable, so a digest over received bytes is checkable only by
//     whoever received them. Canonicalising the message keeps a record
//     verifiable after it is relayed, re-encoded and stored.
//   - Verification hashes the clientDataJSON bytes as stored and only parses
//     them to read `challenge`/`type`; it never re-serialises them. A passkey's
//     clientDataJSON comes from the browser verbatim, so a second
//     canonicalisation of it would be a rule with nothing to check it.
//   - The interpretation (spec, contentType) is a record property sealed by both
//     digests, and the acceptance policy — not a second code path — is the only
//     honest difference between a participant assertion and an institutional one.
package signing

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	contract "github.com/metacensus/api/go/contract"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Spec is the value Interpretation.spec must carry; changing this string
// versions the whole scheme (JCS shape, digest, curve, encoding) as one atom.
const Spec = "metacensus.sig/2"

// clientDataJSON.type values. The participant's is WebAuthn's own; the
// institution's is honestly its own, never a forged "webauthn.get".
const (
	TypeGet         = "webauthn.get"
	TypeCountersign = "metacensus.countersign"
)

// authenticatorData flag bits (WebAuthn §6.1). BE/BS record credential custody
// — synced (backup-eligible, backed-up) vs device-bound — and are read, not set
// by policy.
const (
	FlagUP byte = 1 << 0 // user present
	FlagUV byte = 1 << 2 // user verified (a biometric/PIN gesture, not mere presence)
	FlagBE byte = 1 << 3 // backup eligible (a syncable passkey)
	FlagBS byte = 1 << 5 // backed up (currently synced)
)

// b64 is the encoding every string in this chain uses: base64url, unpadded.
var b64 = base64.RawURLEncoding

// Canonical returns m as RFC 8785 canonical JSON, via protojson (the
// contract's own JSON, not Go's struct encoding).
func Canonical(m proto.Message) ([]byte, error) {
	v, err := toValue(m)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := canonicalize(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Interpretation returns an Interpretation for content under this package's
// Spec: the scheme version and the content's full proto name, the two axes a
// stored record self-describes with.
func Interpretation(content proto.Message) *v1.Interpretation {
	return &v1.Interpretation{
		Spec:        Spec,
		ContentType: string(content.ProtoReflect().Descriptor().FullName()),
	}
}

// UserSigningInput returns the exact canonical bytes the participant digest is
// taken over: {content, interpretation, keyId, time}. Exported so a mismatch
// between implementations reads as two strings, not two hashes.
func UserSigningInput(content proto.Message, interp *v1.Interpretation, keyID string, t *timestamppb.Timestamp) ([]byte, error) {
	base, err := baseFields(interp, keyID, t)
	if err != nil {
		return nil, err
	}
	c, err := toValue(content)
	if err != nil {
		return nil, fmt.Errorf("signing: content: %w", err)
	}
	base["content"] = c
	return canonicalBytes(base)
}

// CountersignInput returns the canonical bytes the institutional digest is taken
// over: {signature, interpretation, keyId, time}. The whole participant
// Signature (its assertion included) is the countersigned object, so an altered
// participant signature invalidates the countersignature.
func CountersignInput(userSig *v1.Signature, interp *v1.Interpretation, keyID string, t *timestamppb.Timestamp) ([]byte, error) {
	if userSig == nil {
		return nil, errors.New("signing: no user signature to countersign")
	}
	base, err := baseFields(interp, keyID, t)
	if err != nil {
		return nil, err
	}
	s, err := toValue(userSig)
	if err != nil {
		return nil, fmt.Errorf("signing: signature: %w", err)
	}
	base["signature"] = s
	return canonicalBytes(base)
}

// UserChallenge is SHA-256 over UserSigningInput — the value that must appear,
// base64url, in the participant assertion's clientDataJSON.challenge.
func UserChallenge(content proto.Message, interp *v1.Interpretation, keyID string, t *timestamppb.Timestamp) ([]byte, error) {
	in, err := UserSigningInput(content, interp, keyID, t)
	if err != nil {
		return nil, err
	}
	return challenge(in), nil
}

// CountersignChallenge is SHA-256 over CountersignInput.
func CountersignChallenge(userSig *v1.Signature, interp *v1.Interpretation, keyID string, t *timestamppb.Timestamp) ([]byte, error) {
	in, err := CountersignInput(userSig, interp, keyID, t)
	if err != nil {
		return nil, err
	}
	return challenge(in), nil
}

func challenge(in []byte) []byte {
	sum := sha256.Sum256(in)
	return sum[:]
}

// baseFields is the part of the digest input both layers share.
func baseFields(interp *v1.Interpretation, keyID string, t *timestamppb.Timestamp) (map[string]any, error) {
	if interp == nil {
		return nil, errors.New("signing: no interpretation; a record must self-describe how to read it")
	}
	if keyID == "" {
		return nil, errors.New("signing: no keyId; the signer's enrolled key must be named")
	}
	if t == nil {
		// Message-typed, so protojson omits rather than defaults it; its
		// absence would otherwise be invisible in the canonical form.
		return nil, errors.New("signing: time is unset; every signature carries the time its signer claims or observes")
	}
	i, err := toValue(interp)
	if err != nil {
		return nil, fmt.Errorf("signing: interpretation: %w", err)
	}
	tv, err := toValue(t)
	if err != nil {
		return nil, fmt.Errorf("signing: time: %w", err)
	}
	return map[string]any{"interpretation": i, "keyId": keyID, "time": tv}, nil
}

func canonicalBytes(fields map[string]any) ([]byte, error) {
	var b bytes.Buffer
	if err := canonicalize(&b, fields); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ClientData is the subset of WebAuthn's clientDataJSON a headless signer sets.
// Challenge is filled by the signer from the digest; Type and Origin are the
// signer's own honest metadata. A participant's clientDataJSON comes from the
// browser and carries more, all of which is preserved verbatim.
type ClientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin,omitempty"`
}

// Assert builds the assertion a headless signer emits: it fills the challenge,
// serialises clientDataJSON, and signs authenticatorData ‖ SHA-256(clientData)
// with priv, DER-encoded as WebAuthn does. authData carries the rpId hash and
// the honest flags (see AuthenticatorData); it is not synthesised here so the
// caller states custody explicitly.
func Assert(priv *ecdsa.PrivateKey, challenge, authData []byte, cd ClientData) (*v1.Signature_Assertion, error) {
	if priv.Curve != elliptic.P256() {
		return nil, fmt.Errorf("signing: key is on %s, and this scheme is ES256 (P-256)", priv.Curve.Params().Name)
	}
	cd.Challenge = b64.EncodeToString(challenge)
	cdj, err := json.Marshal(cd)
	if err != nil {
		return nil, fmt.Errorf("signing: clientDataJSON: %w", err)
	}
	sig, err := ecdsa.SignASN1(rand.Reader, priv, signedDigest(authData, cdj))
	if err != nil {
		return nil, fmt.Errorf("signing: %w", err)
	}
	return &v1.Signature_Assertion{
		AuthenticatorData: b64.EncodeToString(authData),
		ClientDataJson:    b64.EncodeToString(cdj),
		Signature:         b64.EncodeToString(sig),
	}, nil
}

// AuthenticatorData is the 37-byte authenticatorData a headless signer carries:
// SHA-256(rpID) ‖ flags ‖ signCount(0). A server has no browser origin, so it
// binds its domain here as the rpId; a participant's comes from the
// authenticator and carries the real UV/BE/BS flags.
func AuthenticatorData(rpID string, flags byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := make([]byte, 37)
	copy(out[:32], h[:])
	out[32] = flags
	return out
}

// signedDigest is what ES256 actually signs: SHA-256 over authenticatorData ‖
// SHA-256(clientDataJSON).
func signedDigest(authData, clientDataJSON []byte) []byte {
	cdh := sha256.Sum256(clientDataJSON)
	msg := make([]byte, 0, len(authData)+len(cdh))
	msg = append(msg, authData...)
	msg = append(msg, cdh[:]...)
	sum := sha256.Sum256(msg)
	return sum[:]
}

// Policy is what a verifier requires of an assertion's own metadata — the one
// honest difference between the layers, checked here rather than in a second
// verify path. Type is the required clientDataJSON.type. Origins, when set, is
// the closed set a participant's assertion must name; left empty it requires the
// assertion carry no origin (a headless signer). RequireUV demands the
// user-verified flag — a human gesture, the participant property WebAuthn adds.
type Policy struct {
	Type      string
	Origins   []string
	RequireUV bool
}

// ParticipantPolicy accepts a passkey assertion: a webauthn.get from a known
// origin, user-verified.
func ParticipantPolicy(origins ...string) Policy {
	return Policy{Type: TypeGet, Origins: origins, RequireUV: true}
}

// InstitutionPolicy accepts the headless countersignature: the institutional
// type, no browser origin, no forged user-verification.
func InstitutionPolicy() Policy {
	return Policy{Type: TypeCountersign}
}

// VerifyAssertion checks one assertion against a challenge under pub and policy:
// the challenge binding first (the assertion commits to this record), then the
// assertion's own metadata (the acceptance policy), then the signature over the
// wrapper. It never re-serialises clientDataJSON — it hashes the bytes as
// stored, and parses them only to read challenge and type.
func VerifyAssertion(pub *ecdsa.PublicKey, a *v1.Signature_Assertion, challenge []byte, policy Policy) error {
	if a == nil {
		return errors.New("signing: no assertion")
	}
	authData, err := b64.DecodeString(a.GetAuthenticatorData())
	if err != nil {
		return fmt.Errorf("signing: authenticatorData is not base64url: %w", err)
	}
	if len(authData) < 37 {
		return fmt.Errorf("signing: authenticatorData is %d bytes, want at least 37", len(authData))
	}
	cdj, err := b64.DecodeString(a.GetClientDataJson())
	if err != nil {
		return fmt.Errorf("signing: clientDataJSON is not base64url: %w", err)
	}
	sig, err := b64.DecodeString(a.GetSignature())
	if err != nil {
		return fmt.Errorf("signing: signature is not base64url: %w", err)
	}

	var cd struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}
	if err := json.Unmarshal(cdj, &cd); err != nil {
		return fmt.Errorf("signing: clientDataJSON is not JSON: %w", err)
	}

	// Challenge binding: the assertion must commit to this record's digest.
	if cd.Challenge != b64.EncodeToString(challenge) {
		return errors.New("signing: clientDataJSON.challenge is not this record's digest")
	}

	// Acceptance policy — the honest per-layer difference.
	if cd.Type != policy.Type {
		return fmt.Errorf("signing: clientDataJSON.type is %q, this layer requires %q", cd.Type, policy.Type)
	}
	if len(policy.Origins) == 0 {
		if cd.Origin != "" {
			return fmt.Errorf("signing: assertion carries origin %q, a headless signer sets none", cd.Origin)
		}
	} else if !slices.Contains(policy.Origins, cd.Origin) {
		return fmt.Errorf("signing: origin %q is not an accepted origin", cd.Origin)
	}
	if policy.RequireUV && authData[32]&FlagUV == 0 {
		return errors.New("signing: user-verified flag is not set; this layer requires a verified human gesture")
	}
	// A full WebAuthn check also binds authenticatorData[:32] to SHA-256(rpID).
	// That is a verifier-config concern (the expected RP per environment), left
	// with the rest of the deferred write-path verification; origin already
	// binds the participant's domain. See README.md, "Open questions".

	// The signature over the wrapper.
	if !ecdsa.VerifyASN1(pub, signedDigest(authData, cdj), sig) {
		return errors.New("signing: signature does not verify")
	}
	return nil
}

// VerifyUser checks a participant signature over content: the interpretation is
// this package's and names content's type, the assertion binds to the record's
// digest, and it satisfies policy. Resolving pub — the enrolled key key_id
// names — is the caller's, except at sign-up (see EnrolledKey).
func VerifyUser(pub *ecdsa.PublicKey, content proto.Message, interp *v1.Interpretation, sig *v1.Signature, policy Policy) error {
	if err := checkInterpretation(content, interp); err != nil {
		return err
	}
	if sig == nil {
		return errors.New("signing: no signature")
	}
	challenge, err := UserChallenge(content, interp, sig.GetKeyId(), sig.GetTime())
	if err != nil {
		return err
	}
	return VerifyAssertion(pub, sig.GetAssertion(), challenge, policy)
}

// VerifyCountersign checks an institutional signature nesting over userSig: the
// countersignature binds to userSig (plus the record's interpretation), and
// satisfies policy. pub is the institution's enrolled key, resolved by the
// caller against the institution's key history.
func VerifyCountersign(pub *ecdsa.PublicKey, userSig *v1.Signature, interp *v1.Interpretation, inst *v1.Signature, policy Policy) error {
	if inst == nil {
		return errors.New("signing: no institutional signature")
	}
	challenge, err := CountersignChallenge(userSig, interp, inst.GetKeyId(), inst.GetTime())
	if err != nil {
		return err
	}
	return VerifyAssertion(pub, inst.GetAssertion(), challenge, policy)
}

// checkInterpretation holds the interpretation to what this package can speak
// for: its spec, and a contentType that names the content actually presented.
func checkInterpretation(content proto.Message, interp *v1.Interpretation) error {
	if interp.GetSpec() != Spec {
		return fmt.Errorf("signing: spec is %q, and this package implements %q", interp.GetSpec(), Spec)
	}
	want := string(content.ProtoReflect().Descriptor().FullName())
	if interp.GetContentType() != want {
		return fmt.Errorf("signing: contentType is %q but the content is a %s", interp.GetContentType(), want)
	}
	return nil
}

// EncodePublicKey spells pub as the contract carries it: base64url of SPKI
// DER, not PEM (whose line breaks and header spelling make two documents of
// one key).
func EncodePublicKey(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("signing: %w", err)
	}
	return b64.EncodeToString(der), nil
}

// DecodePublicKey reads what EncodePublicKey wrote, and refuses a key on any
// curve but P-256 (ES256, WebAuthn's default and this scheme's only curve).
func DecodePublicKey(s string) (*ecdsa.PublicKey, error) {
	der, err := b64.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("signing: public key is not base64url: %w", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("signing: public key is not SPKI DER: %w", err)
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("signing: public key is a %T, want an ECDSA key", parsed)
	}
	if pub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("signing: public key is on %s, want P-256", pub.Curve.Params().Name)
	}
	return pub, nil
}

// KeyID is base64url(SHA-256(SPKI DER of pub)) — what Signature.key_id carries.
// Derived rather than minted, so a client can compute it before it has an
// account and a verifier can check it rather than take it on trust.
func KeyID(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("signing: %w", err)
	}
	sum := sha256.Sum256(der)
	return b64.EncodeToString(sum[:]), nil
}

// EnrolledKey resolves the key being enrolled at sign-up, the one write whose
// key is not yet in persistence. The key is taken on trust (trust on first
// use), but the binding is checked — keyID must thumbprint the key offered, or a
// key that is not the sender's could be enrolled under their account.
//
// Every other record resolves key_id against its owner's enrolled key, which is
// persistence's to do; nothing carries a verification key inline.
func EnrolledKey(publicKey, keyID string) (*ecdsa.PublicKey, error) {
	if publicKey == "" {
		return nil, errors.New("signing: sign-up carries no enrolling public key")
	}
	pub, err := DecodePublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	id, err := KeyID(pub)
	if err != nil {
		return nil, err
	}
	if keyID != id {
		return nil, fmt.Errorf("signing: keyId is %q but the key offered thumbprints to %q", keyID, id)
	}
	return pub, nil
}

// toValue renders m as the contract's JSON and reads it back as the generic
// tree canonicalize walks (UseNumber, so integers never pass through float64).
func toValue(m proto.Message) (any, error) {
	b, err := contract.Marshal(m)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}
