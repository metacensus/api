package store

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
)

// Caller is the participant a request is authenticated as: the login token's
// subject, resolved above this seam and never read from a body field. On a
// write it must equal the author the record's user_signature.signer_id names;
// the store refuses the pair when they disagree (Unauthenticated). A login
// token says who is connected, a signature says who authored — this type is one
// half of holding the two to each other.
type Caller struct {
	// UserID is the server-minted id of the signed-in user.
	UserID string
}

// Store is the seam between the shared API layer and whichever backend holds
// the data: metacensus/demo over Postgres, metacensus/infra over Hyperledger
// Fabric. It is the whole contract between them; nothing Fabric- or
// Postgres-shaped crosses it.
//
// Three rules fix its shape, and every method's contract is read against them:
//
//   - One method is one atomic unit of work — a chaincode invocation in Fabric,
//     a transaction in Postgres. The seam is drawn where a single call is a
//     single commit; nothing composes two methods into one atom.
//
//   - Every method is a pure function of (request, current state). Ids,
//     timestamps and the enrolling key are minted above and arrive already set
//     on the record; a backend that generated any of them would make two
//     endorsing peers disagree. Writes therefore take a fully-minted
//     *v1.XSigned and return only an error — the server already holds the
//     record it echoes to the client.
//
//   - Verification is inherited. A value checked at the write boundary is
//     trusted by every later read; reads resolve state, they do not re-verify.
//
// Depth comes in two classes, named per method:
//
//   - full-operation (writes): the remaining work has a state-dependent
//     invariant — a parent must exist, an id must be unique — so the whole
//     operation must be endorsed. This is the default.
//
//   - material-return (reads): the answer is a function of state with no write
//     invariant to endorse, so returning the material is admissible. Every read
//     here is material-return.
//
// What this seam deliberately omits, and the question each omission becomes, is
// in doc.go.
type Store interface {
	// EnrollUser persists a new user and, with it, the signing key every later
	// write of theirs is verified against — trust on first use: nobody vouches
	// for the key, the store binds whatever the record carries inline. The
	// record is fully minted (id, recorded); its user_signature has an empty
	// signer_id and carries the enrolling key in public_key. password is
	// deliberately a separate argument: it must never sit inside a signed,
	// stored document. full-operation.
	//
	// AlreadyExists if the email is taken or the minted id collides.
	// InvalidContent if signer_id is non-empty or public_key is absent.
	// SignatureInvalid (Fabric) if the inline signature does not stand.
	//
	//   PG:     one tx — INSERT the user, the credential, and the key row;
	//           a unique index on email turns a race into AlreadyExists.
	//           Signature check is soft.
	//   Fabric: one invocation — verify the inline signature (PublicKeyOf),
	//           AssertExistsNot the user key, PutState the record and the
	//           enrolled key. Email uniqueness needs an email→id index key,
	//           since world state is addressed by id alone.
	EnrollUser(ctx context.Context, record *v1.UserSigned, password string) error

	// Authenticate checks an email/password pair and returns the user it
	// belongs to, so the layer above can mint a session for that id. It writes
	// nothing. material-return.
	//
	// Unauthenticated if no such email exists or the password does not match —
	// one Kind for both, so a caller cannot probe which emails are enrolled.
	//
	//   PG:     SELECT by email, compare the password hash.
	//   Fabric: read the email→id index, then the user; compare the hash.
	//           Awkward — chaincode is a poor place for password hashing, and a
	//           credential in world state is visible to every endorsing peer.
	//           A real Fabric deployment likely keeps credentials off-ledger and
	//           implements only the record half of this method.
	Authenticate(ctx context.Context, email, password string) (*v1.UserSigned, error)

	// GetUser returns one user by id. This is also key resolution's public
	// face: a verifier reads a user's enrolling public_key through it. GetSelf
	// above resolves to GetUser(caller.UserID). material-return. NotFound if
	// absent.
	//
	//   PG:     SELECT by primary key.
	//   Fabric: GetState by id.
	GetUser(ctx context.Context, id string) (*v1.UserSigned, error)

	// ListUsers returns every user; there is no pagination in this contract
	// (see doc.go). material-return.
	//
	//   PG:     SELECT all, ordered by recorded.
	//   Fabric: a range scan over the user key prefix. Note WorldState.Range
	//           does not enter its reads into the tx read/write set, which is
	//           exactly why listing is a read, never part of a write's atom.
	ListUsers(ctx context.Context) ([]*v1.UserSigned, error)

	// CreateTopic persists a new topic. The record is fully minted; caller must
	// equal user_signature.signer_id. full-operation.
	//
	// Unauthenticated if caller is not the author. AlreadyExists on id
	// collision. SignatureInvalid (Fabric) if the signature does not stand.
	//
	//   PG:     one INSERT.
	//   Fabric: verify the author's signature against the key resolved from
	//           signer_id + key_id, AssertExistsNot the id, PutState.
	CreateTopic(ctx context.Context, caller Caller, record *v1.TopicSigned) error

	// GetTopic returns one topic by id. material-return. NotFound if absent.
	GetTopic(ctx context.Context, id string) (*v1.TopicSigned, error)

	// ListTopics returns every topic. material-return. (Range/SELECT as
	// ListUsers.)
	ListTopics(ctx context.Context) ([]*v1.TopicSigned, error)

	// CreateProp persists a new prop under its content.topic_id. The record is
	// fully minted; caller must equal user_signature.signer_id. full-operation.
	//
	// InvalidContent if topic_id is absent or names a topic that does not
	// exist. Unauthenticated if caller is not the author. AlreadyExists on id
	// collision. SignatureInvalid (Fabric) if the signature does not stand.
	//
	//   PG:     one tx — a foreign key to topic makes the parent check the
	//           database's, turning a missing topic into InvalidContent.
	//   Fabric: verify the signature, AssertExists the topic, AssertExistsNot
	//           the prop id, PutState under a (topic, prop) composite key.
	CreateProp(ctx context.Context, caller Caller, record *v1.PropSigned) error

	// GetProp returns one prop, addressed by the (topic, prop) tuple rather
	// than an opaque composite key — how the two ids compose into a stored key
	// is each backend's business. material-return. NotFound if absent.
	//
	//   PG:     SELECT by (topic_id, id).
	//   Fabric: GetState on the (topic, prop) composite key.
	GetProp(ctx context.Context, topicID, propID string) (*v1.PropSigned, error)

	// ListProps returns every prop in one topic. material-return.
	//
	//   PG:     SELECT where topic_id = $1.
	//   Fabric: a partial-composite-key range scan under the topic.
	ListProps(ctx context.Context, topicID string) ([]*v1.PropSigned, error)

	// SetVote records the caller's position on one prop, keyed by
	// (prop, user): a second vote from the same user replaces the first rather
	// than adding to it. The record is fully minted; content.user_id must equal
	// both the signer and the caller, and content.topic_id/prop_id must name an
	// existing prop. full-operation.
	//
	// InvalidContent if any of the three ids is absent, if user_id disagrees
	// with the signer, or if the prop does not exist. Unauthenticated if caller
	// is not the author. SignatureInvalid (Fabric) if the signature does not
	// stand.
	//
	//   PG:     one INSERT ... ON CONFLICT (prop_id, user_id) DO UPDATE; a
	//           foreign key to prop makes the parent check the database's.
	//   Fabric: verify the signature, AssertExists the prop, PutState under the
	//           (prop, user) composite key — the same key on a re-vote
	//           overwrites, which is the intended replace.
	SetVote(ctx context.Context, caller Caller, record *v1.VoteSigned) error

	// ListVotes returns every vote on one prop — the only way to read votes;
	// there is no GetVote, because a vote is addressed only as one member's
	// position within a prop's full tally. material-return.
	//
	//   PG:     SELECT where prop_id = $1.
	//   Fabric: a partial-composite-key range scan under the prop.
	ListVotes(ctx context.Context, topicID, propID string) ([]*v1.VoteSigned, error)
}
