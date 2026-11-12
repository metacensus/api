package storetest

import (
	"crypto/rand"
	"math/big"
	"strings"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var createPublicationCases = []storeCase{
	{
		name:    "persists the publication",
		promise: "CreatePublication persists a new publication; the record is fully minted",
		given:   []step{publicationDrafted},
		call:    submitPublication,
		then:    []step{publicationReadsBack},
	},
	{
		name:    "persists a publication with only a PMID",
		promise: "CreatePublication persists a new publication; the record is fully minted",
		given:   []step{publicationDrafted, publicationLosesDOI},
		call:    submitPublication,
		then:    []step{publicationReadsBack},
	},
	{
		name:    "id is not a publication id",
		promise: "InvalidContent if the id is not a publication id",
		given:   []step{publicationDrafted, publicationIDIsATopicID},
		call:    submitPublication,
		want:    store.InvalidContent,
		then:    []step{publicationUnknown},
	},
	{
		name:    "neither a DOI nor a PMID",
		promise: "or if the record has neither a DOI nor a PMID",
		given:   []step{publicationDrafted, publicationLosesDOI, publicationLosesPMID},
		call:    submitPublication,
		want:    store.InvalidContent,
		then:    []step{publicationUnknown},
	},
	{
		name:    "id collides",
		promise: "AlreadyExists on id collision",
		given:   []step{publicationExists, publicationRedraftedUnderSameID},
		call:    submitPublication,
		want:    store.AlreadyExists,
		then:    []step{storedPublicationUnchanged},
	},
	{
		name:    "another publication holds its DOI",
		promise: "or if another publication already holds its DOI or its PMID",
		given:   []step{publicationExists, publicationRedraftedWithSameDOI},
		call:    submitPublication,
		want:    store.AlreadyExists,
		then:    []step{publicationUnknown, storedPublicationUnchanged},
	},
	{
		name:    "another publication holds its PMID",
		promise: "or if another publication already holds its DOI or its PMID",
		given:   []step{publicationExists, publicationRedraftedWithSamePMID},
		call:    submitPublication,
		want:    store.AlreadyExists,
		then:    []step{publicationUnknown, storedPublicationUnchanged},
	},
}

var getPublicationCases = []storeCase{
	{
		name:    "returns the publication as written",
		promise: readsAsWritten,
		given:   []step{publicationExists},
		call:    getPublication,
		then:    []step{gotPublication},
	},
	{
		name:    "no such id",
		promise: "GetPublication returns one publication by id. NotFound if absent.",
		given:   []step{publicationDrafted},
		call:    getPublication,
		want:    store.NotFound,
	},
}

var findPublicationCases = []storeCase{
	{
		name:    "by its DOI",
		promise: "FindPublication returns the publication holding doi",
		given:   []step{publicationExists},
		call:    findPublicationByDOI,
		then:    []step{foundStoredPublication},
	},
	{
		name:    "by its PMID",
		promise: "or, failing that, the one holding pmid",
		given:   []step{publicationExists},
		call:    findPublicationByPMID,
		then:    []step{foundStoredPublication},
	},
	{
		name:    "the DOI's holder over the PMID's",
		promise: "FindPublication returns the publication holding doi or, failing that, the one holding pmid",
		given:   []step{publicationExists, otherPublicationExists},
		call:    findPublicationByDOIAndOthersPMID,
		then:    []step{foundStoredPublication},
	},
	{
		name:    "neither held",
		promise: "NotFound if neither is held.",
		given:   []step{publicationDrafted},
		call:    findPublicationByDraftsIDs,
		want:    store.NotFound,
	},
	{
		name:    "empty arguments match a publication with no DOI",
		promise: "an empty argument matches nothing",
		given:   []step{publicationDrafted, publicationLosesDOI, publicationSubmitted},
		call:    findPublicationByNothing,
		want:    store.NotFound,
	},
}

// external ids unique to this process, so cases never collide in a shared
// store.
func newDOI() string { return "10.5555/conformance-" + strings.ToLower(rand.Text()) }

func newPMID() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1e15))
	if err != nil {
		panic(err)
	}
	return "9" + n.String()
}

func aPublication() *v1.Publication {
	return &v1.Publication{
		Id:        store.NewID(store.PublicationID),
		Recorded:  timestamppb.New(recordedAt),
		Doi:       newDOI(),
		Pmid:      newPMID(),
		Title:     "A publication the conformance suite drafted",
		Authors:   []string{"Lovelace A", "Babbage C"},
		Journal:   "Conformance",
		Published: "2023 Nov 14",
		Abstract:  "Nothing at all.",
	}
}

func publicationDrafted(sc *scene) { sc.publication = aPublication() }

func publicationSubmitted(sc *scene) {
	sc.must(submitPublication(sc), "create the publication")
	sc.storedPublication = sc.publication
}

func publicationExists(sc *scene) {
	publicationDrafted(sc)
	publicationSubmitted(sc)
}

func otherPublicationExists(sc *scene) {
	sc.otherPublication = aPublication()
	sc.must(sc.s.CreatePublication(sc.ctx, sc.otherPublication), "create the other publication")
}

func publicationLosesDOI(sc *scene)     { sc.publication.Doi = "" }
func publicationLosesPMID(sc *scene)    { sc.publication.Pmid = "" }
func publicationIDIsATopicID(sc *scene) { sc.publication.Id = store.NewID(store.TopicID) }

func publicationRedraftedUnderSameID(sc *scene) {
	sc.publication = aPublication()
	sc.publication.Id = sc.storedPublication.GetId()
}

func publicationRedraftedWithSameDOI(sc *scene) {
	sc.publication = aPublication()
	sc.publication.Doi = sc.storedPublication.GetDoi()
}

func publicationRedraftedWithSamePMID(sc *scene) {
	sc.publication = aPublication()
	sc.publication.Pmid = sc.storedPublication.GetPmid()
}

func submitPublication(sc *scene) error { return sc.s.CreatePublication(sc.ctx, sc.publication) }

func getPublication(sc *scene) (err error) {
	sc.got, err = sc.s.GetPublication(sc.ctx, sc.publication.GetId())
	return err
}

func findPublicationByDOI(sc *scene) (err error) {
	sc.got, err = sc.s.FindPublication(sc.ctx, sc.storedPublication.GetDoi(), "")
	return err
}

func findPublicationByPMID(sc *scene) (err error) {
	sc.got, err = sc.s.FindPublication(sc.ctx, "", sc.storedPublication.GetPmid())
	return err
}

func findPublicationByDOIAndOthersPMID(sc *scene) (err error) {
	sc.got, err = sc.s.FindPublication(sc.ctx, sc.storedPublication.GetDoi(), sc.otherPublication.GetPmid())
	return err
}

func findPublicationByDraftsIDs(sc *scene) (err error) {
	sc.got, err = sc.s.FindPublication(sc.ctx, sc.publication.GetDoi(), sc.publication.GetPmid())
	return err
}

func findPublicationByNothing(sc *scene) (err error) {
	sc.got, err = sc.s.FindPublication(sc.ctx, "", "")
	return err
}

func gotPublication(sc *scene)         { assertRecord(sc, sc.got, sc.publication) }
func foundStoredPublication(sc *scene) { assertRecord(sc, sc.got, sc.storedPublication) }

func publicationReadsBack(sc *scene) {
	sc.t.Helper()
	sc.must(getPublication(sc), "read the publication back")
	gotPublication(sc)
}

func publicationUnknown(sc *scene) {
	sc.t.Helper()
	_, err := sc.s.GetPublication(sc.ctx, sc.publication.GetId())
	if store.KindOf(err) != store.NotFound {
		sc.t.Fatalf("the refused publication is readable: got %v\n  promise: %s", err, atomicity)
	}
}

func storedPublicationUnchanged(sc *scene) {
	sc.t.Helper()
	got, err := sc.s.GetPublication(sc.ctx, sc.storedPublication.GetId())
	sc.must(err, "read the stored publication back")
	if !proto.Equal(got, sc.storedPublication) {
		sc.t.Fatalf("a refused publication overwrote the stored one\n  got:  %v\n  want: %v\n  promise: %s", got, sc.storedPublication, atomicity)
	}
}
