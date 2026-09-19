// Package chitest runs the generated routes against a real chi.Router. It lives
// in the routegen module because that is where chi may be required — the
// contract's own go.mod must not gain a test-only dependency. It checks what
// server.Mux's doc comment claims the router owns, which mux_test.go's fake
// cannot.
package chitest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/routes"
	"github.com/metacensus/api/go/server"
)

// topics records the id it was bound with, which is the whole question here.
type topics struct {
	server.UnimplementedTopicRoutes
	gotID string
}

func (t *topics) GetTopic(_ context.Context, req *v1.TopicGetRequest) (*v1.Topic, error) {
	t.gotID = req.TopicId
	return &v1.Topic{Id: req.TopicId, Content: &v1.TopicContent{Name: "t"}}, nil
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// The generated TypeScript client percent-encodes every path segment
// (encodeURIComponent). net/http's ServeMux decodes before r.PathValue; chi
// does not. So the same client request binds a different id on each router
// unless the runtime is told which one it is on — and ids here are opaque
// strings minted by two backends (go/wireshape_test.go's TestIdsAreStrings),
// so a character worth escaping is a case the contract expects to have.
func TestPathValueDecoding(t *testing.T) {
	const id = "a/b c"
	escaped := url.PathEscape(id) // what encodeURIComponent sends, near enough

	t.Run("StdPathValue leaves chi's segment escaped", func(t *testing.T) {
		impl := &topics{}
		r := chi.NewRouter()
		// Set explicitly: Runtime refuses to guess this for a chi.Router, and
		// what this subtest pins is that the guess would have been wrong.
		server.RegisterTopicRoutes(r, &server.Runtime{
			Prefix:    routes.Prefix,
			PathValue: server.StdPathValue,
		}, impl)

		if rec := get(r, routes.Prefix+"/topic/"+escaped); rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if impl.gotID == id {
			t.Fatal("chi decoded the segment; StdPathValue would now be the right default " +
				"and EscapedPathValue wrong — re-read server.Mux's doc comment before changing it")
		}
	})

	t.Run("EscapedPathValue round-trips what the client sent", func(t *testing.T) {
		impl := &topics{}
		r := chi.NewRouter()
		server.RegisterTopicRoutes(r, &server.Runtime{
			Prefix:    routes.Prefix,
			PathValue: server.EscapedPathValue,
		}, impl)

		if rec := get(r, routes.Prefix+"/topic/"+escaped); rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if impl.gotID != id {
			t.Errorf("bound %q, want %q", impl.gotID, id)
		}
	})

	t.Run("StdPathValue is right on net/http", func(t *testing.T) {
		impl := &topics{}
		mux := http.NewServeMux()
		server.RegisterTopicRoutes(server.StdMux{ServeMux: mux}, &server.Runtime{Prefix: routes.Prefix}, impl)

		if rec := get(mux, routes.Prefix+"/topic/"+escaped); rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if impl.gotID != id {
			t.Errorf("bound %q, want %q", impl.gotID, id)
		}
	})

	// The other direction, which nothing pinned before: EscapedPathValue on a
	// StdMux decodes a segment net/http already decoded. It is silent for an
	// ordinary id and wrong for exactly the ids that made this a question.
	t.Run("EscapedPathValue on a StdMux double-decodes", func(t *testing.T) {
		for _, tc := range []struct {
			id     string
			status int
			bound  string
		}{
			{"plain", 200, "plain"},
			{"50%2Fx", 200, "50/x"}, // one decode too many
			{"a%20b", 200, "a b"},   // likewise
			{"100%", 400, ""},       // trailing % is not valid escaping, twice over
		} {
			impl := &topics{}
			mux := http.NewServeMux()
			server.RegisterTopicRoutes(server.StdMux{ServeMux: mux}, &server.Runtime{
				Prefix:    routes.Prefix,
				PathValue: server.EscapedPathValue,
			}, impl)

			rec := get(mux, routes.Prefix+"/topic/"+url.PathEscape(tc.id))
			if rec.Code != tc.status || impl.gotID != tc.bound {
				t.Errorf("id %q: status %d bound %q, want %d %q",
					tc.id, rec.Code, impl.gotID, tc.status, tc.bound)
			}
			if tc.bound != tc.id && impl.gotID == tc.id {
				t.Errorf("id %q survived; EscapedPathValue may now be safe on a StdMux", tc.id)
			}
		}
	})
}

// Registration refuses a chi.Router until the runtime says which convention
// it uses. Before this, the zero value picked StdPathValue — the wrong one
// for the router metacensus/infra actually mounts.
func TestChiRouterMustDeclareItsPathValue(t *testing.T) {
	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("registered on a chi.Router with PathValue unset")
		}
		if msg, _ := p.(string); !strings.Contains(msg, "EscapedPathValue") {
			t.Errorf("panic does not name the fix: %v", p)
		}
	}()
	server.RegisterTopicRoutes(chi.NewRouter(), &server.Runtime{Prefix: routes.Prefix}, &topics{})
}

// metacensus/infra mounts everything inside chi's Route(routes.Prefix, ...),
// so the routes it registers there must carry no prefix of their own. That is
// Runtime's zero value; before it was, no value of Prefix worked and the
// failure was a silent 404.
func TestMountedUnderAnExistingPrefix(t *testing.T) {
	impl := &topics{}
	r := chi.NewRouter()
	r.Route(routes.Prefix, func(v1Routes chi.Router) {
		v1Routes.Group(func(authed chi.Router) {
			// authed.Use(jwtMiddleware) — infra's shape
			server.RegisterTopicRoutes(authed, &server.Runtime{PathValue: server.EscapedPathValue}, impl)
		})
	})

	rec := get(r, routes.Prefix+"/topic/t1")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if impl.gotID != "t1" {
		t.Errorf("bound %q", impl.gotID)
	}
}

// Every route resolves through chi, not just the one above.
func TestEveryRouteResolvesOnChi(t *testing.T) {
	r := chi.NewRouter()
	rt := &server.Runtime{Prefix: routes.Prefix, PathValue: server.EscapedPathValue}
	server.RegisterAuthRoutes(r, rt, server.UnimplementedAuthRoutes{})
	server.RegisterHealthRoutes(r, rt, server.UnimplementedHealthRoutes{})
	server.RegisterPropRoutes(r, rt, server.UnimplementedPropRoutes{})
	server.RegisterTopicRoutes(r, rt, server.UnimplementedTopicRoutes{})
	server.RegisterUserRoutes(r, rt, server.UnimplementedUserRoutes{})

	// The public surface hangs off its own prefix, so it needs its own
	// Runtime. Both mount on the one router, which is the arrangement a
	// process serving both surfaces is in.
	pub := *rt
	pub.Prefix = routes.PublicPrefix
	server.RegisterPartnerRoutes(r, &pub, server.UnimplementedPartnerRoutes{})

	for _, route := range routes.Routes {
		path := route.Path
		for _, p := range route.Params {
			path = strings.Replace(path, "{"+p+"}", "x", 1)
		}
		var body io.Reader = http.NoBody
		if route.Body == "*" {
			body = strings.NewReader(minimalBody(route))
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(route.Method, route.Prefix+path, body)
		r.ServeHTTP(rec, req)
		// Unimplemented, which means it reached a generated handler.
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s %s: status %d, want 501: %s", route.Method, path, rec.Code, rec.Body)
		}
	}
}

// minimalBody is the smallest body that gets a route past binding and into a
// generated handler, which is all these tests want: enough to reach the 501.
//
// A signed route needs both halves and needs every id the path bound to match
// its signed copy, so the content here repeats each parameter with the same
// placeholder the path was built from. The signature is empty — nothing in
// go/server verifies one, which is the property that makes an empty one
// sufficient.
func minimalBody(route routes.Route) string {
	if !route.Signed {
		return "{}"
	}
	fields := make([]string, 0, len(route.Params))
	for _, p := range route.Params {
		fields = append(fields, `"`+p+`":"x"`)
	}
	return `{"content":{` + strings.Join(fields, ",") + `},"userSignature":{}}`
}

// What chi answers before a generated handler runs. server.Mux's doc comment
// states each of these; this is what makes the statement a check.
func TestWhatChiOwns(t *testing.T) {
	r := chi.NewRouter()
	rt := &server.Runtime{Prefix: routes.Prefix, PathValue: server.EscapedPathValue}
	server.RegisterTopicRoutes(r, rt, &topics{})

	t.Run("HEAD on a GET route is 405, where net/http answers 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("HEAD", routes.Prefix+"/topic/x", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("chi HEAD: status %d", rec.Code)
		}

		mux := http.NewServeMux()
		server.RegisterTopicRoutes(server.StdMux{ServeMux: mux}, &server.Runtime{Prefix: routes.Prefix}, &topics{})
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("HEAD", routes.Prefix+"/topic/x", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("net/http HEAD: status %d", rec.Code)
		}
	})

	t.Run("404 and 405 are not the error envelope", func(t *testing.T) {
		rec := get(r, routes.Prefix+"/nowhere")
		if rec.Code != 404 || rec.Header().Get("Content-Type") == "application/json" {
			t.Errorf("404: status %d ct %q", rec.Code, rec.Header().Get("Content-Type"))
		}
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("DELETE", routes.Prefix+"/topic/x", nil))
		if rec.Code != 405 || rec.Body.Len() != 0 {
			t.Errorf("405: status %d body %q", rec.Code, rec.Body)
		}
	})
}

// metacensus/infra's auth boundary does not follow the service boundary:
// core/api/server/server.go mounts POST /login and POST /user outside the
// auth Group and everything else inside it, while AuthRoutes holds Login,
// SignUp and Logout. Register<Service> registers a whole service on one Mux,
// so without server.Except the only way to mount AuthRoutes on that tree is a
// caller-written Mux matching on pattern strings — the route table back at
// the callsite.
func TestServiceSplitAcrossAnAuthBoundary(t *testing.T) {
	var authed []string
	jwt := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authed = append(authed, r.URL.Path)
			next.ServeHTTP(w, r)
		})
	}

	r := chi.NewRouter()
	rt := &server.Runtime{PathValue: server.EscapedPathValue}
	r.Route(routes.Prefix, func(v1Routes chi.Router) {
		v1Routes.Group(func(authedRoutes chi.Router) {
			authedRoutes.Use(jwt)
			server.RegisterAuthRoutes(
				server.Except(authedRoutes, v1Routes, rt, "AuthRoutes.Login", "AuthRoutes.SignUp"),
				rt, server.UnimplementedAuthRoutes{})
		})
	})

	for _, tc := range []struct {
		path      string
		body      string
		wantAuthd bool
	}{
		{"/login", "{}", false},
		// SignUp is a signed write, so an empty body is a 400 before the
		// handler: the point here is which side of the middleware it mounts
		// on, which only a request that reaches a handler can show.
		{"/signup", `{"content":{},"userSignature":{}}`, false},
		{"/logout", "{}", true},
	} {
		authed = nil
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("POST", routes.Prefix+tc.path, strings.NewReader(tc.body)))
		// Unimplemented, which means it reached a generated handler at all.
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s: status %d, want 501: %s", tc.path, rec.Code, rec.Body)
		}
		if got := len(authed) > 0; got != tc.wantAuthd {
			t.Errorf("%s: went through the middleware = %v, want %v", tc.path, got, tc.wantAuthd)
		}
	}
}

// The patterns stay in the manifest, so a renamed or mistyped rpc is a
// startup failure rather than a route quietly mounted on the wrong router.
func TestExceptRefusesAnRPCTheManifestDoesNotDeclare(t *testing.T) {
	defer func() {
		if p := recover(); p == nil {
			t.Fatal("accepted an rpc that is not in the manifest")
		}
	}()
	server.Except(chi.NewRouter(), chi.NewRouter(), &server.Runtime{}, "AuthRoutes.LogOut")
}
