package manifestgen

import (
	"testing"

	"github.com/metacensus/api/routegen/internal/model"
)

// TestRouteVar pins the Go variable name a route is addressable by: the service
// name with its "Routes" suffix dropped, then the rpc.
func TestRouteVar(t *testing.T) {
	cases := []struct {
		service, rpc, want string
	}{
		{"AuthRoutes", "Refresh", "AuthRefresh"},
		{"AuthRoutes", "SignUp", "AuthSignUp"},
		{"TopicRoutes", "GetTopic", "TopicGetTopic"},
		{"PartnerRoutes", "SubmitPartnerInterest", "PartnerSubmitPartnerInterest"},
	}
	for _, c := range cases {
		if got := routeVar(model.Route{Service: c.service, RPC: c.rpc}); got != c.want {
			t.Errorf("routeVar(%s.%s) = %q, want %q", c.service, c.rpc, got, c.want)
		}
	}
}

// TestRouteVarsAreUnique guards the naming scheme against a future collision:
// two routes must never resolve to the same variable name, or the generated
// var block would not compile.
func TestRouteVarsAreUnique(t *testing.T) {
	all, err := model.Walk()
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	seen := map[string]string{}
	for _, r := range all {
		name := routeVar(r)
		if prev, dup := seen[name]; dup {
			t.Errorf("route var %q is claimed by both %s and %s.%s", name, prev, r.Service, r.RPC)
		}
		seen[name] = r.Service + "." + r.RPC
	}
}
