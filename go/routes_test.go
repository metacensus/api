package contract_test

import (
	"strings"
	"testing"

	v1 "github.com/metacensus/ui/contract/go/metacensus/v1"
	"github.com/metacensus/ui/contract/go/routes"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const serviceName = "metacensus.v1.Routes"

func service(t *testing.T) protoreflect.ServiceDescriptor {
	t.Helper()

	// Referencing the package keeps its descriptors registered.
	_ = v1.File_metacensus_v1_routes_proto

	d, err := protoregistry.GlobalFiles.FindDescriptorByName(serviceName)
	if err != nil {
		t.Fatalf("finding %s: %v", serviceName, err)
	}
	svc, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("%s is not a service", serviceName)
	}
	return svc
}

// TestManifestCoversEveryRPC fails when the generated manifest and the service
// disagree, which is what a silently dropped annotation looks like.
func TestManifestCoversEveryRPC(t *testing.T) {
	svc := service(t)

	declared := map[string]bool{}
	for i := 0; i < svc.Methods().Len(); i++ {
		declared[string(svc.Methods().Get(i).Name())] = true
	}

	for _, r := range routes.Routes {
		if !declared[r.RPC] {
			t.Errorf("manifest has %s, the service does not", r.RPC)
		}
		delete(declared, r.RPC)
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
			t.Errorf("%s is declared by both %s and %s", key, first, r.RPC)
		}
		seen[key] = r.RPC
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
		"POST /paper":             "read over POST",
		"POST /protocol-template": "read over POST",
		"POST /protocol-element":  "read over POST",
		"POST /paper/create":      "verb in path: create",
		"GET /paper/lookup":       "verb in path: lookup",
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
