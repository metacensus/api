// The MetaCensus signing chain, producing the same digest as go/signing. A
// third entry point splitting by concern, not surface. See ts/README.md,
// "Signing". The runtime half is src/signing.ts.

export {
  SPEC,
  TYPE_GET,
  TYPE_COUNTERSIGN,
  FLAG_UP,
  FLAG_UV,
  FLAG_BE,
  FLAG_BS,
  canonicalize,
  interpretation,
  userSigningInput,
  countersignInput,
  userChallenge,
  countersignChallenge,
  authenticatorData,
  assert,
  verifyAssertion,
  verifyUser,
  verifyCountersign,
  participantPolicy,
  institutionPolicy,
  generateKeyPair,
  encodePublicKey,
  decodePublicKey,
  keyId,
  enrolledKey,
} from "./src/signing.js";

export type { ClientData, Policy } from "./src/signing.js";
