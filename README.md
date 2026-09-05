# The MetaCensus API contract, in Protocol Buffers

Every request and response under `/metacensus/api/v1` and `/metacensus/public`, defined once in `.proto` and generated into Go and TypeScript.

This repository was split out of [`metacensus/ui`](https://github.com/metacensus/ui), where it lived as `contract/`. The 16 commits that built it came across intact — they are the derivation argument for why each field is in or out, and `git log` is the place to read it.

**Nothing consumes this yet**, and nothing is published, so nothing can. infra, demo and the SPA each still carry their own hand-maintained types.

The four issues in ui that tracked this work — adoption, what the contract deliberately leaves out, cross-language wire agreement, and the route conventions — were all closed as not-planned when the contract moved out of that repository. Nothing here replaces them yet, so the open questions recorded below are open in the plain sense: written down, not tracked.

## The two surfaces

Two proto packages live here, because two surfaces do, and they differ in almost
every property a contract encodes:

|  | `metacensus.v1` | `metacensus.public.v1` |
| --- | --- | --- |
| prefix | `/metacensus/api/v1` | `/metacensus/public` |
| caller | an authenticated session | anyone on the internet |
| implementers | two, with different persistence | one, `metacensus/service-public-api` |
| compatibility owed | none yet, by stated stance | to whatever is deployed |
| breaking baseline | the latest release tag | `origin/main` |
| threat model | authenticated abuse | spam, floods, forged origins |
| error body | not in the contract | not in the contract |
| success body | the bare resource | the bare message, plus `ok` |

Every row is a difference the two surfaces genuinely have, rather than a
difference in how they were written down. Where a difference turned out to be
only the latter, the public surface follows `metacensus.v1`; see "Errors on the
two surfaces".

They are separate packages rather than a corner of one, and a schema test
(`TestNoMessageFieldCrossesResourceFiles`) refuses a field typed across the
boundary. That is not tidiness: the compatibility policies below differ, and a
message shared between the two would put the laxer policy in charge of the
stricter one's wire shape.

**One repository, though, and one of each generated artifact.** The SPA calls
`POST /metacensus/public/partner` and `POST /metacensus/api/v1/login` from one
build on one origin, and it is the only party that sees every surface — which is
the same argument that put the authenticated contract here. It takes one Go
module and one npm package. What it does not have to take is one *namespace*:
Go already separates them, since `go/metacensus/public/v1` is a package of its
own that pulls nothing else in, and npm separates them with a subpath export.

## Errors on the two surfaces

**Neither surface types its failures.** On both, the HTTP status is the
machine-readable signal, and the body around it is not in the contract.

The public surface briefly had a `Failure` message — `{ok: false, error}` —
because `metacensus/service-public-api` really does send one. It was dropped to
match `metacensus.v1`, which had `Error {string error}` and
[removed it deliberately](https://github.com/metacensus/api/commit/da9db64) on
the argument that a single free-text string buys a client nothing it can act on:
the status already carries the category, and the version that earns its place is
`{code, error}`, which no implementation emits. That argument does not stop being
true because the surface is public. `error` on the public surface is one sentence
from a fixed vocabulary, never formatted from an error value, and its own comment
said not to branch on it — so typing it would have bought a client the same
nothing, at the cost of two answers to one question.

What the public surface does keep is **prose**, in `public/v1/common.proto`: the
statuses it answers with, on every route and on paths no route claims.
`google.api.http` cannot express responses, so that list has nowhere else to
live, and a browser client has to code against it. Documenting a status
vocabulary and typing a body are different commitments; only the second was the
divergence.

**The trigger for typing failures is an implementation emitting a `code`** — a
stable, machine-readable discriminator the status does not already carry. When
one does, both surfaces should gain the same message at the same time. Two notes
so that reinstatement does not re-diverge:

- **Call it `Failure`, not `Error`.** `Error` shadows the TypeScript global, so
  `import type { Error }` breaks `throw new Error(...)` in the importing module.
- **`ok` is not part of it.** See below.

### The one shape that still differs

`PartnerReceipt` carries `ok: true`; `metacensus.v1` returns bare resources. That
is not a design position — it is that `contract.UnmarshalOptions` rejects unknown
fields, and the service sends `ok` on the 200. A `PartnerReceipt` without it
would fail to decode the real response, so removing it is a service change
first, and a contract change second.


## Versioning the public surface

The two questions people conflate here are the release tag and the wire version.
They are not the same thing and they got different answers.

**The wire version is independent.** `metacensus.public.v1` carries its own `v1`,
so the public schema can reach v2 while the authenticated one stays at v1, or the
reverse. Nothing couples them.

**The release tag is shared.** One repository, one `v*` tag, one npm version, one
Go module version. Splitting the Go side would mean a `go.mod` under a
subdirectory, which is versioned by `go/v1.2.3` tags — the exact trap the
"Releasing" section below exists to avoid, paid twice. A shared tag says nothing
about the wire; it says these files were released together.

**The compatibility policy is independent, and it is the part that matters.**
`AGENTS.md` says no backwards compatibility is owed yet. That is true of the
authenticated API, whose consumers are two backends and one SPA that ship
together. It is *not* true of the public surface, and not as a matter of taste:

- its callers are browsers running whatever build of the SPA they loaded, which
  nobody can redeploy, plus whatever a CDN is still serving
- there is no auth handshake, no version negotiation and no client registry, so
  there is no way to find out who would break and no way to tell them
- a rolling deploy, where the old SPA is served for another few minutes, is on
  its own enough to break submissions during the window

So `make breaking` and `make breaking-public` ask different questions. The first
asks whether the schema breaks **what was released**, comparing against the
latest tag, because that is what a module consumer holds. The second asks whether
it breaks **what is deployed**, comparing against `origin/main`, because that is
what a browser talks to. Same tool, two baselines, two populations of caller.

One honest limit: the *rule set* is `FILE` for both, because `FILE` is already
buf's strictest category — there is nothing stricter to switch on. The extra
things that are breaking on this surface and invisible to any schema differ — a
narrowed length cap, a newly-required field, a value that quietly changes meaning
— are held by review and by this paragraph. A config flag that claimed to catch
them would be worse than saying plainly that none does.

**The path carries no version segment**, unlike `/metacensus/api/v1`. Form intake
commits to additive evolution instead: values get added to `Interest`, never
removed or renumbered. If read-only public data ever arrives — the category
[service-public-api#2](https://github.com/metacensus/service-public-api/issues/2)
deliberately leaves open — it should get its own versioned prefix rather than
retrofitting one here. The machinery is ready for that and does not assume it:
the proto package is already `metacensus.public.v1`, and a prefix is one entry in
`cmd/routegen`'s table. `TestPrefix` will fail the day a nested prefix appears,
which is the point — the reverse proxy routes by longest prefix match, and how it
tells `/metacensus/public` from `/metacensus/public/v1` is a decision to take
rather than to discover.

## Why `/healthz` is not in the contract

`metacensus/service-public-api` also answers `GET /healthz`, and it is not here.

The case for including it: it is part of the service's observable surface, it
returns a JSON body, and a contract that omits it is not a complete description
of what the service serves.

The case against, which won: **every route in this manifest is relative to a
prefix, and `/healthz` is relative to nothing.** It sits outside
`/metacensus/public` on purpose — the container runtime probes the service
directly rather than through the proxy, which is exactly why it must not move.
Putting it in would mean either an absolute path in a manifest whose paths are
all relative, or a third, empty prefix, which makes "prefix" stop meaning
anything. `TestEveryRouteHangsOffADeclaredPrefix` records that.

And nothing would consume it. Its caller is a container runtime reading a health
probe out of a compose file or a Kubernetes manifest in a third repository; it
does not import Go types or npm packages. The contract's unit is a route a client
codes against, and "the service's whole surface" is a different set. Note too
that `metacensus.v1` already declares a `HealthRoutes` at `/healthcheck`, which
*is* under a prefix and is the API's — adding a second, prefixless health shape
would leave two of them in one manifest meaning different things.

If the answer ever changes, the thing that changed is that something started
consuming it programmatically. That would be worth noticing.

## What the public contract does and does not mechanise

**The interest set is mechanised, and it is the reason this exists.**
`PartnerSubmission.Interest` is a proto enum. Adding a checkbox is adding a value;
both the service and the SPA regenerate from it, and `buf breaking` refuses a
removal. Before this, `PartnerInterests` in the service had to equal
`partnerInterests` in the SPA, asserted by a test that needed both trees checked
out at once — a test the repository split destroyed. Getting it wrong now fails
in CI; getting it wrong then 400'd every submission carrying the new checkbox, at
runtime, for users.

**The display wording is deliberately not mechanised.** The enum carries the set;
the label ("Fund the work") stays in the SPA as copy. Putting labels in the schema
would make a copy edit into a schema change, a regeneration, a release and a
service deploy. The two also fail differently — a stale label renders an odd
string in a Slack message, a stale set rejects every submission — and only the
second is worth a build failure. The set is a contract; the wording is copy.

**The validation limits are documented, not enforced.** `name` ≤ 120, `email` ≤
254, `message` ≤ 2000, body ≤ 16 KiB. They are comments on the fields rather than
`protovalidate` constraints, because the constraints could not reach the half that
needs them: ts-proto runs with `onlyTypes=true` and drops field options entirely,
so an annotation would generate the server's checks and nothing for the form,
while `protovalidate-es` would put a runtime dependency into a package whose whole
claim is that it has none. A mismatched cap degrades a form; a mismatched interest
set breaks it. Only one was worth new machinery.

**No server stubs and no client.** Same depth as the authenticated surface, for
the same reason: the rpcs are a route declaration, not a gRPC commitment, and the
SPA calls a relative path with `fetch`.

## Layout

```
proto/           .proto sources and buf config — the definition
go/              generated Go, the wire encoder, the manifest generator, tests
go/internal/     helpers shared by the generator and the tests, unexported
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
go get github.com/metacensus/api
npm install @metacensus/api               # types only, zero dependencies
```

```go
import v1 "github.com/metacensus/api/go/metacensus/v1"              // authenticated
import publicv1 "github.com/metacensus/api/go/metacensus/public/v1"  // public
import "github.com/metacensus/api/go/routes"                         // both, one table
```

```ts
import type { Topic } from "@metacensus/api";                  // authenticated
import type { PartnerSubmission } from "@metacensus/api/public"; // public
```

**One package, two entry points.** The SPA calls both surfaces from one build and
takes one dependency; a consumer of only the public surface — a third party
integrating against `/metacensus/public/*`, who never had a session — imports
`@metacensus/api/public` and does not acquire the authenticated types. Go needed
no equivalent: its packages were already separate. The route manifest is exported
from both, deliberately, because "every MetaCensus route on one screen" is the
reason the contract lives in one repository, and the manifest is string literals
rather than a type surface.

Neither is published yet. See "Releasing", below.

## What "breaking" is measured against

There are two comparisons, over two baselines. This section is about the
authenticated one; see "Versioning the public surface" above for why
`make breaking-public` compares `metacensus.public.v1` against `origin/main`
instead, and why that is a fact about callers rather than a preference.

`buf breaking` compares the schema against **the latest release tag**, not against the PR's merge base. The published contract is what a breaking change breaks; unreleased `main` is not a promise to anyone. That makes it a question about state — does the schema as it stands break what consumers have — rather than about a diff, so the answer does not depend on which base a PR happens to have.

Two consequences worth knowing:

- Before the first release there are no tags, so nothing is released, nothing can be broken, and the check no-ops.
- After a release, an *intentional* break stays red until the version carrying it ships. That is accurate rather than annoying — the schema really does break what is published — but it does mean a deliberate break and its release want to be close together.

On a tag push the comparison is against the tag immediately preceding it, so the release itself is checked.

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

Each resource file declares its own routes: `topic.proto` has `TopicRoutes`, `prop.proto` has `PropRoutes`, and so on across all seven. They declare 21 routes between them. The public package adds an eighth service, `PartnerRoutes` in `partner.proto`, for 22 routes in one manifest.

**The prefixes are part of the contract, and they are generated too.** `routes.Prefix` / `apiPrefix` carries `/metacensus/api/v1` and `routes.PublicPrefix` / `publicPrefix` carries `/metacensus/public`, emitted by `cmd/routegen` from the one table it holds. Every `path` in the manifest is relative to one of them, so join the two to get what a client requests. They had been written out by hand in every repository that needed them, with nothing making the copies agree; that is the reason they are generated here rather than left to each consumer.

**Each route carries its own `prefix`.** With one prefix a consumer could hard-code it; with two, a consumer holding a `Route` has no other way to know which to join, and guessing from the service name is exactly the hand-mirroring this repository exists to stop. That is also what keeps one manifest workable instead of one per surface — and one is the point, since the client that sees every surface is why the contract lives here.

`cmd/routegen` fails on a registered `metacensus.*` package its table does not name, rather than generating a manifest silently missing that package's routes.

The prefixes are not expressed in the `.proto`: `google.api.http` carries a path per route and protobuf has no string constant, so putting them there would mean a custom `FileOptions` extension and a non-resource `.proto` inside a schema whose tests assert every file is a resource. `TestPrefix` pins the invariants instead — each prefix is absolute, has no trailing slash, is one a route actually hangs off, and no route path already contains its own prefix. It also refuses a prefix nested inside another, because the reverse proxy routes by longest prefix match and a nested pair moves that decision into a config file in a third repository.

**To read the whole route table at once, read the generated manifest** — `go/routes` or `ts/src/route-manifest.ts`. `params`, `query` and `body` between them account for every field of the request message, so **a request message models the whole request**, not only its body: `PropCreateRequest` carries `topic_id` although `topic_id` never travels in a body.

**They are a route declaration, not a gRPC commitment.** Nothing generates or serves gRPC: no `protoc-gen-go-grpc`, no grpc-gateway, no Connect. ts-proto is given `outputServices=none`, without which it emits service interfaces of `Promise`-returning methods.

**Every route conforms to the conventions.** It did not always: three routes on `Paper` broke them — a read over POST, and `create` and `lookup` as verbs in the path — and `Paper` has since left the contract, because those routes could not be fixed without first settling whether a paper is one resource or two ([#8](https://github.com/metacensus/api/issues/8)). `TestNonConformingRoutes` still runs, pinning the set of deliberate exceptions at empty, so a route that starts breaking a convention fails the build.

## What the tests check

`go test ./...` reads the compiled descriptors, so every check is a property of the schema: JSON name and enum casing, `Unspecified` zero values, string ids, no proto3 `optional` scalars, `{items}` on every list, no pagination fields, no message field typed from another resource's file, and a route manifest that covers every rpc with no two routes sharing a method and full path.

**They run over every package, and nothing can drop out of that set quietly.** Each check iterates `contractPackages`; a package missing from it would not be exempt but invisible, and the suite would pass over a schema smaller than the one that ships. Two checks close that:

- `TestEveryPackageIsGoverned` reads the `.proto` tree — not `protoregistry`, which holds only what the test binary imported — and fails on any package on disk that `contractPackages` does not name. `cmd/routegen` checks its own prefix table the same way, so a new package fails `make gen` before it fails anything else.
- `forEachContractFile` fails **per package** when one registers no files, so a typo or a missing blank import in `go/registered_test.go` cannot leave a package vacuously green while the other keeps the count non-zero.

Route identity is the **full** path, prefix included: a public `/partner` and an authenticated `/partner` are different URLs and must not be reported as a clash, while two routes that genuinely resolve to one URL must be.

Two scripts guard the npm package, both run by `npm run check`:

- `check-no-runtime.mjs` asserts it is genuinely types: empty `dependencies`, and no value imports in anything that ships — the generated files under `src/` and both entry points.
- `check-entry-points.mjs` asserts every generated module is exported by exactly one entry point. `index.ts` and `public.ts` list their exports by hand, so without it a new `.proto` file generates a module that ships in `dist/` and that no consumer can import — an absence, not a failure, and the same shape of hole as a proto package missing from `contractPackages`. Exactly one, because two entry points exist so that a consumer of one surface does not acquire the other.
