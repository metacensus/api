package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contract "github.com/metacensus/api/go/contract"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/server/routes"
	"github.com/metacensus/api/go/signing"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// --- test harness -----------------------------------------------------------

// newTestServer wires a Handlers over a fresh fakeStore onto a StdMux, with a
// deterministic id minter and clock so a test can assert the minted fields.
func newTestServer(t *testing.T) (*fakeStore, http.Handler) {
	t.Helper()
	fake := newFakeStore()
	var n int64
	h := New(Config{
		Store:      fake,
		Now:        func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		NewID:      func() string { return fmt.Sprintf("id-%d", atomic.AddInt64(&n, 1)) },
		BcryptCost: 4, // bcrypt.MinCost: fast, this is a test
	})
	mux := http.NewServeMux()
	h.Register(server.StdMux{ServeMux: mux}, &server.Runtime{Prefix: routes.Prefix})
	return fake, mux
}

// client is a signing API client over an in-process handler.
type client struct {
	t     *testing.T
	mux   http.Handler
	priv  *ecdsa.PrivateKey
	token string
}

func newClient(t *testing.T, mux http.Handler) *client {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &client{t: t, mux: mux, priv: priv}
}

// sign returns a UserSignature over content, filling the signed attributes and
// the value. inlineKey embeds the public key (sign-up only), and signerID is
// empty on sign-up and the caller's id afterward.
func (c *client) sign(content proto.Message, signerID string, inlineKey bool) *v1.UserSignature {
	c.t.Helper()
	keyID, err := signing.KeyID(&c.priv.PublicKey)
	if err != nil {
		c.t.Fatalf("keyID: %v", err)
	}
	sig := &v1.UserSignature{
		SignerId:    signerID,
		KeyId:       keyID,
		Alg:         v1.UserSignature_Es384,
		Spec:        signing.Spec,
		ContentType: string(content.ProtoReflect().Descriptor().FullName()),
		SigningTime: timestamppb.New(time.Unix(1_699_000_000, 0).UTC()),
	}
	if inlineKey {
		pub, err := signing.EncodePublicKey(&c.priv.PublicKey)
		if err != nil {
			c.t.Fatalf("encode key: %v", err)
		}
		sig.PublicKey = pub
	}
	if err := signing.Sign(c.priv, content, sig); err != nil {
		c.t.Fatalf("sign: %v", err)
	}
	return sig
}

// do sends req to path and returns the recorder; body is marshalled with the
// contract encoder, and the bearer token is attached when the client holds one.
func (c *client) do(method, path string, body proto.Message) *httptest.ResponseRecorder {
	c.t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := contract.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal request: %v", err)
		}
		r = httptest.NewRequest(method, routes.Prefix+path, strings.NewReader(string(raw)))
	} else {
		r = httptest.NewRequest(method, routes.Prefix+path, nil)
	}
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	rec := httptest.NewRecorder()
	c.mux.ServeHTTP(rec, r)
	return rec
}

// decode unmarshals a 200 response into msg, failing on any other status.
func decode(t *testing.T, rec *httptest.ResponseRecorder, msg proto.Message) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	if err := contract.Unmarshal(rec.Body.Bytes(), msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// --- the representative journey ---------------------------------------------

// TestJourney is one typical user history through the whole stack: sign up, get
// self, create a topic, create a prop under it, vote, and read the vote back.
// It exercises binding, the session middleware, minting, record assembly and
// the store together — the integration test the Go seam makes possible.
func TestJourney(t *testing.T) {
	_, mux := newTestServer(t)
	c := newClient(t, mux)

	// Sign up. The signer_id is empty and the key travels inline.
	user := &v1.User{Name: "Ada", Email: "ada@example.com", Country: "GB"}
	var session v1.Session
	decode(t, c.do("POST", "/signup", &v1.SignUpRequest{
		Content:       user,
		Password:      "correct horse battery staple",
		UserSignature: c.sign(user, "", true),
	}), &session)
	if session.GetToken() == "" {
		t.Fatal("sign-up returned no token")
	}
	c.token = session.GetToken()

	// Learn our own id.
	var self v1.UserSigned
	decode(t, c.do("GET", "/self", nil), &self)
	id := self.GetId()
	if id == "" {
		t.Fatal("self has no id")
	}
	if self.GetContent().GetEmail() != "ada@example.com" {
		t.Fatalf("self email = %q", self.GetContent().GetEmail())
	}

	// Create a topic, now signing as our id.
	topic := &v1.Topic{Name: "Elections", Description: "voting reform"}
	var topicRec v1.TopicSigned
	decode(t, c.do("POST", "/topic", &v1.TopicCreateRequest{
		Content:       topic,
		UserSignature: c.sign(topic, id, false),
	}), &topicRec)
	topicID := topicRec.GetId()
	if topicID == "" || topicRec.GetRecorded() == nil {
		t.Fatalf("topic not minted: %+v", &topicRec)
	}

	// Create a prop under the topic.
	prop := &v1.Prop{TopicId: topicID, Description: "ranked choice"}
	var propRec v1.PropSigned
	decode(t, c.do("POST", "/topic/"+topicID+"/prop", &v1.PropCreateRequest{
		TopicId:       topicID,
		Content:       prop,
		UserSignature: c.sign(prop, id, false),
	}), &propRec)
	propID := propRec.GetId()
	if propID == "" {
		t.Fatal("prop has no id")
	}

	// Vote on the prop.
	vote := &v1.Vote{TopicId: topicID, PropId: propID, UserId: id}
	var voteRec v1.VoteSigned
	decode(t, c.do("POST", "/topic/"+topicID+"/prop/"+propID+"/vote", &v1.VoteSetRequest{
		TopicId:       topicID,
		PropId:        propID,
		Content:       vote,
		UserSignature: c.sign(vote, id, false),
	}), &voteRec)

	// Read the vote back.
	var votes v1.VoteList
	decode(t, c.do("GET", "/topic/"+topicID+"/prop/"+propID+"/vote", nil), &votes)
	if len(votes.GetItems()) != 1 || votes.GetItems()[0].GetContent().GetUserId() != id {
		t.Fatalf("vote not read back: %+v", &votes)
	}
}

// --- unit tests -------------------------------------------------------------

// TestMintingPreservesSignature is the core invariant: the id and recorded time
// the server mints sit outside the {content, signature} the client hashed, so a
// signature that stood before assembly still verifies after it.
func TestMintingPreservesSignature(t *testing.T) {
	fake, mux := newTestServer(t)
	c := newClient(t, mux)

	user := &v1.User{Name: "Ada", Email: "ada@example.com"}
	var session v1.Session
	decode(t, c.do("POST", "/signup", &v1.SignUpRequest{
		Content: user, Password: "pw", UserSignature: c.sign(user, "", true),
	}), &session)
	c.token = session.GetToken()
	var self v1.UserSigned
	decode(t, c.do("GET", "/self", nil), &self)
	id := self.GetId()

	topic := &v1.Topic{Name: "T"}
	sig := c.sign(topic, id, false)
	var rec v1.TopicSigned
	decode(t, c.do("POST", "/topic", &v1.TopicCreateRequest{Content: topic, UserSignature: sig}), &rec)

	if rec.GetId() == "" || rec.GetRecorded() == nil {
		t.Fatal("server did not mint id/recorded")
	}
	stored := fake.topics[rec.GetId()]
	if err := signing.Verify(&c.priv.PublicKey, stored.GetContent(), stored.GetUserSignature()); err != nil {
		t.Fatalf("signature broke across assembly: %v", err)
	}
}

// TestCreateTopicSignerMismatch is the fail-fast: a signature whose signer is
// not the session caller is a 401 before the store is ever called.
func TestCreateTopicSignerMismatch(t *testing.T) {
	_, mux := newTestServer(t)
	c := newClient(t, mux)
	user := &v1.User{Name: "Ada", Email: "ada@example.com"}
	var session v1.Session
	decode(t, c.do("POST", "/signup", &v1.SignUpRequest{
		Content: user, Password: "pw", UserSignature: c.sign(user, "", true),
	}), &session)
	c.token = session.GetToken()

	topic := &v1.Topic{Name: "T"}
	// Sign as a different id than the session caller.
	rec := c.do("POST", "/topic", &v1.TopicCreateRequest{
		Content: topic, UserSignature: c.sign(topic, "someone-else", false),
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}

// TestUnauthenticatedWithoutToken: an authenticated route with no session is a
// 401, and the handler never runs.
func TestUnauthenticatedWithoutToken(t *testing.T) {
	_, mux := newTestServer(t)
	c := newClient(t, mux)
	rec := c.do("GET", "/self", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestSignUpStructuralChecks rejects a missing inline key and a pre-set signer.
func TestSignUpStructuralChecks(t *testing.T) {
	_, mux := newTestServer(t)
	c := newClient(t, mux)
	user := &v1.User{Name: "Ada", Email: "ada@example.com"}

	// No inline public key.
	noKey := c.sign(user, "", false)
	if rec := c.do("POST", "/signup", &v1.SignUpRequest{Content: user, Password: "pw", UserSignature: noKey}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key: status = %d, want 400", rec.Code)
	}
	// Signer id pre-set.
	withSigner := c.sign(user, "not-empty", true)
	if rec := c.do("POST", "/signup", &v1.SignUpRequest{Content: user, Password: "pw", UserSignature: withSigner}); rec.Code != http.StatusBadRequest {
		t.Fatalf("preset signer: status = %d, want 400", rec.Code)
	}
}

// TestLoginUnknownAndWrongAreIdentical: an unknown email and a wrong password
// return the same status, so login cannot be used to probe which emails exist.
func TestLoginUnknownAndWrongAreIdentical(t *testing.T) {
	_, mux := newTestServer(t)
	c := newClient(t, mux)
	user := &v1.User{Name: "Ada", Email: "ada@example.com"}
	var session v1.Session
	decode(t, c.do("POST", "/signup", &v1.SignUpRequest{
		Content: user, Password: "right", UserSignature: c.sign(user, "", true),
	}), &session)

	unknown := c.do("POST", "/login", &v1.LoginRequest{Email: "nobody@example.com", Password: "x"})
	wrong := c.do("POST", "/login", &v1.LoginRequest{Email: "ada@example.com", Password: "wrong"})
	if unknown.Code != http.StatusUnauthorized || wrong.Code != http.StatusUnauthorized {
		t.Fatalf("unknown=%d wrong=%d, want both 401", unknown.Code, wrong.Code)
	}
	// And the right password works.
	ok := c.do("POST", "/login", &v1.LoginRequest{Email: "ada@example.com", Password: "right"})
	if ok.Code != http.StatusOK {
		t.Fatalf("correct login: status = %d, want 200; body %s", ok.Code, ok.Body.String())
	}
}

// TestMapErr pins every store.Kind to its HTTP status.
func TestMapErr(t *testing.T) {
	cases := []struct {
		kind store.Kind
		want int
	}{
		{store.NotFound, http.StatusNotFound},
		{store.AlreadyExists, http.StatusConflict},
		{store.InvalidContent, http.StatusUnprocessableEntity},
		{store.SignatureInvalid, http.StatusBadRequest},
		{store.Unauthenticated, http.StatusUnauthorized},
		{store.Unavailable, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		if got := mapErr(tc.kind).Status; got != tc.want {
			t.Errorf("mapErr(%s).Status = %d, want %d", tc.kind, got, tc.want)
		}
	}
	// An error carrying no Kind is an internal 500.
	if got := mapErr(fmt.Errorf("raw backend boom")).Status; got != http.StatusInternalServerError {
		t.Errorf("unkinded error mapped to %d, want 500", got)
	}
}
