package store

import (
	"context"

	v1 "github.com/metacensus/api/go/metacensus/v1"
)

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
// A write's callerID is the session's authenticated user, resolved above this
// seam and never a body field; it must equal the owner the author signature's
// key_id resolves to, or the write is refused (Unauthenticated). That
// resolution is the store's — no record carries a verification key.
//
// What this seam deliberately omits is in doc.go.
type Store interface {
	// EnrollUser persists a new user and, with it, the signing key every later
	// write of theirs is verified against — trust on first use: nobody vouches
	// for the key, the store binds whatever the record was enrolled with. The
	// record is fully minted (id, recorded); publicKey is the enrolling key
	// (base64url SPKI DER) carried on the SignUpRequest, kept out of the stored
	// record so no later signature verifies against a key it carries itself. The
	// store binds it under user_signature.key_id (which must thumbprint it) as
	// the owner's first key. passwordHash is deliberately a separate argument:
	// it is the already-hashed credential — bcrypt mints its salt above this
	// seam, so the hash a backend stores is a pure function of (request, current
	// state), and the same string reaches every endorsing peer. The plaintext
	// never crosses the seam, so it is never an invocation argument and never
	// sits inside a signed, stored document.
	//
	// AlreadyExists if the email is taken or the minted id collides.
	// InvalidContent if publicKey is absent or key_id does not thumbprint it.
	// SignatureInvalid (Fabric) if the enrolling assertion does not stand.
	//
	// Email uniqueness is the store's, not a backend's: Postgres gets it from a
	// unique index, but Fabric world state is addressed by id alone and needs a
	// separate email→id key written in the same invocation.
	EnrollUser(ctx context.Context, record *v1.UserSigned, publicKey, passwordHash string) error

	// Credential returns the id and stored password hash of the user an email
	// belongs to, so the layer above can compare the hash and, on a match, mint
	// a session for that id. The comparison lives above the seam because bcrypt
	// verification takes the plaintext, which must never become an invocation
	// argument; the store hands back only the hash it stored. It writes nothing.
	//
	// Unauthenticated if no such email exists — the same Kind the layer above
	// returns for a hash mismatch, so no failure below or above the seam reveals
	// whether an email is enrolled.
	Credential(ctx context.Context, email string) (id, passwordHash string, err error)

	// GetUser returns one user by id. GetSelf above resolves to
	// GetUser(callerID). NotFound if absent. Key resolution (key_id → the
	// owner's enrolled key) is the store's own, from the key history it binds at
	// EnrollUser; it is not read off this record, which carries no key.
	GetUser(ctx context.Context, id string) (*v1.UserSigned, error)

	// ListUsers returns every user; no pagination (see doc.go).
	ListUsers(ctx context.Context) ([]*v1.UserSigned, error)

	// CreateTopic persists a new topic; the record is fully minted.
	//
	// Unauthenticated if callerID is not the author. AlreadyExists on id
	// collision. SignatureInvalid (Fabric) if the signature does not stand.
	CreateTopic(ctx context.Context, callerID string, record *v1.TopicSigned) error

	// GetTopic returns one topic by id. NotFound if absent.
	GetTopic(ctx context.Context, id string) (*v1.TopicSigned, error)

	// ListTopics returns every topic, as ListUsers does.
	ListTopics(ctx context.Context) ([]*v1.TopicSigned, error)

	// CreateProp persists a new prop under its content.topic_id; the record is
	// fully minted.
	//
	// InvalidContent if topic_id is absent or names a topic that does not
	// exist. Unauthenticated if callerID is not the author. AlreadyExists on id
	// collision. SignatureInvalid (Fabric) if the signature does not stand.
	CreateProp(ctx context.Context, callerID string, record *v1.PropSigned) error

	// GetProp returns one prop, addressed by the (topic, prop) tuple rather
	// than an opaque composite key — how the two ids compose into a stored key
	// is each backend's business. NotFound if absent.
	GetProp(ctx context.Context, topicID, propID string) (*v1.PropSigned, error)

	// ListProps returns every prop in one topic.
	ListProps(ctx context.Context, topicID string) ([]*v1.PropSigned, error)

	// SetVote records the caller's position on one prop, keyed by
	// (prop, user): a second vote from the same user replaces the first rather
	// than adding to it. The record is fully minted; content.user_id must equal
	// callerID (and so the owner the author key_id resolves to), and
	// content.topic_id/prop_id must name an existing prop.
	//
	// InvalidContent if any of the three ids is absent, if user_id disagrees
	// with the author, or if the prop does not exist. Unauthenticated if
	// callerID is not the author. SignatureInvalid (Fabric) if the signature
	// does not stand.
	SetVote(ctx context.Context, callerID string, record *v1.VoteSigned) error

	// ListVotes returns every vote on one prop — the only way to read votes;
	// there is no GetVote, because a vote is addressed only as one member's
	// position within a prop's full tally.
	ListVotes(ctx context.Context, topicID, propID string) ([]*v1.VoteSigned, error)
}
