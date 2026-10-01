package storetest

import (
	"strings"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
)

var enrollUserCases = []storeCase{
	{
		name:    "publicKey absent",
		promise: "InvalidContent if publicKey is absent",
		given:   []step{adaDrafted, adaOffersNoKey},
		call:    enrolAda,
		want:    store.InvalidContent,
		then:    []step{adaIDUnknown, adaEmailUnknown},
	},
	{
		name:    "key_id does not thumbprint publicKey",
		promise: "InvalidContent if publicKey is absent or key_id does not thumbprint it",
		given:   []step{adaDrafted, bobDrafted, adaOffersBobsKey},
		call:    enrolAda,
		want:    store.InvalidContent,
		then:    []step{adaIDUnknown, adaEmailUnknown},
	},
	{
		name:    "enrolling assertion does not stand",
		promise: "SignatureInvalid (Fabric) if the enrolling assertion does not stand",
		hard:    true,
		given:   []step{adaDrafted, adaEnrolmentAltered},
		call:    enrolAda,
		want:    store.SignatureInvalid,
		then:    []step{adaIDUnknown, adaEmailUnknown},
	},

	{
		name:    "persists the user and binds their key",
		promise: "EnrollUser persists a new user and, with it, the signing key every later write of theirs is verified against",
		given:   []step{adaDrafted},
		call:    enrolAda,
		then:    []step{adaReadsBack, adaCredentialResolves, adaKeyBound},
	},

	{
		name:    "email taken",
		promise: "AlreadyExists if the email is taken",
		given:   []step{bobEnrolled, adaDrafted, adaTakesBobsEmail},
		call:    enrolAda,
		want:    store.AlreadyExists,
		then:    []step{bobUnchanged, adaIDUnknown, adaKeyUnbound},
	},
	{
		name:    "id collides",
		promise: "the minted id collides",
		given:   []step{bobEnrolled, adaDrafted, adaTakesBobsID},
		call:    enrolAda,
		want:    store.AlreadyExists,
		then:    []step{bobUnchanged, adaEmailUnknown, adaKeyUnbound},
	},
	{
		name:    "key_id already bound",
		promise: "or key_id is already bound",
		given:   []step{bobEnrolled, adaDrafted, adaOffersBobsEnrolledKey},
		call:    enrolAda,
		want:    store.AlreadyExists,
		then:    []step{bobUnchanged, bobKeyBound, adaIDUnknown, adaEmailUnknown},
	},
	{
		name:    "email differs only in case",
		promise: "compared byte-exact: no case folding or normalization",
		given:   []step{bobEnrolled, adaDrafted, adaTakesBobsEmailUpperCased},
		call:    enrolAda,
		then:    []step{adaReadsBack, adaCredentialResolves, bobUnchanged},
	},
	// Omitted: an empty id or email has no Kind stated.
}

var credentialCases = []storeCase{
	{
		name:    "returns the id and the hash stored",
		promise: "the store hands back only the hash it stored",
		given:   []step{adaEnrolled},
		call:    credentialOfAda,
		then:    []step{credentialIsAdas},
	},
	{
		name:    "no such email",
		promise: "Unauthenticated if no such email exists",
		given:   []step{adaDrafted},
		call:    credentialOfAda,
		want:    store.Unauthenticated,
	},
}

var getUserCases = []storeCase{
	{
		name:    "returns the user as written",
		promise: readsAsWritten,
		given:   []step{adaEnrolled},
		call:    getAda,
		then:    []step{gotAda},
	},
	{
		name:    "no such id",
		promise: "NotFound if absent",
		given:   []step{adaDrafted},
		call:    getAda,
		want:    store.NotFound,
	},
}

var listUsersCases = []storeCase{
	{
		name:    "lists each user once",
		promise: "ListUsers returns every user",
		given:   []step{adaEnrolled, bobEnrolled},
		call:    listUsers,
		then:    []step{listsAdaOnce, listsBobOnce},
	},
}

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

func adaOffersNoKey(sc *scene)   { sc.ada.publicKey = "" }
func adaOffersBobsKey(sc *scene) { sc.ada.publicKey = sc.bob.publicKey }
func adaTakesBobsID(sc *scene)   { sc.ada.user.Id = sc.bob.user.GetId() }

func adaOffersBobsEnrolledKey(sc *scene) {
	sc.ada.key, sc.ada.keyID, sc.ada.publicKey = sc.bob.key, sc.bob.keyID, sc.bob.publicKey
	sc.ada.user.Interpretation, sc.ada.user.UserSignature = sc.sign(sc.ada.key, sc.ada.keyID, sc.ada.user.GetContent())
}

func adaTakesBobsEmail(sc *scene) { adaTakesEmail(sc, sc.bob.user.GetContent().GetEmail()) }

func adaTakesBobsEmailUpperCased(sc *scene) {
	adaTakesEmail(sc, strings.ToUpper(sc.bob.user.GetContent().GetEmail()))
}

func adaTakesEmail(sc *scene, email string) {
	content := proto.Clone(sc.ada.user.GetContent()).(*v1.User)
	content.Email = email
	sc.ada.user.Content = content
	sc.ada.user.Interpretation, sc.ada.user.UserSignature = sc.sign(sc.ada.key, sc.ada.keyID, content)
}

func adaEnrolmentAltered(sc *scene) { sc.ada.user.Content.Name += " (altered)" }

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

func adaKeyBound(sc *scene) { sc.keyBound(sc.ada) }
func bobKeyBound(sc *scene) { sc.keyBound(sc.bob) }

func (sc *scene) keyBound(p *participant) {
	sc.t.Helper()
	sc.must(sc.s.CreateTopic(sc.ctx, p.user.GetId(), sc.topicBy(p, aTopic())), "create a topic with "+p.user.GetContent().GetName()+"'s key")
}

// adaKeyUnbound: a refused enrolment bound no key; either disputed Kind is
// accepted (see createTopicCases).
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
