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
// entry point wants only the authenticated one — plus its sugar.
//
// `ClientSigned` returns the `…Signed` envelopes; `Client` is the sugar over
// it, returning flat views ({id, recorded, ...content}) and taking a session
// signer for writes. The `…View` types are those flat shapes; `Signer` is the
// write seam. See ts/README.md, "The sugar Client".
export {
  ClientSigned,
  Client,
  ApiError,
  type Signer,
  type TopicView,
  type PropView,
  type VoteView,
  type UserView,
  type ClientRequest,
  type ClientResponse,
  type Transport,
} from "./src/client.js";
