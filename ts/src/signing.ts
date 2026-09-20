// The TypeScript half of the MetaCensus signing chain; go/signing is the other
// half, and ts/test/wire.test.mjs checks the two agree — neither may be edited
// alone. See README.md, "The signing chain".
//
// One format serves both layers. A participant signs with a passkey (WebAuthn);
// an institution countersigns headless. Both produce the same object — a
// WebAuthn assertion whose challenge is the record's digest:
//
//     challenge = SHA-256( JCS( D ) )
//
// D is {content, interpretation, keyId, time} for the participant and
// {signature, interpretation, keyId, time} for the institution. The assertion
// carries that challenge inside clientDataJSON and signs authenticatorData ‖
// SHA-256(clientDataJSON). Verification recomputes the challenge, confirms the
// binding, applies the layer's acceptance policy, then verifies the signature
// over the wrapper — it never re-serialises clientDataJSON, only hashes the
// stored bytes and parses them for `challenge`/`type`.
//
// This module holds the format primitives: the digest, a headless signer, and
// verification. The browser passkey ceremony — navigator.credentials.create /
// get — belongs to the consumer (metacensus/ui#55), which passes its assertion
// here to be verified. Canonical JSON (RFC 8785) rather than the wire bytes:
// protojson isn't byte-stable, so only canonicalizing keeps a record verifiable
// after it's relayed, re-encoded and stored.

import type { Interpretation, Signature, Signature_Assertion } from "./metacensus/v1/common.js";

// WebCrypto's BufferSource excludes SharedArrayBuffer, so every byte string in
// this module is an ArrayBuffer-backed Uint8Array.
type Bytes = Uint8Array<ArrayBuffer>;

// Versions the whole scheme (JCS + SHA-256 + P-256/ES256 + base64url) as one
// atom; a verifier that doesn't recognize it should refuse rather than guess.
export const SPEC = "metacensus.sig/2";

// clientDataJSON.type values: the participant's is WebAuthn's own, the
// institution's is honestly its own — never a forged "webauthn.get".
export const TYPE_GET = "webauthn.get";
export const TYPE_COUNTERSIGN = "metacensus.countersign";

// authenticatorData flag bits (WebAuthn §6.1). BE/BS record credential custody
// (synced vs device-bound) and are read, not set by policy.
export const FLAG_UP = 1 << 0; // user present
export const FLAG_UV = 1 << 2; // user verified (a gesture, not mere presence)
export const FLAG_BE = 1 << 3; // backup eligible (a syncable passkey)
export const FLAG_BS = 1 << 5; // backed up (currently synced)

// ES256: ECDSA on P-256 with SHA-256 — WebAuthn's default and this scheme's
// only algorithm. The enum is gone; a different curve is a different SPEC.
const ALG: EcdsaParams & EcKeyImportParams & EcKeyGenParams = {
  name: "ECDSA",
  namedCurve: "P-256",
  hash: "SHA-256",
};

// ---------------------------------------------------------------------------
// JCS (RFC 8785)
// ---------------------------------------------------------------------------

/**
 * A value as RFC 8785 canonical JSON of a ts-proto message — shaped to match
 * what protojson emits on the Go side (see buf.gen.yaml, wire.test.mjs).
 */
export function canonicalize(value: unknown): string {
  if (value === null) return "null";
  switch (typeof value) {
    case "boolean":
      return value ? "true" : "false";
    case "number":
      return numberToJCS(value);
    case "string":
      return stringToJCS(value);
    case "object":
      break;
    default:
      throw new TypeError(`signing: ${typeof value} cannot appear in a canonical document`);
  }
  if (Array.isArray(value)) {
    return "[" + value.map(canonicalize).join(",") + "]";
  }
  const entries = Object.entries(value as Record<string, unknown>).filter(
    // Absent message fields are `undefined` in ts-proto; protojson omits them too.
    ([, v]) => v !== undefined,
  );
  // RFC 8785 sorts by UTF-16 code unit — exactly what `<` does on JS strings
  // (Go's sortUTF16 matches this).
  entries.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return "{" + entries.map(([k, v]) => stringToJCS(k) + ":" + canonicalize(v)).join(",") + "}";
}

/**
 * Only int32/uint32 survive JCS identically in both languages (RFC 8785's
 * ECMAScript number formatting doesn't reproduce cleanly in Go); the schema
 * enforces this via `TestNoWideNumbersCrossJCS` — this is its runtime half.
 */
function numberToJCS(n: number): string {
  if (!Number.isInteger(n) || !Number.isSafeInteger(n)) {
    throw new RangeError(
      `signing: ${n} is not a safe integer; the contract carries no float, double ` +
        `or 64-bit integer field (TestNoWideNumbersCrossJCS), because ECMAScript ` +
        `number formatting is the one part of RFC 8785 that does not reimplement cleanly`,
    );
  }
  return String(n);
}

/**
 * RFC 8785 §3.2.2.2 escaping, spelled out rather than delegated to
 * `JSON.stringify` — which would match, but Go's `encoding/json` also
 * escapes `<`, `>` and `&`, and two implementations read alike this way.
 */
const SHORT: Record<string, string> = {
  "\b": "\\b",
  "\t": "\\t",
  "\n": "\\n",
  "\f": "\\f",
  "\r": "\\r",
  '"': '\\"',
  "\\": "\\\\",
};

function stringToJCS(s: string): string {
  let out = '"';
  for (const ch of s) {
    const short = SHORT[ch];
    if (short !== undefined) {
      out += short;
    } else if (ch < " ") {
      out += "\\u" + ch.charCodeAt(0).toString(16).padStart(4, "0");
    } else {
      out += ch;
    }
  }
  return out + '"';
}

// ---------------------------------------------------------------------------
// The digest
// ---------------------------------------------------------------------------

/** An Interpretation for content of the given full proto name, under this SPEC. */
export function interpretation(contentType: string): Interpretation {
  return { spec: SPEC, contentType };
}

/**
 * The exact string the participant digest is taken over: {content,
 * interpretation, keyId, time}. Exported so a mismatch between implementations
 * shows up as two strings, not two hashes.
 */
export function userSigningInput(
  content: unknown,
  interp: Interpretation,
  keyId: string,
  time: string,
): string {
  return canonicalize(baseFields(interp, keyId, time, { content }));
}

/** The string the institutional digest is taken over: {signature, interpretation, keyId, time}. */
export function countersignInput(
  userSignature: Signature,
  interp: Interpretation,
  keyId: string,
  time: string,
): string {
  return canonicalize(baseFields(interp, keyId, time, { signature: userSignature }));
}

function baseFields(
  interp: Interpretation,
  keyId: string,
  time: string,
  extra: Record<string, unknown>,
): Record<string, unknown> {
  if (!time) {
    // Message-typed, so canonicalization can't tell "absent" from a default;
    // it must be set explicitly.
    throw new Error("signing: time is unset; every signature carries the time its signer claims or observes");
  }
  if (!keyId) throw new Error("signing: keyId is unset; the signer's enrolled key must be named");
  return { interpretation: interp, keyId, time, ...extra };
}

/** SHA-256 over userSigningInput — the participant assertion's challenge. */
export async function userChallenge(
  content: unknown,
  interp: Interpretation,
  keyId: string,
  time: string,
): Promise<Bytes> {
  return challenge(userSigningInput(content, interp, keyId, time));
}

/** SHA-256 over countersignInput — the institutional assertion's challenge. */
export async function countersignChallenge(
  userSignature: Signature,
  interp: Interpretation,
  keyId: string,
  time: string,
): Promise<Bytes> {
  return challenge(countersignInput(userSignature, interp, keyId, time));
}

async function challenge(input: string): Promise<Bytes> {
  return new Uint8Array(await sha256(new TextEncoder().encode(input)));
}

// ---------------------------------------------------------------------------
// The assertion
// ---------------------------------------------------------------------------

/** The clientDataJSON fields a headless signer sets; a passkey's carries more. */
export interface ClientData {
  type: string;
  origin?: string;
}

/**
 * authenticatorData for a headless signer: SHA-256(rpId) ‖ flags ‖ signCount(0).
 * A participant's comes from the authenticator with the real UV/BE/BS flags.
 */
export async function authenticatorData(rpId: string, flags: number): Promise<Bytes> {
  const hash = new Uint8Array(await sha256(new TextEncoder().encode(rpId)));
  const out = new Uint8Array(37);
  out.set(hash, 0);
  out[32] = flags & 0xff;
  return out;
}

/**
 * Builds the assertion a headless signer emits: fills the challenge, serialises
 * clientDataJSON, and signs authenticatorData ‖ SHA-256(clientDataJSON) with
 * privateKey, DER-encoded as WebAuthn does. authData carries the honest flags
 * (see authenticatorData). For a participant, the browser produces all of this
 * and this module only verifies it.
 */
export async function assert(
  privateKey: CryptoKey,
  challenge: Bytes,
  authData: Bytes,
  clientData: ClientData,
): Promise<Signature_Assertion> {
  const cdj = new TextEncoder().encode(
    JSON.stringify({ ...clientData, challenge: toBase64Url(challenge) }),
  );
  const raw = new Uint8Array(await crypto.subtle.sign(ALG, privateKey, await signedMessage(authData, cdj)));
  return {
    authenticatorData: toBase64Url(authData),
    clientDataJson: toBase64Url(cdj),
    signature: toBase64Url(rawToDer(raw)),
  };
}

/** authenticatorData ‖ SHA-256(clientDataJSON) — what ES256 then hashes and signs. */
async function signedMessage(authData: Bytes, clientDataJSON: Bytes): Promise<Bytes> {
  const cdh = new Uint8Array(await sha256(clientDataJSON));
  const msg = new Uint8Array(authData.length + cdh.length);
  msg.set(authData, 0);
  msg.set(cdh, authData.length);
  return msg;
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

/**
 * What a verifier requires of an assertion's own metadata — the one honest
 * difference between the layers, and a check here rather than a second verify
 * path. `type` is the required clientDataJSON.type; `origins`, when non-empty,
 * is the closed set a participant assertion must name, and when empty requires
 * no origin (a headless signer); `requireUV` demands the user-verified flag.
 */
export interface Policy {
  type: string;
  origins: string[];
  requireUV: boolean;
}

/** Accepts a passkey assertion: webauthn.get from a known origin, user-verified. */
export function participantPolicy(...origins: string[]): Policy {
  return { type: TYPE_GET, origins, requireUV: true };
}

/** Accepts the headless countersignature: the institutional type, no origin. */
export function institutionPolicy(): Policy {
  return { type: TYPE_COUNTERSIGN, origins: [], requireUV: false };
}

/**
 * Checks one assertion against a challenge under publicKey and policy: the
 * challenge binding, then the assertion's own metadata, then the signature over
 * the wrapper. clientDataJSON is hashed as stored and only parsed to read
 * `challenge`/`type`; it is never re-serialised.
 */
export async function verifyAssertion(
  publicKey: CryptoKey,
  assertion: Signature_Assertion,
  challenge: Bytes,
  policy: Policy,
): Promise<boolean> {
  const authData = fromBase64Url(assertion.authenticatorData);
  const cdj = fromBase64Url(assertion.clientDataJson);
  const sig = fromBase64Url(assertion.signature);
  if (authData.length < 37) return false;

  let cd: { type?: string; challenge?: string; origin?: string };
  try {
    cd = JSON.parse(new TextDecoder().decode(cdj));
  } catch {
    return false;
  }

  // Challenge binding: the assertion must commit to this record's digest.
  if (cd.challenge !== toBase64Url(challenge)) return false;

  // Acceptance policy — the honest per-layer difference.
  if (cd.type !== policy.type) return false;
  if (policy.origins.length === 0) {
    if (cd.origin) return false;
  } else if (!policy.origins.includes(cd.origin ?? "")) {
    return false;
  }
  if (policy.requireUV && (authData[32] & FLAG_UV) === 0) return false;

  return crypto.subtle.verify(ALG, publicKey, derToRaw(sig), await signedMessage(authData, cdj));
}

/**
 * Checks a participant signature over content: the interpretation is this
 * scheme's and names content's type, the assertion binds to the record's
 * digest, and it satisfies policy. Resolving publicKey — the enrolled key keyId
 * names — is the caller's, except at sign-up (see enrolledKey).
 */
export async function verifyUser(
  publicKey: CryptoKey,
  content: unknown,
  interp: Interpretation,
  signature: Signature,
  policy: Policy,
): Promise<boolean> {
  if (interp.spec !== SPEC) return false;
  if (!signature.assertion) return false;
  const challenge = await userChallenge(content, interp, signature.keyId, signature.time ?? "");
  return verifyAssertion(publicKey, signature.assertion, challenge, policy);
}

/**
 * Checks an institutional signature nesting over userSignature: it binds to
 * userSignature (plus the record's interpretation) and satisfies policy.
 */
export async function verifyCountersign(
  publicKey: CryptoKey,
  userSignature: Signature,
  interp: Interpretation,
  institutional: Signature,
  policy: Policy,
): Promise<boolean> {
  if (!institutional.assertion) return false;
  const challenge = await countersignChallenge(userSignature, interp, institutional.keyId, institutional.time ?? "");
  return verifyAssertion(publicKey, institutional.assertion, challenge, policy);
}

// ---------------------------------------------------------------------------
// Keys
// ---------------------------------------------------------------------------

/** Generates a signing keypair. The private key is non-extractable. */
export async function generateKeyPair(): Promise<CryptoKeyPair> {
  return crypto.subtle.generateKey(ALG, false, ["sign", "verify"]) as Promise<CryptoKeyPair>;
}

/**
 * `publicKey` as the contract carries it: base64url of SPKI DER, not PEM —
 * a PEM body's line breaks would make `keyId`'s hash ambiguous.
 */
export async function encodePublicKey(publicKey: CryptoKey): Promise<string> {
  return toBase64Url(new Uint8Array(await crypto.subtle.exportKey("spki", publicKey)));
}

/** Reads what `encodePublicKey` wrote. */
export async function decodePublicKey(encoded: string): Promise<CryptoKey> {
  return crypto.subtle.importKey("spki", fromBase64Url(encoded), ALG, true, ["verify"]);
}

/**
 * base64url of SHA-256 over the SPKI DER — `Signature.keyId`. Derived rather
 * than minted, so a client can compute it before it has an account.
 */
export async function keyId(publicKey: CryptoKey): Promise<string> {
  const spki = await crypto.subtle.exportKey("spki", publicKey);
  return toBase64Url(new Uint8Array(await sha256(new Uint8Array(spki))));
}

/**
 * Resolves the key being enrolled at sign-up, the one write whose key is not
 * yet in persistence: the key offered must thumbprint to the keyId claimed, or
 * a key that is not the sender's could be enrolled under their account. Every
 * other record resolves keyId against its owner's enrolled key.
 */
export async function enrolledKey(publicKey: string, claimedKeyId: string): Promise<CryptoKey> {
  if (!publicKey) throw new Error("signing: sign-up carries no enrolling public key");
  const key = await decodePublicKey(publicKey);
  if ((await keyId(key)) !== claimedKeyId) {
    throw new Error("signing: keyId does not thumbprint the key offered");
  }
  return key;
}

// ---------------------------------------------------------------------------
// Bytes
// ---------------------------------------------------------------------------

function sha256(bytes: BufferSource): Promise<ArrayBuffer> {
  return crypto.subtle.digest("SHA-256", bytes);
}

function toBase64Url(bytes: Uint8Array): string {
  return bytes.toBase64({ alphabet: "base64url", omitPadding: true });
}

// Uint8Array<ArrayBuffer>, not the default <ArrayBufferLike>: WebCrypto's
// BufferSource excludes SharedArrayBuffer. fromBase64's loose chunk handling
// accepts the unpadded input toBase64Url writes.
function fromBase64Url(s: string): Uint8Array<ArrayBuffer> {
  return Uint8Array.fromBase64(s, { alphabet: "base64url" });
}

// WebCrypto signs and verifies fixed-width r‖s (IEEE P1363); WebAuthn stores an
// ASN.1 DER SEQUENCE. These two bridge the formats for P-256 (32-byte coords),
// so one stored format (DER) serves a passkey and a headless signer alike.

function rawToDer(raw: Uint8Array<ArrayBuffer>): Uint8Array<ArrayBuffer> {
  const r = derInt(raw.subarray(0, 32));
  const s = derInt(raw.subarray(32, 64));
  const body = new Uint8Array(r.length + s.length);
  body.set(r, 0);
  body.set(s, r.length);
  const out = new Uint8Array(2 + body.length);
  out[0] = 0x30; // SEQUENCE
  out[1] = body.length;
  out.set(body, 2);
  return out;
}

// One ASN.1 INTEGER from a big-endian coordinate: strip leading zeros, prepend
// a 0x00 when the high bit is set so the value stays positive.
function derInt(coord: Uint8Array): Uint8Array {
  let i = 0;
  while (i < coord.length - 1 && coord[i] === 0) i++;
  let v = coord.subarray(i);
  if (v[0] & 0x80) {
    const pad = new Uint8Array(v.length + 1);
    pad.set(v, 1);
    v = pad;
  }
  const out = new Uint8Array(2 + v.length);
  out[0] = 0x02; // INTEGER
  out[1] = v.length;
  out.set(v, 2);
  return out;
}

function derToRaw(der: Uint8Array): Uint8Array<ArrayBuffer> {
  // SEQUENCE header, then two INTEGERs; left-pad each coordinate to 32 bytes.
  let p = 2; // skip 0x30, length
  if (der[p] !== 0x02) throw new Error("signing: malformed DER signature");
  const rLen = der[p + 1];
  const r = der.subarray(p + 2, p + 2 + rLen);
  p = p + 2 + rLen;
  if (der[p] !== 0x02) throw new Error("signing: malformed DER signature");
  const sLen = der[p + 1];
  const s = der.subarray(p + 2, p + 2 + sLen);
  const out = new Uint8Array(64);
  coordInto(out, 0, r);
  coordInto(out, 32, s);
  return out;
}

// Right-align a DER INTEGER (which may carry a leading 0x00 or be short) into a
// 32-byte coordinate slot.
function coordInto(out: Uint8Array, at: number, v: Uint8Array): void {
  let i = 0;
  while (i < v.length - 32) i++; // drop a leading 0x00 pad if present
  const src = v.subarray(i);
  out.set(src, at + (32 - src.length));
}
