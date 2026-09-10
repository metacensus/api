// The MetaCensus API contract, as TypeScript. Types only, no runtime.
//
// Everything under ./src is generated; a new .proto file needs a line here.

export * from "./src/metacensus/v1/auth.js";
export * from "./src/metacensus/v1/common.js";
export * from "./src/metacensus/v1/prop.js";
export * from "./src/metacensus/v1/protocol.js";
export * from "./src/metacensus/v1/topic.js";
export * from "./src/metacensus/v1/user.js";
export * from "./src/route-manifest.js";

// Every generated file exports an identical `protobufPackage`, so `export *`
// cannot pick one.
export { protobufPackage } from "./src/metacensus/v1/common.js";
