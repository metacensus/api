package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metacensus/api/go/routes"
	"github.com/metacensus/api/go/server"
)

// fakeChiRouter carries chi v5's chi.Router.Method signature and nothing
// else, so a divergence fails to compile here without chi in this module's
// go.mod. What the routes actually do on a real chi.Router is internal/chitest.
type fakeChiRouter struct {
	registered []string
}

func (f *fakeChiRouter) Method(method, pattern string, h http.Handler) {
	f.registered = append(f.registered, method+" "+pattern)
}

// A type with chi's Method signature satisfies server.Mux with no adapter.
// If this line stops compiling, Mux and chi.Router's Method have diverged.
var _ server.Mux = (*fakeChiRouter)(nil)

// EscapedPathValue's failure branch, which no request through net/http can
// reach: the server rejects a bad escape in the URI before routing. A router
// that hands over something else must not turn it into a bound value.
func TestEscapedPathValueRejectsBadEscaping(t *testing.T) {
	r := httptest.NewRequest("GET", "/topic/x", nil)
	r.SetPathValue("topicId", "%zz")
	if _, err := server.EscapedPathValue(r, "topicId"); err == nil {
		t.Error("no error for %zz")
	}
	r.SetPathValue("topicId", "a%2Fb")
	if got, err := server.EscapedPathValue(r, "topicId"); err != nil || got != "a/b" {
		t.Errorf("got %q, %v", got, err)
	}
}

// Prefix is the whole mounting story: "" mounts bare, for a router already
// under the contract's prefix, and routes.Prefix mounts at the origin root.
// Both have to be reachable, which is why "" is not a sentinel for the default.
func TestPrefixIsLiteral(t *testing.T) {
	for _, tc := range []struct{ prefix, want string }{
		{"", "GET /topic"},
		{routes.Prefix, "GET " + routes.Prefix + "/topic"},
		{"/v1", "GET /v1/topic"},
	} {
		fake := &fakeChiRouter{}
		server.RegisterTopicRoutes(fake, &server.Runtime{Prefix: tc.prefix}, server.UnimplementedTopicRoutes{})
		found := false
		for _, got := range fake.registered {
			if got == tc.want {
				found = true
			}
		}
		if !found {
			t.Errorf("Prefix %q: no registration %q; got %v", tc.prefix, tc.want, fake.registered)
		}
	}
}

func TestMuxAcceptsAChiShapedRouter(t *testing.T) {
	fake := &fakeChiRouter{}
	server.RegisterTopicRoutes(fake, &server.Runtime{Prefix: routes.Prefix}, server.UnimplementedTopicRoutes{})
	if len(fake.registered) != 6 {
		t.Fatalf("got %d registrations, want 6 (one per TopicRoutes rpc)", len(fake.registered))
	}
	for _, want := range []string{"GET /metacensus/api/v1/topic", "POST /metacensus/api/v1/topic"} {
		found := false
		for _, got := range fake.registered {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("chi-shaped router never saw %q; got %v", want, fake.registered)
		}
	}
}

// A bare *http.ServeMux does NOT satisfy Mux on its own — it has no
// method-specific registration call — which is exactly why StdMux exists.
// StdMux does, by embedding it and adding Method.
var _ server.Mux = server.StdMux{}

func TestStdMuxSatisfiesMux(t *testing.T) {
	mux := http.NewServeMux()
	std := server.StdMux{ServeMux: mux}
	server.RegisterTopicRoutes(std, &server.Runtime{Prefix: routes.Prefix}, server.UnimplementedTopicRoutes{})

	// UnimplementedTopicRoutes answers 501; the point of this test is that the
	// route reached a handler at all through a *http.ServeMux wrapped in
	// StdMux, not that it succeeded.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metacensus/api/v1/topic", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d (Unimplemented)", rec.Code, http.StatusNotImplemented)
	}
}
