// Command routegen writes the route manifest, a Go server and a TypeScript
// client for metacensus.v1 from the google.api.http annotations on each
// service. One walk of the compiled descriptors — internal/model — feeds four
// renderings: internal/manifestgen, internal/servergen, internal/clientgen.
// Each package's own doc says what it emits.
//
// A renderer imports internal/model and nothing else under cmd/routegen;
// model imports no renderer. imports_test.go asserts that against the build
// graph rather than stating it here.
//
// It reads the descriptors the generated Go package registers, so it runs
// after `buf generate`. It never changes the route table: it only deepens
// what is generated from the routes already declared, and refuses to generate
// at all for a route shape it cannot bind.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/metacensus/api/go/cmd/routegen/internal/clientgen"
	"github.com/metacensus/api/go/cmd/routegen/internal/manifestgen"
	"github.com/metacensus/api/go/cmd/routegen/internal/model"
	"github.com/metacensus/api/go/cmd/routegen/internal/servergen"
)

// Output paths are relative to the repository root, which is where the
// Makefile runs this. Everything else in the build — buf, the generator
// plugins — is rooted there too.
const (
	goOut     = "go/routes/manifest.go"
	tsOut     = "ts/src/route-manifest.ts"
	srvOut    = "go/server/routes_gen.go"
	clientOut = "ts/src/client.ts"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "routegen:", err)
		os.Exit(1)
	}
}

// modulePath anchors the cwd check below.
const modulePath = "module github.com/metacensus/api"

// checkRoot fails loudly when routegen is run from anywhere but the repository
// root. The Out paths are relative, so a wrong cwd does not error — it writes
// the generated files somewhere else and leaves the committed ones stale,
// which the freshness check cannot see because nothing in the tree changed.
func checkRoot() error {
	b, err := os.ReadFile("go.mod")
	if err != nil || !strings.Contains(string(b), modulePath) {
		wd, _ := os.Getwd()
		return fmt.Errorf("run from the repository root (cwd is %s); use `make gen`", wd)
	}
	return nil
}

func run() error {
	if err := checkRoot(); err != nil {
		return err
	}

	routes, err := model.Walk()
	if err != nil {
		return err
	}

	// Every renderer runs before anything is written, so a failure in the
	// last one does not leave the first three's output on disk.
	outputs := []struct {
		path   string
		render func([]model.Route) ([]byte, error)
	}{
		{goOut, manifestgen.RenderGo},
		{tsOut, manifestgen.RenderTS},
		{srvOut, servergen.Render},
		{clientOut, clientgen.Render},
	}
	rendered := make([][]byte, len(outputs))
	for i, o := range outputs {
		src, err := o.render(routes)
		if err != nil {
			return err
		}
		rendered[i] = src
	}
	for i, o := range outputs {
		if err := write(o.path, rendered[i]); err != nil {
			return err
		}
	}
	return nil
}

func write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
