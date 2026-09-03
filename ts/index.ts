// The authenticated MetaCensus API contract (/metacensus/api/v1), as
// TypeScript. Types only, no runtime.
//
// Everything under ./src is generated; a new .proto file needs a line here.
//
// The public surface (/metacensus/public/*) is `@metacensus/api/public`, in
// ./public.ts — one package, two entry points. It is not merged in here:
// `protobufPackage` names a package, so a barrel spanning two of them could
// only export one of the two values under that name, and the point of the
// split entry point is that a consumer of one surface does not acquire the
// other. The route manifest below covers both.

export * from "./src/metacensus/v1/auth.js";
export * from "./src/metacensus/v1/common.js";
export * from "./src/metacensus/v1/extraction.js";
export * from "./src/metacensus/v1/paper.js";
export * from "./src/metacensus/v1/prop.js";
export * from "./src/metacensus/v1/protocol.js";
export * from "./src/metacensus/v1/topic.js";
export * from "./src/metacensus/v1/user.js";
export * from "./src/route-manifest.js";

// Every generated file exports an identical `protobufPackage`, so `export *`
// cannot pick one.
export { protobufPackage } from "./src/metacensus/v1/common.js";
