# The MetaCensus API contract, in Protocol Buffers

Every request and response under `/metacensus/api/v1`, defined once in `.proto` and generated into Go and TypeScript.

This repository was split out of [`metacensus/ui`](https://github.com/metacensus/ui), where it lived as `contract/`. The 16 commits that built it came across intact — they are the derivation argument for why each field is in or out, and `git log` is the place to read it.

**Nothing consumes this yet.** The SPA uses `types/*.ts`, demo hand-writes its handlers, infra uses `core/shared/types`. Adopting it is [metacensus/ui#51](https://github.com/metacensus/ui/issues/51).

Open questions about the contract's content carry the `api-unification` label. What it deliberately leaves out, and the question each omission becomes, is [#50](https://github.com/metacensus/ui/issues/50).

## Layout

```
proto/           .proto sources and buf config — the definition
go/              generated Go, the wire encoder, the manifest generator, tests
ts/              generated TypeScript interfaces, the npm package
internal/tools/  the pinned code generators, a module of its own
scripts/         version.sh, which `make release` uses to mint tags
go.mod           the published Go module, rooted here
```

**`go.mod` is at the repository root, not in `go/`, and must stay there.** A module whose `go.mod` sits in a subdirectory `go/` is versioned by tags of the form `go/v1.2.3`. A plain `v1.2.3` tag would then publish nothing and `go get ...@v1.2.3` would fail with "no matching versions", while a `go/v1.2.3` tag would not match the release workflow's tag filter, so nothing would run and no failure would be reported. Rooted here, one plain semver tag does every job.

## Working on it

```bash
make deps    # npm ci in ts/ (Go needs no install step)
make gen     # regenerate Go, TypeScript and the route manifest
make check   # everything CI runs, bar the freshness diff
make hooks   # optional: lint and format-check .proto on commit
```

Generated code is committed; CI regenerates and fails on any diff.

`buf` and `protoc-gen-go` are `tool` dependencies of **`internal/tools`, a separate module**. Under Go 1.24 a `tool` directive is a real module requirement: left in the published module they added 90 indirect requirements — the Docker CLI, quic-go, the whole buf server graph — to everything that imported the contract. The published module now requires two things.

`make tools` builds those generators into `bin/`, and **buf runs from the repository root**, so every relative path in `buf.gen.yaml` and in `cmd/routegen` is relative to the root. `routegen` refuses to run anywhere else: its output paths are relative, so a wrong working directory would quietly write the manifests elsewhere and leave the committed ones stale — which the freshness check cannot see, because nothing in the tree changed.

## Consuming it

```bash
go get github.com/metacensus/api          # import github.com/metacensus/api/go/metacensus/v1
npm install @metacensus/api               # types only, zero dependencies
```

Neither is published yet. See "Releasing", below.

## Releasing

Tags are minted, never typed:

```bash
make release-patch          # or release-minor, release-major
make release VERSION=1.4.0  # or an explicit version
make latest                 # the current version tag
```

`scripts/version.sh` validates semver and the target refuses a tag that already exists. Pushing the tag is the whole release: `.github/workflows/release.yml` runs the full CI suite first and only then publishes npm, and the Go module needs nothing but the tag for proxy.golang.org to serve it.

## JSON is the wire

Protobuf binary is not used, not supported and not a fallback. Protobuf is here for the schema, the code generation and the breaking-change detection.

- **Marshal with `protojson`, never `encoding/json`.** `go/` exports `Marshal`, `Unmarshal` and `MarshalOptions`. Under `encoding/json` the generated types produce enums as integers, timestamps as `{"seconds":…,"nanos":…}` and 64-bit integers as unquoted numbers.
- **Field names are `snake_case` in proto, `lowerCamelCase` on the wire.** protojson converts; there are no `json_name` overrides.
- **Enum values are PascalCase** and nested in the message that owns them, because protojson serialises an enum as its value name. Zero values are `Unspecified`.
- **All ids are strings.** demo mints integer serials, infra prefixed UUIDs.
- **Presence is expressed only through message-typed fields.** `EmitDefaultValues` plus ts-proto's `useOptionals=messages` makes scalars, enums and repeated fields always present, and message fields `?: T | undefined`. A proto3 `optional` scalar is a third case the two sides would disagree about; use `google.protobuf.Int32Value` and friends instead.

Agreement between the JSON Go emits and the TypeScript generated from the same `.proto` is not yet checked — that needs real documents: [#49](https://github.com/metacensus/ui/issues/49).

## Routes

Each resource file declares its own routes: `topic.proto` has `TopicRoutes`, `paper.proto` has `PaperRoutes`, and so on for all 24.

**To read the whole route table at once, read the generated manifest** — `go/routes` or `ts/src/route-manifest.ts`. `params`, `query` and `body` between them account for every field of the request message, so **a request message models the whole request**, not only its body: `PropCreateRequest` carries `topic_id` although `topic_id` never travels in a body.

**They are a route declaration, not a gRPC commitment.** Nothing generates or serves gRPC: no `protoc-gen-go-grpc`, no grpc-gateway, no Connect. ts-proto is given `outputServices=none`, without which it emits service interfaces of `Promise`-returning methods.

Three routes on `Paper` break the conventions and are left broken deliberately, pinned by `TestNonConformingRoutes` so a fourth fails the build. Fixing them is [#41](https://github.com/metacensus/ui/issues/41).

## What the tests check

`go test ./...` reads the compiled descriptors, so every check is a property of the schema: JSON name and enum casing, `Unspecified` zero values, string ids, no proto3 `optional` scalars, `{items}` on every list, no pagination fields, no message field typed from another resource's file, and a route manifest that covers every rpc with no two routes sharing a method and path.

`ts/scripts/check-no-runtime.mjs` asserts the TypeScript is genuinely types: empty `dependencies`, no value imports under `src/`.
