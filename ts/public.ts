// The public, unauthenticated MetaCensus surface, as TypeScript.
//
// Imported as `@metacensus/api/public`. It is the same package as the root
// entry point, deliberately: the SPA calls both surfaces from one build and
// should take one dependency. What the subpath buys is that a consumer of only
// the public surface — a third party integrating against
// `/metacensus/public/*`, who never had an authenticated session — does not
// pull the authenticated types into their editor's completions or their
// bundle's type graph.
//
// The Go side needs no equivalent: `go/metacensus/public/v1` is already a
// package of its own, and importing it pulls nothing else in. This file is the
// TypeScript half of a separation Go got for free.
//
// The route manifest is exported whole rather than filtered to the public
// routes. That is on purpose: "every MetaCensus route on one screen" is the
// stated reason the contract lives in one repository, and the manifest is
// string literals, not a type surface — reading it costs a consumer nothing.

export * from "./src/metacensus/public/v1/common.js";
export * from "./src/metacensus/public/v1/partner.js";
export * from "./src/route-manifest.js";

// Every generated file exports an identical `protobufPackage`, so `export *`
// cannot pick one.
export { protobufPackage } from "./src/metacensus/public/v1/common.js";
