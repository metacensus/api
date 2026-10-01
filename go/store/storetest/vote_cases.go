package storetest

import (
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
)

var setVoteCases = []storeCase{
	{
		name:    "topic_id absent",
		promise: "InvalidContent if any of the three ids is absent",
		given:   []step{adaEnrolled, adasTopicAndPropExist, adaDraftsVoteWithoutTopic},
		call:    adaSubmitsVote,
		want:    store.InvalidContent,
		then:    []step{noVotes},
	},
	{
		name:    "prop_id absent",
		promise: "InvalidContent if any of the three ids is absent",
		given:   []step{adaEnrolled, adasTopicAndPropExist, adaDraftsVoteWithoutProp},
		call:    adaSubmitsVote,
		want:    store.InvalidContent,
		then:    []step{noVotes},
	},
	{
		name:    "user_id absent",
		promise: "InvalidContent if any of the three ids is absent",
		given:   []step{adaEnrolled, adasTopicAndPropExist, adaDraftsVoteWithoutUser},
		call:    adaSubmitsVote,
		want:    store.InvalidContent,
		then:    []step{noVotes},
	},
	{
		name:    "user_id disagrees with the author",
		promise: "if user_id disagrees with the author",
		given:   []step{adaEnrolled, bobEnrolled, adasTopicAndPropExist, adaDraftsVoteNamingBob},
		call:    adaSubmitsVote,
		want:    store.InvalidContent,
		then:    []step{noVotes},
	},
	{
		name:    "caller is not the author, so user_id is not the caller either",
		promise: "or the write is refused (Unauthenticated) whatever else is wrong with it",
		given:   []step{adaEnrolled, bobEnrolled, adasTopicAndPropExist, adaDraftsVote},
		call:    bobSubmitsVote,
		want:    store.Unauthenticated,
		then:    []step{noVotes},
	},
	{
		name:    "content altered after signing",
		promise: "SignatureInvalid (Fabric) if the signature does not stand",
		hard:    true,
		given:   []step{adaEnrolled, adasTopicAndPropExist, adaDraftsVote, voteAltered},
		call:    adaSubmitsVote,
		want:    store.SignatureInvalid,
		then:    []step{noVotes},
	},

	{
		name:    "records the caller's position",
		promise: "SetVote records the caller's position on one prop",
		given:   []step{adaEnrolled, adasTopicAndPropExist, adaDraftsVote},
		call:    adaSubmitsVote,
		then:    []step{votesAreTheDraftOnly},
	},
	{
		name:    "a second vote replaces the first",
		promise: "a second vote from the same user replaces the first rather than adding to it",
		given:   []step{adaEnrolled, adasTopicAndPropExist, adaVoted, adaDraftsVoteAgainst},
		call:    adaSubmitsVote,
		then:    []step{votesAreTheDraftOnly},
	},

	{
		name:    "prop does not exist",
		promise: "or if the prop does not exist",
		given:   []step{adaEnrolled, adasTopicExists, adaDraftsProp, adaDraftsVoteOnUnstoredProp},
		call:    adaSubmitsVote,
		want:    store.InvalidContent,
	},
	// Omitted from the case above: reading the votes back — ListVotes on a prop
	// that does not exist is unstated.
	{
		name:    "a refused vote leaves the standing one",
		promise: atomicity,
		given:   []step{adaEnrolled, bobEnrolled, adasTopicAndPropExist, adaVoted, adaDraftsVoteNamingBob},
		call:    adaSubmitsVote,
		want:    store.InvalidContent,
		then:    []step{votesAreTheStandingOnly},
	},
}

var listVotesCases = []storeCase{
	{
		name:    "lists exactly the votes on the prop",
		promise: "ListVotes returns every vote on one prop",
		given:   []step{adaEnrolled, bobEnrolled, adasTopicAndPropExist, adaVoted, bobVoted, adaVotedElsewhere},
		call:    listVotes,
		then:    []step{listsAdasAndBobsVotesOnly},
	},
}

func adasTopicAndPropExist(sc *scene) {
	adasTopicExists(sc)
	adasPropExists(sc)
}

func adaDraftsVote(sc *scene) {
	sc.vote = sc.voteBy(sc.ada, aVote(sc.storedProp, sc.ada, v1.Vote_For))
}

func adaDraftsVoteAgainst(sc *scene) {
	sc.vote = sc.voteBy(sc.ada, aVote(sc.storedProp, sc.ada, v1.Vote_Against))
}

func adaDraftsVoteWithoutTopic(sc *scene) { adaDraftsVoteWith(sc, func(v *v1.Vote) { v.TopicId = "" }) }
func adaDraftsVoteWithoutProp(sc *scene)  { adaDraftsVoteWith(sc, func(v *v1.Vote) { v.PropId = "" }) }
func adaDraftsVoteWithoutUser(sc *scene)  { adaDraftsVoteWith(sc, func(v *v1.Vote) { v.UserId = "" }) }

func adaDraftsVoteNamingBob(sc *scene) {
	adaDraftsVoteWith(sc, func(v *v1.Vote) { v.UserId = sc.bob.user.GetId() })
}

func adaDraftsVoteOnUnstoredProp(sc *scene) {
	sc.vote = sc.voteBy(sc.ada, aVote(sc.prop, sc.ada, v1.Vote_For))
}

func adaDraftsVoteWith(sc *scene, change func(*v1.Vote)) {
	content := aVote(sc.storedProp, sc.ada, v1.Vote_For)
	change(content)
	sc.vote = sc.voteBy(sc.ada, content)
}

func adaVoted(sc *scene) {
	adaDraftsVote(sc)
	sc.must(adaSubmitsVote(sc), "record Ada's vote")
	sc.standingVote = sc.vote
}

func bobVoted(sc *scene) {
	sc.otherVote = sc.voteBy(sc.bob, aVote(sc.storedProp, sc.bob, v1.Vote_Against))
	sc.must(sc.s.SetVote(sc.ctx, sc.bob.user.GetId(), sc.otherVote), "record Bob's vote")
}

// adaVotedElsewhere votes on a second prop, which listing the first must not
// return.
func adaVotedElsewhere(sc *scene) {
	adasSecondPropExists(sc)
	elsewhere := sc.voteBy(sc.ada, aVote(sc.secondProp, sc.ada, v1.Vote_Abstain))
	sc.must(sc.s.SetVote(sc.ctx, sc.ada.user.GetId(), elsewhere), "record Ada's vote on her second prop")
}

func voteAltered(sc *scene) { sc.vote.Content.Explanation += " (altered)" }

func adaSubmitsVote(sc *scene) error {
	return sc.s.SetVote(sc.ctx, sc.ada.user.GetId(), sc.vote)
}

func bobSubmitsVote(sc *scene) error {
	return sc.s.SetVote(sc.ctx, sc.bob.user.GetId(), sc.vote)
}

func listVotes(sc *scene) error {
	list, err := sc.s.ListVotes(sc.ctx, sc.storedTopic.GetId(), sc.storedProp.GetId())
	sc.gotList = messages(list)
	return err
}

func listsAdasAndBobsVotesOnly(sc *scene) {
	assertExactly(sc, sc.gotList, sc.standingVote, sc.otherVote)
}

func votesAreTheDraftOnly(sc *scene)    { sc.votesAre(sc.vote) }
func votesAreTheStandingOnly(sc *scene) { sc.votesAre(sc.standingVote) }
func noVotes(sc *scene)                 { sc.votesAre() }

func (sc *scene) votesAre(want ...*v1.VoteSigned) {
	sc.t.Helper()
	sc.must(listVotes(sc), "list the prop's votes")
	assertExactly(sc, sc.gotList, messages(want)...)
}
