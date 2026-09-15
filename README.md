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
ts/              generated TypeScript interfaces, the npm package
ts/src/client.ts generated typed client, over a caller-supplied transport
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

`cmd/routegen` walks the same descriptors that build the manifest and, per service, writes to `go/server/routes_gen.go`:

- a handler interface, one method per rpc — `GetTopic(context.Context, *v1.TopicGetRequest) (*v1.Topic, error)` — carrying path, query and body fields already bound;
- `Unimplemented<Service>`, whose every method answers 501, so a service can be implemented one route at a time by embedding it;
- `Register<Service>(mux Mux, rt *Runtime, impl <Service>)`, which binds each route's request and dispatches to `impl`.

`Mux` is one method, `Method(method, pattern string, h http.Handler)` — deliberately the shape `chi.Router` already has under that exact name, so a `chi.Router` satisfies it with no adapter and no `go.mod` dependency on chi; a bare `*http.ServeMux` needs the one-line `server.StdMux{ServeMux: mux}` wrapper, because it has no method-specific registration call of its own. `go/server/mux_test.go` proves both shapes against a chi-signature fake, without importing chi.

The runtime beside the generated file, `go/server/runtime.go`, is hand-written and owns what does not belong in a route-by-route rendering: a request body is capped at `Runtime.MaxBodyBytes` (`net/http.MaxBytesReader`, default 1 MiB) and decoded with the contract's own `UnmarshalOptions` — protojson, unknown fields rejected — never `encoding/json`; a response is written with the contract's `Marshal`. Errors are `{"error": "<message>", "code": "<code>"}`, because the contract itself declares no error message and the SPA's existing error handling already reads `.error || .message`; a handler chooses the status by returning `*server.Error`, and anything else becomes an opaque 500 that never leaks the underlying error text. `Runtime.VerifyBody(r, raw)`, if set, runs on the exact received body octets, between reading the body and decoding it — the seam for an `X-Signature` check, which must verify what was actually sent rather than a re-encoding, since protojson's own output is not byte-stable.

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
server.RegisterTopicRoutes(server.StdMux{ServeMux: mux}, &server.Runtime{}, topics{})
```

or, mounted on a `chi.Router` directly, with no wrapper:

```go
r := chi.NewRouter()
server.RegisterTopicRoutes(r, &server.Runtime{VerifyBody: verifyXSignature}, topics{})
```

## The generated client

The same walk writes `ts/src/client.ts`: a single `Client` class, one method per rpc, typed against ts-proto's generated types (`import type` only, so it costs nothing at runtime). Each method builds the path from the request's path fields (percent-encoded per segment), the query string from its query fields, serialises the body exactly once, and hands `{method, path, body?}` to a caller-supplied `Transport`. A 2xx response is `JSON.parse`d and cast to the response type — with `onlyTypes`, there is no runtime schema to validate a response against; a non-2xx throws `ApiError`, carrying the status and the raw response text unparsed, since the contract defines no error message.

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

A request tree containing a `bytes` field is refused at generation time: `JSON.stringify` renders a ts-proto `Uint8Array` as `{"0":1,"1":2}`, not the base64 string protojson expects, and there is no such field in the contract today.

## What the tests check

`go test ./...` reads the compiled descriptors, so every check is a property of the schema: JSON name and enum casing, `Unspecified` zero values, string ids, no proto3 `optional` scalars, `{items}` on every list, no pagination fields, no message field typed from another resource's file, and a route manifest that covers every rpc with no two routes sharing a method and path. `go/cmd/routegen/naming_test.go` independently reconstructs a `CodeGeneratorRequest` and cross-checks every proto→Go field mapping the server renderer reads off `protobuf:"...,name=..."` struct tags against `compiler/protogen`, the public package `protoc-gen-go` itself is built on — the naming rule that produces those tags is in an internal, unimportable package, so this is read off the generated code rather than re-derived. `go/server/*_test.go` exercises the runtime: path/body precedence, the body size cap, the `X-Signature` seam seeing raw octets, the error model, and that every manifest route is actually served.

`ts/scripts/check-no-runtime.mjs` asserts the package still declares no runtime: empty `dependencies`, no value imports under `src/`. It does not mean the package contains no executable code any more — `client.ts` does — only that nothing under `src/` reaches for a dependency or a sibling module's runtime value; `npm test` (`ts/test/*.test.mjs`, plain `node --test` against the built client, no test-runner dependency) is what exercises that code.
