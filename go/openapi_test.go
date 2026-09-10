package contract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/metacensus/api/go/routes"
)

// openapiPath is relative to this package, which is go/.
const openapiPath = "../openapi/metacensus.swagger.json"

type openapiDoc struct {
	Swagger  string `json:"swagger"`
	BasePath string `json:"basePath"`
	Info     struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]map[string]struct {
		OperationID string `json:"operationId"`
		Parameters  []struct {
			Name string `json:"name"`
			In   string `json:"in"`
		} `json:"parameters"`
	} `json:"paths"`
}

func loadOpenAPI(t *testing.T) openapiDoc {
	t.Helper()

	b, err := os.ReadFile(filepath.Clean(openapiPath))
	if err != nil {
		t.Fatalf("reading the generated document: %v; run `make gen`", err)
	}
	var doc openapiDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("the generated document is not JSON: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("the generated document declares no paths")
	}
	return doc
}

// basePath is the one value in the document that no .proto expresses, so it is
// configured by hand in proto/openapi.yaml and duplicates cmd/routegen's
// apiPrefix. This is what stops the copies drifting — the same job the
// generated manifest does for every other consumer of the prefix.
func TestOpenAPIBasePathMatchesManifest(t *testing.T) {
	doc := loadOpenAPI(t)

	if doc.BasePath != routes.Prefix {
		t.Errorf("basePath is %q, the manifest's Prefix is %q; proto/openapi.yaml and "+
			"cmd/routegen disagree about where the API is served", doc.BasePath, routes.Prefix)
	}
}

// proto/openapi.yaml keys its options on one .proto file, and with allow_merge
// the merged document takes them from whichever file sorts first. Nothing in
// the toolchain says so, so a new resource sorting before auth.proto would
// silently drop the base path and the title. An empty title is what that looks
// like.
func TestOpenAPIConfigurationWasApplied(t *testing.T) {
	doc := loadOpenAPI(t)

	if doc.Info.Title != "MetaCensus API" {
		t.Errorf("info.title is %q, not the configured title; proto/openapi.yaml keys its "+
			"options on the first .proto by sort order, and that file may have changed",
			doc.Info.Title)
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
