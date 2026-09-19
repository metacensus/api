// Package manifestgen renders go/routes/manifest.go and
// ts/src/route-manifest.ts: the same data-only Route in Go and TypeScript. One
// package because they are one decision rendered twice — a field added to
// model.Route is a field both literals must gain in the same commit.
package manifestgen

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/metacensus/api/routegen/internal/model"
)

//go:embed manifest.go.tmpl
var goTmplSrc string

//go:embed manifest.ts.tmpl
var tsTmplSrc string

// funcs is what these templates may call beyond the builtins. Every Go or
// TS string literal goes through the builtin printf "%q" rather than a
// helper.
var funcs = template.FuncMap{
	"goSlice":  model.GoSlice,
	"quoteAll": model.QuoteAll,
	"join":     func(sep string, items []string) string { return strings.Join(items, sep) },
}

var (
	goTmpl = template.Must(template.New("manifest.go.tmpl").Funcs(funcs).Parse(goTmplSrc))
	tsTmpl = template.Must(template.New("manifest.ts.tmpl").Funcs(funcs).Parse(tsTmplSrc))
)

// packagesData is what the header block needs: the surfaces, so the generated
// prose names each prefix constant beside the path it holds.
type packagesData struct{ Packages []model.Package }

// RenderGo writes go/routes/manifest.go: a constant per prefix and the whole
// route table as a []Route literal, gofmt'd.
func RenderGo(routes []model.Route) ([]byte, error) {
	return render(goTmpl, "manifest.go.tmpl", routes, func(b []byte) ([]byte, error) {
		return model.GoFormat("go/routes/manifest.go", b)
	})
}

// RenderTS writes ts/src/route-manifest.ts: the same constants and the same
// route table as a `routes` array literal. There is no formatter downstream
// of this one, so its whitespace is exactly what the template emits.
func RenderTS(routes []model.Route) ([]byte, error) {
	return render(tsTmpl, "manifest.ts.tmpl", routes, func(b []byte) ([]byte, error) {
		return b, nil
	})
}

// render runs the shared block sequence once, so the two templates differ only
// in what each block says. The prefix block runs per package, so a third surface
// is a table entry, not a template edit.
func render(t *template.Template, name string, routes []model.Route, format func([]byte) ([]byte, error)) ([]byte, error) {
	var b bytes.Buffer
	if err := execute(&b, t, name, "header", packagesData{model.Packages}, "", ""); err != nil {
		return nil, err
	}
	for _, pkg := range model.Packages {
		if err := execute(&b, t, name, "prefix", pkg, "", ""); err != nil {
			return nil, err
		}
	}
	if err := execute(&b, t, name, "routeType", nil, "", ""); err != nil {
		return nil, err
	}
	for _, r := range routes {
		if err := execute(&b, t, name, "route", r, r.Service, r.RPC); err != nil {
			return nil, err
		}
	}
	if err := execute(&b, t, name, "footer", nil, "", ""); err != nil {
		return nil, err
	}
	return format(b.Bytes())
}

// execute runs one named block, naming the template, the block and the route
// on failure.
func execute(b *bytes.Buffer, t *template.Template, tmplName, block string, data any, service, rpc string) error {
	if err := t.ExecuteTemplate(b, block, data); err != nil {
		if service != "" {
			return fmt.Errorf("%s: %s: route %s.%s: %w", tmplName, block, service, rpc, err)
		}
		return fmt.Errorf("%s: %s: %w", tmplName, block, err)
	}
	return nil
}
