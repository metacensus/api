// Package chitest runs the generated routes against a real chi.Router.
//
// go/server/mux_test.go proves Mux's signature against a local copy of chi's;
// that catches a signature drift and nothing a request does. Everything this
// file pins was invisible to it, and every claim go/server/doc.go makes about
// "what the router owns" is checked here rather than asserted there.
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
	return &v1.Topic{Id: req.TopicId, Name: "t"}, nil
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
		server.RegisterTopicRoutes(r, &server.Runtime{Prefix: routes.Prefix}, impl)

		if rec := get(r, routes.Prefix+"/topic/"+escaped); rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if impl.gotID == id {
			t.Fatal("chi decoded the segment; StdPathValue would now be the right default " +
				"and EscapedPathValue wrong — re-read go/server/doc.go before changing it")
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
	server.RegisterProtocolRoutes(r, rt, server.UnimplementedProtocolRoutes{})
	server.RegisterTopicRoutes(r, rt, server.UnimplementedTopicRoutes{})
	server.RegisterUserRoutes(r, rt, server.UnimplementedUserRoutes{})

	for _, route := range routes.Routes {
		path := route.Path
		for _, p := range route.Params {
			path = strings.Replace(path, "{"+p+"}", "x", 1)
		}
		var body io.Reader = http.NoBody
		if route.Body == "*" {
			body = strings.NewReader("{}")
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(route.Method, routes.Prefix+path, body)
		r.ServeHTTP(rec, req)
		// Unimplemented, which means it reached a generated handler.
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s %s: status %d, want 501: %s", route.Method, path, rec.Code, rec.Body)
		}
	}
}

// What chi answers before a generated handler runs. go/server/doc.go states
// each of these; this is what makes the statement a check.
func TestWhatChiOwns(t *testing.T) {
	r := chi.NewRouter()
	server.RegisterTopicRoutes(r, &server.Runtime{Prefix: routes.Prefix}, &topics{})

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
