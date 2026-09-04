package contract_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/routes"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// declaredRPCs is every rpc in the package, as "Service.Method".
func declaredRPCs(t *testing.T) map[string]bool {
	t.Helper()

	// Referencing the package keeps its descriptors registered.
	_ = v1.File_metacensus_v1_topic_proto

	out := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != protoPkg {
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
		t.Fatalf("no rpcs registered for package %s", protoPkg)
	}
	return out
}

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

// Two routes sharing a method and path is something no implementation could
// dispatch.
func TestRoutesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, r := range routes.Routes {
		key := r.Method + " " + r.Path
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

// Pins the set of routes that break the route conventions. That set is empty:
// the only three members were Paper's, and Paper left the contract with its
// design unsettled (metacensus/api#8).
//
// The map stays rather than the check collapsing to "no route may break a
// convention", because a deliberate exception is a thing this contract has had
// and may have again. Empty, it says the exceptions are none — and any route
// that starts breaking a convention fails here with the reason spelled out.
func TestNonConformingRoutes(t *testing.T) {
	want := map[string]string{}

	// Reasons accumulate: a route can break more than one convention, and
	// overwriting would hide the second.
	reasons := map[string][]string{}
	for _, r := range routes.Routes {
		key := r.Method + " " + r.Path
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

// TestPrefix pins the shape of routes.Prefix and its relationship to the paths
// it prefixes. The point of generating the constant beside the manifest is that
// the two cannot drift; this is what "cannot drift" means concretely.
func TestPrefix(t *testing.T) {
	if routes.Prefix == "" {
		t.Fatal("routes.Prefix is empty")
	}
	if !strings.HasPrefix(routes.Prefix, "/") {
		t.Errorf("routes.Prefix %q does not start with /", routes.Prefix)
	}
	if strings.HasSuffix(routes.Prefix, "/") {
		t.Errorf("routes.Prefix %q has a trailing slash; joining it with a "+
			"Path would double the separator", routes.Prefix)
	}

	for _, r := range routes.Routes {
		if !strings.HasPrefix(r.Path, "/") {
			t.Errorf("%s.%s: path %q does not start with /", r.Service, r.RPC, r.Path)
		}
		// Paths are relative to Prefix. One that already carries it would be
		// served at Prefix+Prefix once joined.
		if strings.HasPrefix(r.Path, routes.Prefix) {
			t.Errorf("%s.%s: path %q already includes the prefix %q; paths are "+
				"declared relative to it", r.Service, r.RPC, r.Path, routes.Prefix)
		}
	}
}
