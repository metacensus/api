package store_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A structural claim, not a lexical one: the surface is exactly this, which
// holds against every driver, library and helper that would do the forbidden
// thing under a name nobody predicted.
//
// The seam is a definition two backends implement. A Fabric import here would
// make one implementer's SDK part of the definition — which is not
// hypothetical: infra's shared types import the Fabric chaincode SDK
// (core/shared/types/types.go:8-9) because WorldState and TxContext share a
// package with every request type, so a Postgres backend cannot import that
// package at all. An SQL import would do the same in the other direction, and
// an HTTP import would put status codes below a seam whose whole error
// taxonomy exists to keep them above it.
func TestPackageImportsOnlyTheStandardLibrary(t *testing.T) {
	// A standard-library path has no dot in its first segment.
	external := func(path string) bool {
		return strings.Contains(strings.SplitN(path, "/", 2)[0], ".")
	}

	for _, dir := range []string{".", "storetest", "storetest/internal/memstore"} {
		allowed := map[string]bool{}
		if dir != "." {
			// The suite and the double are the only things here that may
			// import the definition, and they may import nothing else.
			allowed["github.com/metacensus/api/go/store"] = true
		}

		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if len(pkgs) == 0 {
			t.Fatalf("%s: no package found; this test would pass vacuously", dir)
		}

		for _, pkg := range pkgs {
			for name, file := range pkg.Files {
				for _, imp := range file.Imports {
					path, err := strconv.Unquote(imp.Path.Value)
					if err != nil {
						t.Fatalf("%s: %v", name, err)
					}
					if !external(path) || allowed[path] {
						continue
					}
					t.Errorf("%s imports %q. The persistence seam is a definition two "+
						"backends implement; a dependency here becomes part of what every "+
						"implementer must accept. Driver, SDK and transport imports belong "+
						"below the seam, in the backend, or above it, in the shared layer.",
						filepath.ToSlash(name), path)
				}
			}
		}
	}
}
