// The MetaCensus signing chain: how a participant's signature over a content
// message is computed and checked, in the same numbers `go/signing` produces.
//
// An entry point beside `@metacensus/api` and `@metacensus/api/public`, and
// it splits by *concern* rather than by surface: the two surfaces exist so
// that a public-only consumer does not acquire the authenticated types, and
// this exists so that a consumer who only wants types does not acquire a
// canonicaliser and a pile of WebCrypto calls. The public surface has no
// identities and nothing on it is ever signed.
//
// It carries no runtime dependency. The canonicaliser is written here rather
// than installed, and the cryptography is the `crypto` global, so
// `check-no-runtime.mjs` reads this file like any other.

export {
  SPEC,
  canonicalize,
  signingInput,
  digest,
  sign,
  verify,
  generateKeyPair,
  encodePublicKey,
  decodePublicKey,
  keyId,
} from "./src/signing.js";
