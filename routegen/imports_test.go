package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportGraph asserts the boundary the package split establishes: every
// renderer depends on the route model and nothing renders another
// renderer's output. It runs `go list -deps` rather than reading the source
// for import statements, so the check is against what the compiler actually
// links, not what a file happens to say.
func TestImportGraph(t *testing.T) {
	const (
		modelPkg       = "github.com/metacensus/api/routegen/internal/model"
		manifestgenPkg = "github.com/metacensus/api/routegen/internal/manifestgen"
		servergenPkg   = "github.com/metacensus/api/routegen/internal/servergen"
		clientgenPkg   = "github.com/metacensus/api/routegen/internal/clientgen"
	)
	renderers := []string{manifestgenPkg, servergenPkg, clientgenPkg}

	modelDeps := deps(t, modelPkg)
	for _, r := range renderers {
		if modelDeps[r] {
			t.Errorf("internal/model imports %s; the route model must not import a renderer", r)
		}
	}

	for _, r := range renderers {
		rd := deps(t, r)
		if !rd[modelPkg] {
			t.Errorf("%s does not import internal/model", r)
		}
		for _, other := range renderers {
			if other != r && rd[other] {
				t.Errorf("%s imports %s; renderer packages must not import each other", r, other)
			}
		}
	}
}

// deps returns pkg's full transitive import set, standard library included,
// as reported by the go command itself.
func deps(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		set[line] = true
	}
	return set
}
