package storetest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"time"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/signing"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Nanoseconds below a microsecond: a store that truncates reads back a
// different record.
var (
	recordedAt = time.Unix(1_700_000_000, 123_456_789).UTC()
	signedAt   = time.Unix(1_699_999_999, 987_654_321).UTC()
)

// participant is one person with a software passkey: their key, and the
// sign-up they enrol (or are about to) with it.
type participant struct {
	key       *ecdsa.PrivateKey
	keyID     string
	publicKey string // as SignUpRequest.public_key carries it
	hash      string // opaque to the store; never a real bcrypt hash
	user      *v1.UserSigned
}

func (sc *scene) newParticipant(name string) *participant {
	sc.t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	sc.must(err, "generate a key")
	p := &participant{key: key}
	p.keyID, err = signing.KeyID(&key.PublicKey)
	sc.must(err, "thumbprint the key")
	p.publicKey, err = signing.EncodePublicKey(&key.PublicKey)
	sc.must(err, "encode the key")

	id := sc.ids.id("user")
	p.hash = "opaque-hash-of-" + id
	content := &v1.User{Name: name, Email: sc.ids.email(), Country: "GB"}
	interp, sig := sc.sign(p.key, p.keyID, content)
	p.user = &v1.UserSigned{Id: id, Recorded: timestamppb.New(recordedAt), Content: content, Interpretation: interp, UserSignature: sig}
	return p
}

// sign makes a participant signature over content with key, naming keyID —
// normally the key's own; a different one forges an assertion under another
// person's key_id.
func (sc *scene) sign(key *ecdsa.PrivateKey, keyID string, content proto.Message) (*v1.Interpretation, *v1.Signature) {
	sc.t.Helper()
	interp := signing.Interpretation(content)
	at := timestamppb.New(signedAt)
	challenge, err := signing.UserChallenge(content, interp, keyID, at)
	sc.must(err, "compute the challenge")
	authData := signing.AuthenticatorData(RPID, signing.FlagUP|signing.FlagUV)
	assertion, err := signing.Assert(key, challenge, authData, signing.ClientData{Type: signing.TypeGet, Origin: Origin})
	sc.must(err, "assert")
	return interp, &v1.Signature{KeyId: keyID, Time: at, Assertion: assertion}
}

// topicBy, propBy and voteBy draft a fully-minted, correctly-signed record
// authored by p. A fault is drafted by passing the content it needs; the
// record is still signed over exactly that content, so the fault is the only
// one.

func (sc *scene) topicBy(p *participant, content *v1.Topic) *v1.TopicSigned {
	sc.t.Helper()
	interp, sig := sc.sign(p.key, p.keyID, content)
	return &v1.TopicSigned{Id: sc.ids.id("topic"), Recorded: timestamppb.New(recordedAt), Content: content, Interpretation: interp, UserSignature: sig}
}

func (sc *scene) propBy(p *participant, content *v1.Prop) *v1.PropSigned {
	sc.t.Helper()
	interp, sig := sc.sign(p.key, p.keyID, content)
	return &v1.PropSigned{Id: sc.ids.id("prop"), Recorded: timestamppb.New(recordedAt), Content: content, Interpretation: interp, UserSignature: sig}
}

func (sc *scene) voteBy(p *participant, content *v1.Vote) *v1.VoteSigned {
	sc.t.Helper()
	interp, sig := sc.sign(p.key, p.keyID, content)
	return &v1.VoteSigned{Recorded: timestamppb.New(recordedAt), Content: content, Interpretation: interp, UserSignature: sig}
}

func aTopic() *v1.Topic {
	return &v1.Topic{Name: "Conformance", Description: "A topic the conformance suite drafted."}
}

func aPropIn(topic *v1.TopicSigned) *v1.Prop {
	return &v1.Prop{TopicId: topic.GetId(), Type: v1.Prop_Statement, Description: "A prop the conformance suite drafted."}
}

func aVote(prop *v1.PropSigned, voter *participant, pos v1.Vote_Position) *v1.Vote {
	return &v1.Vote{TopicId: prop.GetContent().GetTopicId(), PropId: prop.GetId(), UserId: voter.user.GetId(), Position: pos}
}

func (sc *scene) countersign(userSig *v1.Signature, interp *v1.Interpretation) *v1.Signature {
	sc.t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	sc.must(err, "generate the institution key")
	keyID, err := signing.KeyID(&key.PublicKey)
	sc.must(err, "thumbprint the institution key")
	at := timestamppb.New(signedAt)
	challenge, err := signing.CountersignChallenge(userSig, interp, keyID, at)
	sc.must(err, "compute the countersign challenge")
	assertion, err := signing.Assert(key, challenge, signing.AuthenticatorData(RPID, signing.FlagUP), signing.ClientData{Type: signing.TypeCountersign})
	sc.must(err, "countersign")
	return &v1.Signature{KeyId: keyID, Time: at, Assertion: assertion}
}
