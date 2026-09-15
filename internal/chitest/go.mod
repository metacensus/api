// The chi conformance test, deliberately a separate module.
//
// github.com/metacensus/api is published with exactly two direct
// requirements. chi is not one of them and must not become one: this package
// never ships, is never imported, and exists only so `make test` can run the
// generated routes against the router metacensus/infra actually uses, rather
// than against a local type that merely copies chi's method signature.
//
// The replace is what keeps this honest — the test runs against the working
// tree, not against a published version.
module github.com/metacensus/api/internal/chitest

go 1.24.0

replace github.com/metacensus/api => ../..

require (
	github.com/go-chi/chi/v5 v5.1.0
	github.com/metacensus/api v0.0.0-00010101000000-000000000000
)

require (
	google.golang.org/genproto/googleapis/api v0.0.0-20250908214217-97024824d090 // indirect
	google.golang.org/protobuf v1.36.9 // indirect
)
