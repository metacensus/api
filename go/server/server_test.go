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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// propStub is a v1.PropRoutesServer whose methods are supplied per test.
//
// It satisfies the generated interface directly, with no embedding. That is
// require_unimplemented_servers=false in buf.gen.yaml doing its job: with the
// default, this type would embed UnimplementedPropRoutesServer, a missing
// method would compile, and the completeness this depth is adopted for would
// be a runtime codes.Unimplemented instead.
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

var _ v1.PropRoutesServer = (*propStub)(nil)

func serve(t *testing.T, opts server.Options, h *propStub) http.Handler {
	t.Helper()
	handler, err := server.New(context.Background(), server.Handlers{Prop: h}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return handler
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
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

// The gateway has its own protojson defaults. WithMarshalerOption is what makes
// it use the contract's, and without it go/wire.go would describe an encoder
// nothing was running.
func TestResponseIsProtoJSONNotEncodingJSON(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	h := serve(t, server.Options{}, &propStub{
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

	w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
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
	// EmitDefaultValues, from the contract's options rather than the gateway's.
	if _, ok := got["authorId"]; !ok {
		t.Error("authorId absent; the contract emits default values")
	}
}

func TestPathParametersBind(t *testing.T) {
	var got *v1.PropGetRequest

	h := serve(t, server.Options{}, &propStub{
		getProp: func(_ context.Context, r *v1.PropGetRequest) (*v1.Prop, error) {
			got = r
			return &v1.Prop{Id: r.PropId}, nil
		},
	})

	if w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if got.GetTopicId() != "t-1" || got.GetPropId() != "p-9" {
		t.Errorf("bound (%q, %q), want (t-1, p-9)", got.GetTopicId(), got.GetPropId())
	}
}

func TestPathParameterOverridesBody(t *testing.T) {
	var got *v1.PropCreateRequest

	h := serve(t, server.Options{}, &propStub{
		createProp: func(_ context.Context, r *v1.PropCreateRequest) (*v1.Prop, error) {
			got = r
			return &v1.Prop{Id: "p-1"}, nil
		},
	})

	w := do(t, h, "POST", routes.Prefix+"/topic/from-path/prop",
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

func TestErrorExposure(t *testing.T) {
	secret := "pq: relation \"users\" does not exist at 10.0.0.4:5432"

	t.Run("a plain error becomes an opaque 500", func(t *testing.T) {
		h := serve(t, server.Options{}, &propStub{
			getProp: func(context.Context, *v1.PropGetRequest) (*v1.Prop, error) {
				return nil, errors.New(secret)
			},
		})

		w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", w.Code)
		}
		if strings.Contains(w.Body.String(), "pq:") {
			t.Errorf("the underlying error reached the client: %s", w.Body.String())
		}
		if got := decode(t, w); got["code"] != "Internal" {
			t.Errorf("code = %v, want Internal", got["code"])
		}
	})

	t.Run("a *server.Error is exposed as written", func(t *testing.T) {
		h := serve(t, server.Options{}, &propStub{
			getProp: func(context.Context, *v1.PropGetRequest) (*v1.Prop, error) {
				return nil, server.Wrap(http.StatusNotFound, "PropNotFound",
					"no prop p-9 in topic t-1", errors.New(secret))
			},
		})

		w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
		got := decode(t, w)
		if got["code"] != "PropNotFound" || got["error"] != "no prop p-9 in topic t-1" {
			t.Errorf("body = %v", got)
		}
		if strings.Contains(w.Body.String(), "pq:") {
			t.Errorf("the wrapped cause reached the client: %s", w.Body.String())
		}
	})

	// The gRPC path, which is the idiom grpc-gateway is built around. It is
	// recorded here as behaviour rather than endorsed: the message is exposed,
	// so status.Error(codes.Internal, dbErr.Error()) — ordinary gRPC — puts a
	// datastore's words on the wire. The custom-generator version has no such
	// path, because it has no second error vocabulary to accommodate.
	t.Run("a gRPC status error is mapped by code and its message exposed", func(t *testing.T) {
		h := serve(t, server.Options{}, &propStub{
			getProp: func(context.Context, *v1.PropGetRequest) (*v1.Prop, error) {
				return nil, status.Error(codes.NotFound, "no such prop")
			},
		})

		w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
		if got := decode(t, w); got["code"] != "NotFound" || got["error"] != "no such prop" {
			t.Errorf("body = %v", got)
		}
	})

	t.Run("everything reaches the logger", func(t *testing.T) {
		var logged []error
		h := serve(t, server.Options{Log: func(_ *http.Request, err error) {
			logged = append(logged, err)
		}}, &propStub{
			getProp: func(context.Context, *v1.PropGetRequest) (*v1.Prop, error) {
				return nil, errors.New(secret)
			},
		})

		do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if len(logged) != 1 || !strings.Contains(logged[0].Error(), "pq:") {
			t.Errorf("logger saw %v, want the underlying error", logged)
		}
	})
}

func TestUnknownFieldStrictness(t *testing.T) {
	body := `{"description":"a motion","notAContractField":1}`
	stub := func() *propStub {
		return &propStub{createProp: func(context.Context, *v1.PropCreateRequest) (*v1.Prop, error) {
			return &v1.Prop{Id: "p-1"}, nil
		}}
	}

	t.Run("rejected by default", func(t *testing.T) {
		h := serve(t, server.Options{}, stub())
		w := do(t, h, "POST", routes.Prefix+"/topic/t-1/prop", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("accepted when discarding", func(t *testing.T) {
		h := serve(t, server.Options{DiscardUnknownFields: true}, stub())
		if w := do(t, h, "POST", routes.Prefix+"/topic/t-1/prop", body); w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
	})
}

// grpc-gateway has no body cap and offers no option for one, so this records
// what the package does not do. The custom-generator version caps at 1 MiB by
// default; here the cap has to be an http.MaxBytesReader in whatever wraps the
// handler, and nothing in this package can require it.
func TestLargeBodyIsNotCapped(t *testing.T) {
	h := serve(t, server.Options{}, &propStub{
		createProp: func(_ context.Context, r *v1.PropCreateRequest) (*v1.Prop, error) {
			return &v1.Prop{Id: "p-1", Description: r.GetDescription()}, nil
		},
	})

	body := `{"description":"` + strings.Repeat("x", 4<<20) + `"}`
	if w := do(t, h, "POST", routes.Prefix+"/topic/t-1/prop", body); w.Code != http.StatusOK {
		t.Errorf("status = %d; a 4 MiB body was expected to be accepted, which is the point", w.Code)
	}
}

func TestAuthGuard(t *testing.T) {
	denied := server.Errorf(http.StatusUnauthorized, "NotAuthenticated", "no session")
	auth := server.AuthenticatorFunc(func(r *http.Request) (*http.Request, error) {
		if r.Header.Get("Authorization") == "" {
			return nil, denied
		}
		return r, nil
	})

	t.Run("an authenticated route without credentials is refused", func(t *testing.T) {
		h := serve(t, server.Options{Auth: auth}, &propStub{
			getProp: func(context.Context, *v1.PropGetRequest) (*v1.Prop, error) {
				t.Error("the handler ran for an unauthenticated request")
				return &v1.Prop{}, nil
			},
		})

		w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("credentials let it through", func(t *testing.T) {
		h := serve(t, server.Options{Auth: auth}, &propStub{
			getProp: func(_ context.Context, r *v1.PropGetRequest) (*v1.Prop, error) {
				return &v1.Prop{Id: r.PropId}, nil
			},
		})

		req := httptest.NewRequest("GET", routes.Prefix+"/topic/t-1/prop/p-9", nil)
		req.Header.Set("Authorization", "Bearer x")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
	})

	// The guard matches a public route exactly, so a parameterised one would
	// never match and would be silently authenticated. It refuses instead.
	t.Run("a parameterised public route is refused at construction", func(t *testing.T) {
		_, err := server.New(context.Background(),
			server.Handlers{Prop: &propStub{}},
			server.Options{Auth: auth, Public: map[string]bool{"GET /topic/{topicId}/prop": true}})
		if err == nil {
			t.Fatal("New accepted a parameterised public route")
		}
		if !strings.Contains(err.Error(), "path parameters") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("a public route the manifest does not declare is refused", func(t *testing.T) {
		_, err := server.New(context.Background(),
			server.Handlers{Prop: &propStub{}},
			server.Options{Auth: auth, Public: map[string]bool{"POST /nope": true}})
		if err == nil {
			t.Fatal("New accepted an undeclared public route")
		}
	})
}

// healthStub serves the one public parameterless route that needs no auth
// machinery of its own.
type healthStub struct{}

func (healthStub) Healthcheck(context.Context, *v1.HealthcheckRequest) (*v1.HealthcheckResponse, error) {
	return &v1.HealthcheckResponse{Status: "ok"}, nil
}

var _ v1.HealthRoutesServer = healthStub{}

// The other half of the guard: a route named public is reachable without
// credentials, and one that is not stays refused in the same process.
func TestPublicRouteBypassesAuth(t *testing.T) {
	auth := server.AuthenticatorFunc(func(*http.Request) (*http.Request, error) {
		return nil, server.Errorf(http.StatusUnauthorized, "NotAuthenticated", "no session")
	})

	h, err := server.New(context.Background(),
		server.Handlers{Health: healthStub{}, Prop: &propStub{}},
		server.Options{Auth: auth, Public: map[string]bool{"GET /healthcheck": true}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if w := do(t, h, "GET", routes.Prefix+"/healthcheck", ""); w.Code != http.StatusOK {
		t.Errorf("public route: status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("authenticated route: status = %d, want 401", w.Code)
	}
}
