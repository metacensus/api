// Command wireserver serves the generated routes so ts/test/wire.test.mjs can
// drive the generated TypeScript client against them over real HTTP. It is a
// test fixture, not a server: every handler echoes its request back through
// the response message, because what is under test is the encoding, not any
// behaviour.
//
// It lives in the routegen module, which is never published and never
// imported: the contract's own module requires exactly two things, and a test
// fixture is not one of them.
//
// # Signatures
//
// It also verifies and makes them, which a real API server would not: a user
// signature is checked by the persistence layer inside the chaincode boundary,
// and the edge never looks. Doing both halves in one process here is
// deliberate and is not a model of the deployment. What the test needs is the
// one thing the split would hide — that go/signing and ts/src/signing.ts
// compute the same digest over the same document — and the cheapest way to
// show it is to have each language sign something the other verifies.
//
// It prints its own public key, then "listening <addr>" on a port the OS
// chooses, then serves until killed.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/routes"
	"github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/signing"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// recorded and signingTime are fixed so the test can assert the exact RFC 3339
// strings protojson emits for a google.protobuf.Timestamp — the recording time
// the server chose and the time the signer claimed, which are different facts
// and, here, different values.
var (
	recorded    = timestamppb.New(time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC))
	signingTime = timestamppb.New(time.Date(2023, 11, 14, 22, 13, 19, 0, time.UTC))
)

// serverKey is generated per run rather than checked in: the test reads the
// public half off stdout, so nothing here is a credential and nothing has to
// be rotated.
var serverKey *ecdsa.PrivateKey

// sign fills in the conventional attributes and signs, so the fixture's
// handlers read as one line each. A real signer is a participant and would
// choose these itself; this one stands in for one.
func sign(content proto.Message) *v1.UserSignature {
	id, err := signing.KeyID(&serverKey.PublicKey)
	if err != nil {
		fail(err)
	}
	sig := &v1.UserSignature{
		SignerId:    "wireserver",
		KeyId:       id,
		Alg:         v1.UserSignature_Es384,
		SigningTime: signingTime,
		Spec:        signing.Spec,
		ContentType: string(content.ProtoReflect().Descriptor().FullName()),
	}
	if err := signing.Sign(serverKey, content, sig); err != nil {
		fail(err)
	}
	return sig
}

type topics struct {
	server.UnimplementedTopicRoutes
}

func (topics) GetTopic(_ context.Context, req *v1.TopicGetRequest) (*v1.Topic, error) {
	if req.TopicId == "missing" {
		return nil, server.Errorf(http.StatusNotFound, "topic_not_found", "no topic %q", req.TopicId)
	}
	content := &v1.TopicContent{Name: "n", Description: ""}
	return &v1.Topic{Id: req.TopicId, Recorded: recorded, Content: content, UserSignature: sign(content)}, nil
}

func (topics) ListTopics(context.Context, *v1.TopicListRequest) (*v1.TopicList, error) {
	content := &v1.TopicContent{Name: "n", Description: ""}
	return &v1.TopicList{Items: []*v1.Topic{
		{Id: "t1", Recorded: recorded, Content: content, UserSignature: sign(content)},
	}}, nil
}

// CreateTopic echoes the content and the signature it was handed, unchanged.
// That is the rule the whole envelope exists for — the server wraps, it never
// modifies — and it is what lets the test verify, on the TypeScript side, the
// signature it made there against the document that came back.
func (topics) CreateTopic(_ context.Context, req *v1.TopicCreateRequest) (*v1.Topic, error) {
	return &v1.Topic{
		Id: "new", Recorded: recorded,
		Content: req.Content, UserSignature: req.UserSignature,
	}, nil
}

type props struct{ server.UnimplementedPropRoutes }

func (props) CreateProp(_ context.Context, req *v1.PropCreateRequest) (*v1.Prop, error) {
	// The minted id echoes the topic id the path bound, which is what the
	// test needs to see: that the server bound it from the path after the
	// client stripped the top-level copy out of the body. The signed copy
	// inside content travelled in the body and is returned untouched.
	return &v1.Prop{
		Id: req.TopicId, Recorded: recorded,
		Content: req.Content, UserSignature: req.UserSignature,
	}, nil
}

type auth struct{ server.UnimplementedAuthRoutes }

// SignUp is the one route where a signer's key legitimately travels inline,
// because the signer has no id yet to look one up by. The fixture does what a
// persistence layer would: resolve the key the signature carries, check the
// thumbprint binds it, and verify. A failure comes back as a 400 so the test
// can tell a rejected signature from a crash.
func (auth) SignUp(_ context.Context, req *v1.SignUpRequest) (*v1.Session, error) {
	pub, err := signing.PublicKeyOf(req.UserSignature)
	if err != nil {
		return nil, server.Errorf(http.StatusBadRequest, "key_not_bound", "%v", err)
	}
	if err := signing.Verify(pub, req.Content, req.UserSignature); err != nil {
		return nil, server.Errorf(http.StatusBadRequest, "signature_invalid", "%v", err)
	}
	return &v1.Session{Token: "signed-up:" + req.Content.GetEmail()}, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "wireserver:", err)
	os.Exit(1)
}

func main() {
	var err error
	if serverKey, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader); err != nil {
		fail(err)
	}
	pub, err := signing.EncodePublicKey(&serverKey.PublicKey)
	if err != nil {
		fail(err)
	}

	mux := http.NewServeMux()
	std := server.StdMux{ServeMux: mux}
	rt := &server.Runtime{Prefix: routes.Prefix}
	server.RegisterAuthRoutes(std, rt, auth{})
	server.RegisterTopicRoutes(std, rt, topics{})
	server.RegisterPropRoutes(std, rt, props{})
	server.RegisterUserRoutes(std, rt, server.UnimplementedUserRoutes{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fail(err)
	}
	// Before "listening", which is what the test waits for: by the time it
	// has an address it has the key too.
	fmt.Println("signing-key", pub)
	fmt.Println("listening", ln.Addr().String())
	os.Stdout.Sync()
	if err := http.Serve(ln, mux); err != nil {
		fail(err)
	}
}
