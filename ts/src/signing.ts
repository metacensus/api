// The signing chain of the MetaCensus contract, in TypeScript:
// `go/signing` is the other half, and ts/test/wire.test.mjs drives one
// against the other. A digest that is one number in two languages is the
// whole point, so neither half may be edited alone.
//
// The chain:
//
//     content -> digest -> user signature -> (later) institutional signature
//
// A user signs the content. An institution will later sign the *user's
// signature value*, not the content: it endorses the author, not the data.
//
//     digest = SHA-384( JCS( {"content": C, "signature": S} ) )
//
// C is the content message, S is the UserSignature with `value` set to the
// empty string. JCS is RFC 8785.
//
// Canonical JSON rather than the octets sent: protojson's output is
// deliberately not byte-stable, so a digest over the bytes could only be
// checked by whoever received them. Canonicalising the message is what lets a
// record stay verifiable after it has been relayed, re-encoded and stored —
// and it is why the client hands the transport a body nobody has to sign.
//
// **No runtime dependency, by construction.** The canonicaliser below is ~60
// lines rather than a package, and the cryptography is WebCrypto through the
// `crypto` global — not an import, so `check-no-runtime.mjs` passes over this
// file unchanged. `@metacensus/api/signing` is a third entry point because it
// is a different API surface from the types, not because it needed an
// exception.

import type { UserSignature } from "./metacensus/v1/common.js";

// What `UserSignature.spec` must carry. It names this whole document: JCS
// over {content, signature}, SHA-384, `value` as base64url of r||s. Changing
// any of those changes this string, so a verifier that does not recognise it
// stops rather than guessing.
export const SPEC = "metacensus.sig/1";

// P-384, SHA-384 — the `Es384` of `UserSignature.Alg`. One algorithm; the
// enum is closed and adding to it is a contract release.
const ALG: EcdsaParams & EcKeyImportParams & EcKeyGenParams = {
  name: "ECDSA",
  namedCurve: "P-384",
  hash: "SHA-384",
};

// ---------------------------------------------------------------------------
// JCS (RFC 8785)
// ---------------------------------------------------------------------------

/**
 * `value` as RFC 8785 canonical JSON.
 *
 * The input is a ts-proto message: a plain object whose shape matches what
 * protojson emits on the Go side, which is the agreement `buf.gen.yaml`'s five
 * options and `EmitDefaultValues` exist to hold and `wire.test.mjs` exists to
 * check. Canonicalising it is therefore canonicalising something both
 * languages already agree about, rather than inventing a second agreement.
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
    // An absent message field is `undefined` in ts-proto and simply missing
    // from protojson's output; JSON has no such value, so it is not a key.
    ([, v]) => v !== undefined,
  );
  // RFC 8785 sorts by UTF-16 code unit, which is exactly what comparing two
  // JavaScript strings with < does. Go has to spell that out; here it is the
  // default, and the Go side's sortUTF16 is what matches this.
  entries.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return "{" + entries.map(([k, v]) => stringToJCS(k) + ":" + canonicalize(v)).join(",") + "}";
}

/**
 * Refuses anything that is not an integer both languages print identically.
 *
 * RFC 8785 serialises numbers as ECMAScript would, and reproducing that in Go
 * for an arbitrary double is the trap that makes JCS hard to implement twice.
 * The contract avoids it rather than solving it: `TestNoWideNumbersCrossJCS`
 * refuses every 64-bit integer, float and double in the schema, leaving int32
 * and uint32. This is the runtime half of that test.
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
 * RFC 8785 §3.2.2.2: the two mandatory escapes, the six short forms, `\u00xx`
 * with lowercase hex for every other control character, everything else
 * literal.
 *
 * `JSON.stringify` happens to produce exactly this for a string. It is spelled
 * out rather than delegated because the Go half cannot delegate — `encoding/json`
 * escapes `<`, `>` and `&` — and two implementations of one rule are easier to
 * keep together when they read alike.
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
 * The exact string the digest is taken over. Exported because a mismatch
 * between two implementations reads much better as two strings than as two
 * hashes.
 */
export function signingInput(content: unknown, signature: UserSignature): string {
  if (!signature.signingTime) {
    // The one field whose absence the canonical form could not otherwise tell
    // from a value: it is message-typed, so it is omitted rather than
    // defaulted, and two documents differing only in whether the signer said
    // when would digest differently for a reason no reader could see.
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
 * Signs `content`, returning the `value` to put on the signature.
 *
 * It fills in nothing else. `spec`, `contentType`, `alg`, `signingTime`,
 * `keyId` and `signerId` are the signer's own claims and are inside the digest,
 * so setting them here would be this module making them on the caller's behalf.
 */
export async function sign(
  privateKey: CryptoKey,
  content: unknown,
  signature: UserSignature,
): Promise<string> {
  checkAttributes(signature);
  const raw = await crypto.subtle.sign(ALG, privateKey, new TextEncoder().encode(signingInput(content, signature)));
  // WebCrypto already returns the fixed-width r||s the contract carries, not
  // a DER SEQUENCE. Two DER spellings of one signature would be two strings
  // for one fact, and both would hash.
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

// The signed attributes this module can speak for. Both sign and verify run
// it, so a signature it would refuse is one it will not make. `contentType` is
// the caller's to set correctly — nothing here knows a plain object's proto
// name — but a signature whose `contentType` is wrong is not a valid signature
// over this record, which is the substitution that field exists to stop.
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
 * `publicKey` as the contract carries it: base64url, unpadded, of SPKI DER.
 * Not PEM — a PEM body's line breaks and header spelling are two documents for
 * one key, and `keyId` hashes one of them.
 */
export async function encodePublicKey(publicKey: CryptoKey): Promise<string> {
  return toBase64Url(new Uint8Array(await crypto.subtle.exportKey("spki", publicKey)));
}

/** Reads what `encodePublicKey` wrote. */
export async function decodePublicKey(encoded: string): Promise<CryptoKey> {
  return crypto.subtle.importKey("spki", fromBase64Url(encoded), ALG, true, ["verify"]);
}

/**
 * base64url of SHA-256 over the SPKI DER: what `UserSignature.keyId` carries.
 *
 * Derived rather than minted, so a client can compute it before it has an
 * account and a verifier can check it rather than take it.
 */
export async function keyId(publicKey: CryptoKey): Promise<string> {
  const spki = await crypto.subtle.exportKey("spki", publicKey);
  return toBase64Url(new Uint8Array(await crypto.subtle.digest("SHA-256", spki)));
}

function toBase64Url(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// Uint8Array<ArrayBuffer>, not the default Uint8Array<ArrayBufferLike>:
// WebCrypto's BufferSource excludes SharedArrayBuffer, so the buffer has to be
// known to be a plain one at the type level.
function fromBase64Url(s: string): Uint8Array<ArrayBuffer> {
  const binary = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(new ArrayBuffer(binary.length));
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}
