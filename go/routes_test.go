package contract_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/metacensus/api/go/routes"
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

// Reads are named for what they do, so the name is what to check the route
// against.
func isRead(rpc string) bool {
	for _, prefix := range []string{"List", "Get", "Lookup"} {
		if strings.HasPrefix(rpc, prefix) {
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

// Pins the set of routes that break the route conventions — empty today, but
// kept as a map (rather than the check collapsing to "no exceptions") since
// this contract has had deliberate exceptions before (metacensus/api#8) and
// may again.
func TestNonConformingRoutes(t *testing.T) {
	want := map[string]string{}

	// Reasons accumulate: a route can break more than one convention, and
	// overwriting would hide the second.
	reasons := map[string][]string{}
	for _, r := range routes.Routes {
		key := r.Method + " " + fullPath(r)
		if isRead(r.RPC) && r.Method != "GET" {
			reasons[key] = append(reasons[key], "read over "+r.Method)
		}
		if verb := verbSegment(r.Path); verb != "" {
			reasons[key] = append(reasons[key], "verb in path: "+verb)
		}
	}

	got := map[string]string{}
	for key, rs := range reasons {
		got[key] = strings.Join(rs, "; ")
	}

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
