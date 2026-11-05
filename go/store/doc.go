// Package store is the persistence seam of the MetaCensus API: the Go interface
// the shared server layer calls and every persistence backend implements. Every
// implementation runs go/store/storetest to prove it honours this contract.
//
// The seam lives here, in metacensus/api, beside go/server and go/signing,
// because it is defined entirely in terms of the contract types and shares
// their compatibility story; a separate repo would split the interface from the
// messages it is written in. See Store for the rules that fix its shape.
//
// # What this seam deliberately leaves out
//
// Members. topic.proto's Member routes carry no signature; adding
// EnrollMember/ListMembers now would design a signed membership record before
// its provenance is decided.
//
// Key storage and resolution. How a key_id resolves to a public key and its
// owner, and how a key history lets an old signature resolve against the key in
// force when it was made, is handled below this seam — each backend stores keys
// its own way. The enrolling key is bound at EnrollUser and never carried on a
// later record. Which stored timestamp selects the key from the rotation
// history — recorded, or the signature's own time — is a backend-internal
// choice this interface does not force. Revocation (retiring a key so it
// attributes no new record while its old ones stand) is undesigned; see the
// repository README's open questions.
//
// Pagination, sort and filter. No method takes page/limit/cursor or an order,
// and TestNoPaginationFields guards the contract against it drifting back in.
// Lists return everything; when a route genuinely needs to page, that is a
// deliberate per-route decision, not a default this interface bakes in.
//
// MVCC. This pass does not model versioning, so a conflict a backend detects at
// commit is absorbed into Unavailable rather than given a Kind of its own.
//
// The institutional signature's runtime. common.proto carries the
// institutional_signature (a second Signature, in the same format as the user's;
// the reserved slot is spent), so no later wire break is needed to populate it;
// but computing and verifying it — its digest over the user signature it nests
// over, its acceptance policy — is not defined here. The store persists the
// field as given and, this pass, defines no check on it.
package store
