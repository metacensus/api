// Package store is the persistence seam between the shared MetaCensus API
// layer and a backend that implements it: Postgres in metacensus/demo,
// Hyperledger Fabric in metacensus/infra.
//
// It is a definition, not an implementation. Nothing here talks to a store,
// and nothing here may: the package imports only the standard library, and
// TestPackageImportsOnlyTheStandardLibrary keeps it that way. A Fabric, SQL or
// HTTP import in this package would make one implementer's constraints part of
// the definition every implementer answers to.
//
// # The rule
//
// One interface method is one atomic unit of work in the store — a chaincode
// invocation in Fabric, a transaction in Postgres. Below the seam, every
// operation is a pure function of (request, current state).
//
// The two clauses depend on each other. The first fixes the depth: a
// key-level interface would put each read and write in its own Fabric
// invocation, which breaks every existence invariant into a race and turns one
// extraction into N block commits. The second is what makes the first
// survivable under endorsement: several peers execute the same invocation
// independently and their write-sets must match byte for byte, so nothing
// below the seam may read a clock, draw randomness, or mint an id.
//
// "One method per HTTP route" is not the rule. It coincides with the rule
// today and stops coinciding the moment a route needs two units of work, or a
// unit of work serves two routes.
//
// # Determinism: what the caller must resolve
//
// Every value a deterministic replicated executor cannot produce is minted
// above this seam and arrives as request data. That includes ids, timestamps,
// password salts and hashes, and any nonce. Implementations generate none of
// them: an implementation that stamps its own Created, or mints its own id
// when the request supplies one, has broken endorsement whether or not it
// fails on a single-peer network today.
//
// This is already the convention in metacensus/infra, where the ids and
// clock reads happen in the HTTP handler (core/api/server/prop.go:22,42,
// core/api/server/user.go:30,35,47, core/api/server/vote.go:56) and the type
// comment says so in as many words: "Set by server (non-deterministic)",
// core/shared/types/types.go:172. This package promotes it from convention to
// contract, because a convention has no failure mode a reviewer can point at.
//
// The conformance suite in ./storetest checks the observable half of it: what
// the caller minted is what comes back.
//
// # What sits on each side
//
// The full placement rule, its named near-misses and the depth classes are in
// README.md next to this file. In summary:
//
//   - Non-determinism — above, all of it.
//   - Well-formedness — both. Above as a courtesy that produces a good error,
//     below as the authoritative check. Never only above.
//   - State-dependent invariants (does it exist, has this user voted, is the
//     balance sufficient) — below, inside the same unit of work as the write.
//     Checked above, they are a TOCTOU race and unendorsed.
//   - Session authentication — above. The store never sees a token.
//   - Storage layout, keys, transaction mechanics, submit versus evaluate —
//     below, invisibly.
//   - HTTP status mapping and response shaping — above, from a typed Code.
//     Implementations return errors, never HTTP concepts.
//
// # Three type representations, two mappings
//
// The types here are the middle one:
//
//   - Wire: the generated types in ../metacensus/v1, from proto/. Owned by
//     neither backend. This is the neutral authority.
//   - Store: this package. No JSON tags, no serialisation, no driver imports.
//     Never crosses a wire.
//   - Storage-internal: ledger JSON in Fabric chaincode, SQL rows in
//     Postgres. Each backend's private business.
//
// One type spanning all three is what metacensus/infra has today, where
// TopicGetResponse.Topic is literally typed as TopicCreateRequest
// (core/shared/types/types.go:224,233) and the shared types import the Fabric
// chaincode SDK (core/shared/types/types.go:8-9, which is why a Postgres
// backend could not import that package at all). The cost of three
// representations is two mappings. The thing bought is that the wire format
// stops being answerable to one store's key layout.
//
// # What this package does not do
//
// It does not serve HTTP, does not verify signatures, does not decide
// authorization, and does not implement anything. It also does not cover
// object storage, literature search, offset pagination, filtering or sorting —
// see the "Left out" section of README.md, where each omission is stated as
// the question that has to be answered before it is admitted.
package store
