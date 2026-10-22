package contract_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/metacensus/api/go/server/routes"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// declaredRPCs is every rpc across every contract package, as
// "Service.Method". Registration is the blank imports in registered_test.go.
func declaredRPCs(t *testing.T) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !isContractPackage(string(fd.Package())) {
			return true
		}
		for i := 0; i < fd.Services().Len(); i++ {
			svc := fd.Services().Get(i)
			for j := 0; j < svc.Methods().Len(); j++ {
				out[string(svc.Name())+"."+string(svc.Methods().Get(j).Name())] = true
			}
		}
		return true
	})
	if len(out) == 0 {
		t.Fatalf("no rpcs registered for %v", contractPackages)
	}
	return out
}

// fullPath is the route's identity wherever uniqueness matters: two surfaces
// can each declare a "/partner" and mean different URLs.
func fullPath(r routes.Route) string { return r.Prefix + r.Path }

// The manifest and the services disagreeing is what a silently dropped
// annotation looks like.
func TestManifestCoversEveryRPC(t *testing.T) {
	declared := declaredRPCs(t)

	for _, r := range routes.Routes {
		key := r.Service + "." + r.RPC
		if !declared[key] {
			t.Errorf("manifest has %s, no service declares it", key)
		}
		delete(declared, key)
	}
	for name := range declared {
		t.Errorf("%s is not in the manifest; run `make gen`", name)
	}
}

// Two routes sharing a method and full path is something no implementation
// could dispatch.
func TestRoutesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, r := range routes.Routes {
		key := r.Method + " " + fullPath(r)
		if first, ok := seen[key]; ok {
			t.Errorf("%s is declared by both %s and %s.%s", key, first, r.Service, r.RPC)
		}
		seen[key] = r.Service + "." + r.RPC
	}
}

func isRead(r routes.Route) bool {
	if r.Service == "HealthRoutes" && r.RPC == "Healthcheck" {
		return true
	}
	for _, prefix := range []string{"List", "Get", "Lookup"} {
		if strings.HasPrefix(r.RPC, prefix) {
			return true
		}
	}
	return false
}

func verbSegment(path string) string {
	verbs := map[string]bool{"create": true, "edit": true, "update": true, "delete": true, "lookup": true}
	for _, segment := range strings.Split(path, "/") {
		if verbs[segment] {
			return segment
		}
	}
	return ""
}

func nonConformingRoutes(routeSet []routes.Route) map[string]string {
	reasons := map[string][]string{}
	for _, r := range routeSet {
		key := r.Method + " " + fullPath(r)
		want, naming := "POST", "reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck"
		if isRead(r) {
			want, naming = "GET", "List*/Get*/Lookup* and HealthRoutes.Healthcheck are reads"
		}
		if r.Method != want {
			reasons[key] = append(reasons[key], "expected "+want+" ("+naming+")")
		}
		if verb := verbSegment(r.Path); verb != "" {
			reasons[key] = append(reasons[key], "verb in path: "+verb)
		}
	}

	got := map[string]string{}
	for key, rs := range reasons {
		got[key] = strings.Join(rs, "; ")
	}
	return got
}

func TestNonConformingRoutes_Methods(t *testing.T) {
	tests := []struct {
		name, service, rpc, method, path, want string
	}{
		{"error - write over GET", "TopicRoutes", "CreateTopic", "GET", "/topic", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"error - unrecognized read name over GET", "TopicRoutes", "SearchTopics", "GET", "/topic", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"error - set over GET", "PropRoutes", "SetVote", "GET", "/vote", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"error - auth over GET", "AuthRoutes", "Login", "GET", "/login", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"error - read over POST", "TopicRoutes", "GetTopic", "POST", "/topic", "expected GET (List*/Get*/Lookup* and HealthRoutes.Healthcheck are reads)"},
		{"error - list over POST", "TopicRoutes", "ListTopics", "POST", "/topic", "expected GET (List*/Get*/Lookup* and HealthRoutes.Healthcheck are reads)"},
		{"error - lookup over POST", "TopicRoutes", "LookupTopic", "POST", "/topic", "expected GET (List*/Get*/Lookup* and HealthRoutes.Healthcheck are reads)"},
		{"error - PUT", "TopicRoutes", "CreateTopic", "PUT", "/topic", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"error - PATCH", "TopicRoutes", "CreateTopic", "PATCH", "/topic", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"error - DELETE", "TopicRoutes", "CreateTopic", "DELETE", "/topic", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"error - method read and path", "TopicRoutes", "GetTopic", "PUT", "/topic/update", "expected GET (List*/Get*/Lookup* and HealthRoutes.Healthcheck are reads); verb in path: update"},
		{"error - write and path", "TopicRoutes", "CreateTopic", "GET", "/topic/create", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck); verb in path: create"},
		{"error - path alone", "TopicRoutes", "CreateTopic", "POST", "/topic/create", "verb in path: create"},
		{"error - health over POST", "HealthRoutes", "Healthcheck", "POST", "/healthcheck", "expected GET (List*/Get*/Lookup* and HealthRoutes.Healthcheck are reads)"},
		{"error - health name on another service", "TopicRoutes", "Healthcheck", "GET", "/healthcheck", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"error - health name prefix", "HealthRoutes", "HealthcheckReset", "GET", "/healthcheck", "expected POST (reads must be named List*/Get*/Lookup* or HealthRoutes.Healthcheck)"},
		{"success - get", "TopicRoutes", "GetTopic", "GET", "/topic/{topicId}", ""},
		{"success - list", "TopicRoutes", "ListTopics", "GET", "/topic", ""},
		{"success - lookup", "TopicRoutes", "LookupTopic", "GET", "/topic", ""},
		{"success - create", "TopicRoutes", "CreateTopic", "POST", "/topic", ""},
		{"success - set", "PropRoutes", "SetVote", "POST", "/vote", ""},
		{"success - auth", "AuthRoutes", "Login", "POST", "/login", ""},
		{"success - health", "HealthRoutes", "Healthcheck", "GET", "/healthcheck", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := routes.Route{Prefix: routes.Prefix, Service: tt.service, RPC: tt.rpc, Method: tt.method, Path: tt.path}
			want := map[string]string{}
			if tt.want != "" {
				want[r.Method+" "+fullPath(r)] = tt.want
			}
			if got := nonConformingRoutes([]routes.Route{r}); !maps.Equal(got, want) {
				t.Errorf("non-conforming routes = %v, want %v", got, want)
			}
		})
	}
}

func TestNonConformingRoutes_AccumulatesByFullPath(t *testing.T) {
	tests := []struct {
		name   string
		routes []routes.Route
		want   map[string]string
	}{
		{
			name: "error - shared route keeps both reasons",
			routes: []routes.Route{
				{Prefix: routes.Prefix, RPC: "GetTopic", Method: "POST", Path: "/topic/create"},
				{Prefix: routes.Prefix, RPC: "CreateTopic", Method: "POST", Path: "/topic/create"},
			},
			want: map[string]string{
				"POST " + routes.Prefix + "/topic/create": "expected GET (List*/Get*/Lookup* and HealthRoutes.Healthcheck are reads); verb in path: create; verb in path: create",
			},
		},
		{
			name: "error - surfaces keep separate reasons",
			routes: []routes.Route{
				{Prefix: routes.Prefix, RPC: "GetTopic", Method: "POST", Path: "/topic/create"},
				{Prefix: routes.PublicPrefix, RPC: "CreateTopic", Method: "POST", Path: "/topic/create"},
			},
			want: map[string]string{
				"POST " + routes.Prefix + "/topic/create":       "expected GET (List*/Get*/Lookup* and HealthRoutes.Healthcheck are reads); verb in path: create",
				"POST " + routes.PublicPrefix + "/topic/create": "verb in path: create",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nonConformingRoutes(tt.routes); !maps.Equal(got, tt.want) {
				t.Errorf("non-conforming routes = %v, want %v", got, tt.want)
			}
		})
	}
}

// Pins the set of routes that break the route conventions — empty today, but
// kept as a map (rather than the check collapsing to "no exceptions") since
// this contract has had deliberate exceptions before (metacensus/api#8) and
// may again.
func TestNonConformingRoutes(t *testing.T) {
	want := map[string]string{}

	got := nonConformingRoutes(routes.Routes)

	for _, key := range slices.Sorted(maps.Keys(got)) {
		if reason, ok := want[key]; !ok {
			t.Errorf("%s is newly non-conforming: %s", key, got[key])
		} else if reason != got[key] {
			t.Errorf("%s: non-conforming for %q, expected %q", key, got[key], reason)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(want)) {
		if _, ok := got[key]; !ok {
			t.Errorf("%s now conforms; strike it off this test", key)
		}
	}
}

// TestPrefix pins the shape of each declared prefix and its relationship to
// the paths it prefixes. It reads prefixes off the routes, so a third is
// covered the day it appears.
func TestPrefix(t *testing.T) {
	declared := map[string]bool{}
	for _, r := range routes.Routes {
		declared[r.Prefix] = true
	}
	if len(declared) == 0 {
		t.Fatal("no route declares a prefix")
	}

	// A constant no route uses is a prefix nothing serves.
	for name, prefix := range map[string]string{
		"routes.Prefix":       routes.Prefix,
		"routes.PublicPrefix": routes.PublicPrefix,
	} {
		if !declared[prefix] {
			t.Errorf("%s is %q, which no route hangs off", name, prefix)
		}
	}

	for prefix := range declared {
		if prefix == "" {
			t.Error("a route has an empty prefix")
			continue
		}
		if !strings.HasPrefix(prefix, "/") {
			t.Errorf("prefix %q does not start with /", prefix)
		}
		if strings.HasSuffix(prefix, "/") {
			t.Errorf("prefix %q has a trailing slash; joining it with a Path "+
				"would double the separator", prefix)
		}
	}

	// No prefix may contain another: the reverse proxy routes by longest-
	// prefix match, so a nested pair would move "which service answers this"
	// into a config file in a third repository.
	for outer := range declared {
		for inner := range declared {
			if outer == inner {
				continue
			}
			if strings.HasPrefix(inner, outer+"/") {
				t.Errorf("prefix %q nests inside %q. Which service answers a request "+
					"then depends on the order of rules in the reverse proxy's config, "+
					"which is not in this repository. Decide that deliberately before "+
					"relaxing this.", inner, outer)
			}
		}
	}

	for _, r := range routes.Routes {
		if !strings.HasPrefix(r.Path, "/") {
			t.Errorf("%s.%s: path %q does not start with /", r.Service, r.RPC, r.Path)
		}
		// A path that already carries its prefix would be served at
		// Prefix+Prefix once joined.
		if strings.HasPrefix(r.Path, r.Prefix) {
			t.Errorf("%s.%s: path %q already includes the prefix %q; paths are "+
				"declared relative to it", r.Service, r.RPC, r.Path, r.Prefix)
		}
	}
}

// A route whose prefix is neither declared constant is one nothing routes.
// This is also why /healthz is not in the contract — see AGENTS.md.
func TestEveryRouteHangsOffADeclaredPrefix(t *testing.T) {
	known := map[string]bool{
		routes.Prefix:       true,
		routes.PublicPrefix: true,
	}
	for _, r := range routes.Routes {
		if !known[r.Prefix] {
			t.Errorf("%s.%s hangs off %q, which is neither routes.Prefix nor "+
				"routes.PublicPrefix", r.Service, r.RPC, r.Prefix)
		}
	}
}
