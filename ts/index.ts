// The authenticated MetaCensus API contract (/metacensus/api/v1): generated
// types, route manifest, and typed Client. Everything under ./src is
// generated; a new .proto file needs a line here.
//
// See ts/README.md for why the public surface (./public.ts) is a separate
// entry point rather than merged in here.

export * from "./src/metacensus/v1/auth.js";
export * from "./src/metacensus/v1/common.js";
export * from "./src/metacensus/v1/prop.js";
export * from "./src/metacensus/v1/topic.js";
export * from "./src/metacensus/v1/user.js";
export * from "./src/route-manifest.js";

// `export *` can't pick one file's `protobufPackage` over another's.
export { protobufPackage } from "./src/metacensus/v1/common.js";

// Named, not `export *`: src/client.ts has one class per surface, and this
// entry point wants only the authenticated one.
export {
  Client,
  ApiError,
  type ClientRequest,
  type ClientResponse,
  type Transport,
} from "./src/client.js";
