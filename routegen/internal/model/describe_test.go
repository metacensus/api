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
			// `content` really is a field of TopicCreateRequest, so this
			// reaches the named-body rejection rather than the missing-field
			// one below.
			name: "body names a field",
			rpc: rpc("NamedBody", "TopicCreateRequest", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Post{Post: "/topic"},
				Body:    "content",
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
			// VoteContent.citations is repeated PropCitation: a message-typed
			// field the query string cannot carry.
			name: "message-typed query parameter",
			rpc: rpc("MessageQuery", "VoteContent", "Topic", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Get{Get: "/x/{topic_id}"},
			}),
			wants: "only scalar query fields are supported",
		},

		{
			// The path binds a field the signed content does not carry, so
			// the record could be filed under one address while attesting to
			// another.
			//
			// SignUpRequest.password is the shape the real messages offer, and
			// it is a good one: password is deliberately outside content,
			// because content is what gets persisted and a password must never
			// be inside a signed document. Binding it from the path would be
			// asking for it to be signed.
			name: "path parameter absent from signed content",
			rpc: rpc("PathOutsideContent", "SignUpRequest", "Session", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Post{Post: "/signup/{password}"},
				Body:    "*",
			}),
			wants: "does not carry it",
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
		name          string
		rpc           *descriptorpb.MethodDescriptorProto
		path          string
		params        []string
		body          string
		signed        bool
		contentParams []string
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
			// Both path ids are repeated inside VoteContent, so this is also
			// the signed case: two ContentParams, each pairing the field the
			// path binds with the signed copy the binding compares it to.
			name: "two distinct parameters and a star body",
			rpc: rpc("SetVote", "VoteSetRequest", "Vote", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Post{Post: "/topic/{topic_id}/prop/{prop_id}/vote"},
				Body:    "*",
			}),
			path:   "/topic/{topicId}/prop/{propId}/vote",
			params: []string{"topicId", "propId"}, body: "*",
			signed: true, contentParams: []string{"topicId", "propId"},
		},
		{
			// A signed route need not bind anything from the path: sign-up
			// carries content and a signature and no parameters at all.
			name: "signed with no path parameters",
			rpc: rpc("SignUp", "SignUpRequest", "Session", &annotations.HttpRule{
				Pattern: &annotations.HttpRule_Post{Post: "/signup"},
				Body:    "*",
			}),
			path: "/signup", body: "*", signed: true,
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
			if r.Signed != tc.signed {
				t.Errorf("signed %v, want %v", r.Signed, tc.signed)
			}
			var got []string
			for _, cp := range r.ContentParams {
				got = append(got, cp.JSONName)
				// Both Go field names come off the generated structs, so an
				// empty one means the content message was read as the wrong
				// type rather than that the field is missing.
				if cp.PathGoField == "" || cp.ContentGoField == "" {
					t.Errorf("%q: Go fields %q / %q", cp.JSONName, cp.PathGoField, cp.ContentGoField)
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.contentParams, ",") {
				t.Errorf("content params %v, want %v", got, tc.contentParams)
			}
		})
	}
}

// The two halves of a signed request travel together, and neither real
// message can express one without the other — so unlike every case above,
// these two shapes are built rather than borrowed.
//
// describeSigned is called directly for that reason: a synthetic message has
// no generated Go type, so a route carrying one cannot survive goNames, and
// these rejections happen before any Go name is needed.
func TestDescribeSignedPairing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field *descriptorpb.FieldDescriptorProto
		wants string
	}{
		{
			name:  "signature without content",
			field: messageField(SignatureField, 1, "."+testPkg.Proto+".UserSignature"),
			wants: "a signature over nothing attests to nothing",
		},
		{
			name:  "content without signature",
			field: messageField(ContentField, 1, "."+testPkg.Proto+".TopicContent"),
			wants: "content nobody signed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			md := messageWith(t, "Synthetic", tc.field)
			_, _, _, _, err := describeSigned(testPkg, md, goType{}, nil)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error %q does not mention %q", err, tc.wants)
			}
		})
	}
}

func messageField(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(number),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(typeName),
	}
}

// messageWith compiles a throwaway file holding one message and returns its
// descriptor. Its dependencies are the real metacensus/v1 files, so a field
// may be typed from them even though the message itself exists only here.
func messageWith(t *testing.T, name string, fields ...*descriptorpb.FieldDescriptorProto) protoreflect.MessageDescriptor {
	t.Helper()

	fdp := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("routegen/describe_test_messages.proto"),
		Package:    proto.String(testPkg.Proto),
		Syntax:     proto.String("proto3"),
		Dependency: contractDeps(),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String(name),
			Field: fields,
		}},
	}
	fd, err := protodesc.NewFile(fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("building the synthetic message: %v", err)
	}
	return fd.Messages().Get(0)
}

// contractDeps is every registered file of the contract package under test,
// which is what lets a synthetic file name the real messages.
func contractDeps() []string {
	var deps []string
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) == testPkg.Proto {
			deps = append(deps, fd.Path())
		}
		return true
	})
	return deps
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

	fdp := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("routegen/describe_test.proto"),
		Package:    proto.String(testPkg.Proto),
		Syntax:     proto.String("proto3"),
		Dependency: contractDeps(),
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
