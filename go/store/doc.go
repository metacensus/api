// Package store is the persistence seam of the MetaCensus API: the Go interface
// the shared server layer calls, and the two backends — metacensus/demo over
// Postgres, metacensus/infra over Hyperledger Fabric — implement. It replaces
// infra's DataSource prototype (core/api/server/server.go), which was
// Fabric-shaped (it leaked chaincode/tx into the seam and typed its arguments
// from a package that imports the Fabric SDK) and error-flat (every application
// failure reached the client as HTTP 500 with the raw backend string).
//
// The seam lives here, in metacensus/api, beside go/server and go/signing,
// because it is defined entirely in terms of the contract types and shares
// their compatibility story; infra's shared/types cannot host it (it imports
// the chaincode SDK), and a separate repo would split the interface from the
// messages it is written in. See Store for the rules that fix its shape.
//
// # What this seam deliberately leaves out
//
// Members. topic.proto's Member routes are implemented by no backend and carry
// no signature; adding EnrollMember/ListMembers now would design a signed
// membership record before its provenance is decided. Omitted until a backend
// needs it.
//
// Key storage and resolution. How a key_id resolves to a public key and its
// owner, and how a key history lets an old signature resolve against the key in
// force when it was made, is handled below this seam — each backend stores keys
// its own way (a Postgres table; Fabric world state). The enrolling key is bound
// at EnrollUser and never carried on a later record. Which stored timestamp
// selects the key from the rotation history — recorded, or the signature's own
// time — is a backend-internal choice this interface does not force. Revocation
// (retiring a key so it attributes no new record while its old ones stand) is
// undesigned; see the repository README's open questions.
//
// Pagination, sort and filter. No method takes page/limit/cursor or an order.
// Offset pagination is not implementable over a Fabric range scan, and
// TestNoPaginationFields guards the contract against it drifting back in. Lists
// return everything; when a route genuinely needs to page, that is a deliberate
// per-route decision, not a default this interface bakes in.
//
// MVCC. Fabric raises a read-conflict at commit that Postgres at READ COMMITTED
// never will. This pass does not model versioning, so a conflict is absorbed
// into Unavailable rather than given a Kind of its own that only one backend
// could ever return.
//
// The institutional signature's runtime. common.proto carries the
// institutional_signature (a second Signature, in the same format as the user's;
// the reserved slot is spent), so no later wire break is needed to populate it;
// but computing and verifying it — its digest over the user signature it nests
// over, its acceptance policy — is not defined here. The store persists the
// field as given and, this pass, checks it no harder than the Postgres backend
// checks a user signature.
package store
