package store

import "time"

// ID is an opaque identifier, minted above the seam and never by an
// implementation. The two backends mint incompatible formats — demo integer
// serials, infra prefixed UUIDs — so this ratifies neither. An implementation
// may reject an ID whose shape it cannot store; it may not replace one.
type ID string

// Kind names the sort of thing a Ref points at.
type Kind string

const (
	KindUser       Kind = "user"
	KindTopic      Kind = "topic"
	KindProp       Kind = "prop"
	KindVote       Kind = "vote"
	KindExtraction Kind = "extraction"

	// Papers, protocols and protocol elements are referenced by this
	// interface — an extraction is keyed by a paper and a protocol, and a
	// Datum by a protocol element — but none of them is written through it.
	// They have Kinds so an id can be minted for the right sort of thing, and
	// no Ref constructor, because there is nothing here to attest to yet. When
	// one is admitted it comes with its own constructor and its own answers to
	// the seven questions in README.md.
	KindPaper           Kind = "paper"
	KindProtocol        Kind = "protocol"
	KindProtocolElement Kind = "protocolElement"
)

// Ref names one record by its natural key. It exists so an Error and an
// Attestation can say what they are about without a per-resource type, and so
// that neither has to carry a surrogate id no natural-key store could mint.
//
// Key holds the segments in the order the Kind fixes, which is the order the
// constructors below take them.
type Ref struct {
	Kind Kind
	Key  []ID
}

// UserRef identifies one user.
func UserRef(user ID) Ref { return Ref{Kind: KindUser, Key: []ID{user}} }

// TopicRef identifies one topic.
func TopicRef(topic ID) Ref { return Ref{Kind: KindTopic, Key: []ID{topic}} }

// PropRef identifies one prop within a topic.
func PropRef(topic, prop ID) Ref { return Ref{Kind: KindProp, Key: []ID{topic, prop}} }

// VoteRef identifies one user's vote on one prop. A vote has no id of its own:
// (prop, user) is the key, which is what makes a recast a replacement.
func VoteRef(topic, prop, user ID) Ref {
	return Ref{Kind: KindVote, Key: []ID{topic, prop, user}}
}

// ExtractionRef identifies one reviewer's pass over one paper against one
// protocol. The reviewer is a key segment, not a field: that is what stops two
// reviewers colliding and what makes an anonymous extraction unrepresentable.
func ExtractionRef(topic, paper, protocol, reviewer ID) Ref {
	return Ref{Kind: KindExtraction, Key: []ID{topic, paper, protocol, reviewer}}
}

// String renders a Ref for logs and error messages. It is not a storage key:
// how a backend lays out its keys is entirely below the seam.
func (r Ref) String() string {
	s := string(r.Kind)
	for _, k := range r.Key {
		s += "/" + string(k)
	}
	return s
}

// Equal reports whether two Refs name the same record.
func (r Ref) Equal(o Ref) bool {
	if r.Kind != o.Kind || len(r.Key) != len(o.Key) {
		return false
	}
	for i := range r.Key {
		if r.Key[i] != o.Key[i] {
			return false
		}
	}
	return true
}

// Caller is the authenticated principal a method acts as.
//
// It is built from the session by the shared layer and is never parsed from a
// request body: a client does not get to assert who it is. No request type in
// this package carries a user id, so authorship cannot be forgotten the way
// demo's data_extraction_review.user_id is forgotten today — the column exists
// (api/src/schema.ts:274) and no handler sets it (api/src/routes/extraction.ts:58-62),
// so every extraction in the working product is anonymous while topics default
// to requiring two reviews (api/src/schema.ts:70). Structurally absent beats
// conventionally ignored.
type Caller struct {
	// UserID is the authenticated user. Required on every method that takes a
	// Caller; an implementation must reject a zero UserID with CodeUnauthenticated
	// rather than writing an unattributed record.
	UserID ID

	// Signed is the caller's signature over Request.Digest.
	//
	// Nil means unsigned, which is legal today and is the thing that has to
	// stop being legal before the ledger proves anything about a person: the
	// API holds one Fabric identity for every user
	// (infra core/api/fabric/client/config.go:41-69, client/client.go:17-28),
	// so a chaincode GetClientIdentity() names the API server on every
	// transaction. Until a caller signature travels down intact, the ledger
	// records that the API server asserted a vote.
	//
	// An implementation stores whatever it is given, verbatim, and returns it
	// from Attestation. It does not require it, and it does not treat a valid
	// signature as authorization.
	Signed *Signature
}

// Signature is a caller's statement over a Digest.
//
// Verifying it is a separate act from carrying it, and the two live on
// opposite sides of the seam — see the "Two things called verification"
// section of README.md. The short form: the crypto check (does this signature
// verify over this digest with this key?) is stateless and may run anywhere as
// a fail-fast; the authoritative gate (is this key bound to this user?) reads
// persisted state and runs only where endorsement makes it non-bypassable.
type Signature struct {
	// PersonalKeyID names the key the person signed with.
	PersonalKeyID string

	// OrgKeyID is set only when acting in an institutional capacity.
	//
	// Capacity is stored, not derived. A vote cast as an FDA official stays
	// stamped FDA after the person joins a sponsor, so this is written with
	// the action and never recomputed from the user's current affiliation.
	OrgKeyID string

	// Alg names the signature algorithm.
	Alg string

	// Bytes is the signature itself.
	Bytes []byte
}

// Equal reports whether two signatures are identical. Used by the conformance
// suite to check that an implementation carried one down intact.
func (s *Signature) Equal(o *Signature) bool {
	if s == nil || o == nil {
		return s == o
	}
	if s.PersonalKeyID != o.PersonalKeyID || s.OrgKeyID != o.OrgKeyID || s.Alg != o.Alg {
		return false
	}
	if len(s.Bytes) != len(o.Bytes) {
		return false
	}
	for i := range s.Bytes {
		if s.Bytes[i] != o.Bytes[i] {
			return false
		}
	}
	return true
}

// Digest is what a caller signs: a hash the shared layer computes over an
// explicit, versioned field order.
//
// It is deliberately not "the serialised request". Relying on the byte
// stability of any serialiser across Go and TypeScript is the trap that
// produced infra's two mutually unparseable encodings of one value —
// core/chaincode/contracts/types emits an id as "prop:<uuid>" while
// core/shared/types has no marshaller and emits {"Prefix":…,"Suffix":…}. Two
// encodings of one value means signer and verifier compute different digests
// and every signature fails.
//
// Where the canonical digest is computed, and by whom, is open decision 4 in
// README.md. This package holds the type, not the algorithm: computing it here
// would make Go the specification, which is the failure the contract exists to
// avoid.
type Digest [32]byte

// Zero reports whether no digest was supplied.
func (d Digest) Zero() bool { return d == Digest{} }

// User is a person. It carries no credential material: see Credential.
type User struct {
	ID      ID
	Name    string
	Email   string
	Country string
	Created time.Time
}

// Credential is the material CredentialGet returns so the shared layer can
// compare a password against it. It is the only Class B return in this
// interface — see the depth classes in README.md.
type Credential struct {
	UserID ID

	// PasswordHash is the stored bcrypt hash, including its embedded salt.
	// Plaintext never reaches an implementation, in either direction.
	PasswordHash []byte
}

// Topic is one deliberative scope. In Fabric it is intended to become a
// channel of its own; in Postgres it is a row and a filter. Which of those is
// real is open decision 3 in README.md.
// The wire's Topic carries no author (proto/metacensus/v1/topic.proto), and
// neither does this one. The caller who created it is still recorded — it
// comes back from Attestation, which is where authorship the resource does not
// itself publish belongs.
type Topic struct {
	ID          ID
	Name        string
	Description string
	Created     time.Time
}

// PropType is what a proposition would do if it passed. The values mirror
// Prop.Type in proto/metacensus/v1/prop.proto; the mapping between the two is
// the wire layer's business, not an implementation's.
type PropType int

const (
	PropTypeUnspecified PropType = iota
	PropTypeStatement
	PropTypeTopicQuestion
	PropTypeTopicChange
	PropTypeUserAdmit
	PropTypeUserExpulse
	PropTypePaperExtractionComplete
	PropTypePaperIncludeMetaAnalysis
)

// Prop is a motion a topic's members vote on.
type Prop struct {
	TopicID     ID
	ID          ID
	AuthorID    ID
	Type        PropType
	Description string
	Created     time.Time
}

// Position is one member's stance on a prop.
type Position int

const (
	PositionUnspecified Position = iota
	PositionFor
	PositionAgainst
	PositionAbstain
)

// Citation is a half-open character range in a prop's description.
type Citation struct {
	Start uint32
	End   uint32
}

// Vote is one member's position on one prop, keyed by (prop, user).
type Vote struct {
	TopicID     ID
	PropID      ID
	UserID      ID
	Position    Position
	Explanation string
	Citations   []Citation
	Cast        time.Time
}

// SourceRect is one highlight rectangle in a PDF viewer's coordinate space.
type SourceRect struct {
	X, Y, W, H float64
}

// SourcePage is the highlighted rectangles on one page of a PDF.
type SourcePage struct {
	Page  int32
	Rects []SourceRect
}

// Datum is one answered protocol element within an extraction.
type Datum struct {
	// ProtocolElementID is required. An implementation rejects an empty one
	// with CodeInvalid, and rejects the whole call: see ExtractionUpsert.
	ProtocolElementID ID

	// Value is the extracted value.
	Value string

	// Source is where in the PDF the value was read from. Empty when the value
	// was typed rather than selected.
	Source []SourcePage

	// Retract clears this element.
	//
	// Absence means untouched; only an explicit Retract removes. Per-element
	// keys mean omission cannot mean deletion — a Fabric range scan cannot
	// tell "not sent" from "deleted" — so retraction has to be said. On a
	// ledger this writes a tombstone: the original value stays legible in
	// history forever, which for a provenance system is a feature. Whether a
	// datum should be removable at all is open decision 6 in README.md.
	Retract bool
}

// Extraction is one reviewer's pass over one paper against one protocol.
//
// The reviewer is part of the identity, not a field on it. That is the
// difference between this and the merged wire contract, where DataExtraction
// is keyed by (topic, paper, protocol) alone (proto/metacensus/v1/extraction.proto)
// and two reviewers of one paper have no way to both exist — while independent
// review is the reason the feature exists.
type Extraction struct {
	TopicID    ID
	PaperID    ID
	ProtocolID ID
	ReviewerID ID
	Data       []Datum
	Recorded   time.Time
}

// Ref returns the natural key of this extraction.
func (e Extraction) Ref() Ref {
	return ExtractionRef(e.TopicID, e.PaperID, e.ProtocolID, e.ReviewerID)
}

// AttestationLevel is how strong a claim the store can make about a record.
//
// It is always populated, so a caller that needs proof asks for it and gets a
// definite answer from every backend, rather than inferring it from a nil
// field that only one backend ever fills. Asymmetry between the two stores is
// real; this makes it a value rather than an absence.
type AttestationLevel int

const (
	// LevelUnspecified is never returned by a conforming implementation. It
	// exists so that a zero value is visibly wrong rather than quietly weak.
	LevelUnspecified AttestationLevel = iota

	// LevelRecorded: the store holds an author and a time it was told.
	// Nothing prevents the operator rewriting it. This is what Postgres
	// offers, and demo is not expected to reach higher.
	LevelRecorded

	// LevelCommitted: written into an append-only log at a known position.
	LevelCommitted

	// LevelEndorsed: committed, and independently validated by named peers
	// under an endorsement policy. Only this level is evidence to a third
	// party.
	LevelEndorsed
)

// Attestation is what the store can say about a record it holds.
type Attestation struct {
	// Level is always set to something above LevelUnspecified.
	Level AttestationLevel

	// Subject is the record attested to.
	Subject Ref

	// Author is the Caller.UserID the record was written under.
	Author ID

	// Recorded is the time the record carries, which the caller minted.
	Recorded time.Time

	// TxID and Height are set at LevelCommitted and above.
	TxID   string
	Height uint64

	// Endorsers is set at LevelEndorsed.
	Endorsers []string

	// Signed is the caller signature presented at write time, returned
	// verbatim, or nil if there was none. An implementation that drops it has
	// broken the one property provenance depends on.
	Signed *Signature
}
