// The MetaCensus signing chain, producing the same digest as go/signing. A
// third entry point splitting by concern, not surface. See ts/README.md,
// "Signing". The runtime half is src/signing.ts.

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
