// The public, unauthenticated MetaCensus surface (/metacensus/public), as
// TypeScript: the generated types, the route manifest, and PublicClient. Same
// package as the authenticated surface, separate entry point; the manifest is
// exported whole. See ts/README.md, "The public surface".

export * from "./src/metacensus/public/v1/common.js";
export * from "./src/metacensus/public/v1/partner.js";
export * from "./src/route-manifest.js";

// Every generated file exports an identical `protobufPackage`, so `export *`
// cannot pick one.
export { protobufPackage } from "./src/metacensus/public/v1/common.js";

// src/client.ts holds a class per surface; this entry point exports only
// PublicClient. A public-only consumer carries the authenticated Client as dead
// code a bundler drops, but not its types — which is what the split is for.
export {
  PublicClient,
  ApiError,
  type ClientRequest,
  type ClientResponse,
  type Transport,
} from "./src/client.js";
