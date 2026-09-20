// Command wireserver serves the generated read routes so ts/test/wire.test.mjs
// can drive the generated TypeScript client against them over real HTTP. It is
// a test fixture, not a server: every handler echoes fixed data, because what
// is under test is the encoding — that protojson and ts-proto emit the same
// document.
//
// It signs the records it returns so the TypeScript side can verify a signature
// Go made; the reverse direction, and every write, are cross-language
// integration concerns deferred to the mock-persistence suite
// (https://github.com/metacensus/api/issues/31).
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
	"github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/server/routes"
	"github.com/metacensus/api/go/signing"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// recorded and signingTime are fixed so the test can assert the exact RFC 3339
// strings protojson emits for a google.protobuf.Timestamp.
var (
	recorded    = timestamppb.New(time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC))
	signingTime = timestamppb.New(time.Date(2023, 11, 14, 22, 13, 19, 0, time.UTC))
)

// Origin is the origin the fixture's participant assertions carry; the
// TypeScript test verifies against it. Exported to stdout so the test needn't
// hardcode a copy.
const Origin = "https://wire.test.example"

// serverKey is generated per run rather than checked in: the test reads the
// public half off stdout, so nothing here is a credential to rotate.
var serverKey *ecdsa.PrivateKey

// sign produces the interpretation and a participant Signature over content, so
// the fixture's handlers read as one line each. It stands in for a passkey: a
// user-verified webauthn.get from Origin, with a software key.
func sign(content proto.Message) (*v1.Interpretation, *v1.Signature) {
	interp := signing.Interpretation(content)
	keyID, err := signing.KeyID(&serverKey.PublicKey)
	if err != nil {
		fail(err)
	}
	challenge, err := signing.UserChallenge(content, interp, keyID, signingTime)
	if err != nil {
		fail(err)
	}
	authData := signing.AuthenticatorData("wire.test.example", signing.FlagUP|signing.FlagUV)
	a, err := signing.Assert(serverKey, challenge, authData, signing.ClientData{Type: signing.TypeGet, Origin: Origin})
	if err != nil {
		fail(err)
	}
	return interp, &v1.Signature{KeyId: keyID, Time: signingTime, Assertion: a}
}

type topics struct {
	server.UnimplementedTopicRoutes
}

func (topics) GetTopic(_ context.Context, req *v1.TopicGetRequest) (*v1.TopicSigned, error) {
	if req.TopicId == "missing" {
		return nil, server.Errorf(http.StatusNotFound, "topic_not_found", "no topic %q", req.TopicId)
	}
	content := &v1.Topic{Name: "n", Description: ""}
	interp, sig := sign(content)
	return &v1.TopicSigned{Id: req.TopicId, Recorded: recorded, Content: content, Interpretation: interp, UserSignature: sig}, nil
}

func (topics) ListTopics(context.Context, *v1.TopicListRequest) (*v1.TopicList, error) {
	content := &v1.Topic{Name: "n", Description: ""}
	interp, sig := sign(content)
	return &v1.TopicList{Items: []*v1.TopicSigned{
		{Id: "t1", Recorded: recorded, Content: content, Interpretation: interp, UserSignature: sig},
	}}, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "wireserver:", err)
	os.Exit(1)
}

func main() {
	var err error
	if serverKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		fail(err)
	}
	pub, err := signing.EncodePublicKey(&serverKey.PublicKey)
	if err != nil {
		fail(err)
	}

	mux := http.NewServeMux()
	std := server.StdMux{ServeMux: mux}
	rt := &server.Runtime{Prefix: routes.Prefix}
	server.RegisterTopicRoutes(std, rt, topics{})
	// User routes stay unimplemented, for the 501 case.
	server.RegisterUserRoutes(std, rt, server.UnimplementedUserRoutes{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fail(err)
	}
	// Before "listening", which is what the test waits for.
	fmt.Println("signing-key", pub)
	fmt.Println("origin", Origin)
	fmt.Println("listening", ln.Addr().String())
	os.Stdout.Sync()
	if err := http.Serve(ln, mux); err != nil {
		fail(err)
	}
}
