package server_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contract "github.com/metacensus/api/go/contract"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/server/routes"
	"google.golang.org/protobuf/types/known/structpb"
)

// fakeTopics records what it was called with and answers with a canned Topic.
type fakeTopics struct {
	server.UnimplementedTopicRoutes
	gotGet    *v1.TopicGetRequest
	gotCreate *v1.TopicCreateRequest
	err       error
}

func (f *fakeTopics) GetTopic(_ context.Context, req *v1.TopicGetRequest) (*v1.TopicSigned, error) {
	f.gotGet = req
	if f.err != nil {
		return nil, f.err
	}
	return &v1.TopicSigned{Id: req.TopicId, Content: &v1.Topic{Name: "t"}}, nil
}

// The server wraps and never modifies: the content it answers with is the
// content it was handed, and the id beside it is the server's own.
func (f *fakeTopics) CreateTopic(_ context.Context, req *v1.TopicCreateRequest) (*v1.TopicSigned, error) {
	f.gotCreate = req
	return &v1.TopicSigned{Id: "new", Content: req.Content, UserSignature: req.UserSignature}, nil
}

type fakeProps struct {
	server.UnimplementedPropRoutes
	got *v1.PropCreateRequest
}

func (f *fakeProps) CreateProp(_ context.Context, req *v1.PropCreateRequest) (*v1.PropSigned, error) {
	f.got = req
	return &v1.PropSigned{Id: "p", Content: req.Content, UserSignature: req.UserSignature}, nil
}

// signedBody wraps a content document in the envelope every write carries.
// The signature is empty on purpose — this package checks presence and id
// agreement, never signature validity.
func signedBody(content string) string {
	return `{"content":` + content + `,"userSignature":{}}`
}

// registerAll registers every service, proving the manifest's patterns don't
// conflict under ServeMux's rules (which would panic here, not just at
// runtime in a real backend).
func registerAll(t *testing.T, rt *server.Runtime, topics server.TopicRoutes, props server.PropRoutes) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	std := server.StdMux{ServeMux: mux}
	server.RegisterAuthRoutes(std, rt, server.UnimplementedAuthRoutes{})
	server.RegisterHealthRoutes(std, rt, server.UnimplementedHealthRoutes{})
	server.RegisterPropRoutes(std, rt, props)
	server.RegisterTopicRoutes(std, rt, topics)
	server.RegisterUserRoutes(std, rt, server.UnimplementedUserRoutes{})

	// The public surface gets its own Runtime, a copy of rt with only Prefix
	// changed — so e.g. MaxBodyBytes set by a test applies to both surfaces.
	pub := *rt
	pub.Prefix = routes.PublicPrefix
	server.RegisterPartnerRoutes(std, &pub, server.UnimplementedPartnerRoutes{})
	return mux
}

func do(t *testing.T, mux *http.ServeMux, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, routes.Prefix+path, rd)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) (code, msg string) {
	t.Helper()
	s := new(structpb.Struct)
	if err := contract.Unmarshal(rec.Body.Bytes(), s); err != nil {
		t.Fatalf("error body is not the envelope: %v: %s", err, rec.Body.String())
	}
	return s.Fields["code"].GetStringValue(), s.Fields["error"].GetStringValue()
}

func TestGetBindsPathParam(t *testing.T) {
	topics := &fakeTopics{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, topics, &fakeProps{})

	rec := do(t, mux, "GET", "/topic/abc-123", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if topics.gotGet.GetTopicId() != "abc-123" {
		t.Errorf("topicId bound as %q", topics.gotGet.GetTopicId())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q", ct)
	}
	// EmitDefaultValues: the empty description inside content and the absent
	// recorded and userSignature are the contract's presence rules, not
	// encoding/json's.
	var got v1.TopicSigned
	if err := contract.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"description":""`) {
		t.Errorf("default-valued scalar not emitted: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"recorded"`) {
		t.Errorf("absent message field emitted: %s", rec.Body.String())
	}
}

func TestPostDecodesBodyWithContractOptions(t *testing.T) {
	topics := &fakeTopics{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, topics, &fakeProps{})

	rec := do(t, mux, "POST", "/topic", signedBody(`{"name":"n","description":"d"}`))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if c := topics.gotCreate.GetContent(); c.GetName() != "n" || c.GetDescription() != "d" {
		t.Errorf("body bound as %v", topics.gotCreate)
	}

	// Unknown fields are rejected, as UnmarshalOptions says — inside the
	// nested content as much as at the top level.
	rec = do(t, mux, "POST", "/topic", signedBody(`{"name":"n","bogus":1}`))
	if rec.Code != 400 {
		t.Fatalf("unknown field: status %d: %s", rec.Code, rec.Body.String())
	}
	if code, msg := errorBody(t, rec); code != "body_invalid" || !strings.Contains(msg, "unknown field") {
		t.Errorf("code %q msg %q", code, msg)
	}

	// snake_case is not the wire spelling; protojson accepts it anyway. This
	// pins that fact rather than endorsing it.
	rec = do(t, mux, "POST", "/topic/t1/prop",
		`{"topic_id":"t1","content":{"topic_id":"t1","type":"Statement","description":"x"},"user_signature":{}}`)
	if rec.Code != 200 {
		t.Errorf("snake_case body: status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPathWinsOverBodyButNotSilently(t *testing.T) {
	props := &fakeProps{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, &fakeTopics{}, props)

	content := `{"topicId":"t1","type":"Statement","description":"x"}`

	// Body repeats the path-bound field with the same value: fine.
	rec := do(t, mux, "POST", "/topic/t1/prop", `{"topicId":"t1","content":`+content+`,"userSignature":{}}`)
	if rec.Code != 200 {
		t.Fatalf("agreeing body: status %d: %s", rec.Code, rec.Body.String())
	}
	if props.got.GetTopicId() != "t1" || props.got.GetContent().GetType() != v1.Prop_Statement {
		t.Errorf("bound %v", props.got)
	}

	// Body omits the top-level copy: the path fills it in. The signed copy
	// inside content is the caller's and is never filled in for them.
	rec = do(t, mux, "POST", "/topic/t1/prop", signedBody(content))
	if rec.Code != 200 || props.got.GetTopicId() != "t1" {
		t.Errorf("absent in body: status %d topicId %q", rec.Code, props.got.GetTopicId())
	}

	// Top-level copy disagrees with the path: 400.
	rec = do(t, mux, "POST", "/topic/t1/prop", `{"topicId":"other","content":`+content+`,"userSignature":{}}`)
	if rec.Code != 400 {
		t.Fatalf("disagreeing body: status %d: %s", rec.Code, rec.Body.String())
	}
	if code, _ := errorBody(t, rec); code != "path_body_conflict" {
		t.Errorf("code %q", code)
	}
}

// Path and signed content can disagree about an id — filed under one address
// while attesting to another. It's a 400 for the message's sake, not as a
// control; see contentParam.
func TestPathAndSignedContentMustAgree(t *testing.T) {
	props := &fakeProps{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, &fakeTopics{}, props)

	rec := do(t, mux, "POST", "/topic/t1/prop",
		signedBody(`{"topicId":"somewhere-else","type":"Statement","description":"x"}`))
	if rec.Code != 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	code, msg := errorBody(t, rec)
	if code != "path_content_conflict" {
		t.Errorf("code %q", code)
	}
	if !strings.Contains(msg, "somewhere-else") || !strings.Contains(msg, "t1") {
		t.Errorf("message %q names neither value", msg)
	}

	// An empty signed copy is still a disagreement: the server may not fill
	// in part of what the signature covers.
	rec = do(t, mux, "POST", "/topic/t1/prop", signedBody(`{"type":"Statement","description":"x"}`))
	if rec.Code != 400 {
		t.Errorf("empty signed copy: status %d: %s", rec.Code, rec.Body.String())
	}
}

// A signed route needs both content and userSignature; this is a shape
// check only — the empty signature is accepted since verification isn't
// this layer's job.
func TestSignedRouteRequiresBothHalves(t *testing.T) {
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, &fakeTopics{}, &fakeProps{})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"no signature", `{"content":{"name":"n","description":"d"}}`},
		{"no content", `{"userSignature":{}}`},
		{"neither", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, mux, "POST", "/topic", tc.body)
			if rec.Code != 400 {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if code, _ := errorBody(t, rec); code != "field_missing" {
				t.Errorf("code %q", code)
			}
		})
	}
}

func TestBodySizeCap(t *testing.T) {
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix, MaxBodyBytes: 64}, &fakeTopics{}, &fakeProps{})

	big := `{"name":"` + strings.Repeat("x", 100) + `"}`
	rec := do(t, mux, "POST", "/topic", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if code, _ := errorBody(t, rec); code != "body_too_large" {
		t.Errorf("code %q", code)
	}
}

// protojson's output is not byte-stable, so a digest over raw bytes could
// not survive re-encoding — only a digest over the decoded message can. This
// pins that a body with unreproducible whitespace/key order is still
// accepted and answered normally.
func TestABodyIsAcceptedHoweverItWasSpelled(t *testing.T) {
	topics := &fakeTopics{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, topics, &fakeProps{})

	sent := "{ \"userSignature\" : {},\n\t\"content\":{ \"description\" : \"d\",\n\t\"name\":\"n\" } }"
	rec := do(t, mux, "POST", "/topic", sent)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if c := topics.gotCreate.GetContent(); c.GetName() != "n" || c.GetDescription() != "d" {
		t.Errorf("bound %v", topics.gotCreate)
	}
	if rec.Body.String() == sent {
		t.Error("the response reproduced the request octets; this test assumes it cannot")
	}
}

func TestErrorModel(t *testing.T) {
	topics := &fakeTopics{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, topics, &fakeProps{})

	// A *server.Error chooses its status and message.
	topics.err = server.Errorf(http.StatusNotFound, "topic_not_found", "no topic %q", "x")
	rec := do(t, mux, "GET", "/topic/x", "")
	if rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
	if code, msg := errorBody(t, rec); code != "topic_not_found" || msg != `no topic "x"` {
		t.Errorf("code %q msg %q", code, msg)
	}

	// Anything else is a 500 that does not leak its text.
	topics.err = errors.New("pq: connection refused")
	rec = do(t, mux, "GET", "/topic/x", "")
	if rec.Code != 500 {
		t.Fatalf("status %d", rec.Code)
	}
	if code, msg := errorBody(t, rec); code != "internal" || strings.Contains(msg, "pq") {
		t.Errorf("code %q msg %q", code, msg)
	}

	// Unimplemented is 501.
	rec = do(t, mux, "GET", "/user/u1", "")
	if rec.Code != 501 {
		t.Errorf("unimplemented: status %d", rec.Code)
	}

	// ServeMux's own answers are not the JSON envelope.
	rec = do(t, mux, "DELETE", "/topic/x", "")
	if rec.Code != 405 || rec.Header().Get("Allow") == "" {
		t.Errorf("method not allowed: status %d Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	rec = do(t, mux, "GET", "/nowhere", "")
	if rec.Code != 404 || strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("not found: status %d Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// No route declares a query field, so every query parameter is unknown
// everywhere; silently ignoring one would let ?page=2 slip past
// TestNoPaginationFields at the only layer a caller can see.
func TestUnknownQueryParamsAreRejectedOnEveryRoute(t *testing.T) {
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, &fakeTopics{}, &fakeProps{})

	for _, r := range routes.Routes {
		if len(r.Query) > 0 {
			continue
		}
		path := r.Path
		for _, p := range r.Params {
			path = strings.Replace(path, "{"+p+"}", "x", 1)
		}
		// r.Prefix, not routes.Prefix: do() prepends the authenticated one,
		// and a public route requested under it is a 404 rather than the
		// rejection this is about.
		req := httptest.NewRequest(r.Method, r.Prefix+path+"?page=2", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Errorf("%s %s?page=2: status %d, want 400", r.Method, path, rec.Code)
			continue
		}
		if code, _ := errorBody(t, rec); code != "query_unknown" {
			t.Errorf("%s %s?page=2: code %q", r.Method, path, code)
		}
	}
}

// Every route in the manifest resolves to a registered handler, and nothing
// else does: the mux and the manifest describe the same table.
func TestEveryManifestRouteIsServed(t *testing.T) {
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, &fakeTopics{}, &fakeProps{})
	for _, r := range routes.Routes {
		path := r.Path
		for _, p := range r.Params {
			path = strings.Replace(path, "{"+p+"}", "x", 1)
		}
		req := httptest.NewRequest(r.Method, r.Prefix+path, nil)
		_, pattern := mux.Handler(req)
		if pattern != r.Method+" "+r.Prefix+r.Path {
			t.Errorf("%s %s resolved to pattern %q", r.Method, path, pattern)
		}
	}
}

// An *Error left with Status unset (or a typed-nil *Error, which reaches
// writeError as a non-nil error interface) used to panic on WriteHeader(0)
// instead of answering; now it's a 500.
func TestErrorWithNoUsableStatusIs500(t *testing.T) {
	topics := &fakeTopics{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, topics, &fakeProps{})

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"status left out", &server.Error{Code: "not_found", Message: "no such topic"}},
		{"status out of range", &server.Error{Status: 42, Code: "nonsense", Message: "m"}},
		{"typed-nil *Error", (*server.Error)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			topics.err = tc.err
			rec := do(t, mux, "GET", "/topic/x", "")
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status %d, want 500: %s", rec.Code, rec.Body.String())
			}
			if _, _ = errorBody(t, rec); rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type %q", rec.Header().Get("Content-Type"))
			}
		})
	}
}
