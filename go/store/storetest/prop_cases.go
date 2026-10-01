package storetest

import (
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
)

var createPropCases = []storeCase{
	{
		name:    "topic_id absent",
		promise: "InvalidContent if topic_id is absent",
		given:   []step{adaEnrolled, adaDraftsPropWithoutTopic},
		call:    adaSubmitsProp,
		want:    store.InvalidContent,
		then:    []step{propUnknown},
	},
	{
		name:    "caller is not the author",
		promise: "Unauthenticated if callerID is not the author",
		given:   []step{adaEnrolled, bobEnrolled, adasTopicExists, adaDraftsProp},
		call:    bobSubmitsProp,
		want:    store.Unauthenticated,
		then:    []step{propUnknown},
	},
	{
		name:    "content altered after signing",
		promise: "SignatureInvalid (Fabric) if the signature does not stand",
		hard:    true,
		given:   []step{adaEnrolled, adasTopicExists, adaDraftsProp, propAltered},
		call:    adaSubmitsProp,
		want:    store.SignatureInvalid,
		then:    []step{propUnknown},
	},

	{
		name:    "persists the prop under its topic",
		promise: "CreateProp persists a new prop under its content.topic_id; the record is fully minted.",
		given:   []step{adaEnrolled, adasTopicExists, adaDraftsProp},
		call:    adaSubmitsProp,
		then:    []step{propReadsBack},
	},

	{
		name:    "topic does not exist",
		promise: "InvalidContent if topic_id is absent or names a topic that does not exist",
		given:   []step{adaEnrolled, adaDraftsTopic, adaDraftsPropInUnstoredTopic},
		call:    adaSubmitsProp,
		want:    store.InvalidContent,
		then:    []step{propUnknown},
	},
	{
		name:    "id collides within its topic",
		promise: "AlreadyExists on id collision",
		given:   []step{adaEnrolled, adasTopicExists, adasPropExists, adaRedraftsPropUnderSameID},
		call:    adaSubmitsProp,
		want:    store.AlreadyExists,
		then:    []step{storedPropUnchanged},
	},
	// Omitted: whether one prop id under two topics collides — how the two ids
	// compose into a stored key is each backend's business.
}

var getPropCases = []storeCase{
	{
		name:    "returns the prop as written",
		promise: readsAsWritten,
		given:   []step{adaEnrolled, adasTopicExists, adasPropExists},
		call:    getProp,
		then:    []step{gotProp},
	},
	{
		name:    "no such prop",
		promise: "NotFound if absent",
		given:   []step{adaEnrolled, adasTopicExists, adaDraftsProp},
		call:    getProp,
		want:    store.NotFound,
	},
	{
		name:    "the prop's id under another topic",
		promise: "GetProp returns one prop, addressed by the (topic, prop) tuple",
		given:   []step{adaEnrolled, adasTopicExists, adasPropExists, adasOtherTopicExists},
		call:    getPropUnderOtherTopic,
		want:    store.NotFound,
	},
}

var listPropsCases = []storeCase{
	{
		name:    "lists exactly the props in the topic",
		promise: "ListProps returns every prop in one topic.",
		given:   []step{adaEnrolled, adasTopicExists, adasPropExists, adasSecondPropExists, adasPropElsewhereExists},
		call:    listProps,
		then:    []step{listsBothPropsOnly},
	},
	// Omitted: a topic that does not exist — an empty list or NotFound is
	// unstated.
}

// --- drafts and stored props ---------------------------------------------------

func adaDraftsProp(sc *scene) { sc.prop = sc.propBy(sc.ada, aPropIn(sc.storedTopic)) }

func adaDraftsPropWithoutTopic(sc *scene) {
	content := aPropIn(nil)
	content.TopicId = ""
	sc.prop = sc.propBy(sc.ada, content)
}

// adaDraftsPropInUnstoredTopic names a topic that was drafted, never created.
func adaDraftsPropInUnstoredTopic(sc *scene) { sc.prop = sc.propBy(sc.ada, aPropIn(sc.topic)) }

func adasPropExists(sc *scene) {
	adaDraftsProp(sc)
	sc.must(adaSubmitsProp(sc), "create Ada's prop")
	sc.storedProp = sc.prop
}

func adasSecondPropExists(sc *scene) {
	sc.secondProp = sc.propBy(sc.ada, aPropIn(sc.storedTopic))
	sc.must(sc.s.CreateProp(sc.ctx, sc.ada.user.GetId(), sc.secondProp), "create Ada's second prop")
}

func adasPropElsewhereExists(sc *scene) {
	adasOtherTopicExists(sc)
	elsewhere := sc.propBy(sc.ada, aPropIn(sc.otherTopic))
	sc.must(sc.s.CreateProp(sc.ctx, sc.ada.user.GetId(), elsewhere), "create a prop in Ada's other topic")
}

// adaRedraftsPropUnderSameID drafts a different, correctly-signed prop in the
// same topic that reuses the stored one's id.
func adaRedraftsPropUnderSameID(sc *scene) {
	content := aPropIn(sc.storedTopic)
	content.Description = "Another prop, same id"
	sc.prop = sc.propBy(sc.ada, content)
	sc.prop.Id = sc.storedProp.GetId()
}

// --- faults in the drafted prop ------------------------------------------------

func propAltered(sc *scene) { sc.prop.Content.Description += " (altered)" }

// --- calls -------------------------------------------------------------------

func adaSubmitsProp(sc *scene) error {
	return sc.s.CreateProp(sc.ctx, sc.ada.user.GetId(), sc.prop)
}

func bobSubmitsProp(sc *scene) error {
	return sc.s.CreateProp(sc.ctx, sc.bob.user.GetId(), sc.prop)
}

func getProp(sc *scene) (err error) {
	sc.got, err = sc.s.GetProp(sc.ctx, sc.prop.GetContent().GetTopicId(), sc.prop.GetId())
	return err
}

func getPropUnderOtherTopic(sc *scene) (err error) {
	sc.got, err = sc.s.GetProp(sc.ctx, sc.otherTopic.GetId(), sc.storedProp.GetId())
	return err
}

func listProps(sc *scene) error {
	list, err := sc.s.ListProps(sc.ctx, sc.storedTopic.GetId())
	sc.gotList = messages(list)
	return err
}

// --- observations --------------------------------------------------------------

func gotProp(sc *scene)            { assertRecord(sc, sc.got, sc.prop) }
func listsBothPropsOnly(sc *scene) { assertExactly(sc, sc.gotList, sc.storedProp, sc.secondProp) }

func propReadsBack(sc *scene) {
	sc.t.Helper()
	sc.must(getProp(sc), "read the prop back")
	gotProp(sc)
}

func propUnknown(sc *scene) {
	sc.t.Helper()
	_, err := sc.s.GetProp(sc.ctx, sc.prop.GetContent().GetTopicId(), sc.prop.GetId())
	if store.KindOf(err) != store.NotFound {
		sc.t.Fatalf("the refused prop is readable: got %v\n  promise: %s", err, atomicity)
	}
}

func storedPropUnchanged(sc *scene) {
	sc.t.Helper()
	got, err := sc.s.GetProp(sc.ctx, sc.storedTopic.GetId(), sc.storedProp.GetId())
	sc.must(err, "read the stored prop back")
	if !proto.Equal(got, sc.storedProp) {
		sc.t.Fatalf("a refused prop overwrote the stored one\n  got:  %v\n  want: %v\n  promise: %s", got, sc.storedProp, atomicity)
	}
}
