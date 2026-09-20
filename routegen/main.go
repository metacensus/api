// Command routegen writes the route manifest, a Go server and TypeScript
// clients for every contract package from the google.api.http annotations on
// each service. One walk of the compiled descriptors — internal/model — feeds
// the renderers: internal/manifestgen, internal/servergen, internal/clientgen.
//
// It reads the descriptors the generated Go package registers, so it runs
// after `buf generate`. It never changes the route table; it refuses to
// generate at all for a route shape it cannot bind.
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
// Makefile runs this.
const (
	goOut     = "go/server/routes/manifest.go"
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
// path is relative to it. It walks up checking the module line rather than
// trusting cwd, so it refuses outside the repository instead of writing
// generated files into the wrong place.
func chdirRoot() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	start := dir
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		// The whole line must match, so routegen/go.mod's own module line
		// does not.
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

	// Before the walk: a proto package the table does not name would
	// otherwise generate nothing and say nothing.
	if err := model.CheckPackages(); err != nil {
		return err
	}

	routes, err := model.Walk()
	if err != nil {
		return err
	}

	// Every renderer runs before anything is written, so a late failure
	// does not leave partial output on disk.
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
