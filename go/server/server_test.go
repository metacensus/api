package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/routes"
	"github.com/metacensus/api/go/server"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// stdlibRouter adapts http.ServeMux to server.Router.
//
// Its existence is a claim under test: the package names no router, so a
// router it has never heard of drives it. ServeMux's {name} patterns and
// PathValue are close enough to chi's URLParam that the standard library alone
// is enough, which is why these tests add no dependency.
type stdlibRouter struct{ mux *http.ServeMux }

func (s stdlibRouter) Method(method, pattern string, h http.Handler) {
	s.mux.Handle(method+" "+pattern, h)
}

func pathValue(r *http.Request, name string) string { return r.PathValue(name) }

// propStub is a PropRoutes whose methods are supplied per test. An unset method
// returning an error rather than panicking keeps a wrong-route failure legible
// as a response.
type propStub struct {
	listProps  func(context.Context, *v1.PropListRequest) (*v1.PropList, error)
	getProp    func(context.Context, *v1.PropGetRequest) (*v1.Prop, error)
	createProp func(context.Context, *v1.PropCreateRequest) (*v1.Prop, error)
	listVotes  func(context.Context, *v1.VoteListRequest) (*v1.VoteList, error)
	setVote    func(context.Context, *v1.VoteSetRequest) (*v1.Vote, error)
}

var errUnset = errors.New("stub method not set")

func (p *propStub) ListProps(ctx context.Context, r *v1.PropListRequest) (*v1.PropList, error) {
	if p.listProps == nil {
		return nil, errUnset
	}
	return p.listProps(ctx, r)
}

func (p *propStub) GetProp(ctx context.Context, r *v1.PropGetRequest) (*v1.Prop, error) {
	if p.getProp == nil {
		return nil, errUnset
	}
	return p.getProp(ctx, r)
}

func (p *propStub) CreateProp(ctx context.Context, r *v1.PropCreateRequest) (*v1.Prop, error) {
	if p.createProp == nil {
		return nil, errUnset
	}
	return p.createProp(ctx, r)
}

func (p *propStub) ListVotes(ctx context.Context, r *v1.VoteListRequest) (*v1.VoteList, error) {
	if p.listVotes == nil {
		return nil, errUnset
	}
	return p.listVotes(ctx, r)
}

func (p *propStub) SetVote(ctx context.Context, r *v1.VoteSetRequest) (*v1.Vote, error) {
	if p.setVote == nil {
		return nil, errUnset
	}
	return p.setVote(ctx, r)
}

// The generated interface is the guarantee; this is where it is claimed.
var _ server.PropRoutes = (*propStub)(nil)

// serve registers h on a fresh stdlib mux and returns something to call.
func serve(t *testing.T, mux server.Mux, h server.PropRoutes) *http.ServeMux {
	t.Helper()
	sm := http.NewServeMux()
	mux.Router = stdlibRouter{sm}
	if mux.PathParam == nil {
		mux.PathParam = pathValue
	}
	mux.RegisterPropRoutes(h)
	return sm
}

func do(t *testing.T, sm *http.ServeMux, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	sm.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v\nbody: %s", err, w.Body.String())
	}
	return out
}

// The whole reason this package exists: the generated types marshal through
// encoding/json into a second encoding that looks plausible and is wrong. An
// enum has to be its name and a timestamp has to be RFC 3339, because that is
// what the TypeScript side is generated to read.
func TestResponseIsProtoJSONNotEncodingJSON(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	sm := serve(t, server.Mux{}, &propStub{
		getProp: func(_ context.Context, r *v1.PropGetRequest) (*v1.Prop, error) {
			return &v1.Prop{
				Id:          r.PropId,
				AuthorId:    "user-1",
				Created:     timestamppb.New(created),
				Type:        v1.Prop_PaperExtractionComplete,
				Description: "a motion",
			}, nil
		},
	})

	w := do(t, sm, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	got := decode(t, w)
	if got["type"] != "PaperExtractionComplete" {
		t.Errorf("type = %v, want %q — encoding/json would write the integer 6",
			got["type"], "PaperExtractionComplete")
	}
	if got["created"] != "2026-03-04T05:06:07Z" {
		t.Errorf("created = %v, want RFC 3339 — encoding/json would write {seconds, nanos}", got["created"])
	}
}

func TestPathParametersBind(t *testing.T) {
	var got *v1.PropGetRequest

	sm := serve(t, server.Mux{}, &propStub{
		getProp: func(_ context.Context, r *v1.PropGetRequest) (*v1.Prop, error) {
			got = r
			return &v1.Prop{Id: r.PropId}, nil
		},
	})

	if w := do(t, sm, "GET", routes.Prefix+"/topic/t-1/prop/p-9", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if got.GetTopicId() != "t-1" || got.GetPropId() != "p-9" {
		t.Errorf("bound (%q, %q), want (t-1, p-9)", got.GetTopicId(), got.GetPropId())
	}
}

// The path is what routed the request, so a body field of the same name is the
// caller disagreeing with the URL it called.
func TestPathParameterOverridesBody(t *testing.T) {
	var got *v1.PropCreateRequest

	sm := serve(t, server.Mux{}, &propStub{
		createProp: func(_ context.Context, r *v1.PropCreateRequest) (*v1.Prop, error) {
			got = r
			return &v1.Prop{Id: "p-1"}, nil
		},
	})

	w := do(t, sm, "POST", routes.Prefix+"/topic/from-path/prop",
		`{"topicId":"from-body","description":"a motion"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if got.GetTopicId() != "from-path" {
		t.Errorf("topicId = %q, want from-path", got.GetTopicId())
	}
	if got.GetDescription() != "a motion" {
		t.Errorf("description = %q; the body should still be bound", got.GetDescription())
	}
}

// Exposure is opt-in. infra echoes err.Error() from every failure today, which
// hands a caller whatever a datastore driver put in a string.
func TestOnlyWrappedErrorsReachTheClient(t *testing.T) {
	secret := "pq: relation \"users\" does not exist at 10.0.0.4:5432"

	t.Run("unwrapped becomes an opaque 500", func(t *testing.T) {
		sm := serve(t, server.Mux{}, &propStub{
			getProp: func(context.Context, *v1.PropGetRequest) (*v1.Prop, error) {
				return nil, errors.New(secret)
			},
		})

		w := do(t, sm, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", w.Code)
		}
		if strings.Contains(w.Body.String(), "pq:") || strings.Contains(w.Body.String(), "10.0.0.4") {
			t.Errorf("the underlying error reached the client: %s", w.Body.String())
		}
		if got := decode(t, w); got["code"] != "Internal" {
			t.Errorf("code = %v, want Internal", got["code"])
		}
	})

	t.Run("wrapped is exposed as written", func(t *testing.T) {
		sm := serve(t, server.Mux{}, &propStub{
			getProp: func(context.Context, *v1.PropGetRequest) (*v1.Prop, error) {
				return nil, server.Wrap(http.StatusNotFound, "PropNotFound",
					"no prop p-9 in topic t-1", errors.New(secret))
			},
		})

		w := do(t, sm, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
		got := decode(t, w)
		if got["code"] != "PropNotFound" {
			t.Errorf("code = %v, want PropNotFound", got["code"])
		}
		if got["error"] != "no prop p-9 in topic t-1" {
			t.Errorf("error = %v", got["error"])
		}
		// Wrapped cause is for the log, not the wire.
		if strings.Contains(w.Body.String(), "pq:") {
			t.Errorf("the wrapped cause reached the client: %s", w.Body.String())
		}
	})

	t.Run("both are handed to the logger", func(t *testing.T) {
		var logged []error
		mux := server.Mux{OnError: server.NewErrorHandler(func(_ *http.Request, err error) {
			logged = append(logged, err)
		})}
		sm := serve(t, mux, &propStub{
			getProp: func(context.Context, *v1.PropGetRequest) (*v1.Prop, error) {
				return nil, errors.New(secret)
			},
		})

		do(t, sm, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if len(logged) != 1 || !strings.Contains(logged[0].Error(), "pq:") {
			t.Errorf("logger saw %v, want the underlying error", logged)
		}
	})
}

// Strictness is a decision, not a default. The contract's decoder rejects
// unknown fields, which is right for conformance and wrong while a sender that
// is ahead of the contract migrates onto it.
func TestUnknownFieldStrictness(t *testing.T) {
	body := `{"description":"a motion","notAContractField":1}`

	t.Run("rejected by default", func(t *testing.T) {
		sm := serve(t, server.Mux{}, &propStub{
			createProp: func(context.Context, *v1.PropCreateRequest) (*v1.Prop, error) {
				return &v1.Prop{Id: "p-1"}, nil
			},
		})

		w := do(t, sm, "POST", routes.Prefix+"/topic/t-1/prop", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
		if got := decode(t, w); got["code"] != "InvalidRequest" {
			t.Errorf("code = %v, want InvalidRequest", got["code"])
		}
	})

	t.Run("accepted when discarding", func(t *testing.T) {
		sm := serve(t, server.Mux{DiscardUnknownFields: true}, &propStub{
			createProp: func(_ context.Context, r *v1.PropCreateRequest) (*v1.Prop, error) {
				if r.GetDescription() != "a motion" {
					t.Errorf("description = %q", r.GetDescription())
				}
				return &v1.Prop{Id: "p-1"}, nil
			},
		})

		if w := do(t, sm, "POST", routes.Prefix+"/topic/t-1/prop", body); w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestBodyIsCapped(t *testing.T) {
	sm := serve(t, server.Mux{MaxBodyBytes: 64}, &propStub{
		createProp: func(context.Context, *v1.PropCreateRequest) (*v1.Prop, error) {
			return &v1.Prop{Id: "p-1"}, nil
		},
	})

	body := `{"description":"` + strings.Repeat("x", 512) + `"}`
	w := do(t, sm, "POST", routes.Prefix+"/topic/t-1/prop", body)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413; body: %s", w.Code, w.Body.String())
	}
}

// Wrap is where auth goes, because the contract does not model which routes are
// public. It has to see every route, including ones it lets through.
func TestWrapSeesEveryRoute(t *testing.T) {
	var wrapped []string
	mux := server.Mux{Wrap: func(r routes.Route, h http.Handler) http.Handler {
		wrapped = append(wrapped, r.Method+" "+r.Path)
		return h
	}}
	serve(t, mux, &propStub{})

	var want []string
	for _, r := range routes.Routes {
		if r.Service == "PropRoutes" {
			want = append(want, r.Method+" "+r.Path)
		}
	}
	if len(wrapped) != len(want) {
		t.Fatalf("Wrap saw %d routes, the manifest declares %d: %v", len(wrapped), len(want), wrapped)
	}
	for i := range want {
		if wrapped[i] != want[i] {
			t.Errorf("route %d: Wrap saw %q, manifest declares %q", i, wrapped[i], want[i])
		}
	}
}

// A parameterised route on a Mux that cannot read parameters would bind every
// id to "" and fail per request as though the caller were at fault.
func TestRegistrationRefusesAMuxThatCannotBind(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a parameterised route with no PathParam did not panic")
		}
	}()

	sm := http.NewServeMux()
	mux := server.Mux{Router: stdlibRouter{sm}}
	mux.RegisterPropRoutes(&propStub{})
}
