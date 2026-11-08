package contract_test

import (
	"maps"
	"regexp"
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

var pathParamPattern = regexp.MustCompile(`\{([^{}]*)\}`)

// routegen spells each {param} as the field's JSON name, which
// TestJSONNamesAreLowerCamelCase already holds; this holds the manifest anyway,
// since the manifest is what a consumer reads.
func TestPathParamsAreLowerCamelCase(t *testing.T) {
	for _, r := range routes.Routes {
		key := r.Method + " " + fullPath(r)
		var params []string
		for _, match := range pathParamPattern.FindAllStringSubmatch(fullPath(r), -1) {
			param := match[1]
			params = append(params, param)
			if !lowerCamel.MatchString(param) {
				t.Errorf("%s: parameter {%s} must be lowerCamelCase", key, param)
			}
		}
		if !slices.Equal(params, r.Params) {
			t.Errorf("%s: path parameters %q differ from manifest Params %q", key, params, r.Params)
		}
	}
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
// this contract has had deliberate exceptions before (the Paper routes,
// removed in 9edc6fe) and may again.
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

	for outer := range declared {
		for inner := range declared {
			if outer == inner {
				continue
			}
			if strings.HasPrefix(inner, outer+"/") {
				t.Errorf("prefix %q nests inside %q, so a request's prefix no longer "+
					"names the service that answers it. Decide that deliberately before "+
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

// requestMessage is the route's request message, which routegen names in the
// manifest relative to the route's proto package.
func requestMessage(t *testing.T, r routes.Route) protoreflect.MessageDescriptor {
	t.Helper()
	pkg := "metacensus.v1"
	if r.Prefix == routes.PublicPrefix {
		pkg = "metacensus.public.v1"
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(pkg + "." + r.Request))
	if err != nil {
		t.Fatalf("%s.%s: request %s: %v", r.Service, r.RPC, r.Request, err)
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("%s.%s: request %s is not a message", r.Service, r.RPC, r.Request)
	}
	return md
}

// recordAliases names the record a caller-scoped resource stands for: GET
// /self is the caller's own User, so it answers with UserSigned.
var recordAliases = map[string]string{
	"Self": "User",
}

var rpcName = regexp.MustCompile(`^([A-Z][a-z]+)([A-Z][a-zA-Z0-9]*)$`)

// messageNameProblem says how a route's messages break the naming rule, or ""
// if they keep it. An rpc is {Action}{Resource} (plural for List); its request
// is {Resource}{Action}Request and its response {Record}List for a List,
// otherwise {Record} or {Record}Signed.
func messageNameProblem(rpc, request, response string) string {
	m := rpcName.FindStringSubmatch(rpc)
	if m == nil {
		return "rpc " + rpc + " is not {Action}{Resource}"
	}
	action, resource := m[1], m[2]
	if action == "List" {
		resource = strings.TrimSuffix(resource, "s")
	}
	var problems []string
	if want := resource + action + "Request"; request != want {
		problems = append(problems, "request "+request+", want "+want)
	}
	record := resource
	if alias, ok := recordAliases[resource]; ok {
		record = alias
	}
	want := []string{record, record + "Signed"}
	if action == "List" {
		want = []string{record + "List"}
	}
	if !slices.Contains(want, response) {
		problems = append(problems, "response "+response+", want "+strings.Join(want, " or "))
	}
	return strings.Join(problems, "; ")
}

func TestMessageNameProblem(t *testing.T) {
	for _, tt := range []struct {
		rpc, request, response string
		ok                     bool
	}{
		{"ListProps", "PropListRequest", "PropList", true},
		{"GetProp", "PropGetRequest", "PropSigned", true},
		{"GetMember", "MemberGetRequest", "Member", true},
		{"GetSelf", "SelfGetRequest", "UserSigned", true},
		{"CreateProp", "CreatePropRequest", "PropSigned", false},
		{"CreateProp", "PropCreateRequest", "Prop", true},
		{"CreateProp", "PropCreateRequest", "PropCreateResponse", false},
		{"ListProps", "PropListRequest", "PropSigned", false},
		{"GetProp", "PropGetRequest", "PropList", false},
		{"GetProp", "PropGetRequest", "TopicSigned", false},
		{"Healthcheck", "HealthcheckRequest", "HealthcheckResponse", false},
	} {
		t.Run(tt.rpc+"/"+tt.request+"/"+tt.response, func(t *testing.T) {
			if got := messageNameProblem(tt.rpc, tt.request, tt.response); (got == "") != tt.ok {
				t.Errorf("messageNameProblem = %q, want ok=%v", got, tt.ok)
			}
		})
	}
}

// unrecordedRoutes lists the routes whose messages are not named for a record,
// each with its reason; TestRecordMessageNames fails if either side drifts.
var unrecordedRoutes = map[string]string{
	"POST /metacensus/api/v1/login":      "a session verb: it answers with a Session, not a record",
	"POST /metacensus/api/v1/signup":     "a session verb: it creates a User and answers with a Session",
	"POST /metacensus/api/v1/refresh":    "a session verb: it answers with a Session, not a record",
	"POST /metacensus/api/v1/logout":     "a session verb: it answers with an empty LogoutResponse",
	"GET /metacensus/api/v1/healthcheck": "a status, not a resource: HealthcheckResponse",
}

// Holds the record message names of every metacensus.v1 route unless listed in
// unrecordedRoutes. The public surface is outside the request standard.
func TestRecordMessageNames(t *testing.T) {
	got := map[string]string{}
	for _, r := range routes.Routes {
		if r.Prefix != routes.Prefix {
			continue
		}
		if problem := messageNameProblem(r.RPC, r.Request, r.Response); problem != "" {
			got[r.Method+" "+fullPath(r)] = problem
		}
	}

	for _, key := range slices.Sorted(maps.Keys(got)) {
		if _, ok := unrecordedRoutes[key]; !ok {
			t.Errorf("%s misnames its messages: %s. Records are {Resource}, "+
				"{Resource}Signed, {Resource}{Action}Request and {Resource}List "+
				"(AGENTS.md, \"The request standard\") — or add it to "+
				"unrecordedRoutes with the reason it moves no record.", key, got[key])
		}
	}
	for _, key := range slices.Sorted(maps.Keys(unrecordedRoutes)) {
		if _, ok := got[key]; !ok {
			t.Errorf("%s is listed in unrecordedRoutes and names its messages for "+
				"a record now; strike it off.", key)
		}
	}
}
