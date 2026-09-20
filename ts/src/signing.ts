// The TypeScript half of the MetaCensus signing chain; go/signing is the
// other half, and ts/test/wire.test.mjs checks the two agree — neither may
// be edited alone.
//
//     digest = SHA-384( JCS( {"content": C, "signature": S} ) )
//
// C is the content message, S is the UserSignature with `value` cleared.
// Canonical JSON (RFC 8785) rather than the wire bytes: protojson isn't
// byte-stable, so only canonicalizing keeps a record verifiable after it's
// relayed, re-encoded and stored.
//
// The canonicalizer below is hand-written — it must match go/signing byte for
// byte, which no general RFC 8785 library does — and signing uses the WebCrypto
// `crypto` global and the platform's own base64 codec, so it pulls in nothing.

import type { UserSignature } from "./metacensus/v1/common.js";

// Identifies this scheme (JCS + SHA-384 + base64url r||s); a verifier that
// doesn't recognize it should refuse rather than guess.
export const SPEC = "metacensus.sig/1";

// P-384/SHA-384 — `UserSignature.Alg`'s `Es384`. The enum is closed; adding
// another algorithm is a contract release.
const ALG: EcdsaParams & EcKeyImportParams & EcKeyGenParams = {
  name: "ECDSA",
  namedCurve: "P-384",
  hash: "SHA-384",
};

// ---------------------------------------------------------------------------
// JCS (RFC 8785)
// ---------------------------------------------------------------------------

/**
 * `value` as RFC 8785 canonical JSON of a ts-proto message — shaped to
 * match what protojson emits on the Go side (see buf.gen.yaml, wire.test.mjs).
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
// The chain
// ---------------------------------------------------------------------------

/**
 * The exact string the digest is taken over — exported so a mismatch between
 * implementations shows up as two strings, not two hashes.
 */
export function signingInput(content: unknown, signature: UserSignature): string {
  if (!signature.signingTime) {
    // Message-typed, so canonicalization can't tell "absent" from a default;
    // it must be set explicitly.
    throw new Error("signing: signingTime is unset; every signature carries the time its signer claims");
  }
  return canonicalize({ content, signature: { ...signature, value: "" } });
}

/** SHA-384 over `signingInput`. */
export async function digest(content: unknown, signature: UserSignature): Promise<ArrayBuffer> {
  const bytes = new TextEncoder().encode(signingInput(content, signature));
  return crypto.subtle.digest("SHA-384", bytes);
}

/**
 * Signs `content`, returning the `value` for the signature. Fills in
 * nothing else — the other fields are the signer's own claims and are
 * already inside the digest.
 */
export async function sign(
  privateKey: CryptoKey,
  content: unknown,
  signature: UserSignature,
): Promise<string> {
  checkAttributes(signature);
  const raw = await crypto.subtle.sign(ALG, privateKey, new TextEncoder().encode(signingInput(content, signature)));
  // WebCrypto returns fixed-width r||s directly, not a DER SEQUENCE — no
  // re-encoding needed.
  return toBase64Url(new Uint8Array(raw));
}

/** Checks `signature` against `content` under `publicKey`. */
export async function verify(
  publicKey: CryptoKey,
  content: unknown,
  signature: UserSignature,
): Promise<boolean> {
  checkAttributes(signature);
  const value = fromBase64Url(signature.value);
  return crypto.subtle.verify(ALG, publicKey, value, new TextEncoder().encode(signingInput(content, signature)));
}

// Checked by both sign and verify, so a signature this module would refuse
// is one it will never produce. `contentType` is the caller's responsibility
// to set correctly — nothing here knows a plain object's proto name.
function checkAttributes(signature: UserSignature): void {
  if (signature.spec !== SPEC) {
    throw new Error(`signing: spec is ${JSON.stringify(signature.spec)}, and this module implements ${JSON.stringify(SPEC)}`);
  }
  if (signature.alg !== "Es384") {
    throw new Error(`signing: alg is ${JSON.stringify(signature.alg)}, and this module implements "Es384"`);
  }
  if (!signature.contentType) {
    throw new Error("signing: contentType is empty; it names the message the signature covers");
  }
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
 * base64url of SHA-256 over the SPKI DER — `UserSignature.keyId`. Derived
 * rather than minted, so a client can compute it before it has an account.
 */
export async function keyId(publicKey: CryptoKey): Promise<string> {
  const spki = await crypto.subtle.exportKey("spki", publicKey);
  return toBase64Url(new Uint8Array(await crypto.subtle.digest("SHA-256", spki)));
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
