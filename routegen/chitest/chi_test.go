// Package chitest runs the generated routes against a real chi.Router.
//
// It lives in the routegen module, not beside go/server, because the
// contract's own go.mod has two direct requirements and Go does not
// distinguish a test-only one from a runtime one.
//
// go/server/mux_test.go proves Mux's signature against a local copy of chi's;
// this file pins the request-handling behavior that catches.
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
	"github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/server/routes"
)

// topics records the id it was bound with, which is the whole question here.
type topics struct {
	server.UnimplementedTopicRoutes
	gotID string
}

func (t *topics) GetTopic(_ context.Context, req *v1.TopicGetRequest) (*v1.TopicSigned, error) {
	t.gotID = req.TopicId
	return &v1.TopicSigned{Id: req.TopicId, Content: &v1.Topic{Name: "t"}}, nil
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// The generated TypeScript client percent-encodes every path segment
// (encodeURIComponent). net/http's ServeMux decodes before r.PathValue; chi
// does not. So the same client request binds a different id on each router
// unless the runtime is told which one it is on.
func TestPathValueDecoding(t *testing.T) {
	const id = "a/b c"
	escaped := url.PathEscape(id) // what encodeURIComponent sends, near enough

	t.Run("StdPathValue leaves chi's segment escaped", func(t *testing.T) {
		impl := &topics{}
		r := chi.NewRouter()
		// Set explicitly: pins that Runtime's guess would have been wrong here.
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

	// The other direction: EscapedPathValue on a StdMux decodes a segment
	// net/http already decoded.
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
// it uses; the zero value used to silently pick the wrong one.
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
// so routes registered there must carry no prefix of their own — Runtime's
// zero value.
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
	// Runtime, though both mount on the one router.
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
// generated handler. A signed route needs both halves and needs every id the
// path bound to match its signed copy; go/server never verifies the
// signature itself, so an empty one is sufficient.
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
// SignUp and Logout. server.Except is what makes that split mountable without
// a caller-written Mux matching on pattern strings.
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
		// SignUp is a signed write; an empty body would be a 400 before the
		// handler, so this needs a real one to reach it.
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
