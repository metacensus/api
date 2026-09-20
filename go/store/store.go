package store

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
)

// Caller is the participant a request is authenticated as: the login token's
// subject, resolved above this seam and never read from a body field. On a
// write it must equal the author the record's user_signature.signer_id names,
// or the store refuses the pair — see Unauthenticated for why the two are held
// to each other.
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
// Depth comes in two classes, one per kind of method: a write is
// full-operation — the remaining work has a state-dependent invariant, a parent
// that must exist or an id that must be unique, so the whole operation is
// endorsed — and a read is material-return, a function of state with no write
// invariant to endorse.
//
// What this seam deliberately omits is in doc.go.
type Store interface {
	// EnrollUser persists a new user and, with it, the signing key every later
	// write of theirs is verified against — trust on first use: nobody vouches
	// for the key, the store binds whatever the record carries inline. The
	// record is fully minted (id, recorded); its user_signature has an empty
	// signer_id and carries the enrolling key in public_key. password is
	// deliberately a separate argument: it must never sit inside a signed,
	// stored document.
	//
	// AlreadyExists if the email is taken or the minted id collides.
	// InvalidContent if signer_id is non-empty or public_key is absent.
	// SignatureInvalid (Fabric) if the inline signature does not stand.
	//
	// Email uniqueness is the store's, not a backend's: Postgres gets it from a
	// unique index, but Fabric world state is addressed by id alone and needs a
	// separate email→id key written in the same invocation.
	EnrollUser(ctx context.Context, record *v1.UserSigned, password string) error

	// Authenticate checks an email/password pair and returns the user it
	// belongs to, so the layer above can mint a session for that id. It writes
	// nothing.
	//
	// Unauthenticated if no such email exists or the password does not match —
	// one Kind for both, so a caller cannot probe which emails are enrolled.
	//
	// This is where the two backends part: a credential in Fabric world state
	// is visible to every endorsing peer and chaincode is a poor place to hash
	// one, so a real Fabric deployment likely keeps credentials off-ledger and
	// implements only the record half of this method.
	Authenticate(ctx context.Context, email, password string) (*v1.UserSigned, error)

	// GetUser returns one user by id. This is also key resolution's public
	// face: a verifier reads a user's enrolling public_key through it. GetSelf
	// above resolves to GetUser(caller.UserID). NotFound if absent.
	GetUser(ctx context.Context, id string) (*v1.UserSigned, error)

	// ListUsers returns every user; there is no pagination in this contract
	// (see doc.go). Fabric serves it from a range scan, and WorldState.Range
	// does not enter its reads into the tx read/write set — which is exactly why
	// a list is a read, never part of a write's atom.
	ListUsers(ctx context.Context) ([]*v1.UserSigned, error)

	// CreateTopic persists a new topic. The record is fully minted; caller must
	// equal user_signature.signer_id.
	//
	// Unauthenticated if caller is not the author. AlreadyExists on id
	// collision. SignatureInvalid (Fabric) if the signature does not stand.
	CreateTopic(ctx context.Context, caller Caller, record *v1.TopicSigned) error

	// GetTopic returns one topic by id. NotFound if absent.
	GetTopic(ctx context.Context, id string) (*v1.TopicSigned, error)

	// ListTopics returns every topic, as ListUsers does.
	ListTopics(ctx context.Context) ([]*v1.TopicSigned, error)

	// CreateProp persists a new prop under its content.topic_id. The record is
	// fully minted; caller must equal user_signature.signer_id.
	//
	// InvalidContent if topic_id is absent or names a topic that does not
	// exist. Unauthenticated if caller is not the author. AlreadyExists on id
	// collision. SignatureInvalid (Fabric) if the signature does not stand.
	CreateProp(ctx context.Context, caller Caller, record *v1.PropSigned) error

	// GetProp returns one prop, addressed by the (topic, prop) tuple rather
	// than an opaque composite key — how the two ids compose into a stored key
	// is each backend's business. NotFound if absent.
	GetProp(ctx context.Context, topicID, propID string) (*v1.PropSigned, error)

	// ListProps returns every prop in one topic.
	ListProps(ctx context.Context, topicID string) ([]*v1.PropSigned, error)

	// SetVote records the caller's position on one prop, keyed by
	// (prop, user): a second vote from the same user replaces the first rather
	// than adding to it. The record is fully minted; content.user_id must equal
	// both the signer and the caller, and content.topic_id/prop_id must name an
	// existing prop.
	//
	// InvalidContent if any of the three ids is absent, if user_id disagrees
	// with the signer, or if the prop does not exist. Unauthenticated if caller
	// is not the author. SignatureInvalid (Fabric) if the signature does not
	// stand.
	SetVote(ctx context.Context, caller Caller, record *v1.VoteSigned) error

	// ListVotes returns every vote on one prop — the only way to read votes;
	// there is no GetVote, because a vote is addressed only as one member's
	// position within a prop's full tally.
	ListVotes(ctx context.Context, topicID, propID string) ([]*v1.VoteSigned, error)
}
