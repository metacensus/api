package storetest

import (
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var createPaperCases = []storeCase{
	{
		name:    "persists the paper under its topic",
		promise: "CreatePaper persists a new paper under its content.topic_id; the record is fully minted.",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adaDraftsPaper},
		call:    adaSubmitsPaper,
		then:    []step{paperReadsBack},
	},
	{
		name:    "persists one publication in two topics",
		promise: "CreatePaper persists a new paper under its content.topic_id; the record is fully minted.",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adasPaperExists, adasOtherTopicExists, adaDraftsPaperInOtherTopic},
		call:    adaSubmitsPaper,
		then:    []step{paperReadsBack, storedPaperUnchanged},
	},
	{
		name:    "caller is not the author",
		promise: "Unauthenticated if callerID is not the author",
		given:   []step{adaEnrolled, bobEnrolled, adasTopicExists, publicationExists, adaDraftsPaper},
		call:    bobSubmitsPaper,
		want:    store.Unauthenticated,
		then:    []step{paperUnknown},
	},
	{
		name:    "content altered after signing",
		promise: "SignatureInvalid if the signature does not stand",
		hard:    true,
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, otherPublicationExists, adaDraftsPaper, paperAltered},
		call:    adaSubmitsPaper,
		want:    store.SignatureInvalid,
		then:    []step{paperUnknown},
	},
	{
		name:    "topic_id absent",
		promise: "InvalidContent if topic_id or publication_id is absent",
		given:   []step{adaEnrolled, publicationExists, adaDraftsPaperWithoutTopic},
		call:    adaSubmitsPaper,
		want:    store.InvalidContent,
	},
	{
		name:    "publication_id absent",
		promise: "InvalidContent if topic_id or publication_id is absent",
		given:   []step{adaEnrolled, adasTopicExists, adaDraftsPaperWithoutPublication},
		call:    adaSubmitsPaper,
		want:    store.InvalidContent,
		then:    []step{paperUnknown},
	},
	{
		name:    "topic does not exist",
		promise: "or names a record that does not exist",
		given:   []step{adaEnrolled, adaDraftsTopic, publicationExists, adaDraftsPaperInUnstoredTopic},
		call:    adaSubmitsPaper,
		want:    store.InvalidContent,
		then:    []step{paperUnknown},
	},
	{
		name:    "publication does not exist",
		promise: "or names a record that does not exist",
		given:   []step{adaEnrolled, adasTopicExists, publicationDrafted, adaDraftsPaper},
		call:    adaSubmitsPaper,
		want:    store.InvalidContent,
		then:    []step{paperUnknown},
	},
	{
		name:    "id is not a paper id",
		promise: "or if the id is not a paper id",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adaDraftsPaper, paperIDIsAPropID},
		call:    adaSubmitsPaper,
		want:    store.InvalidContent,
		then:    []step{paperUnknown},
	},
	{
		name:    "id collides within its topic",
		promise: "AlreadyExists on id collision",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adasPaperExists, otherPublicationExists, adaRedraftsPaperUnderSameID},
		call:    adaSubmitsPaper,
		want:    store.AlreadyExists,
		then:    []step{storedPaperUnchanged},
	},
	{
		name:    "the topic already includes the publication",
		promise: "or if the topic already includes the publication",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adasPaperExists, adaDraftsPaper},
		call:    adaSubmitsPaper,
		want:    store.AlreadyExists,
		then:    []step{paperUnknown, storedPaperUnchanged},
	},
}

var getPaperCases = []storeCase{
	{
		name:    "returns the paper as written",
		promise: readsAsWritten,
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adasPaperExists},
		call:    getPaper,
		then:    []step{gotPaper},
	},
	{
		name:    "no such paper",
		promise: "NotFound if absent",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adaDraftsPaper},
		call:    getPaper,
		want:    store.NotFound,
	},
	{
		name:    "the paper's id under another topic",
		promise: "GetPaper returns one paper, addressed by the (topic, paper) tuple",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adasPaperExists, adasOtherTopicExists},
		call:    getPaperUnderOtherTopic,
		want:    store.NotFound,
	},
}

var findPaperCases = []storeCase{
	{
		name:    "the topic's paper for the publication",
		promise: "FindPaper returns the paper by which a topic includes a publication.",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adasPaperExists},
		call:    findStoredPaper,
		then:    []step{foundStoredPaper},
	},
	{
		name:    "another topic includes it",
		promise: "NotFound if the topic does not include it.",
		given:   []step{adaEnrolled, adasTopicExists, publicationExists, adasPaperExists, adasOtherTopicExists},
		call:    findStoredPaperInOtherTopic,
		want:    store.NotFound,
	},
}

func (sc *scene) paperBy(p *participant, content *v1.Paper) *v1.PaperSigned {
	sc.t.Helper()
	interp, sig := sc.sign(p.key, p.keyID, content)
	return &v1.PaperSigned{Id: store.NewID(store.PaperID), Recorded: timestamppb.New(recordedAt), Content: content, Interpretation: interp, UserSignature: sig}
}

func aPaper(topic *v1.TopicSigned, publication *v1.Publication) *v1.Paper {
	return &v1.Paper{TopicId: topic.GetId(), PublicationId: publication.GetId()}
}

func adaDraftsPaper(sc *scene) {
	sc.paper = sc.paperBy(sc.ada, aPaper(sc.storedTopic, sc.publication))
}

func adaDraftsPaperInOtherTopic(sc *scene) {
	sc.paper = sc.paperBy(sc.ada, aPaper(sc.otherTopic, sc.storedPublication))
}

func adaDraftsPaperInUnstoredTopic(sc *scene) {
	sc.paper = sc.paperBy(sc.ada, aPaper(sc.topic, sc.storedPublication))
}

func adaDraftsPaperWithoutTopic(sc *scene) {
	sc.paper = sc.paperBy(sc.ada, &v1.Paper{PublicationId: sc.storedPublication.GetId()})
}

func adaDraftsPaperWithoutPublication(sc *scene) {
	sc.paper = sc.paperBy(sc.ada, &v1.Paper{TopicId: sc.storedTopic.GetId()})
}

func adasPaperExists(sc *scene) {
	adaDraftsPaper(sc)
	sc.must(adaSubmitsPaper(sc), "create Ada's paper")
	sc.storedPaper = sc.paper
}

// paperAltered points the signed paper at another publication that exists, so
// only the signature is wrong.
func paperAltered(sc *scene) { sc.paper.Content.PublicationId = sc.otherPublication.GetId() }

func paperIDIsAPropID(sc *scene) { sc.paper.Id = store.NewID(store.PropID) }

func adaRedraftsPaperUnderSameID(sc *scene) {
	sc.paper = sc.paperBy(sc.ada, aPaper(sc.storedTopic, sc.otherPublication))
	sc.paper.Id = sc.storedPaper.GetId()
}

func adaSubmitsPaper(sc *scene) error {
	return sc.s.CreatePaper(sc.ctx, sc.ada.user.GetId(), sc.paper)
}

func bobSubmitsPaper(sc *scene) error {
	return sc.s.CreatePaper(sc.ctx, sc.bob.user.GetId(), sc.paper)
}

func getPaper(sc *scene) (err error) {
	sc.got, err = sc.s.GetPaper(sc.ctx, sc.paper.GetContent().GetTopicId(), sc.paper.GetId())
	return err
}

func getPaperUnderOtherTopic(sc *scene) (err error) {
	sc.got, err = sc.s.GetPaper(sc.ctx, sc.otherTopic.GetId(), sc.storedPaper.GetId())
	return err
}

func findStoredPaper(sc *scene) (err error) {
	sc.got, err = sc.s.FindPaper(sc.ctx, sc.storedTopic.GetId(), sc.storedPublication.GetId())
	return err
}

func findStoredPaperInOtherTopic(sc *scene) (err error) {
	sc.got, err = sc.s.FindPaper(sc.ctx, sc.otherTopic.GetId(), sc.storedPublication.GetId())
	return err
}

func gotPaper(sc *scene)         { assertRecord(sc, sc.got, sc.paper) }
func foundStoredPaper(sc *scene) { assertRecord(sc, sc.got, sc.storedPaper) }

func paperReadsBack(sc *scene) {
	sc.t.Helper()
	sc.must(getPaper(sc), "read the paper back")
	gotPaper(sc)
}

func paperUnknown(sc *scene) {
	sc.t.Helper()
	_, err := sc.s.GetPaper(sc.ctx, sc.paper.GetContent().GetTopicId(), sc.paper.GetId())
	if store.KindOf(err) != store.NotFound {
		sc.t.Fatalf("the refused paper is readable: got %v\n  promise: %s", err, atomicity)
	}
}

func storedPaperUnchanged(sc *scene) {
	sc.t.Helper()
	got, err := sc.s.GetPaper(sc.ctx, sc.storedTopic.GetId(), sc.storedPaper.GetId())
	sc.must(err, "read the stored paper back")
	if !proto.Equal(got, sc.storedPaper) {
		sc.t.Fatalf("a refused paper overwrote the stored one\n  got:  %v\n  want: %v\n  promise: %s", got, sc.storedPaper, atomicity)
	}
}
