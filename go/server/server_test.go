package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/metacensus/api/go/routes"
)

var errUnset = errors.New("stub method not set")

// unimplemented satisfies all 21 methods of StrictServerInterface.
//
// It exists because oapi-codegen emits one interface for the whole surface
// rather than one per resource, so a test exercising two Prop routes must
// still satisfy Auth, Topic, User, Protocol, Extraction and Health. The other
// two branches let an implementation take one resource at a time.
type unimplemented struct{}

func (unimplemented) ExtractionRoutesGetExtraction(context.Context, ExtractionRoutesGetExtractionRequestObject) (ExtractionRoutesGetExtractionResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) ExtractionRoutesUpsertExtraction(context.Context, ExtractionRoutesUpsertExtractionRequestObject) (ExtractionRoutesUpsertExtractionResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) HealthRoutesHealthcheck(context.Context, HealthRoutesHealthcheckRequestObject) (HealthRoutesHealthcheckResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) AuthRoutesLogin(context.Context, AuthRoutesLoginRequestObject) (AuthRoutesLoginResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) AuthRoutesLogout(context.Context, AuthRoutesLogoutRequestObject) (AuthRoutesLogoutResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) ProtocolRoutesCreateProtocol(context.Context, ProtocolRoutesCreateProtocolRequestObject) (ProtocolRoutesCreateProtocolResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) UserRoutesGetSelf(context.Context, UserRoutesGetSelfRequestObject) (UserRoutesGetSelfResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) AuthRoutesSignUp(context.Context, AuthRoutesSignUpRequestObject) (AuthRoutesSignUpResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) TopicRoutesListTopics(context.Context, TopicRoutesListTopicsRequestObject) (TopicRoutesListTopicsResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) TopicRoutesCreateTopic(context.Context, TopicRoutesCreateTopicRequestObject) (TopicRoutesCreateTopicResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) TopicRoutesGetTopic(context.Context, TopicRoutesGetTopicRequestObject) (TopicRoutesGetTopicResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) TopicRoutesListMembers(context.Context, TopicRoutesListMembersRequestObject) (TopicRoutesListMembersResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) TopicRoutesGetMember(context.Context, TopicRoutesGetMemberRequestObject) (TopicRoutesGetMemberResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) PropRoutesListProps(context.Context, PropRoutesListPropsRequestObject) (PropRoutesListPropsResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) PropRoutesCreateProp(context.Context, PropRoutesCreatePropRequestObject) (PropRoutesCreatePropResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) PropRoutesGetProp(context.Context, PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) PropRoutesListVotes(context.Context, PropRoutesListVotesRequestObject) (PropRoutesListVotesResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) PropRoutesSetVote(context.Context, PropRoutesSetVoteRequestObject) (PropRoutesSetVoteResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) TopicRoutesGetProtocol(context.Context, TopicRoutesGetProtocolRequestObject) (TopicRoutesGetProtocolResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) UserRoutesListUsers(context.Context, UserRoutesListUsersRequestObject) (UserRoutesListUsersResponseObject, error) {
	return nil, errUnset
}

func (unimplemented) UserRoutesGetUser(context.Context, UserRoutesGetUserRequestObject) (UserRoutesGetUserResponseObject, error) {
	return nil, errUnset
}

// stub overrides the handful of operations these tests drive.
type stub struct {
	unimplemented
	getProp     func(context.Context, PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error)
	createProp  func(context.Context, PropRoutesCreatePropRequestObject) (PropRoutesCreatePropResponseObject, error)
	healthcheck func(context.Context, HealthRoutesHealthcheckRequestObject) (HealthRoutesHealthcheckResponseObject, error)
}

func (s *stub) PropRoutesGetProp(ctx context.Context, r PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error) {
	if s.getProp == nil {
		return s.unimplemented.PropRoutesGetProp(ctx, r)
	}
	return s.getProp(ctx, r)
}

func (s *stub) PropRoutesCreateProp(ctx context.Context, r PropRoutesCreatePropRequestObject) (PropRoutesCreatePropResponseObject, error) {
	if s.createProp == nil {
		return s.unimplemented.PropRoutesCreateProp(ctx, r)
	}
	return s.createProp(ctx, r)
}

func (s *stub) HealthRoutesHealthcheck(ctx context.Context, r HealthRoutesHealthcheckRequestObject) (HealthRoutesHealthcheckResponseObject, error) {
	if s.healthcheck == nil {
		return s.unimplemented.HealthRoutesHealthcheck(ctx, r)
	}
	return s.healthcheck(ctx, r)
}

var _ StrictServerInterface = (*stub)(nil)

func ptr[T any](v T) *T { return &v }

func serve(t *testing.T, opts Options, h StrictServerInterface) http.Handler {
	t.Helper()
	handler, err := New(h, opts)
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

// The same assertion the other two branches make.
//
// Enums and timestamps come out right: oapi-codegen renders the OpenAPI enum as
// a Go string type, and time.Time marshals RFC 3339. An empty string comes out
// right too -- omitempty on a *string keys off the pointer, so a field set to
// "" is still emitted.
//
// What differs is who decides presence. Every generated field is a pointer, so
// a field the implementation leaves nil is simply absent, and nothing makes it
// set one. The contract marshals with EmitDefaultValues and the TypeScript
// generated from the same .proto declares `description: string` as always
// present, so a client is typed to expect a field the server is free to omit.
func TestResponseEncoding(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	h := serve(t, Options{}, &stub{
		getProp: func(_ context.Context, r PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error) {
			return PropRoutesGetProp200JSONResponse(V1Prop{
				Id:       ptr(r.PropId),
				AuthorId: ptr(""), // set, and empty
				Created:  ptr(created),
				Type:     ptr(V1PropType("PaperExtractionComplete")),
				// Description left nil: the contract declares it always
				// present, and this type does not require it.
			}), nil
		},
	})

	w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	got := decode(t, w)
	if got["type"] != "PaperExtractionComplete" {
		t.Errorf("type = %v, want the enum's name", got["type"])
	}
	if got["created"] != "2026-03-04T05:06:07Z" {
		t.Errorf("created = %v, want RFC 3339", got["created"])
	}

	// An empty string the implementation set is emitted, matching the contract.
	if v, present := got["authorId"]; !present || v != "" {
		t.Errorf("authorId = %v (present=%v), want \"\": omitempty on a *string "+
			"keys off the pointer, so a set-but-empty field is still emitted", v, present)
	}

	// Recorded, not asserted away. This is the divergence, and it is in
	// generated code: the field is optional in Go and required in the
	// TypeScript generated from the same .proto.
	if _, present := got["description"]; present {
		t.Error("description is present; if oapi-codegen has stopped making " +
			"fields pointers, this test and the README should be corrected")
	} else {
		t.Log("description left nil is omitted, though the contract and the " +
			"generated TypeScript both declare it always present")
	}
}

func TestPathParametersBind(t *testing.T) {
	var got PropRoutesGetPropRequestObject

	h := serve(t, Options{}, &stub{
		getProp: func(_ context.Context, r PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error) {
			got = r
			return PropRoutesGetProp200JSONResponse(V1Prop{Id: ptr(r.PropId)}), nil
		},
	})

	if w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if got.TopicId != "t-1" || got.PropId != "p-9" {
		t.Errorf("bound (%q, %q), want (t-1, p-9)", got.TopicId, got.PropId)
	}
}

// The other two branches let the body carry topicId and have the path win.
// Here the question does not arise: the path parameter and the body are
// separate fields of the request object, so a body field of the same name is
// not represented at all.
func TestBodyAndPathAreSeparateFields(t *testing.T) {
	var got PropRoutesCreatePropRequestObject

	h := serve(t, Options{}, &stub{
		createProp: func(_ context.Context, r PropRoutesCreatePropRequestObject) (PropRoutesCreatePropResponseObject, error) {
			got = r
			return PropRoutesCreateProp200JSONResponse(V1Prop{Id: ptr("p-1")}), nil
		},
	})

	w := do(t, h, "POST", routes.Prefix+"/topic/from-path/prop",
		`{"topicId":"from-body","description":"a motion"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if got.TopicId != "from-path" {
		t.Errorf("TopicId = %q, want from-path", got.TopicId)
	}
	if got.Body == nil || got.Body.Description == nil || *got.Body.Description != "a motion" {
		t.Errorf("body did not bind: %+v", got.Body)
	}
}

func TestOnlyWrappedErrorsReachTheClient(t *testing.T) {
	secret := `pq: relation "users" does not exist at 10.0.0.4:5432`

	t.Run("unwrapped becomes an opaque 500", func(t *testing.T) {
		h := serve(t, Options{}, &stub{
			getProp: func(context.Context, PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error) {
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

	t.Run("wrapped is exposed as written", func(t *testing.T) {
		h := serve(t, Options{}, &stub{
			getProp: func(context.Context, PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error) {
				return nil, Wrap(http.StatusNotFound, "PropNotFound",
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

	t.Run("everything reaches the logger", func(t *testing.T) {
		var logged []error
		h := serve(t, Options{Log: func(_ *http.Request, err error) { logged = append(logged, err) }},
			&stub{getProp: func(context.Context, PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error) {
				return nil, errors.New(secret)
			}})

		do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", "")
		if len(logged) != 1 || !strings.Contains(logged[0].Error(), "pq:") {
			t.Errorf("logger saw %v", logged)
		}
	})
}

// Unknown fields are accepted, and there is no option to reject them.
// oapi-codegen's generated binding calls json.NewDecoder(...).Decode, which
// ignores them; the contract's decoder rejects them. Recorded rather than
// asserted away.
func TestUnknownFieldsAreAccepted(t *testing.T) {
	var got PropRoutesCreatePropRequestObject

	h := serve(t, Options{}, &stub{
		createProp: func(_ context.Context, r PropRoutesCreatePropRequestObject) (PropRoutesCreatePropResponseObject, error) {
			got = r
			return PropRoutesCreateProp200JSONResponse(V1Prop{Id: ptr("p-1")}), nil
		},
	})

	w := do(t, h, "POST", routes.Prefix+"/topic/t-1/prop",
		`{"description":"a motion","notAContractField":1}`)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d; the unknown field was expected to be ignored", w.Code)
	}
	if got.Body == nil || got.Body.Description == nil {
		t.Error("the known field did not bind")
	}
}

// oapi-codegen hands its strict middleware the operation id, so the guard knows
// which route it is on. That holds for parameterised routes too.
func TestAuthGuard(t *testing.T) {
	auth := AuthenticatorFunc(func(r *http.Request) (*http.Request, error) {
		if r.Header.Get("Authorization") == "" {
			return nil, Errorf(http.StatusUnauthorized, "NotAuthenticated", "no session")
		}
		return r, nil
	})

	newStub := func(t *testing.T) *stub {
		return &stub{
			getProp: func(_ context.Context, r PropRoutesGetPropRequestObject) (PropRoutesGetPropResponseObject, error) {
				return PropRoutesGetProp200JSONResponse(V1Prop{Id: ptr(r.PropId)}), nil
			},
			healthcheck: func(context.Context, HealthRoutesHealthcheckRequestObject) (HealthRoutesHealthcheckResponseObject, error) {
				return HealthRoutesHealthcheck200JSONResponse(V1HealthcheckResponse{Status: ptr("ok")}), nil
			},
		}
	}

	t.Run("a parameterised route is refused without credentials", func(t *testing.T) {
		h := serve(t, Options{Auth: auth}, newStub(t))
		if w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", ""); w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("credentials let it through", func(t *testing.T) {
		h := serve(t, Options{Auth: auth}, newStub(t))
		r := httptest.NewRequest("GET", routes.Prefix+"/topic/t-1/prop/p-9", nil)
		r.Header.Set("Authorization", "Bearer x")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("a public operation bypasses auth", func(t *testing.T) {
		h := serve(t, Options{Auth: auth, Public: map[string]bool{"HealthRoutesHealthcheck": true}},
			newStub(t))
		if w := do(t, h, "GET", routes.Prefix+"/healthcheck", ""); w.Code != http.StatusOK {
			t.Errorf("public: status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if w := do(t, h, "GET", routes.Prefix+"/topic/t-1/prop/p-9", ""); w.Code != http.StatusUnauthorized {
			t.Errorf("authenticated: status = %d, want 401", w.Code)
		}
	})

	t.Run("an operation the manifest does not declare is refused", func(t *testing.T) {
		_, err := New(&stub{}, Options{Auth: auth, Public: map[string]bool{"NopeRoutesNope": true}})
		if err == nil {
			t.Fatal("New accepted an undeclared operation")
		}
	})
}

// Every operation the manifest declares exists on the generated interface, by
// the name the guard uses to address it.
func TestOperationNamesCoverTheManifest(t *testing.T) {
	h := &stub{}
	for _, r := range routes.Routes {
		if Operation(r) == "" {
			t.Errorf("%s.%s has no operation name", r.Service, r.RPC)
		}
	}
	if _, err := New(h, Options{}); err != nil {
		t.Fatalf("New: %v", err)
	}
}
