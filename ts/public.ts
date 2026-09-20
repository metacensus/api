// The public, unauthenticated MetaCensus surface (/metacensus/public):
// generated types, route manifest, and PublicClient. See ts/README.md for
// why this is a separate entry point from the authenticated one.

export * from "./src/metacensus/public/v1/common.js";
export * from "./src/metacensus/public/v1/partner.js";
export * from "./src/route-manifest.js";

// `export *` can't pick one file's `protobufPackage` over another's.
export { protobufPackage } from "./src/metacensus/public/v1/common.js";

// Named, not `export *`: src/client.ts has one class per surface, and this
// entry point wants only PublicClient. A public-only consumer still carries
// the authenticated ClientSigned as dead code (a bundler drops it), but not
// the authenticated types, which is what the split is for.
export {
  PublicClient,
  ApiError,
  type ClientRequest,
  type ClientResponse,
  type Transport,
} from "./src/client.js";
