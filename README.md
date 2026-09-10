# The MetaCensus API contract, in Protocol Buffers

Every request and response under `/metacensus/api/v1`, defined once in `.proto` and generated into Go and TypeScript.

This repository was split out of [`metacensus/ui`](https://github.com/metacensus/ui), where it lived as `contract/`. The 16 commits that built it came across intact — they are the derivation argument for why each field is in or out, and `git log` is the place to read it.

**Nothing consumes this yet.** infra, demo and the SPA each still carry their own hand-maintained types. One version, `v0.1.0`, was tagged to exercise the release path rather than to promise anything, and what reached the registries is uneven: the Go module is live on proxy.golang.org and permanently so, while npm carries only `@metacensus/api@0.0.0`, the placeholder version, as `latest` — `0.1.0` never got there.

The four issues in ui that tracked this work — adoption, what the contract deliberately leaves out, cross-language wire agreement, and the route conventions — were all closed as not-planned when the contract moved out of that repository. Nothing here replaces them yet, so the open questions recorded below are open in the plain sense: written down, not tracked.

## Layout

```
proto/           .proto sources and buf config — the definition
go/              generated Go, the wire encoder, the manifest generator, tests
ts/              generated TypeScript interfaces, the npm package
internal/tools/  the pinned code generators, a module of its own
scripts/         version.sh, which `make release` uses to mint tags
go.mod           the published Go module, rooted here
```

**The Go packages sit under `go/`, so imports read `github.com/metacensus/api/go/metacensus/v1`.** The alternative — hoisting `metacensus/` to the root for `github.com/metacensus/api/metacensus/v1` — reads better at the call site, and `go.mod` at the root already breaks the `proto/ go/ ts/` symmetry. The reason it stays is that the root is shared with `ts/`, `proto/`, `internal/` and `scripts/`, and a generated `metacensus/` tree alongside them would be the only directory whose name says nothing about which language reads it. Both are defensible; this one is a one-way door once a tag exists, because the import path is the module's public surface.

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

`make tools` builds those generators with `GOWORK=off` and a pinned `GOTOOLCHAIN`. Both are load-bearing, and both are lessons from [metacensus/infra#52](https://github.com/metacensus/infra/pull/52): a `go.work` above the checkout resolves tool versions against the union of its members and silently lifts the pins, and the `go` directive is a floor rather than a ceiling, so an unpinned toolchain builds the plugins against whatever stdlib the developer has. `protoc-gen-go` stamps its own version into every `.pb.go`, so either one surfaces as generated-code drift in a PR that never touched a `.proto`. It builds them into `bin/`, and **buf runs from the repository root**, so every relative path in `buf.gen.yaml` and in `cmd/routegen` is relative to the root. `routegen` refuses to run anywhere else: its output paths are relative, so a wrong working directory would quietly write the manifests elsewhere and leave the committed ones stale — which the freshness check cannot see, because nothing in the tree changed.

## Consuming it

```bash
go get github.com/metacensus/api          # import github.com/metacensus/api/go/metacensus/v1
npm install @metacensus/api               # types only, zero dependencies
```

Neither is ready to depend on: `go get` resolves `v0.1.0`, which exists to test the release path, and `npm install` resolves the `0.0.0` placeholder rather than any released contract. See "Releasing", below.

## What "breaking" is measured against

**CI does not enforce this.** The step is commented out in `.github/workflows/ci.yml`, and `make breaking` is intact for running by hand. Nothing consumes this contract and the only tag, `v0.1.0`, exists to exercise the release path rather than to promise anything — so the check was measuring every branch against a throwaway baseline, failing on the removal of `paper.proto`, and taking the rest of the suite down with it before the tests ever ran. [#14](https://github.com/metacensus/api/issues/14) records the trigger for turning it back on and the release-workflow bug that has to be fixed alongside it.

What the target is, by hand now and in CI again later:

`buf breaking` compares the schema against **the latest release tag**, not against the PR's merge base. The published contract is what a breaking change breaks; unreleased `main` is not a promise to anyone. That makes it a question about state — does the schema as it stands break what consumers have — rather than about a diff, so the answer does not depend on which base a PR happens to have.

Two consequences worth knowing:

- Before the first release there are no tags, so nothing is released, nothing can be broken, and the check no-ops.
- After a release, an *intentional* break stays red until the version carrying it ships. That is accurate rather than annoying — the schema really does break what is published — but it does mean a deliberate break and its release want to be close together.

On a tag push the comparison is against the tag immediately preceding it, so the release itself would be checked. That is also the trap in [#14](https://github.com/metacensus/api/issues/14): a release carrying a deliberate break is measured against exactly the version it means to break, and fails its own gate, while the Go module publishes from the tag regardless.

## Releasing

Tags are minted, never typed:

```bash
make release-patch          # or release-minor, release-major
make release VERSION=1.4.0  # or an explicit version
make latest                 # the current version tag
```

**There is no LICENSE yet.** This is a deliberate follow-up, not an oversight: a licence review is planned separately. Until then npm would publish the package as unlicensed-by-omission and the Go module carries the same ambiguity, which is worth settling before this is widely depended on.

`scripts/version.sh` validates semver, the target refuses a tag that already exists, and it prompts before pushing — a release cannot be withdrawn (npm unpublish is limited to 72 hours and the Go module proxy is an immutable cache, so deleting the tag does not unpublish the version). Pass `YES=1` to skip the prompt; without a TTY it refuses unless you do. Pushing the tag is the whole release: `.github/workflows/release.yml` runs the full CI suite first and only then publishes npm, and the Go module needs nothing but the tag for proxy.golang.org to serve it.

## JSON is the wire

Protobuf binary is not used, not supported and not a fallback. Protobuf is here for the schema, the code generation and the breaking-change detection.

- **Marshal with `protojson`, never `encoding/json`.** `go/` exports `Marshal`, `Unmarshal` and `MarshalOptions`. Under `encoding/json` the generated types produce enums as integers, timestamps as `{"seconds":…,"nanos":…}` and 64-bit integers as unquoted numbers.
- **Field names are `snake_case` in proto, `lowerCamelCase` on the wire.** protojson converts; there are no `json_name` overrides.
- **Enum values are PascalCase** and nested in the message that owns them, because protojson serialises an enum as its value name. Zero values are `Unspecified`.
- **All ids are strings.** demo mints integer serials, infra prefixed UUIDs.
- **Presence is expressed only through message-typed fields.** `EmitDefaultValues` plus ts-proto's `useOptionals=messages` makes scalars, enums and repeated fields always present, and message fields `?: T | undefined`. A proto3 `optional` scalar is a third case the two sides would disagree about; use `google.protobuf.Int32Value` and friends instead.

Agreement between the JSON Go emits and the TypeScript generated from the same `.proto` is not checked. That needs real documents from a backend actually serving the contract, and nothing serves it yet.

## Routes

Each resource file declares its own routes: `topic.proto` has `TopicRoutes`, `prop.proto` has `PropRoutes`, and so on across all six. They declare 19 routes between them.

**The prefix is part of the contract, and it is generated too.** `routes.Prefix` in Go and `apiPrefix` in TypeScript both carry `/metacensus/api/v1`, emitted by `cmd/routegen` from the single constant it holds. Every `path` in the manifest is relative to it, so join the two to get what a client requests. It had been written out by hand in every repository that needed it, with nothing making the copies agree; that is the reason it is generated here rather than left to each consumer.

It is not expressed in the `.proto`: `google.api.http` carries a path per route and protobuf has no string constant, so putting it there would mean a custom `FileOptions` extension and a non-resource `.proto` inside a schema whose tests assert every file is a resource. `TestPrefix` pins the invariant instead — the prefix is absolute, has no trailing slash, and no route path already contains it.

**To read the whole route table at once, read the generated manifest** — `go/routes` or `ts/src/route-manifest.ts`. `params`, `query` and `body` between them account for every field of the request message, so **a request message models the whole request**, not only its body: `PropCreateRequest` carries `topic_id` although `topic_id` never travels in a body.

**They are a route declaration, not a gRPC commitment.** Nothing generates or serves gRPC: no `protoc-gen-go-grpc`, no grpc-gateway, no Connect. ts-proto is given `outputServices=none`, without which it emits service interfaces of `Promise`-returning methods.

**Every route conforms to the conventions.** It did not always: three routes on `Paper` broke them — a read over POST, and `create` and `lookup` as verbs in the path — and `Paper` has since left the contract, because those routes could not be fixed without first settling whether a paper is one resource or two ([#8](https://github.com/metacensus/api/issues/8)). `TestNonConformingRoutes` still runs, pinning the set of deliberate exceptions at empty, so a route that starts breaking a convention fails the build.

## What the tests check

`go test ./...` reads the compiled descriptors, so every check is a property of the schema: JSON name and enum casing, `Unspecified` zero values, string ids, no proto3 `optional` scalars, `{items}` on every list, no pagination fields, no message field typed from another resource's file, and a route manifest that covers every rpc with no two routes sharing a method and path.

`ts/scripts/check-no-runtime.mjs` asserts the TypeScript is genuinely types: empty `dependencies`, no value imports under `src/`.
