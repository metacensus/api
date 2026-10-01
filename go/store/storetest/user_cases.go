package storetest

import (
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
)

var enrollUserCases = []storeCase{
	// Refused before any state is consulted.
	{
		name:    "error - InvalidContent: publicKey absent",
		promise: "InvalidContent if publicKey is absent",
		given:   []step{adaDrafted, adaOffersNoKey},
		call:    enrolAda,
		want:    store.InvalidContent,
		then:    []step{adaIDUnknown, adaEmailUnknown},
	},
	{
		name:    "error - InvalidContent: key_id does not thumbprint publicKey",
		promise: "InvalidContent if publicKey is absent or key_id does not thumbprint it",
		given:   []step{adaDrafted, bobDrafted, adaOffersBobsKey},
		call:    enrolAda,
		want:    store.InvalidContent,
		then:    []step{adaIDUnknown, adaEmailUnknown},
	},
	{
		name:    "error - SignatureInvalid: enrolling assertion does not stand",
		promise: "SignatureInvalid (Fabric) if the enrolling assertion does not stand",
		hard:    true,
		given:   []step{adaDrafted, adaEnrolmentAltered},
		call:    enrolAda,
		want:    store.SignatureInvalid,
		then:    []step{adaIDUnknown, adaEmailUnknown},
	},

	// Accepted.
	{
		name:    "success - persists the user and binds their key",
		promise: "EnrollUser persists a new user and, with it, the signing key every later write of theirs is verified against",
		given:   []step{adaDrafted},
		call:    enrolAda,
		then:    []step{adaReadsBack, adaCredentialResolves, adaKeyBound},
	},

	// Refused against stored state; nothing of the refused enrolment remains.
	{
		name:    "error - AlreadyExists: email taken",
		promise: "AlreadyExists if the email is taken",
		given:   []step{bobEnrolled, adaDrafted, adaTakesBobsEmail},
		call:    enrolAda,
		want:    store.AlreadyExists,
		then:    []step{bobUnchanged, adaIDUnknown, adaKeyUnbound},
	},
	{
		name:    "error - AlreadyExists: id collides",
		promise: "AlreadyExists if the email is taken or the minted id collides",
		given:   []step{bobEnrolled, adaDrafted, adaTakesBobsID},
		call:    enrolAda,
		want:    store.AlreadyExists,
		then:    []step{bobUnchanged, adaEmailUnknown, adaKeyUnbound},
	},
	// Omitted: an empty id or email has no Kind stated.
	// Omitted: whether emails differing only in case collide is unstated.
}

var credentialCases = []storeCase{
	{
		name:    "success - returns the id and the hash stored",
		promise: "the store hands back only the hash it stored",
		given:   []step{adaEnrolled},
		call:    credentialOfAda,
		then:    []step{credentialIsAdas},
	},
	{
		name:    "error - Unauthenticated: no such email",
		promise: "Unauthenticated if no such email exists",
		given:   []step{adaDrafted},
		call:    credentialOfAda,
		want:    store.Unauthenticated,
	},
}

var getUserCases = []storeCase{
	{
		name:    "success - returns the user as written",
		promise: readsAsWritten,
		given:   []step{adaEnrolled},
		call:    getAda,
		then:    []step{gotAda},
	},
	{
		name:    "error - NotFound: no such id",
		promise: "NotFound if absent",
		given:   []step{adaDrafted},
		call:    getAda,
		want:    store.NotFound,
	},
}

var listUsersCases = []storeCase{
	{
		name:    "success - lists each user once",
		promise: "ListUsers returns every user",
		given:   []step{adaEnrolled, bobEnrolled},
		call:    listUsers,
		then:    []step{listsAdaOnce, listsBobOnce},
	},
}

// --- people -----------------------------------------------------------------

func adaDrafted(sc *scene) { sc.ada = sc.newParticipant("Ada") }
func bobDrafted(sc *scene) { sc.bob = sc.newParticipant("Bob") }

func adaEnrolled(sc *scene) {
	adaDrafted(sc)
	sc.must(enrolAda(sc), "enrol Ada")
}

func bobEnrolled(sc *scene) {
	bobDrafted(sc)
	sc.must(sc.s.EnrollUser(sc.ctx, sc.bob.user, sc.bob.publicKey, sc.bob.hash), "enrol Bob")
}

// --- faults in Ada's enrolment ------------------------------------------------

func adaOffersNoKey(sc *scene)   { sc.ada.publicKey = "" }
func adaOffersBobsKey(sc *scene) { sc.ada.publicKey = sc.bob.publicKey }
func adaTakesBobsID(sc *scene)   { sc.ada.user.Id = sc.bob.user.GetId() }

func adaTakesBobsEmail(sc *scene) {
	content := proto.Clone(sc.ada.user.GetContent()).(*v1.User)
	content.Email = sc.bob.user.GetContent().GetEmail()
	sc.ada.user.Content = content
	sc.ada.user.Interpretation, sc.ada.user.UserSignature = sc.sign(sc.ada.key, sc.ada.keyID, content)
}

// adaEnrolmentAltered changes the content after it was signed.
func adaEnrolmentAltered(sc *scene) { sc.ada.user.Content.Name += " (altered)" }

// --- calls -------------------------------------------------------------------

func enrolAda(sc *scene) error {
	return sc.s.EnrollUser(sc.ctx, sc.ada.user, sc.ada.publicKey, sc.ada.hash)
}

func credentialOfAda(sc *scene) (err error) {
	sc.gotID, sc.gotHash, err = sc.s.Credential(sc.ctx, sc.ada.user.GetContent().GetEmail())
	return err
}

func getAda(sc *scene) (err error) {
	sc.got, err = sc.s.GetUser(sc.ctx, sc.ada.user.GetId())
	return err
}

func listUsers(sc *scene) error {
	list, err := sc.s.ListUsers(sc.ctx)
	sc.gotList = messages(list)
	return err
}

// --- observations --------------------------------------------------------------

func gotAda(sc *scene)       { assertRecord(sc, sc.got, sc.ada.user) }
func listsAdaOnce(sc *scene) { assertOnceIn(sc, sc.gotList, sc.ada.user) }
func listsBobOnce(sc *scene) { assertOnceIn(sc, sc.gotList, sc.bob.user) }

func adaReadsBack(sc *scene) {
	sc.t.Helper()
	sc.must(getAda(sc), "read Ada back")
	gotAda(sc)
}

func credentialIsAdas(sc *scene) {
	sc.t.Helper()
	if sc.gotID != sc.ada.user.GetId() || sc.gotHash != sc.ada.hash {
		sc.t.Fatalf("credential is (%q, %q), want (%q, %q)\n  promise: %s", sc.gotID, sc.gotHash, sc.ada.user.GetId(), sc.ada.hash, sc.c.promise)
	}
}

func adaCredentialResolves(sc *scene) {
	sc.t.Helper()
	sc.must(credentialOfAda(sc), "read Ada's credential")
	credentialIsAdas(sc)
}

// adaKeyBound: a topic Ada signs with her enrolling key is accepted from her.
func adaKeyBound(sc *scene) {
	sc.t.Helper()
	sc.must(sc.s.CreateTopic(sc.ctx, sc.ada.user.GetId(), sc.topicBy(sc.ada, aTopic())), "create a topic with Ada's newly enrolled key")
}

// adaKeyUnbound: a refused enrolment bound no key, so a topic signed with it is
// refused. Either Kind the doc comments name for a key_id that resolves to no
// one is accepted here — store.go says Unauthenticated, errors.go says
// SignatureInvalid — since the two disagree.
func adaKeyUnbound(sc *scene) {
	sc.t.Helper()
	err := sc.s.CreateTopic(sc.ctx, sc.ada.user.GetId(), sc.topicBy(sc.ada, aTopic()))
	if k := store.KindOf(err); k != store.Unauthenticated && k != store.SignatureInvalid {
		sc.t.Fatalf("a topic signed with the refused enrolment's key: got %v (Kind %q), want it refused\n  promise: %s", err, k, atomicity)
	}
}

func adaIDUnknown(sc *scene) {
	sc.t.Helper()
	_, err := sc.s.GetUser(sc.ctx, sc.ada.user.GetId())
	if store.KindOf(err) != store.NotFound {
		sc.t.Fatalf("the refused user is readable by id: got %v\n  promise: %s", err, atomicity)
	}
}

func adaEmailUnknown(sc *scene) {
	sc.t.Helper()
	_, _, err := sc.s.Credential(sc.ctx, sc.ada.user.GetContent().GetEmail())
	if store.KindOf(err) != store.Unauthenticated {
		sc.t.Fatalf("the refused user's email resolves: got %v\n  promise: %s", err, atomicity)
	}
}

func bobUnchanged(sc *scene) {
	sc.t.Helper()
	got, err := sc.s.GetUser(sc.ctx, sc.bob.user.GetId())
	sc.must(err, "read Bob back")
	assertRecord(sc, got, sc.bob.user)
	id, hash, err := sc.s.Credential(sc.ctx, sc.bob.user.GetContent().GetEmail())
	sc.must(err, "read Bob's credential")
	if id != sc.bob.user.GetId() || hash != sc.bob.hash {
		sc.t.Fatalf("Bob's credential changed to (%q, %q)\n  promise: %s", id, hash, atomicity)
	}
}
