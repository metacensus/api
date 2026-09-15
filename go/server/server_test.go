package server_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contract "github.com/metacensus/api/go"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/routes"
	"github.com/metacensus/api/go/server"
	"google.golang.org/protobuf/types/known/structpb"
)

// fakeTopics records what it was called with and answers with a canned Topic.
type fakeTopics struct {
	server.UnimplementedTopicRoutes
	gotGet    *v1.TopicGetRequest
	gotCreate *v1.TopicCreateRequest
	err       error
}

func (f *fakeTopics) GetTopic(_ context.Context, req *v1.TopicGetRequest) (*v1.Topic, error) {
	f.gotGet = req
	if f.err != nil {
		return nil, f.err
	}
	return &v1.Topic{Id: req.TopicId, Name: "t"}, nil
}

func (f *fakeTopics) CreateTopic(_ context.Context, req *v1.TopicCreateRequest) (*v1.Topic, error) {
	f.gotCreate = req
	return &v1.Topic{Id: "new", Name: req.Name, Description: req.Description}, nil
}

type fakeProps struct {
	server.UnimplementedPropRoutes
	got *v1.PropCreateRequest
}

func (f *fakeProps) CreateProp(_ context.Context, req *v1.PropCreateRequest) (*v1.Prop, error) {
	f.got = req
	return &v1.Prop{Id: "p", AuthorId: "u", Type: req.Type, Description: req.Description}, nil
}

// Registering every service proves the 19 patterns do not conflict under
// ServeMux's rules, which would panic here rather than at runtime in a
// backend.
func registerAll(t *testing.T, rt *server.Runtime, topics server.TopicRoutes, props server.PropRoutes) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	std := server.StdMux{ServeMux: mux}
	server.RegisterAuthRoutes(std, rt, server.UnimplementedAuthRoutes{})
	server.RegisterHealthRoutes(std, rt, server.UnimplementedHealthRoutes{})
	server.RegisterPropRoutes(std, rt, props)
	server.RegisterProtocolRoutes(std, rt, server.UnimplementedProtocolRoutes{})
	server.RegisterTopicRoutes(std, rt, topics)
	server.RegisterUserRoutes(std, rt, server.UnimplementedUserRoutes{})
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
	// EmitDefaultValues: the empty description and absent created are the
	// contract's presence rules, not encoding/json's.
	var got v1.Topic
	if err := contract.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"description":""`) {
		t.Errorf("default-valued scalar not emitted: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"created"`) {
		t.Errorf("absent message field emitted: %s", rec.Body.String())
	}
}

func TestPostDecodesBodyWithContractOptions(t *testing.T) {
	topics := &fakeTopics{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, topics, &fakeProps{})

	rec := do(t, mux, "POST", "/topic", `{"name":"n","description":"d"}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if topics.gotCreate.GetName() != "n" || topics.gotCreate.GetDescription() != "d" {
		t.Errorf("body bound as %v", topics.gotCreate)
	}

	// Unknown fields are rejected, as UnmarshalOptions says.
	rec = do(t, mux, "POST", "/topic", `{"name":"n","bogus":1}`)
	if rec.Code != 400 {
		t.Fatalf("unknown field: status %d: %s", rec.Code, rec.Body.String())
	}
	if code, msg := errorBody(t, rec); code != "body_invalid" || !strings.Contains(msg, "unknown field") {
		t.Errorf("code %q msg %q", code, msg)
	}

	// snake_case is not the wire spelling; protojson accepts it anyway. This
	// pins that fact rather than endorsing it.
	rec = do(t, mux, "POST", "/topic/t1/prop", `{"topic_id":"t1","type":"Statement","description":"x"}`)
	if rec.Code != 200 {
		t.Errorf("snake_case body: status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPathWinsOverBodyButNotSilently(t *testing.T) {
	props := &fakeProps{}
	mux := registerAll(t, &server.Runtime{Prefix: routes.Prefix}, &fakeTopics{}, props)

	// Body repeats the path-bound field with the same value: fine.
	rec := do(t, mux, "POST", "/topic/t1/prop", `{"topicId":"t1","type":"Statement","description":"x"}`)
	if rec.Code != 200 {
		t.Fatalf("agreeing body: status %d: %s", rec.Code, rec.Body.String())
	}
	if props.got.GetTopicId() != "t1" || props.got.GetType() != v1.Prop_Statement {
		t.Errorf("bound %v", props.got)
	}

	// Body omits it: the path fills it in.
	rec = do(t, mux, "POST", "/topic/t2/prop", `{"type":"Statement","description":"x"}`)
	if rec.Code != 200 || props.got.GetTopicId() != "t2" {
		t.Errorf("absent in body: status %d topicId %q", rec.Code, props.got.GetTopicId())
	}

	// Body disagrees: 400.
	rec = do(t, mux, "POST", "/topic/t3/prop", `{"topicId":"other","type":"Statement","description":"x"}`)
	if rec.Code != 400 {
		t.Fatalf("disagreeing body: status %d: %s", rec.Code, rec.Body.String())
	}
	if code, _ := errorBody(t, rec); code != "path_body_conflict" {
		t.Errorf("code %q", code)
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

func TestVerifyBodySeesRawOctets(t *testing.T) {
	// Whitespace and key order that protojson would never reproduce: the
	// hook must see exactly what was sent.
	sent := "{ \"description\" : \"d\",\n\t\"name\":\"n\" }"
	var seen []byte
	rt := &server.Runtime{Prefix: routes.Prefix, VerifyBody: func(r *http.Request, raw []byte) error {
		seen = append([]byte(nil), raw...)
		if r.Header.Get("X-Signature") == "" {
			return errors.New("no signature")
		}
		return nil
	}}
	mux := registerAll(t, rt, &fakeTopics{}, &fakeProps{})

	req := httptest.NewRequest("POST", routes.Prefix+"/topic", strings.NewReader(sent))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("unsigned: status %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(seen, []byte(sent)) {
		t.Errorf("hook saw %q, want %q", seen, sent)
	}

	req = httptest.NewRequest("POST", routes.Prefix+"/topic", strings.NewReader(sent))
	req.Header.Set("X-Signature", "x")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("signed: status %d: %s", rec.Code, rec.Body.String())
	}

	// The hook is not consulted for a route without a body.
	seen = nil
	rec = do(t, mux, "GET", "/topic/x", "")
	if rec.Code != 200 || seen != nil {
		t.Errorf("GET: status %d, hook saw %v", rec.Code, seen)
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

// No route declares a query field, so every query parameter is unknown on
// every route. Silently ignoring one would let ?page=2 through at the only
// layer a caller can see, which is what TestNoPaginationFields exists to stop.
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
		rec := do(t, mux, r.Method, path+"?page=2", `{}`)
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
		req := httptest.NewRequest(r.Method, routes.Prefix+path, nil)
		_, pattern := mux.Handler(req)
		if pattern != r.Method+" "+routes.Prefix+r.Path {
			t.Errorf("%s %s resolved to pattern %q", r.Method, path, pattern)
		}
	}
}
