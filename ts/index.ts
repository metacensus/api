// The authenticated MetaCensus API contract (/metacensus/api/v1), as
// TypeScript: the generated types, the route manifest, and the typed Client.
// Everything under ./src is generated; a new .proto file needs a line here.
//
// The public surface is `@metacensus/api/public`, in ./public.ts. It is not
// merged in here: `protobufPackage` names a package, so a barrel spanning two
// could only export one of them under that name, and the point of the second
// entry point is that a consumer of one surface does not acquire the other.

export * from "./src/metacensus/v1/auth.js";
export * from "./src/metacensus/v1/common.js";
export * from "./src/metacensus/v1/prop.js";
export * from "./src/metacensus/v1/topic.js";
export * from "./src/metacensus/v1/user.js";
export * from "./src/route-manifest.js";

// Every generated file exports an identical `protobufPackage`, so `export *`
// cannot pick one.
export { protobufPackage } from "./src/metacensus/v1/common.js";

// Named rather than `export *`: src/client.ts holds a class per surface, and
// this entry point is the authenticated one. The transport envelope is shared
// deliberately; that file's header says why.
export {
  Client,
  ApiError,
  type ClientRequest,
  type ClientResponse,
  type Transport,
} from "./src/client.js";
