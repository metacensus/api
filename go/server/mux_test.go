package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metacensus/api/go/server"
)

// fakeChiRouter has exactly chi v5's chi.Router.Method signature
// (Method(method, pattern string, h http.Handler)) and nothing else this
// package needs. It exists so this test can prove Mux's shape matches chi's
// router without adding chi as a module dependency: the published module
// stays at two direct requirements (see go.mod), and this is checked at
// compile time, not asserted in a comment.
//
// The signature was read from
// github.com/go-chi/chi/v5@v5.1.0/chi.go's Router interface, the version
// metacensus/infra pins.
type fakeChiRouter struct {
	registered []string
}

func (f *fakeChiRouter) Method(method, pattern string, h http.Handler) {
	f.registered = append(f.registered, method+" "+pattern)
}

// A type with chi's Method signature satisfies server.Mux with no adapter.
// If this line stops compiling, Mux and chi.Router's Method have diverged.
var _ server.Mux = (*fakeChiRouter)(nil)

func TestMuxAcceptsAChiShapedRouter(t *testing.T) {
	fake := &fakeChiRouter{}
	server.RegisterTopicRoutes(fake, &server.Runtime{}, server.UnimplementedTopicRoutes{})
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
	server.RegisterTopicRoutes(std, &server.Runtime{}, server.UnimplementedTopicRoutes{})

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
