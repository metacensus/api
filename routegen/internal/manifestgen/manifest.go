// Package manifestgen renders go/routes/manifest.go and
// ts/src/route-manifest.ts: the same data-only Route, in Go and in
// TypeScript. One package for both because they are one decision rendered
// twice — a field added to model.Route is a field both literals must gain
// in the same commit, and splitting them would only add two import lines
// for renderers that share every fact they emit.
//
// The loop, not the template, decides how many times the "route" block runs,
// so a failure can name the route it was rendering.
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

type apiPrefixData struct{ APIPrefix string }

// RenderGo writes go/routes/manifest.go: the whole route table as a
// []Route literal, gofmt'd.
func RenderGo(routes []model.Route) ([]byte, error) {
	var b bytes.Buffer
	if err := execute(&b, goTmpl, "manifest.go.tmpl", "header", apiPrefixData{model.APIPrefix}, "", ""); err != nil {
		return nil, err
	}
	for _, r := range routes {
		if err := execute(&b, goTmpl, "manifest.go.tmpl", "route", r, r.Service, r.RPC); err != nil {
			return nil, err
		}
	}
	if err := execute(&b, goTmpl, "manifest.go.tmpl", "footer", nil, "", ""); err != nil {
		return nil, err
	}
	return model.GoFormat("go/routes/manifest.go", b.Bytes())
}

// RenderTS writes ts/src/route-manifest.ts: the same route table as a
// `routes` array literal. There is no formatter downstream of this one, so
// its whitespace is exactly what the template emits.
func RenderTS(routes []model.Route) ([]byte, error) {
	var b bytes.Buffer
	if err := execute(&b, tsTmpl, "manifest.ts.tmpl", "header", apiPrefixData{model.APIPrefix}, "", ""); err != nil {
		return nil, err
	}
	for _, r := range routes {
		if err := execute(&b, tsTmpl, "manifest.ts.tmpl", "route", r, r.Service, r.RPC); err != nil {
			return nil, err
		}
	}
	if err := execute(&b, tsTmpl, "manifest.ts.tmpl", "footer", nil, "", ""); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
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
