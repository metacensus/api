// Package signing is the signing chain of the MetaCensus contract, and the Go
// half of a digest ts/signing.ts must reproduce exactly. See README.md, "The
// signing chain".
//
//	digest = SHA-384( JCS( {"content": C, "signature": S} ) )
//
// C is the content as protojson; S is the UserSignature as protojson with Value
// "". Two non-obvious choices this file is the source for:
//
//   - Canonical JSON of the decoded message, not the octets received: protojson
//     is not byte-stable, so a digest over received bytes is checkable only by
//     whoever received them. Canonicalising the message keeps a record
//     verifiable after it is relayed, re-encoded and stored.
//   - Value is emptied rather than dropped, because EmitDefaultValues and
//     ts-proto's useOptionals=messages already agree every scalar is present; an
//     omission rule would be one more generator agreement with nothing checking it.
package signing

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	contract "github.com/metacensus/api/go"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"google.golang.org/protobuf/proto"
)

// Spec is what UserSignature.Spec must carry, and it names this whole
// document: JCS over {content, signature}, SHA-384, Value as base64url of
// r||s. Changing any of those changes this string, so a verifier that does
// not recognise it stops rather than guessing.
const Spec = "metacensus.sig/1"

// coordBytes is the fixed width of each half of a P-384 signature. Fixed
// width, not DER: two DER encodings of one signature would be two strings for
// one fact, and both would hash.
const coordBytes = 48

// b64 is the encoding every string in this chain uses — base64url, no
// padding. One spelling, because key_id is a hash of one of them.
var b64 = base64.RawURLEncoding

// Canonical returns m as RFC 8785 canonical JSON.
//
// It goes through protojson, so the input is the contract's own JSON —
// lowerCamelCase field names, enums as value names, timestamps as RFC 3339 —
// and not Go's struct encoding.
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

// SigningInput returns the exact bytes the digest is taken over: the canonical
// form of {"content": content, "signature": sig with Value ""}.
//
// Exported because a mismatch between two implementations is much easier to
// read as two strings than as two hashes.
func SigningInput(content proto.Message, sig *v1.UserSignature) ([]byte, error) {
	if content == nil {
		return nil, errors.New("signing: no content")
	}
	if sig == nil {
		return nil, errors.New("signing: no signature")
	}
	if sig.GetSigningTime() == nil {
		// The one field whose absence the canonical form could not otherwise
		// distinguish from a value: it is message-typed, so protojson omits
		// it rather than emitting a default, and two documents that differ
		// only in whether the signer said when would digest differently for
		// a reason no reader could see. Required, so there is no such pair.
		return nil, errors.New("signing: signingTime is unset; every signature carries the time its signer claims")
	}

	bare := proto.Clone(sig).(*v1.UserSignature)
	bare.Value = ""

	c, err := toValue(content)
	if err != nil {
		return nil, fmt.Errorf("signing: content: %w", err)
	}
	s, err := toValue(bare)
	if err != nil {
		return nil, fmt.Errorf("signing: signature: %w", err)
	}

	var b bytes.Buffer
	if err := canonicalize(&b, map[string]any{"content": c, "signature": s}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Digest is SHA-384 over SigningInput.
func Digest(content proto.Message, sig *v1.UserSignature) ([]byte, error) {
	in, err := SigningInput(content, sig)
	if err != nil {
		return nil, err
	}
	sum := sha512.Sum384(in)
	return sum[:], nil
}

// Sign fills sig.Value with a signature over content.
//
// It sets nothing else: Spec, ContentType, Alg, SigningTime, KeyId and
// SignerId are the signer's claims and are inside the digest, so filling them
// in here would be this package signing them on the caller's behalf. A caller
// builds them; checkAttributes is what refuses a set this package cannot speak
// for.
func Sign(priv *ecdsa.PrivateKey, content proto.Message, sig *v1.UserSignature) error {
	if err := checkAttributes(content, sig); err != nil {
		return err
	}
	if priv.Curve != elliptic.P384() {
		return fmt.Errorf("signing: key is on %s, and %s is P-384", priv.Curve.Params().Name, v1.UserSignature_Es384)
	}
	digest, err := Digest(content, sig)
	if err != nil {
		return err
	}
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest)
	if err != nil {
		return fmt.Errorf("signing: %w", err)
	}
	out := make([]byte, 2*coordBytes)
	r.FillBytes(out[:coordBytes])
	s.FillBytes(out[coordBytes:])
	sig.Value = b64.EncodeToString(out)
	return nil
}

// Verify checks sig against content under pub.
//
// It checks the signed attributes first — spec, content type, algorithm — and
// then the signature. A signature that is cryptographically sound over the
// wrong content type is not a valid signature over this record, which is the
// substitution ContentType exists to stop.
//
// Resolving pub is the caller's: a verifier looks up the signer's enrolled key
// by SignerId and KeyId, except on the sign-up that enrols it, where the key
// travels in PublicKey. See PublicKeyOf.
func Verify(pub *ecdsa.PublicKey, content proto.Message, sig *v1.UserSignature) error {
	if err := checkAttributes(content, sig); err != nil {
		return err
	}
	raw, err := b64.DecodeString(sig.GetValue())
	if err != nil {
		return fmt.Errorf("signing: value is not base64url: %w", err)
	}
	if len(raw) != 2*coordBytes {
		return fmt.Errorf("signing: value is %d bytes, want %d (r||s, fixed width, not DER)", len(raw), 2*coordBytes)
	}
	digest, err := Digest(content, sig)
	if err != nil {
		return err
	}
	r := new(big.Int).SetBytes(raw[:coordBytes])
	s := new(big.Int).SetBytes(raw[coordBytes:])
	if !ecdsa.Verify(pub, digest, r, s) {
		return errors.New("signing: signature does not verify")
	}
	return nil
}

// checkAttributes holds the signed attributes to what this package can speak
// for. Both Sign and Verify run it, so a signature this package would refuse
// is a signature it will not make.
func checkAttributes(content proto.Message, sig *v1.UserSignature) error {
	if sig.GetSpec() != Spec {
		return fmt.Errorf("signing: spec is %q, and this package implements %q", sig.GetSpec(), Spec)
	}
	if sig.GetAlg() != v1.UserSignature_Es384 {
		return fmt.Errorf("signing: alg is %s, and this package implements %s", sig.GetAlg(), v1.UserSignature_Es384)
	}
	want := string(content.ProtoReflect().Descriptor().FullName())
	if sig.GetContentType() != want {
		return fmt.Errorf("signing: contentType is %q but the content is a %s", sig.GetContentType(), want)
	}
	return nil
}

// EncodePublicKey spells pub the way the contract carries it: base64url,
// unpadded, of SPKI DER. Not PEM — a PEM body's line breaks and header
// spelling are two documents for one key, and KeyID hashes one of them.
func EncodePublicKey(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("signing: %w", err)
	}
	return b64.EncodeToString(der), nil
}

// DecodePublicKey reads what EncodePublicKey wrote, and refuses a key on any
// curve but P-384.
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
	if pub.Curve != elliptic.P384() {
		return nil, fmt.Errorf("signing: public key is on %s, want P-384", pub.Curve.Params().Name)
	}
	return pub, nil
}

// KeyID is base64url of SHA-256 over the SPKI DER of pub: what
// UserSignature.KeyId carries.
//
// Derived rather than minted, so a client can compute it before it has an
// account and a verifier can check it rather than take it. A KeyId that is not
// the thumbprint of the key it travels with is a record to refuse.
func KeyID(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("signing: %w", err)
	}
	sum := sha256.Sum256(der)
	return b64.EncodeToString(sum[:]), nil
}

// PublicKeyOf resolves the key a signature was made with, for the one case
// where the signature carries it: the sign-up that enrols it.
//
// That record is the only one whose signer cannot be looked up, because no id
// has been minted for them yet, so the key travels inline and the service
// takes it on trust — trust on first use. What is not taken on trust is the
// *binding*: the signature proves the sender holds the private key, and this
// checks that KeyId is the thumbprint of the key offered, so a public key that
// is not the sender's cannot be enrolled under their account.
//
// Every other record must resolve KeyId against the key its SignerId enrolled,
// which is persistence's to do and not this package's.
func PublicKeyOf(sig *v1.UserSignature) (*ecdsa.PublicKey, error) {
	if sig.GetPublicKey() == "" {
		return nil, errors.New("signing: signature carries no inline public key; resolve keyId against the signer's enrolled key")
	}
	pub, err := DecodePublicKey(sig.GetPublicKey())
	if err != nil {
		return nil, err
	}
	id, err := KeyID(pub)
	if err != nil {
		return nil, err
	}
	if sig.GetKeyId() != id {
		return nil, fmt.Errorf("signing: keyId is %q but the key offered thumbprints to %q", sig.GetKeyId(), id)
	}
	return pub, nil
}

// toValue renders m as the contract's JSON and reads it back as the generic
// tree canonicalize walks, with UseNumber so that no integer passes through a
// float64 on the way.
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
