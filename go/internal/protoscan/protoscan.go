// Package protoscan lists the proto packages declared under a directory tree.
//
// It exists so the hand-written lists that drive generation and the schema
// tests can be checked against something that cannot forget. Both of those
// lists used to be checked against protoregistry, which holds only what the
// binary imported — so a package nobody imported was absent from the registry,
// absent from the check, and silently ungoverned.
package protoscan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var packageDecl = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z0-9_.]+)\s*;`)

// Packages returns every distinct proto package declared under root, sorted.
func Packages(root string) ([]string, error) {
	seen := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		m := packageDecl.FindSubmatch(b)
		if m == nil {
			return fmt.Errorf("%s declares no package", path)
		}
		seen[string(m[1])] = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(seen))
	for pkg := range seen {
		out = append(out, pkg)
	}
	sort.Strings(out)
	return out, nil
}
