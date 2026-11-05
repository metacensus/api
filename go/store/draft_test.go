package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"testing"
)

// Each Draft is a single-method interface named for its method and not yet in
// Store; storetest and the server find a Draft method by that name.
func TestDraftsAreSingleMethodInterfaces(t *testing.T) {
	stable := reflect.TypeFor[Store]()
	for _, d := range Drafts {
		if d.Kind() != reflect.Interface || d.NumMethod() != 1 {
			t.Errorf("Drafts lists %v; want an interface declaring one method", d)
			continue
		}
		m := d.Method(0).Name
		if d.Name() != m {
			t.Errorf("Draft %s declares %s; name it for its method", d.Name(), m)
		}
		if _, ok := stable.MethodByName(m); ok {
			t.Errorf("Draft %s is already in Store; drop it from draft.go", m)
		}
	}
}

// An interface declared in draft.go but missing from Drafts would be checked
// by nothing, and would slip past make release-check.
func TestDraftsListsEveryInterfaceInDraftGo(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "draft.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, d := range Drafts {
		listed = append(listed, d.Name())
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts := spec.(*ast.TypeSpec)
			if _, ok := ts.Type.(*ast.InterfaceType); ok && !slices.Contains(listed, ts.Name.Name) {
				t.Errorf("draft.go declares %s, which Drafts does not list", ts.Name.Name)
			}
		}
	}
}
