// Command routegen writes the route manifest, a Go server and TypeScript
// clients for every contract package from the google.api.http annotations on
// each service. One walk of the compiled descriptors — internal/model — feeds
// four renderings: internal/manifestgen, internal/servergen,
// internal/clientgen. Each package's own doc says what it emits.
//
// Which packages those are, and the prefix each one's routes hang off, is
// model.Packages — the one place either prefix is written down.
//
// A renderer imports internal/model and nothing else in this module;
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

	"github.com/metacensus/api/routegen/internal/clientgen"
	"github.com/metacensus/api/routegen/internal/manifestgen"
	"github.com/metacensus/api/routegen/internal/model"
	"github.com/metacensus/api/routegen/internal/servergen"
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

// contractModule is the module line of the repository root's go.mod — not
// this module's, which sits one directory below it.
const contractModule = "module github.com/metacensus/api"

// chdirRoot finds the repository root and moves there, because every output
// path is relative to it. The generator now lives in a module of its own, so
// `make gen` runs it from routegen/ and a plain cwd check would reject the
// only working directory it is ever invoked from.
//
// Walking up and checking the module line is stricter than trusting cwd, not
// looser: from anywhere inside the repository it finds the same root, and
// from outside it refuses rather than writing four generated files into
// somebody's home directory and leaving the committed ones stale — which the
// freshness check cannot see, because nothing in the tree changed.
func chdirRoot() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	start := dir
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		// A module line is the whole line, so routegen/go.mod's
		// "module github.com/metacensus/api/routegen" does not match.
		if err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.TrimSpace(line) == contractModule {
					return os.Chdir(dir)
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("no %s above %s; run this from inside the repository, or use `make gen`", contractModule, start)
		}
		dir = parent
	}
}

func run() error {
	if err := chdirRoot(); err != nil {
		return err
	}

	// Before the walk: a proto package on disk that the table does not name
	// would generate nothing and say nothing, and the walk cannot notice it
	// because it only ever looks at packages the table already lists.
	if err := model.CheckPackages(); err != nil {
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
