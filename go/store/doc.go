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
// needs it. The question: is membership a signed record like the rest, or
// server-minted state derived from admission props?
//
// Key storage and resolution. How a signer_id + key_id resolves to a public
// key, and how a key history lets an old signature resolve against the key in
// force when it was made, is handled below this seam — each backend stores keys
// its own way (a Postgres table; Fabric world state). The interface exposes
// only GetUser, through which the enrolling key is read. The question left
// open above (which stored timestamp selects the key: recorded, or
// signing_time) is a backend-internal one this interface does not force.
//
// Pagination, sort and filter. No method takes page/limit/cursor or an order.
// Offset pagination is not implementable over a Fabric range scan, and
// TestNoPaginationFields guards the contract against it drifting back in. Lists
// return everything; when a route genuinely needs to page, that is a deliberate
// per-route decision, not a default this interface bakes in. The question: what
// bounded, seek-based cursor could both backends honor?
//
// MVCC. Fabric raises a read-conflict at commit that Postgres at READ COMMITTED
// never will. This pass does not model versioning, so a conflict is absorbed
// into Unavailable — the one Kind that already means "retry may work" — rather
// than given a Kind of its own that only one backend could ever return. The
// question the omission becomes: when optimistic concurrency does land (a
// compare-and-set on a record's recorded, say), does it earn a distinct
// Conflict Kind, and does Postgres then have to raise it too so the two
// backends stay symmetric?
//
// The institutional signature's runtime. common.proto now carries
// InstitutionalSignature (the reserved slot is spent), so no later wire break
// is needed to populate it; but computing and verifying it — its
// canonicalization, its digest over user_signature.value — is not defined here.
// The store persists the field as given and, this pass, checks it no harder
// than the Postgres backend checks a user signature. The question: is the
// countersignature minted above the seam and passed in the record like every
// other field, or produced by Fabric's endorsement itself, in which case the
// field is how Postgres imitates what Fabric gets for free?
package store
