package store

import (
	"context"
	"time"
)

// Store is the whole persistence seam. Every method is exactly one atomic unit
// of work: one Fabric chaincode invocation, one Postgres transaction. Nothing
// in this interface can be composed into a larger transaction, and nothing
// should be — see "A cross-operation transaction handle" in README.md.
//
// It is split into five interfaces so that a backend implementing three of
// them fails to satisfy Store at compile time. The alternative is what infra
// does today: eleven live routes wired to a handler that returns 501
// (core/api/server/server.go:176-209,293-297), which is a runtime discovery on
// a deployed system.
//
// Assert conformance at compile time, in the implementation's own package:
//
//	var _ store.Store = (*fabricStore)(nil)
//
// and behavioural conformance in its tests, with the suite in ./storetest.
// The first makes shape divergence impossible; the second makes behavioural
// divergence detectable. They are different guarantees and neither substitutes
// for the other.
type Store interface {
	Users
	Topics
	Props
	Extractions
	Attestations
}

// Users is user records and the credential material behind a login.
type Users interface {
	// UserCreate writes a new user.
	//
	// It is the one method whose Caller is the user being created: signup
	// happens before there is a session, and something still has to attribute
	// the record. The shared layer passes Caller.UserID equal to r.UserID, and
	// an implementation rejects a mismatch with CodeInvalid — otherwise one
	// authenticated user could mint another and the attestation would name the
	// wrong author. It is also where a self-signed key registration would
	// attach, which is the prerequisite for anything below the seam verifying
	// a signature at all.
	//
	// Pure function of (request, state): the id, the creation time and the
	// password hash with its salt are all minted by the caller. An
	// implementation that generates any of them has broken endorsement.
	//
	// Class A. Atomic: the user record and whatever uniqueness index backs the
	// email are one unit of work. Fabric does an AssertExistsNot on the user
	// key and on an "email:" key, then two Puts, in one write-set
	// (infra core/chaincode/contracts/contracts/users.go:45-88); Postgres does
	// one INSERT against a unique index on email (demo api/src/schema.ts:28).
	//
	// Returns CodeAlreadyExists if the id or the email is taken, CodeInvalid
	// if a required field is missing or an id is malformed.
	UserCreate(ctx context.Context, c Caller, r *UserCreateRequest) (*User, error)

	// UserGet reads one user by id. CodeNotFound if there is none.
	//
	// Class A. Read-only: a Fabric backend evaluates rather than submits.
	UserGet(ctx context.Context, c Caller, r *UserGetRequest) (*User, error)

	// UserList returns every user.
	//
	// Every user, with no limit and in no promised order. There is no
	// pagination in this interface and adding it is not a default — Fabric
	// offers an opaque bookmark rather than an offset, and a total requires an
	// unbounded scan, so offset pagination is an operation only one
	// implementer could express. See "Left out" in README.md for the question
	// that has to be answered before a cursor is admitted.
	//
	// Class A. Read-only.
	UserList(ctx context.Context, c Caller, r *UserListRequest) (*UserList, error)

	// CredentialGet returns the stored password hash for an email address.
	//
	// It takes no Caller: it runs before there is one. That is the whole
	// reason it is separate from UserGet rather than a flag on it.
	//
	// Class B — the one exception in this interface, and the exception has to
	// be argued rather than assumed. The remaining work is a bcrypt
	// comparison: it reads no state beyond the record already returned, and no
	// invariant depends on its outcome, so nothing about it needs endorsing.
	// A credit check would not qualify. A membership check would not qualify.
	// Any future Class B method passes that test explicitly and is added to
	// the roster in README.md, or the seam erodes upward one method at a time
	// with no diff ever showing it.
	//
	// What this replaces: infra sends the plaintext password into chaincode as
	// a transaction argument (core/chaincode/contracts/contracts/users.go:123,138;
	// core/shared/types/types.go:155-158). It is registered
	// as an evaluate rather than a submit, so it is not committed — but that
	// safety rests on a one-line registration list, and a plaintext password
	// submitted once is on an append-only ledger permanently. It also puts
	// bcrypt's cost on every endorsing peer. Comparing above the seam removes
	// both, and leaks nothing new: the hash is already in public world state.
	//
	// Plaintext must never be passed to an implementation, in either
	// direction. CodeNotFound if no user has that email — and the shared layer
	// must not let that distinction reach an unauthenticated caller as a
	// different response from a wrong password.
	CredentialGet(ctx context.Context, r *CredentialGetRequest) (*Credential, error)
}

// Topics is deliberative scopes.
type Topics interface {
	// TopicCreate writes a new topic. The caller is recorded as its author and
	// comes back from Attestation, though Topic itself has no author field.
	//
	// Pure function of (request, state): id and Created are minted above.
	// Class A. CodeAlreadyExists if the id is taken.
	TopicCreate(ctx context.Context, c Caller, r *TopicCreateRequest) (*Topic, error)

	// TopicGet reads one topic. Class A, read-only. CodeNotFound if there is none.
	TopicGet(ctx context.Context, c Caller, r *TopicGetRequest) (*Topic, error)

	// TopicList returns every topic, unlimited and unordered. Class A, read-only.
	//
	// This is the operation that decides open decision 3 in README.md: a
	// Fabric backend running one channel per topic has no way to answer it,
	// because Fabric does not do cross-channel queries. If per-topic channels
	// are real, this method is where that costs something.
	TopicList(ctx context.Context, c Caller, r *TopicListRequest) (*TopicList, error)
}

// Props is propositions and the votes on them.
type Props interface {
	// PropCreate writes a new proposition inside a topic.
	//
	// The author is Caller.UserID. PropCreateRequest deliberately has no
	// author field: a body field can be forgotten, an absent field cannot be
	// filled in wrong.
	//
	// Pure function of (request, state): id and Created are minted above.
	// Class A. Fabric does AssertExistsNot then Put in one transaction
	// (infra core/chaincode/contracts/contracts/prop.go:105-127); Postgres
	// does one INSERT with the supplied id. Both are idempotent under retry
	// because the id came from the caller.
	//
	// CodeAlreadyExists if the prop id is taken, CodeNotFound if the topic
	// does not exist.
	PropCreate(ctx context.Context, c Caller, r *PropCreateRequest) (*Prop, error)

	// PropGet reads one proposition. Class A, read-only.
	//
	// It does not return votes. Read those with VoteList. infra's chaincode
	// inlines them with a range scan per prop
	// (core/chaincode/contracts/contracts/prop.go:132-149,152-172) — an N+1
	// inside a single evaluate — and the wire contract already separates them
	// (proto/metacensus/v1/prop.proto, ListVotes).
	PropGet(ctx context.Context, c Caller, r *PropGetRequest) (*Prop, error)

	// PropList returns every proposition in one topic, without their votes,
	// unlimited and unordered. Class A, read-only.
	PropList(ctx context.Context, c Caller, r *PropListRequest) (*PropList, error)

	// VoteSet records the caller's position on a prop. It is an upsert on
	// (PropID, Caller.UserID): a recast replaces rather than adds.
	//
	// The voter is Caller.UserID; VoteSetRequest has no user field.
	//
	// Pure function of (request, state): Cast is minted above. Class A. This
	// is the operation both stores already express identically and the shape
	// every write in this interface follows — Postgres has it today as
	// INSERT ... ON CONFLICT (prop_id, user_id) DO UPDATE
	// (demo api/src/routes/topic.ts:299-313, backed by a unique index at
	// api/src/schema.ts:302-305), Fabric as AssertExists on the prop then a
	// read-modify-write on a vote:propId:userId composite key
	// (infra core/chaincode/contracts/contracts/prop.go:176-208).
	//
	// CodeNotFound if the prop does not exist. CodeConflict if a concurrent
	// write invalidated this one; retrying the identical request is safe.
	VoteSet(ctx context.Context, c Caller, r *VoteSetRequest) (*Vote, error)

	// VoteList returns every vote on one prop, unlimited and unordered.
	// Class A, read-only. A range scan on the composite prefix in Fabric;
	// a WHERE prop_id = $1 in Postgres.
	VoteList(ctx context.Context, c Caller, r *VoteListRequest) (*VoteList, error)
}

// Extractions is reviewers' passes over papers.
//
// A review is (topic, paper, protocol, reviewer). That natural key is the
// design decision this whole sub-interface turns on, and it is open decision 1
// in README.md. Adopting it makes POST /extraction-review unnecessary — that
// endpoint exists in demo only to resolve a surrogate serial id into something
// readable (api/src/routes/extraction.ts:8-45, api/src/schema.ts:267), and
// because nothing constrains (topic, paper, protocol, user) to be unique it
// returns a paginated list of reviews rather than one. It also makes two
// reviewers of one paper structurally distinct rather than colliding.
type Extractions interface {
	// ExtractionUpsert writes one reviewer's pass over one paper against one
	// protocol. The reviewer is Caller.UserID.
	//
	// Merge semantics, per element: elements present are written, elements
	// absent are left untouched, and only a Datum with Retract set removes
	// one. Omission cannot mean deletion, because neither store can tell "not
	// sent" from "deleted" when each element is its own key.
	//
	// Atomic across the whole fan-out. Every Datum is written or none is.
	// Fabric gets this free — N PutState calls inside one invocation are one
	// write-set, committed whole — and Postgres gets it by wrapping the N
	// upserts in one transaction. It is a stronger guarantee than the working
	// product provides today: demo inserts the review row and then loops N
	// unbatched inserts with no surrounding transaction
	// (api/src/routes/extraction.ts:56-76, and the same shape in the edit path
	// at 88-123), so a failure at element 4 of 9 leaves a partial extraction
	// behind an id that reads as complete.
	//
	// The atomicity requirement has teeth on the validation path too: a
	// request carrying one malformed Datum fails with CodeInvalid and writes
	// none of the others. The conformance suite checks exactly that, because
	// it is the cheap version of a fault injection that neither backend
	// otherwise offers.
	//
	// Pure function of (request, state): Recorded is minted above.
	// Class A. CodeInvalid for a malformed Datum, CodeNotFound if the topic,
	// paper or protocol does not exist.
	ExtractionUpsert(ctx context.Context, c Caller, r *ExtractionUpsertRequest) (*Extraction, error)

	// ExtractionGet reads one named reviewer's pass. The reviewer is a request
	// field, not the caller: reading someone else's extraction is the normal
	// case in an adjudication.
	//
	// Class A, read-only. A single lookup on the four-part natural key in both
	// stores. CodeNotFound if that reviewer has no pass over that paper.
	ExtractionGet(ctx context.Context, c Caller, r *ExtractionGetRequest) (*Extraction, error)

	// ExtractionList reads every reviewer's pass over one (topic, paper,
	// protocol).
	//
	// This is the adjudication read, and the reason a single Extraction cannot
	// be what the API returns for a paper. The wire contract's
	// GetExtraction(topic, paper, protocol) returns one DataExtraction
	// (proto/metacensus/v1/extraction.proto) while topics default to requiring
	// two reviews (demo api/src/schema.ts:70), so the contract cannot express
	// independent review at all — which is the reason the feature exists.
	//
	// Class A, read-only. A three-part prefix scan grouped by the reviewer
	// segment in Fabric; a WHERE on three ids grouped in memory in Postgres.
	// Returns an empty list, not CodeNotFound, when nobody has reviewed.
	ExtractionList(ctx context.Context, c Caller, r *ExtractionListRequest) (*ExtractionList, error)
}

// Attestations is what the store can say about a record it holds.
//
// This is where the asymmetry between the two stores is made explicit instead
// of hidden. Postgres is not expected to reach LevelEndorsed, now or later:
// cryptographic proof is Fabric's job and demo's job is the feature surface.
// The interface's contribution is that a caller always gets a definite answer
// about which it is talking to.
type Attestations interface {
	// Attestation returns what the store can prove about one record.
	//
	// Level is always populated above LevelUnspecified. Signed is whatever
	// Caller.Signed was at write time, returned verbatim — an implementation
	// that drops it has broken the one property provenance depends on, and the
	// conformance suite fails it.
	//
	// Class A, read-only. Fabric answers from GetHistoryForKey plus the
	// transaction's block position and returns LevelEndorsed with a tx id and
	// endorsers; Postgres returns LevelRecorded with an author and a
	// timestamp, honest about offering no proof.
	//
	// CodeNotFound if the subject does not exist. CodeInvalid if the Ref's
	// Kind and Key do not match.
	Attestation(ctx context.Context, c Caller, r *AttestationRequest) (*Attestation, error)
}

// --- Requests -------------------------------------------------------------
//
// Every request type carries a Digest: the hash the caller signed, over an
// explicit versioned field order computed above the seam. An implementation
// stores it and does not recompute it. Recomputing would require an agreed
// serialisation, which is exactly what does not exist yet.
//
// No request type carries a user id. Attribution comes from Caller.

// UserCreateRequest creates a user. ID, Created and PasswordHash are all
// minted above the seam.
type UserCreateRequest struct {
	// UserID is minted above. An implementation stores it and does not
	// substitute one of its own.
	UserID ID

	// Created is minted above, by the only clock in the system a replicated
	// executor may read.
	Created time.Time

	// PasswordHash is bcrypt output, salted above the seam. Never plaintext.
	PasswordHash []byte

	Name    string
	Email   string
	Country string

	Digest Digest
}

// UserGetRequest reads one user.
type UserGetRequest struct {
	UserID ID
}

// UserListRequest reads every user. It is empty, and stays empty.
//
// It carries no Ignored int — infra's list requests do
// (core/shared/types/types.go:195,228,326, "This is a placeholder and not
// respected"), and the reason is not that Fabric needs an argument.
// contractapi accepts a transaction taking only a context, and an empty struct
// marshals to {} in any case; the actual cause is a generic client helper that
// always marshals some request. Nothing forces a non-empty field, and
// pagination is the wrong thing to have put in it.
type UserListRequest struct{}

// UserList is every user. A wrapper rather than a bare slice, mirroring the
// wire's `items` envelope — and a place a cursor could one day go without
// changing a signature, if one is ever admitted.
type UserList struct {
	Items []User
}

// CredentialGetRequest looks up credential material by email. It carries no
// Caller and no password.
type CredentialGetRequest struct {
	Email string
}

// TopicCreateRequest creates a topic.
type TopicCreateRequest struct {
	TopicID     ID
	Created     time.Time
	Name        string
	Description string
	Digest      Digest
}

// TopicGetRequest reads one topic.
type TopicGetRequest struct {
	TopicID ID
}

// TopicListRequest reads every topic.
type TopicListRequest struct{}

// TopicList is every topic.
type TopicList struct {
	Items []Topic
}

// PropCreateRequest creates a proposition. It takes no author: that is
// Caller.UserID.
type PropCreateRequest struct {
	// TopicID is the scope, and it travels in the request rather than as a
	// separate positional argument. infra threads a *types.Id topicId through
	// handler, DataSource and client and then drops it — every request set
	// hardcodes the channel "metacensus.topics"
	// (core/shared/types/types.go:528 and 26 more). The scope is not a fossil:
	// per-topic channels are what strategy states and what channelByTopicId
	// implements (core/chaincode/contracts/topic.go:33,40-42). Keeping it in the
	// request lets a Fabric backend derive a channel from it and a Postgres
	// backend use it as a filter, without a second argument that can disagree
	// with the first.
	TopicID ID

	PropID      ID
	Created     time.Time
	Type        PropType
	Description string
	Digest      Digest
}

// PropGetRequest reads one proposition.
type PropGetRequest struct {
	TopicID ID
	PropID  ID
}

// PropListRequest reads every proposition in a topic.
type PropListRequest struct {
	TopicID ID
}

// PropList is every proposition in one topic, without votes.
type PropList struct {
	Items []Prop
}

// VoteSetRequest sets the caller's vote on a prop. It takes no user id: that
// is Caller.UserID.
type VoteSetRequest struct {
	TopicID     ID
	PropID      ID
	Position    Position
	Explanation string
	Citations   []Citation

	// Cast is minted above the seam.
	Cast time.Time

	Digest Digest
}

// VoteListRequest reads every vote on one prop.
type VoteListRequest struct {
	TopicID ID
	PropID  ID
}

// VoteList is every vote on one prop.
type VoteList struct {
	Items []Vote
}

// ExtractionUpsertRequest writes the caller's pass over one paper. It takes no
// reviewer id: that is Caller.UserID.
type ExtractionUpsertRequest struct {
	TopicID    ID
	PaperID    ID
	ProtocolID ID

	// Data is the elements to write. Absent elements are untouched; a Datum
	// with Retract set clears one.
	Data []Datum

	// Recorded is minted above the seam.
	Recorded time.Time

	Digest Digest
}

// ExtractionGetRequest reads one named reviewer's pass.
type ExtractionGetRequest struct {
	TopicID    ID
	PaperID    ID
	ProtocolID ID
	ReviewerID ID
}

// ExtractionListRequest reads every reviewer's pass over one paper.
type ExtractionListRequest struct {
	TopicID    ID
	PaperID    ID
	ProtocolID ID
}

// ExtractionList is every reviewer's pass over one paper, one entry per
// reviewer.
type ExtractionList struct {
	Items []Extraction
}

// AttestationRequest asks what the store can prove about one record.
type AttestationRequest struct {
	Subject Ref
}
