// Command wireserver serves the generated routes so ts/test/wire.test.mjs can
// drive the generated TypeScript client against them over real HTTP. It is a
// test fixture, not a server: every handler echoes its request back through
// the response message, because what is under test is the encoding, not any
// behaviour.
//
// It lives in the chitest module, which is never published and never imported,
// for the same reason chi does: the contract's own module requires exactly two
// things, and a fixture is not one of them.
//
// It prints "listening <addr>" on a port the OS chooses, then serves until
// killed.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/routes"
	"github.com/metacensus/api/go/server"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// created is fixed so the test can assert the exact RFC 3339 string protojson
// emits for a google.protobuf.Timestamp.
var created = timestamppb.New(time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC))

type topics struct{ server.UnimplementedTopicRoutes }

func (topics) GetTopic(_ context.Context, req *v1.TopicGetRequest) (*v1.Topic, error) {
	if req.TopicId == "missing" {
		return nil, server.Errorf(http.StatusNotFound, "topic_not_found", "no topic %q", req.TopicId)
	}
	return &v1.Topic{Id: req.TopicId, Name: "n", Description: "", Created: created}, nil
}

func (topics) ListTopics(context.Context, *v1.TopicListRequest) (*v1.TopicList, error) {
	return &v1.TopicList{Items: []*v1.Topic{{Id: "t1", Name: "n", Description: "", Created: created}}}, nil
}

func (topics) CreateTopic(_ context.Context, req *v1.TopicCreateRequest) (*v1.Topic, error) {
	return &v1.Topic{Id: "new", Name: req.Name, Description: req.Description, Created: created}, nil
}

type props struct{ server.UnimplementedPropRoutes }

func (props) CreateProp(_ context.Context, req *v1.PropCreateRequest) (*v1.Prop, error) {
	// Prop carries no topic id of its own, so the one the path bound comes
	// back as author_id: what the test needs to see is that the server bound
	// it from the path after the client stripped it out of the body.
	return &v1.Prop{
		Id: "p1", AuthorId: req.TopicId,
		Type: req.Type, Description: req.Description, Created: created,
	}, nil
}

func main() {
	mux := http.NewServeMux()
	std := server.StdMux{ServeMux: mux}
	rt := &server.Runtime{Prefix: routes.Prefix}
	server.RegisterTopicRoutes(std, rt, topics{})
	server.RegisterPropRoutes(std, rt, props{})
	server.RegisterUserRoutes(std, rt, server.UnimplementedUserRoutes{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "wireserver:", err)
		os.Exit(1)
	}
	fmt.Println("listening", ln.Addr().String())
	os.Stdout.Sync()
	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(os.Stderr, "wireserver:", err)
		os.Exit(1)
	}
}
