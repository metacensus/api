package contract_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/metacensus/api/go/routes"
)

// openapiPath is relative to this package, which is go/.
//
// The name is protoc-gen-openapi's, not ours: in its default merged mode the
// plugin always writes openapi.yaml into the directory buf.gen.yaml gives it,
// with no option to rename it. YAML for the same reason — it emits nothing
// else.
const openapiPath = "../openapi/openapi.yaml"

type openapiDoc struct {
	OpenAPI string `yaml:"openapi"`
	Servers []struct {
		URL string `yaml:"url"`
	} `yaml:"servers"`
	Info struct {
		Title   string `yaml:"title"`
		Version string `yaml:"version"`
	} `yaml:"info"`
	Paths map[string]map[string]struct {
		OperationID string `yaml:"operationId"`
		Parameters  []struct {
			Name string `yaml:"name"`
			In   string `yaml:"in"`
		} `yaml:"parameters"`
	} `yaml:"paths"`
}

func loadOpenAPI(t *testing.T) openapiDoc {
	t.Helper()

	b, err := os.ReadFile(filepath.Clean(openapiPath))
	if err != nil {
		t.Fatalf("reading the generated document: %v; run `make gen`", err)
	}
	var doc openapiDoc
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("the generated document is not YAML: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("the generated document declares no paths")
	}
	return doc
}

// The document is OpenAPI 3, which is the whole reason there is no conversion
// step between the generator and oapi-codegen. If a future release of
// protoc-gen-openapi moved to 3.1, oapi-codegen would have to be checked
// against it rather than assumed to cope.
func TestOpenAPIIsVersion3(t *testing.T) {
	doc := loadOpenAPI(t)

	if !strings.HasPrefix(doc.OpenAPI, "3.0") {
		t.Errorf("openapi is %q; oapi-codegen reads this document as OpenAPI 3.0", doc.OpenAPI)
	}
}

// The document says where the API's routes are relative to each other and
// nothing about where they are absolutely: it carries no `servers` block, and
// every path is written bare -- /topic/{topicId}, not
// /metacensus/api/v1/topic/{topicId}.
//
// This is a limitation of the generator and not a choice. protoc-gen-openapi
// has no base-path or server option; the only thing it will turn into a
// `servers` entry is google.api.default_host on a service, which is a
// hostname commitment (it forces the scheme to https) rather than a path
// prefix, and would have to be written into every .proto.
//
// Nothing on the serving side depends on the gap -- server.New passes
// routes.Prefix as oapi-codegen's BaseURL, so the routes are mounted where the
// manifest says. It matters to whoever publishes this document: a client
// generated from it as it stands will request /topic/t-1 and get a 404, and
// the prefix has to be supplied out of band.
func TestOpenAPICarriesNoBasePath(t *testing.T) {
	doc := loadOpenAPI(t)

	if len(doc.Servers) != 0 {
		t.Errorf("the document declares servers %v; protoc-gen-openapi was not thought to "+
			"emit any, so this comment and the README should be corrected", doc.Servers)
	}

	for path := range doc.Paths {
		if strings.HasPrefix(path, routes.Prefix) {
			t.Errorf("%s already carries the manifest's Prefix; server.New adds it again as "+
				"oapi-codegen's BaseURL, so the routes would be served twice-prefixed", path)
		}
	}
}

// title and version are the two values in the document that no .proto
// expresses, so they are configured as plugin options in proto/buf.gen.yaml.
// Without them the title is empty -- protoc-gen-openapi only borrows a service
// name when there is exactly one service, and there are seven -- and the
// version defaults to 0.0.1, which is not this contract's.
func TestOpenAPIConfigurationWasApplied(t *testing.T) {
	doc := loadOpenAPI(t)

	if doc.Info.Title != "MetaCensus API" {
		t.Errorf("info.title is %q, not the title proto/buf.gen.yaml configures", doc.Info.Title)
	}
	if doc.Info.Version != "v1" {
		t.Errorf("info.version is %q, want v1", doc.Info.Version)
	}
}

// The document and the manifest are generated from the same annotations, so a
// route in one and not the other is a generator dropping something.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	doc := loadOpenAPI(t)

	declared := map[string]bool{}
	for path, ops := range doc.Paths {
		for method := range ops {
			declared[strings.ToUpper(method)+" "+path] = true
		}
	}

	for _, r := range routes.Routes {
		key := r.Method + " " + r.Path
		if !declared[key] {
			t.Errorf("the manifest declares %s, the document does not", key)
		}
		delete(declared, key)
	}
	for key := range declared {
		t.Errorf("the document declares %s, the manifest does not", key)
	}
}

// operationId is what a generated client names its method, so it is contract
// for anything built from this document rather than an internal label.
func TestOpenAPIOperationIDsNameTheirRPC(t *testing.T) {
	doc := loadOpenAPI(t)

	byKey := map[string]routes.Route{}
	for _, r := range routes.Routes {
		byKey[r.Method+" "+r.Path] = r
	}

	for path, ops := range doc.Paths {
		for method, op := range ops {
			r, ok := byKey[strings.ToUpper(method)+" "+path]
			if !ok {
				continue // TestOpenAPICoversEveryRoute reports this.
			}
			if want := r.Service + "_" + r.RPC; op.OperationID != want {
				t.Errorf("%s %s: operationId is %q, want %q", r.Method, r.Path, op.OperationID, want)
			}
		}
	}
}

// Params and Query in the manifest account for every field that does not travel
// in the body. The document describes the same split, so the two agreeing is
// what makes it safe to generate a client from either.
func TestOpenAPIParametersMatchTheManifest(t *testing.T) {
	doc := loadOpenAPI(t)

	byKey := map[string]routes.Route{}
	for _, r := range routes.Routes {
		byKey[r.Method+" "+r.Path] = r
	}

	for path, ops := range doc.Paths {
		for method, op := range ops {
			r, ok := byKey[strings.ToUpper(method)+" "+path]
			if !ok {
				continue
			}

			var inPath, inQuery []string
			for _, p := range op.Parameters {
				switch p.In {
				case "path":
					inPath = append(inPath, p.Name)
				case "query":
					inQuery = append(inQuery, p.Name)
				}
			}
			slices.Sort(inPath)
			slices.Sort(inQuery)

			wantPath := slices.Clone(r.Params)
			wantQuery := slices.Clone(r.Query)
			slices.Sort(wantPath)
			slices.Sort(wantQuery)

			if !slices.Equal(inPath, wantPath) {
				t.Errorf("%s %s: document path params %v, manifest %v",
					r.Method, r.Path, inPath, wantPath)
			}
			if !slices.Equal(inQuery, wantQuery) {
				t.Errorf("%s %s: document query params %v, manifest %v",
					r.Method, r.Path, inQuery, wantQuery)
			}
		}
	}
}
