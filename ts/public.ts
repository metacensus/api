// The public, unauthenticated MetaCensus surface (/metacensus/public), as
// TypeScript: the generated types, the route manifest, and PublicClient.
//
// One package with the authenticated surface, because the SPA calls both from
// one build and should take one dependency; a separate entry point, so a third
// party integrating only against /metacensus/public does not pull the
// authenticated types into their completions or their type graph. Go needs no
// equivalent — its packages were already separate.
//
// The route manifest is exported whole rather than filtered: "every MetaCensus
// route on one screen" is why the contract lives in one repository, and the
// manifest is string literals, not a type surface.

export * from "./src/metacensus/public/v1/common.js";
export * from "./src/metacensus/public/v1/partner.js";
export * from "./src/route-manifest.js";

// Every generated file exports an identical `protobufPackage`, so `export *`
// cannot pick one.
export { protobufPackage } from "./src/metacensus/public/v1/common.js";

// src/client.ts holds a class per surface; this entry point exports only this
// one, out of the same module the authenticated entry point takes Client from.
// That file's header says why it is one module. What it costs here: a
// public-only consumer carries the authenticated Client as dead code — a few
// hundred bytes a bundler drops (`sideEffects: false`), and not the
// authenticated *types*, which is what the split is for.
export {
  PublicClient,
  ApiError,
  type ClientRequest,
  type ClientResponse,
  type Transport,
} from "./src/client.js";
