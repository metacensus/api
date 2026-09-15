# The MetaCensus API contract, in Protocol Buffers

Every request and response under `/metacensus/api/v1`, defined once in `.proto` and generated into Go and TypeScript.

This repository was split out of [`metacensus/ui`](https://github.com/metacensus/ui), where it lived as `contract/`. The 16 commits that built it came across intact — they are the derivation argument for why each field is in or out, and `git log` is the place to read it.

**Nothing consumes this yet.** infra, demo and the SPA each still carry their own hand-maintained types. One version, `v0.1.0`, was tagged to exercise the release path rather than to promise anything, and what reached the registries is uneven: the Go module is live on proxy.golang.org and permanently so, while npm carries only `@metacensus/api@0.0.0`, the placeholder version, as `latest` — `0.1.0` never got there.

`cmd/routegen` also generates a Go server (`go/server`) and a TypeScript client (`ts/src/client.ts`) from the same route table, so that adopting this contract does not mean hand-writing the binding between it and an HTTP handler on one side or a `fetch` call on the other. Neither has a consumer wired up yet, and neither touches persistence — see "The generated server" and "The generated client" below.

The four issues in ui that tracked this work — adoption, what the contract deliberately leaves out, cross-language wire agreement, and the route conventions — were all closed as not-planned when the contract moved out of that repository. Nothing here replaces them yet, so the open questions recorded below are open in the plain sense: written down, not tracked.

## Layout

```
proto/           .proto sources and buf config — the definition
go/              generated Go, the wire encoder, the manifest generator, tests
go/server/       generated handler interfaces + registration, and the hand-written runtime beside them
go/server/chitest/ the generated routes run against real chi, a nested module of its own
ts/              generated TypeScript interfaces, the npm package
ts/src/client.ts generated typed client, over a caller-supplied transport
internal/tools/  the pinned code generators, a module of its own
scripts/         version.sh, which `make release` uses to mint tags
go.mod           the published Go module, rooted here
```

**The Go packages sit under `go/`, so imports read `github.com/metacensus/api/go/metacensus/v1`.** The alternative — hoisting `metacensus/` to the root for `github.com/metacensus/api/metacensus/v1` — reads better at the call site, and `go.mod` at the root already breaks the `proto/ go/ ts/` symmetry. The reason it stays is that the root is shared with `ts/`, `proto/`, `internal/` and `scripts/`, and a generated `metacensus/` tree alongside them would be the only directory whose name says nothing about which language reads it. Both are defensible; this one is a one-way door once a tag exists, because the import path is the module's public surface.

**`go.mod` is at the repository root, not in `go/`, and must stay there.** A module whose `go.mod` sits in a subdirectory `go/` is versioned by tags of the form `go/v1.2.3`. A plain `v1.2.3` tag would then publish nothing and `go get ...@v1.2.3` would fail with "no matching versions", while a `go/v1.2.3` tag would not match the release workflow's tag filter, so nothing would run and no failure would be reported. Rooted here, one plain semver tag does every job.

## Working on it

`make` on its own lists every target. From a fresh clone nothing has to be installed first — `gen` and `check` build the pinned generators and install the npm toolchain as prerequisites.

```bash
make gen     # regenerate Go, TypeScript, the route manifest, the server and the client
make check   # everything CI runs, bar the freshness diff
make hooks   # optional: lint, format and freshness checks on commit
```

**Generated code is committed, and `make gen` is the only way to write it.** CI regenerates and fails on any diff, and on any file generation produced that is not committed. The paths `gen` writes are named once, in the Makefile's `GENERATED`; `make clean` removes exactly those and `make generated-paths` prints them, which is how the pre-commit hook asks git the same question CI asks. `routegen` refuses to run outside the repository root: its output paths are relative, so a wrong working directory would quietly write the manifests elsewhere and leave the committed ones stale — which the freshness check cannot see, because nothing in the tree changed.

`make hooks` is opt-in and does nothing unless a `.proto` or a file under `go/cmd/routegen/` is staged, in which case it lints, format-checks and regenerates, and refuses the commit if regeneration produced anything unstaged. Warm, that is about a second; it is the failure this repository actually has.

### Where each version is pinned, and where it is read

| Thing | Pinned in | Read by |
|---|---|---|
| Go, for the module and for building the generators | `go.mod` | CI's `setup-go` (`go-version-file`), and the Makefile, which derives `GOTOOLCHAIN_PIN` from the same line with `awk` rather than repeating it |
| `buf`, `protoc-gen-go` | `internal/tools/go.mod` `tool` directives | `make tools`, which rebuilds whenever that module's `go.mod` or `go.sum` moves |
| `ts-proto`, `typescript` | `ts/package.json` + `ts/package-lock.json` | `npm ci`, which `make gen` runs as a prerequisite when the lockfile is newer than the installed plugin |
| Node | `ts/.nvmrc` (and a floor in `engines`) | `nvm use`, and CI's `setup-node` (`node-version-file`) |
| `chi`, for the conformance test only | `go/server/chitest/go.mod` | `make test`; never the published module |

`buf` and `protoc-gen-go` live in **`internal/tools`, a module of its own**. Under Go 1.24 a `tool` directive is a real module requirement: left in the published module they added 90 indirect requirements — the Docker CLI, quic-go, the whole buf server graph — to everything that imported the contract. The published module requires two things. `go/server/chitest` is a second such module, for chi.

`make tools` builds the generators with `GOWORK=off` and the pinned `GOTOOLCHAIN`. Both are load-bearing, and both are lessons from [metacensus/infra#52](https://github.com/metacensus/infra/pull/52): a `go.work` above the checkout resolves tool versions against the union of its members and silently lifts the pins, and the `go` directive is a floor rather than a ceiling, so an unpinned toolchain builds the plugins against whatever stdlib the developer has. `protoc-gen-go` stamps its own version into every `.pb.go`, so either one surfaces as generated-code drift in a pull request that never touched a `.proto`. The binaries land in `bin/`, and **buf runs from the repository root**, so every relative path in `buf.gen.yaml` and in `cmd/routegen` is relative to the root.

## Consuming it

```bash
go get github.com/metacensus/api          # import github.com/metacensus/api/go/metacensus/v1
npm install @metacensus/api               # types, a client, zero runtime dependencies
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

## The generated server

`cmd/routegen` walks the same descriptors that build the manifest and, per service, writes to `go/server/routes_gen.go`: a handler interface with one method per rpc — `GetTopic(context.Context, *v1.TopicGetRequest) (*v1.Topic, error)`, path, query and body already bound — an `Unimplemented<Service>` answering 501, and a `Register<Service>(mux Mux, rt *Runtime, impl <Service>)` that binds and dispatches. The hand-written half beside it is organized by concern, one file each: `binding.go` (request binding), `errors.go` (the error model), `mux.go` (the mux and path-value seam), `response.go` (response encoding), `runtime.go` (the `Runtime` struct and its options).

**`go/server`'s own comments are the account of itself**: `runtime.go`'s package comment says what the package is, `Mux`'s doc comment says what it leaves to the router and why, and every other constraint — `Unimplemented<Service>`, the error envelope, `Prefix` — is a comment beside the declaration it constrains. It is not repeated here. Two facts belong in a README because they decide how you mount:

- **`Runtime.Prefix` is literal, and `""` means no prefix.** A router already mounted at the contract's prefix — chi's `Route`, `http.StripPrefix` — wants the zero value. A router at the origin root wants `routes.Prefix`.
- **Any `Mux` that is not a `StdMux` must set `PathValue`**, and a `chi.Router` wants `server.EscapedPathValue`. `net/http`'s `ServeMux` percent-decodes a path segment and chi does not, and the generated client percent-encodes every one of them, so either wrong answer binds a wrong id and answers 200. `Register<Service>` therefore refuses to guess: it panics at registration until the field is set. `go/server/chitest` pins both directions against real chi v5.1.0 in a module of its own, so chi stays out of the published `go.mod`.

**Deliberately excluded:** persistence or a store of any kind. `Unimplemented<Service>` is the whole default implementation; wiring a real one to a database, a cache, or another service is entirely the implementer's, and nothing here assumes a shape for it.

An implementer writes:

```go
type topics struct{ server.UnimplementedTopicRoutes }

func (topics) GetTopic(ctx context.Context, req *v1.TopicGetRequest) (*v1.Topic, error) {
	t, err := store.Lookup(ctx, req.TopicId) // your persistence, your choice
	if err != nil {
		return nil, server.Errorf(http.StatusNotFound, "not_found", "no such topic")
	}
	return t, nil
}

mux := http.NewServeMux()
server.RegisterTopicRoutes(
	server.StdMux{ServeMux: mux},
	&server.Runtime{Prefix: routes.Prefix},
	topics{},
)
```

or on a `chi.Router`, with no wrapper, inside whatever it is already mounted under:

```go
r.Route(routes.Prefix, func(v1 chi.Router) {
	v1.Group(func(authed chi.Router) {
		authed.Use(jwtMiddleware)
		server.RegisterTopicRoutes(authed, &server.Runtime{
			PathValue:  server.EscapedPathValue, // chi leaves segments escaped
			VerifyBody: verifyXSignature,
		}, topics{})
	})
})
```

## The generated client

The same walk writes `ts/src/client.ts`: a single `Client` class, one method per rpc, typed against ts-proto's generated types (`import type` only, so it costs nothing at runtime). Each method builds the path from the request's path fields (percent-encoded per segment), the query string from its query fields, serialises the body exactly once, and hands `{method, path, body?}` to a caller-supplied `Transport`. A 2xx response is `JSON.parse`d and cast to the response type — with `onlyTypes`, there is no runtime schema to validate a response against; a non-2xx throws `ApiError`, carrying the status, the path requested and the raw response text unparsed. Parse that text defensively: a 404 or 405 comes from the router before any handler runs, so it is often not JSON at all.

**The `Transport` is where the caller's own concerns live**, deliberately: auth headers, an `X-Signature` computed over `req.body` — the exact octets sent, which is why the client serialises the body once and hands over the string rather than an object — and status policy beyond "2xx parses, the rest throws" (the SPA's 401-clears-the-token-and-redirects behavior, or retries). The client does not call `fetch` itself and carries no cache of its own.

A caller writes:

```ts
import { Client, ApiError, type Transport } from "@metacensus/api";

const transport: Transport = async ({ method, path, body }) => {
  const headers = new Headers({ "Content-Type": "application/json" });
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (body !== undefined) headers.set("X-Signature", await sign(body)); // exactly the octets sent
  const r = await fetch(baseUrl + path, { method, headers, body });
  return { status: r.status, body: await r.text() };
};

const api = new Client(transport);
const topic = await api.getTopic({ topicId });
const created = await api.createTopic({ name, description: "" });
```

A `bytes` field anywhere in a request **or** response tree is refused at generation time: ts-proto types it `Uint8Array`, which `JSON.stringify` renders as `{"0":1,"1":2}` rather than the base64 protojson expects, and which `JSON.parse` cannot produce from the base64 that comes back. There is no such field in the contract today.

`useOptionals=messages` makes every scalar required, which pairs with `EmitDefaultValues` on the Go side: `api.createTopic({ name })` does not type-check, `{ name, description: "" }` does.

## What the tests check

`go test ./...` reads the compiled descriptors, so every check is a property of the schema: JSON name and enum casing, `Unspecified` zero values, string ids, no proto3 `optional` scalars, `{items}` on every list, no pagination fields, no message field typed from another resource's file, and a route manifest that covers every rpc with no two routes sharing a method and path. `go/cmd/routegen/internal/model/naming_test.go` independently reconstructs a `CodeGeneratorRequest` and cross-checks every proto→Go field mapping the server renderer reads off `protobuf:"...,name=..."` struct tags against `compiler/protogen`, the public package `protoc-gen-go` itself is built on — the naming rule that produces those tags is in an internal, unimportable package, so this is read off the generated code rather than re-derived. `go/server/*_test.go` exercises the runtime: path/body precedence, the body size cap, the `X-Signature` seam seeing raw octets, the error model, unknown query parameters rejected on every route, and that every manifest route is actually served. `go/server/chitest` re-runs the routes on real chi, which is where every claim `server.Mux`'s doc comment makes about router-owned behaviour is checked rather than asserted.

`ts/scripts/check-no-runtime.mjs` asserts the package ships no runtime: empty `dependencies`, no value imports under `src/`. `npm test` (`ts/test/*.test.mjs`, plain `node --test` against the built `dist/`, no test-runner dependency) exercises the client, including driving every method the manifest declares and comparing what reaches the transport against the route it says it is.

## Open questions

Written down, not tracked — the four ui issues that held this work were closed as not-planned when the contract moved here, and nothing has replaced them. Each of these is a decision nobody has standing to take yet because the consumer that would settle it does not exist.

- **Should the contract declare an `Error` message?** Today the server emits an ad hoc `{"error","code"}` envelope and the client hands back the response text unparsed. A real message (an error-code enum, field-level validation errors) is the obvious next step and is exactly the kind of schema that goes wrong when it is invented before a second consumer exists. Refs [#3](https://github.com/metacensus/api/issues/3).
- **Where does `Runtime.VerifyBody` sit relative to infra's JWT middleware?** infra has no `X-Signature` verification today, so this seam has no existing behaviour to match — whether it composes with the bearer check or replaces it for these routes is open, as is whether `Register<Service>` mounts at the root or under a sub-`Route`. Refs [#3](https://github.com/metacensus/api/issues/3).
- **Should the generated client emit a query parameter holding its zero value?** No route declares a query field, so both answers are untested against a real caller, and `EmitDefaultValues` on the response side argues one way while URL length argues the other. Refs [#4](https://github.com/metacensus/api/issues/4).
- **Should the npm package have a second entry point** (`@metacensus/api/client`) so a bundler can tree-shake the client away from the types, rather than the single root export used here? Refs [#4](https://github.com/metacensus/api/issues/4).
- **Cross-language wire agreement is still unchecked.** `ts/test` drives the client against the manifest and `go/server` against itself; nothing drives the generated client against the generated server. This was [metacensus/ui#49](https://github.com/metacensus/ui/issues/49), and generating both halves widens what it covers without narrowing it.
