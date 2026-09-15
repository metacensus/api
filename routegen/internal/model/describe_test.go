package model

import (
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// What describe refuses, as a list rather than as a memory.
//
// Every rejection used to be verified by mutating a .proto by hand and
// reading the output — which is assertion confidence on the one node every
// renderer derives from, and it is why two accepted-but-mis-rendered shapes
// ({id=**} and a repeated parameter) sat here unnoticed. This feeds describe
// synthetic rpcs instead, so the set of refused shapes is something the build
// knows.
//
// The rpcs are synthetic; the request and response messages are the real
// metacensus.v1 ones, resolved out of the global registry, because describe
// reads Go type names off the generated structs and a made-up message has
// none.
func TestDescribeRejects(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rpc   *descriptorpb.MethodDescriptorProto
		wants string
	}{
		{
			name:  "no http option",
			rpc:   rpc("NoOption", "TopicGetRequest", "Topic", nil),
			wants: "no google.api.http option",
		},
		{
			name: "additional bindings",
			rpc: rpc("Extra", "TopicGetRequest", "Topic", &annotations.HttpRule{
				Pattern:            &annotations.HttpRule_Get{Get: "/topic/{topic_id}"},
				AdditionalBindings: []*annotations.HttpRule{{Pattern: &annotations.HttpRule_Get{Get: "/t/{topic_id}"}}},
			}),
			wants: "additional_bindings",
		},
		{
			name: "unsupported http pattern",
			rpc: rpc("Custom", "TopicGetRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Custom{Custom: &annotations.CustomHttpPattern{Kind: "OPTIONS", Path: "/topic"}},
			}),
			wants: "unsupported http pattern",
		},
		{
			name: "unterminated path parameter",
			rpc: rpc("Unterminated", "TopicGetRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/topic/{topic_id"},
			}),
			wants: "unterminated path parameter",
		},
		{
			// google.api.http's multi-segment wildcard. Accepted before this
			// test existed, and silently narrowed to one segment by every
			// renderer.
			name: "multi-segment path pattern",
			rpc: rpc("Wildcard", "TopicGetRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/topic/{topic_id=**}"},
			}),
			wants: "segment pattern",
		},
		{
			name: "single-segment path pattern",
			rpc: rpc("Star", "TopicGetRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/topic/{topic_id=*}"},
			}),
			wants: "segment pattern",
		},
		{
			// ServeMux panics on a duplicate wildcard name, so this used to
			// reach a reader as a stack trace out of go/server's tests.
			name: "path parameter twice",
			rpc: rpc("Twice", "TopicGetRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/topic/{topic_id}/x/{topic_id}"},
			}),
			wants: "appears twice",
		},
		{
			name: "path parameter is not a field",
			rpc: rpc("Unknown", "TopicGetRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/topic/{nope}"},
			}),
			wants: "is not a field of",
		},
		{
			name: "body names a field",
			rpc: rpc("NamedBody", "TopicCreateRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Post{Post: "/topic"},
				Body:    "name",
			}),
			wants: `only body: "*" or no body is supported`,
		},
		{
			name: "body is not a field",
			rpc: rpc("BogusBody", "TopicCreateRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Post{Post: "/topic"},
				Body:    "nope",
			}),
			wants: "is not a field of",
		},
		{
			// PropCitation.start is uint32. Path segments are strings on the
			// wire and the generated binding assigns one straight to a struct
			// field.
			name: "non-string path parameter",
			rpc: rpc("NotAString", "PropCitation", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/x/{start}"},
			}),
			wants: "not a singular string",
		},
		{
			// Prop.citations is repeated PropCitation: a message-typed field
			// the query string cannot carry.
			name: "message-typed query parameter",
			rpc: rpc("MessageQuery", "Prop", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/x/{id}"},
			}),
			wants: "only scalar query fields are supported",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			md := method(t, tc.rpc)
			r, err := describe(testPkg, md)
			if err == nil {
				t.Fatalf("accepted, as %+v", r)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error %q does not mention %q", err, tc.wants)
			}
			if !strings.Contains(err.Error(), string(md.Name())) {
				t.Errorf("error %q does not name the rpc", err)
			}
		})
	}
}

// The shapes describe accepts, so the list above is a boundary rather than a
// blanket refusal.
func TestDescribeAccepts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rpc    *descriptorpb.MethodDescriptorProto
		path   string
		params []string
		body   string
	}{
		{
			name: "no path parameters, no body",
			rpc: rpc("List", "TopicListRequest", "TopicList", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/topic"},
			}),
			path: "/topic",
		},
		{
			name: "snake_case parameter is rewritten to its json name",
			rpc: rpc("Get", "TopicGetRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/topic/{topic_id}"},
			}),
			path: "/topic/{topicId}", params: []string{"topicId"},
		},
		{
			name: "two distinct parameters and a star body",
			rpc: rpc("SetVote", "VoteSetRequest", "Vote", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Post{Post: "/topic/{topic_id}/prop/{prop_id}/vote"},
				Body:    "*",
			}),
			path:   "/topic/{topicId}/prop/{propId}/vote",
			params: []string{"topicId", "propId"}, body: "*",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := describe(testPkg, method(t, tc.rpc))
			if err != nil {
				t.Fatal(err)
			}
			if r.Path != tc.path {
				t.Errorf("path %q, want %q", r.Path, tc.path)
			}
			if strings.Join(r.Params, ",") != strings.Join(tc.params, ",") {
				t.Errorf("params %v, want %v", r.Params, tc.params)
			}
			if r.Body != tc.body {
				t.Errorf("body %q, want %q", r.Body, tc.body)
			}
		})
	}
}

// testPkg is the contract package these synthetic routes are built over,
// looked up by name rather than by position so reordering Packages does not
// silently change what this file tests.
var testPkg = packageNamed("metacensus.v1")

func packageNamed(proto string) Package {
	for _, pkg := range Packages {
		if pkg.Proto == proto {
			return pkg
		}
	}
	panic("no contract package " + proto)
}

// rpc builds one synthetic method over real metacensus.v1 messages. rule nil
// means no google.api.http option at all.
func rpc(name, in, out string, rule *annotations.HttpRule) *descriptorpb.MethodDescriptorProto {
	m := &descriptorpb.MethodDescriptorProto{
		Name:       proto.String(name),
		InputType:  proto.String("." + testPkg.Proto + "." + in),
		OutputType: proto.String("." + testPkg.Proto + "." + out),
	}
	if rule != nil {
		m.Options = &descriptorpb.MethodOptions{}
		proto.SetExtension(m.Options, annotations.E_Http, rule)
	}
	return m
}

// method compiles a throwaway file holding one service and returns its single
// method. Its dependencies are the real metacensus/v1 files, resolved through
// the global registry, so md.Input() and md.Output() are the same descriptors
// Walk sees and goNames finds the generated Go types behind them.
func method(t *testing.T, m *descriptorpb.MethodDescriptorProto) protoreflect.MethodDescriptor {
	t.Helper()

	var deps []string
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) == testPkg.Proto {
			deps = append(deps, fd.Path())
		}
		return true
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("routegen/describe_test.proto"),
		Package:    proto.String(testPkg.Proto),
		Syntax:     proto.String("proto3"),
		Dependency: deps,
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:   proto.String("DescribeTestRoutes"),
			Method: []*descriptorpb.MethodDescriptorProto{m},
		}},
	}

	fd, err := protodesc.NewFile(fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("building the synthetic file: %v", err)
	}
	return fd.Services().Get(0).Methods().Get(0)
}
