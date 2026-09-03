package storetest

import (
	"context"
	"sync"
	"testing"

	"github.com/metacensus/api/go/store"
)

// Cases returns every conformance check, in a stable order.
//
// It is exported so an implementation can list what it is being held to, and
// so a backend that legitimately cannot satisfy one can say which and why in
// its own test file rather than silently not running it.
func Cases() []Case {
	var cs []Case
	cs = append(cs, determinismCases()...)
	cs = append(cs, attributionCases()...)
	cs = append(cs, idempotencyCases()...)
	cs = append(cs, extractionCases()...)
	cs = append(cs, errorCases()...)
	cs = append(cs, attestationCases()...)
	cs = append(cs, concurrencyCases()...)
	return cs
}

// --- determinism ----------------------------------------------------------

func determinismCases() []Case {
	return []Case{{
		Name: "Determinism/MintedValuesComeBackUnchanged",
		Why: "Ids and timestamps are minted above the seam. An implementation that " +
			"substitutes its own has broken endorsement: two peers executing the same " +
			"invocation would produce different write-sets.",
		Fn: func(t *testing.T, h *H) {
			id := h.ID(store.KindUser)
			created := h.At(7)
			c := store.Caller{UserID: id}
			u, err := h.S.UserCreate(h.Ctx, c, &store.UserCreateRequest{
				UserID: id, Created: created, PasswordHash: []byte("hash"),
				Name: "Ada", Email: "ada@example.test", Country: "GB",
			})
			h.OK(err, "UserCreate")
			if u.ID != id {
				t.Errorf("UserCreate returned id %q, want the minted %q", u.ID, id)
			}
			if !u.Created.Equal(created) {
				t.Errorf("UserCreate returned Created %v, want the minted %v", u.Created, created)
			}

			got, err := h.S.UserGet(h.Ctx, c, &store.UserGetRequest{UserID: id})
			h.OK(err, "UserGet")
			if got.ID != id || !got.Created.Equal(created) {
				t.Errorf("UserGet returned (%q, %v), want (%q, %v)", got.ID, got.Created, id, created)
			}
			if got.Name != "Ada" || got.Email != "ada@example.test" || got.Country != "GB" {
				t.Errorf("UserGet round-tripped fields wrong: %+v", got)
			}
		},
	}, {
		Name: "Determinism/TwoFreshStoresAgreeOnTheSameRequests",
		Why: "The closest a black-box suite gets to replicated execution: the same " +
			"requests against independent empty stores must produce the same records.",
		Fn: func(t *testing.T, h *H) {
			// The ids are minted once and replayed into both stores. That is
			// the arrangement the seam requires: the caller mints, the store
			// receives.
			uid := h.ID(store.KindUser)
			tid := h.ID(store.KindTopic)
			pid := h.ID(store.KindProp)

			run := func(s store.Store) (*store.Prop, *store.Vote) {
				c := store.Caller{UserID: uid}
				_, err := s.UserCreate(h.Ctx, c, &store.UserCreateRequest{
					UserID: uid, Created: h.At(1), PasswordHash: []byte("h"),
					Name: "N", Email: "fixed@example.test", Country: "US",
				})
				h.OK(err, "UserCreate")
				_, err = s.TopicCreate(h.Ctx, c, &store.TopicCreateRequest{
					TopicID: tid, Created: h.At(2), Name: "T", Description: "D",
				})
				h.OK(err, "TopicCreate")
				p, err := s.PropCreate(h.Ctx, c, &store.PropCreateRequest{
					TopicID: tid, PropID: pid, Created: h.At(3),
					Type: store.PropTypeStatement, Description: "same text",
				})
				h.OK(err, "PropCreate")
				v, err := s.VoteSet(h.Ctx, c, &store.VoteSetRequest{
					TopicID: tid, PropID: pid,
					Position: store.PositionFor, Explanation: "because", Cast: h.At(4),
				})
				h.OK(err, "VoteSet")
				return p, v
			}

			// h.S is the first store; New gives an independent second.
			p1, v1 := run(h.S)
			p2, v2 := run(h.New())

			if p1.ID != p2.ID || p1.AuthorID != p2.AuthorID || p1.Description != p2.Description ||
				!p1.Created.Equal(p2.Created) || p1.Type != p2.Type {
				t.Errorf("two stores disagree on the same PropCreate:\n %+v\n %+v", p1, p2)
			}
			if v1.UserID != v2.UserID || v1.Position != v2.Position ||
				v1.Explanation != v2.Explanation || !v1.Cast.Equal(v2.Cast) {
				t.Errorf("two stores disagree on the same VoteSet:\n %+v\n %+v", v1, v2)
			}
		},
	}}
}

// --- attribution ----------------------------------------------------------

func attributionCases() []Case {
	return []Case{{
		Name: "Attribution/PropAuthorComesFromTheCaller",
		Why: "PropCreateRequest has no author field. If the author did not come from " +
			"the Caller, a client could assert who it is.",
		Fn: func(t *testing.T, h *H) {
			ca, _ := h.User("a@example.test")
			cb, _ := h.User("b@example.test")
			tp := h.Topic(ca)

			pa := h.Prop(ca, tp.ID)
			pb := h.Prop(cb, tp.ID)

			if pa.AuthorID != ca.UserID {
				t.Errorf("prop author is %q, want the caller %q", pa.AuthorID, ca.UserID)
			}
			if pb.AuthorID != cb.UserID {
				t.Errorf("prop author is %q, want the caller %q", pb.AuthorID, cb.UserID)
			}

			got, err := h.S.PropGet(h.Ctx, ca, &store.PropGetRequest{TopicID: tp.ID, PropID: pb.ID})
			h.OK(err, "PropGet")
			if got.AuthorID != cb.UserID {
				t.Errorf("PropGet author is %q, want %q", got.AuthorID, cb.UserID)
			}
		},
	}, {
		Name: "Attribution/TwoCallersVotingAreTwoVotes",
		Why: "A vote is keyed by (prop, user). Two voters collapsing into one would " +
			"mean the key lost its user dimension.",
		Fn: func(t *testing.T, h *H) {
			ca, _ := h.User("a@example.test")
			cb, _ := h.User("b@example.test")
			tp := h.Topic(ca)
			p := h.Prop(ca, tp.ID)

			for i, c := range []store.Caller{ca, cb} {
				_, err := h.S.VoteSet(h.Ctx, c, &store.VoteSetRequest{
					TopicID: tp.ID, PropID: p.ID, Position: store.PositionFor,
					Explanation: "v", Cast: h.At(10 + i),
				})
				h.OK(err, "VoteSet")
			}

			vl, err := h.S.VoteList(h.Ctx, ca, &store.VoteListRequest{TopicID: tp.ID, PropID: p.ID})
			h.OK(err, "VoteList")
			seen := map[store.ID]int{}
			for _, v := range vl.Items {
				seen[v.UserID]++
			}
			if len(vl.Items) != 2 || seen[ca.UserID] != 1 || seen[cb.UserID] != 1 {
				t.Errorf("want one vote each from %q and %q, got %d votes: %+v",
					ca.UserID, cb.UserID, len(vl.Items), vl.Items)
			}
		},
	}, {
		Name: "Attribution/UserCreateRejectsACallerThatIsNotTheNewUser",
		Why: "Signup runs before there is a session, so the caller is the user being " +
			"created. Accepting a mismatch would let one user mint another.",
		Fn: func(t *testing.T, h *H) {
			other, _ := h.User("other@example.test")
			id := h.ID(store.KindUser)
			_, err := h.S.UserCreate(h.Ctx, other, &store.UserCreateRequest{
				UserID: id, Created: h.At(1), PasswordHash: []byte("h"),
				Name: "N", Email: "mismatch@example.test", Country: "US",
			})
			h.WantCode(err, store.CodeInvalid, "UserCreate with a mismatched caller")
		},
	}}
}

// --- idempotency and lookups ----------------------------------------------

func idempotencyCases() []Case {
	return []Case{{
		Name: "Write/RecastReplacesRatherThanAdds",
		Why:  "VoteSet is an upsert on (prop, user). A second vote that adds a row is a duplicate, not a recast.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			p := h.Prop(c, tp.ID)

			_, err := h.S.VoteSet(h.Ctx, c, &store.VoteSetRequest{
				TopicID: tp.ID, PropID: p.ID, Position: store.PositionFor,
				Explanation: "first", Cast: h.At(10),
				Citations: []store.Citation{{Start: 0, End: 4}},
			})
			h.OK(err, "VoteSet first")

			v, err := h.S.VoteSet(h.Ctx, c, &store.VoteSetRequest{
				TopicID: tp.ID, PropID: p.ID, Position: store.PositionAgainst,
				Explanation: "second", Cast: h.At(11),
			})
			h.OK(err, "VoteSet recast")
			if v.Position != store.PositionAgainst || v.Explanation != "second" {
				t.Errorf("recast returned %+v, want the second vote", v)
			}
			if !v.Cast.Equal(h.At(11)) {
				t.Errorf("recast Cast is %v, want the minted %v", v.Cast, h.At(11))
			}

			vl, err := h.S.VoteList(h.Ctx, c, &store.VoteListRequest{TopicID: tp.ID, PropID: p.ID})
			h.OK(err, "VoteList")
			if len(vl.Items) != 1 {
				t.Fatalf("want 1 vote after a recast, got %d: %+v", len(vl.Items), vl.Items)
			}
			if vl.Items[0].Position != store.PositionAgainst {
				t.Errorf("stored vote is %+v, want the recast", vl.Items[0])
			}
		},
	}, {
		Name: "Write/CreateTwiceIsAlreadyExists",
		Why: "Ids come from the caller, so a repeat is a duplicate rather than a new " +
			"record. AlreadyExists is distinct from Conflict: retrying will not help.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			p := h.Prop(c, tp.ID)

			_, err := h.S.PropCreate(h.Ctx, c, &store.PropCreateRequest{
				TopicID: tp.ID, PropID: p.ID, Created: h.At(3),
				Type: store.PropTypeStatement, Description: "again",
			})
			h.WantCode(err, store.CodeAlreadyExists, "PropCreate with a taken id")

			_, err = h.S.TopicCreate(h.Ctx, c, &store.TopicCreateRequest{
				TopicID: tp.ID, Created: h.At(2), Name: "again", Description: "d",
			})
			h.WantCode(err, store.CodeAlreadyExists, "TopicCreate with a taken id")
		},
	}, {
		Name: "Write/DuplicateEmailIsAlreadyExists",
		Why:  "Email is a natural key behind login. Two users sharing one makes CredentialGet ambiguous.",
		Fn: func(t *testing.T, h *H) {
			h.User("dup@example.test")
			id := h.ID(store.KindUser)
			_, err := h.S.UserCreate(h.Ctx, store.Caller{UserID: id}, &store.UserCreateRequest{
				UserID: id, Created: h.At(1), PasswordHash: []byte("h"),
				Name: "N", Email: "dup@example.test", Country: "US",
			})
			h.WantCode(err, store.CodeAlreadyExists, "UserCreate with a taken email")
		},
	}, {
		Name: "Read/MissingRecordsAreNotFound",
		Why: "A not-found that arrives as Internal, or as a zero value and a nil error, " +
			"is indistinguishable from a bug at the callsite.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)

			_, err := h.S.UserGet(h.Ctx, c, &store.UserGetRequest{UserID: h.ID(store.KindUser)})
			h.WantCode(err, store.CodeNotFound, "UserGet on an absent user")

			_, err = h.S.TopicGet(h.Ctx, c, &store.TopicGetRequest{TopicID: h.ID(store.KindTopic)})
			h.WantCode(err, store.CodeNotFound, "TopicGet on an absent topic")

			_, err = h.S.PropGet(h.Ctx, c, &store.PropGetRequest{TopicID: tp.ID, PropID: h.ID(store.KindProp)})
			h.WantCode(err, store.CodeNotFound, "PropGet on an absent prop")

			_, err = h.S.CredentialGet(h.Ctx, &store.CredentialGetRequest{Email: "nobody@example.test"})
			h.WantCode(err, store.CodeNotFound, "CredentialGet on an unknown email")
		},
	}, {
		Name: "Read/WritingIntoAnAbsentParentIsNotFound",
		Why:  "Existence is a state-dependent invariant, so it is checked below the seam, inside the write's unit of work.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")

			_, err := h.S.PropCreate(h.Ctx, c, &store.PropCreateRequest{
				TopicID: h.ID(store.KindTopic), PropID: h.ID(store.KindProp), Created: h.At(3),
				Type: store.PropTypeStatement, Description: "orphan",
			})
			h.WantCode(err, store.CodeNotFound, "PropCreate into an absent topic")

			tp := h.Topic(c)
			_, err = h.S.VoteSet(h.Ctx, c, &store.VoteSetRequest{
				TopicID: tp.ID, PropID: h.ID(store.KindProp),
				Position: store.PositionFor, Cast: h.At(10),
			})
			h.WantCode(err, store.CodeNotFound, "VoteSet on an absent prop")
		},
	}, {
		Name: "Read/ListsReturnEverythingWritten",
		Why: "There is no pagination in this interface. A backend that quietly caps a " +
			"list has admitted an operation only it can express.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)

			const n = 25
			want := map[store.ID]bool{}
			for i := 0; i < n; i++ {
				want[h.Prop(c, tp.ID).ID] = true
			}

			pl, err := h.S.PropList(h.Ctx, c, &store.PropListRequest{TopicID: tp.ID})
			h.OK(err, "PropList")
			if len(pl.Items) != n {
				t.Fatalf("PropList returned %d props, want %d — a silent limit is a "+
					"pagination decision taken by one implementer", len(pl.Items), n)
			}
			for _, p := range pl.Items {
				if !want[p.ID] {
					t.Errorf("PropList returned an unexpected prop %q", p.ID)
				}
				delete(want, p.ID)
			}
			for id := range want {
				t.Errorf("PropList omitted %q", id)
			}
		},
	}, {
		Name: "Read/PropsAreScopedToTheirTopic",
		Why: "TopicID travels in the request rather than as a separate argument, and it " +
			"has to actually scope: infra threads one through and then drops it.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			t1, t2 := h.Topic(c), h.Topic(c)
			p1 := h.Prop(c, t1.ID)
			h.Prop(c, t2.ID)

			pl, err := h.S.PropList(h.Ctx, c, &store.PropListRequest{TopicID: t1.ID})
			h.OK(err, "PropList")
			if len(pl.Items) != 1 || pl.Items[0].ID != p1.ID {
				t.Errorf("PropList for topic %q returned %+v, want only %q", t1.ID, pl.Items, p1.ID)
			}
		},
	}, {
		Name: "Read/CredentialGetReturnsTheStoredHash",
		Why: "The one Class B method. It returns material, and the shared layer does the " +
			"bcrypt comparison — so the material has to arrive intact.",
		Fn: func(t *testing.T, h *H) {
			id := h.ID(store.KindUser)
			hash := []byte("$2a$08$aparticularstoredhashvalue")
			_, err := h.S.UserCreate(h.Ctx, store.Caller{UserID: id}, &store.UserCreateRequest{
				UserID: id, Created: h.At(1), PasswordHash: hash,
				Name: "N", Email: "cred@example.test", Country: "US",
			})
			h.OK(err, "UserCreate")

			cr, err := h.S.CredentialGet(h.Ctx, &store.CredentialGetRequest{Email: "cred@example.test"})
			h.OK(err, "CredentialGet")
			if cr.UserID != id {
				t.Errorf("CredentialGet returned user %q, want %q", cr.UserID, id)
			}
			if string(cr.PasswordHash) != string(hash) {
				t.Errorf("CredentialGet returned hash %q, want %q", cr.PasswordHash, hash)
			}
		},
	}}
}

// --- extractions ----------------------------------------------------------

func extractionCases() []Case {
	upsert := func(h *H, c store.Caller, topic, paper, protocol store.ID, at int, data ...store.Datum) (*store.Extraction, error) {
		return h.S.ExtractionUpsert(h.Ctx, c, &store.ExtractionUpsertRequest{
			TopicID: topic, PaperID: paper, ProtocolID: protocol,
			Data: data, Recorded: h.At(at), Digest: h.Digest(9),
		})
	}
	datum := func(el, val string) store.Datum {
		return store.Datum{ProtocolElementID: store.ID(el), Value: val}
	}
	values := func(e *store.Extraction) map[store.ID]string {
		m := map[store.ID]string{}
		for _, d := range e.Data {
			m[d.ProtocolElementID] = d.Value
		}
		return m
	}

	return []Case{{
		Name: "Extraction/TwoReviewersDoNotCollide",
		Why: "The reviewer is a key segment, not a field. This is the whole reason the " +
			"natural key is (topic, paper, protocol, reviewer): the merged wire contract " +
			"keys a DataExtraction by three ids and cannot express independent review at all.",
		Fn: func(t *testing.T, h *H) {
			ca, _ := h.User("a@example.test")
			cb, _ := h.User("b@example.test")
			tp := h.Topic(ca)
			paper, protocol := h.ID(store.KindPaper), h.ID(store.KindProtocol)

			ea, err := upsert(h, ca, tp.ID, paper, protocol, 20, datum("el-1", "from-a"))
			h.OK(err, "ExtractionUpsert a")
			eb, err := upsert(h, cb, tp.ID, paper, protocol, 21, datum("el-1", "from-b"))
			h.OK(err, "ExtractionUpsert b")

			if ea.ReviewerID != ca.UserID || eb.ReviewerID != cb.UserID {
				t.Errorf("reviewers are %q and %q, want %q and %q",
					ea.ReviewerID, eb.ReviewerID, ca.UserID, cb.UserID)
			}

			list, err := h.S.ExtractionList(h.Ctx, ca, &store.ExtractionListRequest{
				TopicID: tp.ID, PaperID: paper, ProtocolID: protocol,
			})
			h.OK(err, "ExtractionList")
			if len(list.Items) != 2 {
				t.Fatalf("ExtractionList returned %d passes, want 2 — one per reviewer: %+v",
					len(list.Items), list.Items)
			}

			got, err := h.S.ExtractionGet(h.Ctx, cb, &store.ExtractionGetRequest{
				TopicID: tp.ID, PaperID: paper, ProtocolID: protocol, ReviewerID: ca.UserID,
			})
			h.OK(err, "ExtractionGet of another reviewer's pass")
			if v := values(got)["el-1"]; v != "from-a" {
				t.Errorf("ExtractionGet for reviewer a returned %q, want \"from-a\"", v)
			}
		},
	}, {
		Name: "Extraction/AbsentElementsAreUntouched",
		Why: "Upsert merges per element. Omission cannot mean deletion, because neither " +
			"store can tell \"not sent\" from \"deleted\" when each element is its own key.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			paper, protocol := h.ID(store.KindPaper), h.ID(store.KindProtocol)

			_, err := upsert(h, c, tp.ID, paper, protocol, 20,
				datum("el-1", "one"), datum("el-2", "two"), datum("el-3", "three"))
			h.OK(err, "ExtractionUpsert")

			e, err := upsert(h, c, tp.ID, paper, protocol, 21, datum("el-2", "TWO"))
			h.OK(err, "ExtractionUpsert partial")

			got := values(e)
			if got["el-1"] != "one" || got["el-2"] != "TWO" || got["el-3"] != "three" {
				t.Errorf("after a partial upsert the elements are %v, want el-1=one el-2=TWO el-3=three", got)
			}
		},
	}, {
		Name: "Extraction/RetractClearsOnlyItsOwnElement",
		Why:  "Retraction has to be explicit, and it has to be narrow.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			paper, protocol := h.ID(store.KindPaper), h.ID(store.KindProtocol)

			_, err := upsert(h, c, tp.ID, paper, protocol, 20,
				datum("el-1", "one"), datum("el-2", "two"))
			h.OK(err, "ExtractionUpsert")

			e, err := upsert(h, c, tp.ID, paper, protocol, 21,
				store.Datum{ProtocolElementID: "el-1", Retract: true})
			h.OK(err, "ExtractionUpsert retract")

			got := values(e)
			if _, still := got["el-1"]; still {
				t.Errorf("el-1 survived a Retract: %v", got)
			}
			if got["el-2"] != "two" {
				t.Errorf("el-2 did not survive another element's Retract: %v", got)
			}
		},
	}, {
		Name: "Extraction/AMalformedDatumWritesNothing",
		Why: "One method is one atomic unit of work, so a fan-out is all-or-nothing. " +
			"demo inserts a review row and then loops N unbatched inserts with no " +
			"transaction, so a failure at element 4 of 9 leaves a partial extraction " +
			"behind an id that reads as complete. This is the cheap fault injection: a " +
			"malformed element the store itself must reject.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			paper, protocol := h.ID(store.KindPaper), h.ID(store.KindProtocol)

			_, err := upsert(h, c, tp.ID, paper, protocol, 20, datum("el-0", "already here"))
			h.OK(err, "ExtractionUpsert seed")

			_, err = upsert(h, c, tp.ID, paper, protocol, 21,
				datum("el-1", "good"),
				datum("el-2", "good"),
				store.Datum{ProtocolElementID: "", Value: "malformed"},
				datum("el-4", "good"),
			)
			h.WantCode(err, store.CodeInvalid, "ExtractionUpsert with a malformed datum")

			e, err := h.S.ExtractionGet(h.Ctx, c, &store.ExtractionGetRequest{
				TopicID: tp.ID, PaperID: paper, ProtocolID: protocol, ReviewerID: c.UserID,
			})
			h.OK(err, "ExtractionGet after a rejected upsert")
			got := values(e)
			if len(got) != 1 || got["el-0"] != "already here" {
				t.Errorf("a rejected upsert left state behind: %v — the elements before the "+
					"malformed one must not have been written", got)
			}
			if !e.Recorded.Equal(h.At(20)) {
				t.Errorf("Recorded is %v, want the seed's %v: the rejected call must not have "+
					"advanced it", e.Recorded, h.At(20))
			}
		},
	}, {
		Name: "Extraction/AnUnreviewedPaperListsEmpty",
		Why: "Nobody having reviewed is not an error. Returning NotFound would make the " +
			"adjudication read impossible to distinguish from a bad paper id.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)

			paper, protocol := h.ID(store.KindPaper), h.ID(store.KindProtocol)

			list, err := h.S.ExtractionList(h.Ctx, c, &store.ExtractionListRequest{
				TopicID: tp.ID, PaperID: paper, ProtocolID: protocol,
			})
			h.OK(err, "ExtractionList on an unreviewed paper")
			if len(list.Items) != 0 {
				t.Errorf("want an empty list, got %+v", list.Items)
			}

			_, err = h.S.ExtractionGet(h.Ctx, c, &store.ExtractionGetRequest{
				TopicID: tp.ID, PaperID: paper, ProtocolID: protocol,
				ReviewerID: c.UserID,
			})
			h.WantCode(err, store.CodeNotFound, "ExtractionGet on an unreviewed paper")
		},
	}}
}

// --- errors ---------------------------------------------------------------

func errorCases() []Case {
	return []Case{{
		Name: "Errors/AreTypedAndInTheVocabulary",
		Why: "The shared layer maps a status from a Code. A bare driver error forces it " +
			"to string-match instead, which is what infra does today by turning every " +
			"application error into a 500 carrying the raw backend string.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)

			bad := []struct {
				what string
				call func() error
			}{
				{"UserGet on nothing", func() error {
					_, err := h.S.UserGet(h.Ctx, c, &store.UserGetRequest{UserID: ""})
					return err
				}},
				{"TopicCreate with no id", func() error {
					_, err := h.S.TopicCreate(h.Ctx, c, &store.TopicCreateRequest{
						Created: h.At(2), Name: "n", Description: "d"})
					return err
				}},
				{"PropCreate with no topic", func() error {
					_, err := h.S.PropCreate(h.Ctx, c, &store.PropCreateRequest{
						PropID: h.ID(store.KindProp), Created: h.At(3), Description: "d"})
					return err
				}},
				{"VoteSet with no prop", func() error {
					_, err := h.S.VoteSet(h.Ctx, c, &store.VoteSetRequest{
						TopicID: tp.ID, Position: store.PositionFor, Cast: h.At(10)})
					return err
				}},
				{"Attestation on a malformed ref", func() error {
					_, err := h.S.Attestation(h.Ctx, c, &store.AttestationRequest{
						Subject: store.Ref{Kind: store.KindProp, Key: nil}})
					return err
				}},
			}

			for _, b := range bad {
				code := h.AnyCode(b.call(), b.what)
				switch code {
				case store.CodeInternal, store.CodeUnspecified:
					t.Errorf("%s: got %v. A malformed request has a name in the taxonomy; "+
						"Internal moves the caller's problem into a log line", b.what, code)
				}
			}
		},
	}, {
		Name: "Errors/ZeroCallerIsUnauthenticated",
		Why:  "Rather than writing an unattributed record. Attribution is structural, so its absence must be an error.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			p := h.Prop(c, tp.ID)

			_, err := h.S.VoteSet(h.Ctx, store.Caller{}, &store.VoteSetRequest{
				TopicID: tp.ID, PropID: p.ID, Position: store.PositionFor, Cast: h.At(10),
			})
			h.WantCode(err, store.CodeUnauthenticated, "VoteSet with a zero caller")

			_, err = h.S.ExtractionUpsert(h.Ctx, store.Caller{}, &store.ExtractionUpsertRequest{
				TopicID: tp.ID, PaperID: h.ID(store.KindPaper), ProtocolID: h.ID(store.KindProtocol),
				Data:     []store.Datum{{ProtocolElementID: h.ID(store.KindProtocolElement), Value: "v"}},
				Recorded: h.At(20),
			})
			h.WantCode(err, store.CodeUnauthenticated, "ExtractionUpsert with a zero caller")
		},
	}, {
		Name: "Errors/ACancelledContextIsDeadlineExceeded",
		Why: "Every method takes a context precisely so this is reachable. infra's " +
			"DataSource takes none, so today a slow request cannot be cancelled, only " +
			"globally capped at gateway construction.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			_, err := h.S.UserGet(ctx, c, &store.UserGetRequest{UserID: c.UserID})
			h.WantCode(err, store.CodeDeadlineExceeded, "UserGet with a cancelled context")
		},
	}, {
		Name: "Errors/ConflictIsReachable",
		Why: "Conflict is the one asymmetric code: Fabric raises it structurally at MVCC " +
			"validation, Postgres at READ COMMITTED never does. A code reachable from one " +
			"backend and not the other means callers write handling that is untested " +
			"everywhere it does not occur.",
		Fn: func(t *testing.T, h *H) {
			if h.cfg.Contend == nil {
				t.Skip("Config.Contend not supplied: this implementation has not shown that " +
					"store.CodeConflict is reachable at all, so nothing here checks that a " +
					"caller's retry path is ever exercised against it")
			}
			err := h.cfg.Contend(t, h.S)
			h.WantCode(err, store.CodeConflict, "the error Config.Contend produced")
		},
	}}
}

// --- attestation ----------------------------------------------------------

func attestationCases() []Case {
	return []Case{{
		Name: "Attestation/LevelIsAlwaysPopulated",
		Why: "Guarantee strength is a value, not an absence. A caller that needs proof " +
			"asks and gets a definite answer from every backend, rather than inferring it " +
			"from a nil field only one backend ever fills.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			p := h.Prop(c, tp.ID)

			a, err := h.S.Attestation(h.Ctx, c, &store.AttestationRequest{
				Subject: store.PropRef(tp.ID, p.ID)})
			h.OK(err, "Attestation")

			if a.Level < store.LevelRecorded {
				t.Fatalf("Level is %d, want at least LevelRecorded", a.Level)
			}
			if a.Author != c.UserID {
				t.Errorf("Author is %q, want the writing caller %q", a.Author, c.UserID)
			}
			if !a.Recorded.Equal(p.Created) {
				t.Errorf("Recorded is %v, want the record's minted time %v", a.Recorded, p.Created)
			}
			if !a.Subject.Equal(store.PropRef(tp.ID, p.ID)) {
				t.Errorf("Subject is %v, want %v", a.Subject, store.PropRef(tp.ID, p.ID))
			}
			if a.Level >= store.LevelCommitted && a.TxID == "" {
				t.Errorf("Level is %d but TxID is empty: at Committed and above a record has a position", a.Level)
			}
			if a.Level == store.LevelEndorsed && len(a.Endorsers) == 0 {
				t.Errorf("Level is Endorsed but no endorsers are named, so the claim is not checkable")
			}
		},
	}, {
		Name: "Attestation/CarriesTheCallerSignatureVerbatim",
		Why: "The interface's whole provenance job is moving a caller-signed statement " +
			"down intact. An implementation that drops it makes the ledger an expensive " +
			"audit log of what the API server asserted.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			signed := c
			signed.Signed = h.Sig("personal-key-1", "org-key-fda")

			tp := h.Topic(signed)
			p := h.Prop(signed, tp.ID)

			a, err := h.S.Attestation(h.Ctx, c, &store.AttestationRequest{
				Subject: store.PropRef(tp.ID, p.ID)})
			h.OK(err, "Attestation")
			if !a.Signed.Equal(signed.Signed) {
				t.Errorf("Attestation returned signature %+v, want %+v", a.Signed, signed.Signed)
			}

			// An unsigned write reports nil rather than an invented signature.
			p2 := h.Prop(c, tp.ID)
			a2, err := h.S.Attestation(h.Ctx, c, &store.AttestationRequest{
				Subject: store.PropRef(tp.ID, p2.ID)})
			h.OK(err, "Attestation of an unsigned write")
			if a2.Signed != nil {
				t.Errorf("an unsigned write reports %+v, want nil", a2.Signed)
			}
		},
	}, {
		Name: "Attestation/CapacityIsStoredNotDerived",
		Why: "A vote cast as an FDA official stays stamped FDA after the person joins a " +
			"sponsor. Capacity is fixed at the moment of the action, so it is storage, not display.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)

			asFDA := c
			asFDA.Signed = h.Sig("personal-key-1", "org-key-fda")
			first := h.Prop(asFDA, tp.ID)

			asSponsor := c
			asSponsor.Signed = h.Sig("personal-key-1", "org-key-sponsor")
			h.Prop(asSponsor, tp.ID)

			a, err := h.S.Attestation(h.Ctx, c, &store.AttestationRequest{
				Subject: store.PropRef(tp.ID, first.ID)})
			h.OK(err, "Attestation")
			if a.Signed == nil || a.Signed.OrgKeyID != "org-key-fda" {
				t.Errorf("the earlier record's capacity is %+v, want org-key-fda: a later "+
					"action in another capacity must not restamp it", a.Signed)
			}
		},
	}, {
		Name: "Attestation/AnAbsentSubjectIsNotFound",
		Why:  "So a caller can tell \"no proof available\" from \"no such record\".",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			_, err := h.S.Attestation(h.Ctx, c, &store.AttestationRequest{
				Subject: store.PropRef(tp.ID, h.ID(store.KindProp))})
			h.WantCode(err, store.CodeNotFound, "Attestation of an absent prop")
		},
	}}
}

// --- concurrency ----------------------------------------------------------

func concurrencyCases() []Case {
	return []Case{{
		Name: "Concurrency/ContendedRecastsConvergeOrConflict",
		Why: "Every write here is an upsert on a natural key or a create with a " +
			"caller-supplied id, so a conflicted transaction can be retried with the " +
			"identical request. What must not happen is a third outcome: a duplicate row, " +
			"a lost record, or a code outside the taxonomy.",
		Fn: func(t *testing.T, h *H) {
			c, _ := h.User("a@example.test")
			tp := h.Topic(c)
			p := h.Prop(c, tp.ID)

			const n = 8
			var wg sync.WaitGroup
			errs := make([]error, n)
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					_, err := h.S.VoteSet(h.Ctx, c, &store.VoteSetRequest{
						TopicID: tp.ID, PropID: p.ID, Position: store.PositionFor,
						Explanation: "concurrent", Cast: h.At(30 + i),
					})
					errs[i] = err
				}(i)
			}
			wg.Wait()

			ok := 0
			for i, err := range errs {
				if err == nil {
					ok++
					continue
				}
				if code := store.CodeOf(err); code != store.CodeConflict {
					t.Errorf("concurrent VoteSet %d failed with %v, want nil or CodeConflict: %v", i, code, err)
				}
			}
			if ok == 0 {
				t.Fatal("every concurrent VoteSet conflicted; at least one must commit")
			}

			vl, err := h.S.VoteList(h.Ctx, c, &store.VoteListRequest{TopicID: tp.ID, PropID: p.ID})
			h.OK(err, "VoteList")
			if len(vl.Items) != 1 {
				t.Errorf("after %d contended recasts there are %d votes, want 1", n, len(vl.Items))
			}
		},
	}}
}
