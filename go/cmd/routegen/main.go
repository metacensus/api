// Command routegen writes the route manifest, a Go server and a TypeScript
// client for metacensus.v1 from the google.api.http annotations on each
// service. One walk of the compiled descriptors — internal/model — feeds
// four renderers, one package each:
//   - internal/manifestgen writes go/routes/manifest.go and
//     ts/src/route-manifest.ts, the data-only manifest;
//   - internal/servergen writes go/server/routes_gen.go, a handler
//     interface, an Unimplemented* and a Register* per service that binds
//     path/query/body and dispatches to it — the hand-written runtime
//     beside it is go/server/runtime.go;
//   - internal/clientgen writes ts/src/client.ts, a typed method per rpc
//     over a caller-supplied transport.
//
// A renderer package imports internal/model and nothing else under
// cmd/routegen; model imports no renderer. imports_test.go asserts this
// against the actual import graph.
//
// Every renderer's templates are text/template, embedded with go:embed and
// parsed at init. A template file is named <name>.<destination-extension>.tmpl
// — manifest.go.tmpl and manifest.ts.tmpl render the same route to Go and
// TypeScript, server.go.tmpl renders Go, client.ts.tmpl renders TypeScript —
// so the extension the file is embedded to render is visible in its name
// rather than only in the //go:embed line that reads it.
//
// It reads the descriptors the generated Go package registers, so it runs
// after `buf generate`. None of this changes the route table: routegen only
// deepens what is generated from the routes already declared, and refuses
// to generate at all for a route shape it cannot bind (see
// internal/model.Walk and internal/clientgen.Render).
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

	if err := write(goOut, manifestgen.RenderGo(routes)); err != nil {
		return err
	}
	if err := write(tsOut, manifestgen.RenderTS(routes)); err != nil {
		return err
	}
	if err := write(srvOut, servergen.Render(routes)); err != nil {
		return err
	}
	clientSrc, err := clientgen.Render(routes)
	if err != nil {
		return err
	}
	return write(clientOut, clientSrc)
}

func write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
