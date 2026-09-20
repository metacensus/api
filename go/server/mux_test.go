package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/metacensus/api/go/server"
	"github.com/metacensus/api/go/server/routes"
)

// fakeChiRouter mimics chi v5's chi.Router.Method signature without pulling
// chi into this module's go.mod; a divergence fails to compile here. What
// routes actually do against a real chi.Router is routegen/chitest.
type fakeChiRouter struct {
	registered []string
}

func (f *fakeChiRouter) Method(method, pattern string, h http.Handler) {
	f.registered = append(f.registered, method+" "+pattern)
}

// A type with chi's Method signature satisfies server.Mux with no adapter.
// If this line stops compiling, Mux and chi.Router's Method have diverged.
var _ server.Mux = (*fakeChiRouter)(nil)

// Exercises EscapedPathValue's failure branch, unreachable via net/http
// itself (it rejects a bad escape before routing).
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

// "" mounts bare (for a router already under the contract's prefix) and
// routes.Prefix mounts at the origin root; both must be reachable.
func TestPrefixIsLiteral(t *testing.T) {
	for _, tc := range []struct{ prefix, want string }{
		{"", "GET /topic"},
		{routes.Prefix, "GET " + routes.Prefix + "/topic"},
		{"/v1", "GET /v1/topic"},
	} {
		fake := &fakeChiRouter{}
		server.RegisterTopicRoutes(fake, &server.Runtime{
			Prefix:    tc.prefix,
			PathValue: server.EscapedPathValue,
		}, server.UnimplementedTopicRoutes{})
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
	server.RegisterTopicRoutes(fake, &server.Runtime{
		Prefix:    routes.Prefix,
		PathValue: server.EscapedPathValue,
	}, server.UnimplementedTopicRoutes{})
	// Read off the manifest so adding/removing an rpc needs no literal update.
	want := 0
	for _, r := range routes.Routes {
		if r.Service == "TopicRoutes" {
			want++
		}
	}
	if len(fake.registered) != want {
		t.Fatalf("got %d registrations, want %d (one per TopicRoutes rpc)", len(fake.registered), want)
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

// A bare *http.ServeMux does not satisfy Mux (no method-specific
// registration); StdMux does, by embedding it and adding Method.
var _ server.Mux = server.StdMux{}

func TestStdMuxSatisfiesMux(t *testing.T) {
	mux := http.NewServeMux()
	std := server.StdMux{ServeMux: mux}
	server.RegisterTopicRoutes(std, &server.Runtime{Prefix: routes.Prefix}, server.UnimplementedTopicRoutes{})

	// The point is that the route reached a handler through StdMux, not that
	// it succeeded — UnimplementedTopicRoutes always answers 501.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metacensus/api/v1/topic", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d (Unimplemented)", rec.Code, http.StatusNotImplemented)
	}
}

// The zero value may not guess PathValue's convention: registration refuses
// a Mux this package cannot identify until told which one the router uses.
func TestRegisterRefusesAnUnidentifiedMuxWithNoPathValue(t *testing.T) {
	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("registered on a non-StdMux with PathValue unset")
		}
		if msg, _ := p.(string); !strings.Contains(msg, "EscapedPathValue") {
			t.Errorf("panic does not name the fix: %v", p)
		}
	}()
	server.RegisterTopicRoutes(&fakeChiRouter{}, &server.Runtime{Prefix: routes.Prefix}, server.UnimplementedTopicRoutes{})
}

// A StdMux is the one router this package wrote the adapter for, so it is the
// one whose convention can be inferred.
func TestStdMuxMayLeavePathValueUnset(t *testing.T) {
	server.RegisterTopicRoutes(server.StdMux{ServeMux: http.NewServeMux()},
		&server.Runtime{Prefix: routes.Prefix}, server.UnimplementedTopicRoutes{})
}
