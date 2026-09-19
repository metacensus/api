// How a participant's signature over a content message is computed and
// checked, in the same numbers `go/signing` produces. A separate entry
// point (see ts/README.md) so a types-only consumer doesn't acquire a
// canonicalizer and a pile of WebCrypto calls.
//
// Implementation is in src/signing.ts, which go/signing is the other side
// of; neither may be edited alone.

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
