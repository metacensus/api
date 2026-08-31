// The MetaCensus API contract, as TypeScript. Types only, no runtime.
//
// Everything under ./src is generated; adding a .proto file means adding a line
// here.

export * from "./src/metacensus/v1/auth.js";
export * from "./src/metacensus/v1/common.js";
export * from "./src/metacensus/v1/extraction.js";
export * from "./src/metacensus/v1/paper.js";
export * from "./src/metacensus/v1/prop.js";
export * from "./src/metacensus/v1/protocol.js";
export * from "./src/metacensus/v1/routes.js";
export * from "./src/metacensus/v1/topic.js";
export * from "./src/metacensus/v1/user.js";

// The route table, generated from the google.api.http annotations.
export * from "./src/route-manifest.js";

// Every generated file exports `protobufPackage`, so `export *` cannot pick
// one. They are identical.
export { protobufPackage } from "./src/metacensus/v1/common.js";
