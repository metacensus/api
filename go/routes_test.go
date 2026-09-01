package contract_test

import (
	"strings"
	"testing"

	v1 "github.com/metacensus/ui/contract/go/metacensus/v1"
	"github.com/metacensus/ui/contract/go/routes"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// declaredRPCs is every rpc across every service in the package, as
// "Service.Method".
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

// TestManifestCoversEveryRPC fails when the generated manifest and the service
// disagree, which is what a silently dropped annotation looks like.
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

// TestRoutesAreUnique fails on two routes sharing a method and path, which no
// implementation could dispatch.
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

// Reads are named for what they do, so the name is what a route can be checked
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

// TestNonConformingRoutes pins the routes that break the conventions in
// contract/DERIVATION.md. They are the substance of
// https://github.com/metacensus/ui/issues/41 and are kept declared rather than
// quietly fixed; a new one fails here, and a fixed one has to be struck off.
func TestNonConformingRoutes(t *testing.T) {
	want := map[string]string{
		"POST /paper":        "read over POST",
		"POST /paper/create": "verb in path: create",
		"GET /paper/lookup":  "verb in path: lookup",
	}

	got := map[string]string{}
	for _, r := range routes.Routes {
		key := r.Method + " " + r.Path
		if isRead(r.RPC) && r.Method != "GET" {
			got[key] = "read over " + r.Method
		}
		if verb := verbSegment(r.Path); verb != "" {
			got[key] = "verb in path: " + verb
		}
	}

	for _, key := range sortedKeys2(got) {
		if reason, ok := want[key]; !ok {
			t.Errorf("%s is newly non-conforming: %s", key, got[key])
		} else if reason != got[key] {
			t.Errorf("%s: non-conforming for %q, expected %q", key, got[key], reason)
		}
	}
	for _, key := range sortedKeys2(want) {
		if _, ok := got[key]; !ok {
			t.Errorf("%s now conforms; remove it from this test and from issue #41", key)
		}
	}
}
