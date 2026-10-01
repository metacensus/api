package storetest

import (
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
)

var createTopicCases = []storeCase{
	// Refused before any state is consulted.
	{
		name:    "error - Unauthenticated: caller is not the author",
		promise: "Unauthenticated if callerID is not the author",
		given:   []step{adaEnrolled, bobEnrolled, adaDraftsTopic},
		call:    bobSubmitsTopic,
		want:    store.Unauthenticated,
		then:    []step{topicUnknown},
	},
	// Omitted: a key_id that resolves to no enrolled key — store.go says
	// Unauthenticated, errors.go says SignatureInvalid.
	{
		name:    "error - SignatureInvalid: content altered after signing",
		promise: "SignatureInvalid (Fabric) if the signature does not stand",
		hard:    true,
		given:   []step{adaEnrolled, adaDraftsTopic, topicAltered},
		call:    adaSubmitsTopic,
		want:    store.SignatureInvalid,
		then:    []step{topicUnknown},
	},
	{
		name:    "error - SignatureInvalid: asserted by a key other than the one key_id names",
		promise: "SignatureInvalid (Fabric) if the signature does not stand",
		hard:    true,
		given:   []step{adaEnrolled, bobDrafted, adaDraftsTopic, topicAssertedByBobsKey},
		call:    adaSubmitsTopic,
		want:    store.SignatureInvalid,
		then:    []step{topicUnknown},
	},

	// Accepted.
	{
		name:    "success - persists the topic",
		promise: "CreateTopic persists a new topic; the record is fully minted.",
		given:   []step{adaEnrolled, adaDraftsTopic},
		call:    adaSubmitsTopic,
		then:    []step{topicReadsBack},
	},
	{
		name:    "success - persists an institutional signature as given",
		promise: "The store persists the field as given",
		given:   []step{adaEnrolled, adaDraftsTopic, topicCountersigned},
		call:    adaSubmitsTopic,
		then:    []step{topicReadsBack},
	},

	// Refused against stored state.
	{
		name:    "error - AlreadyExists: id collides",
		promise: "AlreadyExists on id collision",
		given:   []step{adaEnrolled, adasTopicExists, adaRedraftsTopicUnderSameID},
		call:    adaSubmitsTopic,
		want:    store.AlreadyExists,
		then:    []step{storedTopicUnchanged},
	},
	// Omitted: an empty id has no Kind stated.
}

var getTopicCases = []storeCase{
	{
		name:    "success - returns the topic as written",
		promise: readsAsWritten,
		given:   []step{adaEnrolled, adasTopicExists},
		call:    getTopic,
		then:    []step{gotTopic},
	},
	{
		name:    "error - NotFound: no such id",
		promise: "GetTopic returns one topic by id. NotFound if absent.",
		given:   []step{adaEnrolled, adaDraftsTopic},
		call:    getTopic,
		want:    store.NotFound,
	},
}

var listTopicsCases = []storeCase{
	{
		name:    "success - lists each topic once",
		promise: "ListTopics returns every topic, as ListUsers does.",
		given:   []step{adaEnrolled, adasTopicExists, adasOtherTopicExists},
		call:    listTopics,
		then:    []step{listsTopicOnce, listsOtherTopicOnce},
	},
}

// --- drafts and stored topics -------------------------------------------------

func adaDraftsTopic(sc *scene) { sc.topic = sc.topicBy(sc.ada, aTopic()) }

func adasTopicExists(sc *scene) {
	adaDraftsTopic(sc)
	sc.must(adaSubmitsTopic(sc), "create Ada's topic")
	sc.storedTopic = sc.topic
}

func adasOtherTopicExists(sc *scene) {
	sc.otherTopic = sc.topicBy(sc.ada, aTopic())
	sc.must(sc.s.CreateTopic(sc.ctx, sc.ada.user.GetId(), sc.otherTopic), "create Ada's other topic")
}

// adaRedraftsTopicUnderSameID drafts a different, correctly-signed topic that
// reuses the stored one's id.
func adaRedraftsTopicUnderSameID(sc *scene) {
	content := aTopic()
	content.Name = "Another topic, same id"
	sc.topic = sc.topicBy(sc.ada, content)
	sc.topic.Id = sc.storedTopic.GetId()
}

func topicCountersigned(sc *scene) {
	sc.topic.InstitutionalSignature = sc.countersign(sc.topic.GetUserSignature(), sc.topic.GetInterpretation())
}

// --- faults in the drafted topic ----------------------------------------------

func topicAltered(sc *scene) { sc.topic.Content.Name += " (altered)" }

// topicAssertedByBobsKey re-signs the topic with Bob's key while still naming
// Ada's key_id: the key_id resolves to Ada, the assertion is not hers.
func topicAssertedByBobsKey(sc *scene) {
	sc.topic.Interpretation, sc.topic.UserSignature = sc.sign(sc.bob.key, sc.ada.keyID, sc.topic.GetContent())
}

// --- calls -------------------------------------------------------------------

func adaSubmitsTopic(sc *scene) error {
	return sc.s.CreateTopic(sc.ctx, sc.ada.user.GetId(), sc.topic)
}

func bobSubmitsTopic(sc *scene) error {
	return sc.s.CreateTopic(sc.ctx, sc.bob.user.GetId(), sc.topic)
}

func getTopic(sc *scene) (err error) {
	sc.got, err = sc.s.GetTopic(sc.ctx, sc.topic.GetId())
	return err
}

func listTopics(sc *scene) error {
	list, err := sc.s.ListTopics(sc.ctx)
	sc.gotList = messages(list)
	return err
}

// --- observations --------------------------------------------------------------

func gotTopic(sc *scene)            { assertRecord(sc, sc.got, sc.topic) }
func listsTopicOnce(sc *scene)      { assertOnceIn(sc, sc.gotList, sc.storedTopic) }
func listsOtherTopicOnce(sc *scene) { assertOnceIn(sc, sc.gotList, sc.otherTopic) }

func topicReadsBack(sc *scene) {
	sc.t.Helper()
	sc.must(getTopic(sc), "read the topic back")
	gotTopic(sc)
}

func topicUnknown(sc *scene) {
	sc.t.Helper()
	_, err := sc.s.GetTopic(sc.ctx, sc.topic.GetId())
	if store.KindOf(err) != store.NotFound {
		sc.t.Fatalf("the refused topic is readable: got %v\n  promise: %s", err, atomicity)
	}
}

func storedTopicUnchanged(sc *scene) {
	sc.t.Helper()
	got, err := sc.s.GetTopic(sc.ctx, sc.storedTopic.GetId())
	sc.must(err, "read the stored topic back")
	if !proto.Equal(got, sc.storedTopic) {
		sc.t.Fatalf("a refused topic overwrote the stored one\n  got:  %v\n  want: %v\n  promise: %s", got, sc.storedTopic, atomicity)
	}
}
