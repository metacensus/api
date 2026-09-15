# The MetaCensus API contract, in Protocol Buffers

Every request and response under `/metacensus/api/v1` and `/metacensus/public`, defined once in `.proto` and generated into Go and TypeScript.

This repository was split out of [`metacensus/ui`](https://github.com/metacensus/ui), where it lived as `contract/`. The 16 commits that built it came across intact — they are the derivation argument for why each field is in or out, and `git log` is the place to read it.

**Nothing consumes this yet.** infra, demo and the SPA each still carry their own hand-maintained types. One version was tagged to exercise the release path rather than to promise anything (`make latest` prints it), and what reached the registries is uneven: the Go module is live on proxy.golang.org and permanently so, while npm carries only the placeholder version in `ts/package.json` as `latest` — the tagged one never got there.

`routegen` also generates a Go server (`go/server`) and a TypeScript client per surface (`ts/src/client.ts`) from the same route table, so that adopting this contract does not mean hand-writing the binding between it and an HTTP handler on one side or a `fetch` call on the other. Neither has a consumer wired up yet, and neither touches persistence — see "The generated server" and "The generated client" below.

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
The org's standing position is that no backwards compatibility is owed yet —
[infra's `AGENTS.md`](https://github.com/metacensus/infra/blob/main/AGENTS.md),
which is where it is written down; this repository has no `AGENTS.md` of its
own. That is true of the authenticated API, whose consumers are two backends
and one SPA that ship together. It is *not* true of the public surface, and not
as a matter of taste:

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
Only the second runs in CI today; see "What \"breaking\" is measured against"
below for why the first is off.

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
`routegen`'s `model.Packages`. `TestPrefix` will fail the day a nested prefix appears,
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

**The server and the client are generated, at the same depth as the
authenticated surface.** `routegen` renders every surface in `model.Packages`,
so the public routes get `PartnerRoutes`, `UnimplementedPartnerRoutes` and
`RegisterPartnerRoutes` in `go/server`, and a `PublicClient` in
`ts/src/client.ts`. Generating less for one surface than for the other is the
absence this contract is built to refuse — the route is in the manifest either
way, and a surface that is declared but not rendered is invisible rather than
exempt.

They are still route declarations rather than a gRPC commitment: the transport
is HTTP+JSON and nothing here serves gRPC. Two things are per surface rather
than shared, and both fall out of the prefix:

- **A `Runtime` each.** `Runtime.Prefix` is the path a service's routes hang
  off, so a process serving both mounts `RegisterPartnerRoutes` against a
  `Runtime` holding `routes.PublicPrefix` and the rest against one holding
  `routes.Prefix`. Everything else on it — the body cap, `PathValue` — can be
  the same value.
- **A transport each.** `PublicClient` sends no credentials: the `X-Signature`
  and bearer concerns that `Runtime.VerifyBody` and the authenticated
  transport exist for have no counterpart here, where the rejections are
  `Origin` and rate limit. What the two clients share rather than duplicate is
  in "The generated client", below.

Adopting any of this in `metacensus/service-public-api` or `metacensus/ui` is
separate work; nothing in either repository is touched here.

## Layout

**Two Go modules.** The contract, which consumers import, and the generator, which nobody does.

```
proto/            .proto sources and buf config — the definition, both surfaces
go/               the contract in Go: generated types, the route manifest,
                  the wire encoder, the server, and the schema tests
go/metacensus/v1/ the authenticated surface; go/metacensus/public/v1/ the public one
go/server/        generated handler interfaces + registration, and the
                  hand-written runtime beside them
ts/               the contract in TypeScript: generated types, the route
                  manifest, a client per surface — the npm package
ts/index.ts       the authenticated entry point; ts/public.ts the public one
internal/         protoscan, the .proto tree scan both modules check their
                  package lists against; unexported, imported by neither
                  consumers nor anything published
routegen/         module 2: the generator, its renderers and templates, the
                  pinned buf and protoc-gen-go, and the suites that exercise
                  what it emits
routegen/chitest/ the generated routes against a real chi.Router
routegen/wireserver/ the server ts/test/wire.test.mjs drives over HTTP
scripts/          version.sh, which `make release` uses to mint tags
go.mod            module 1, github.com/metacensus/api, rooted here
routegen/go.mod   module 2, github.com/metacensus/api/routegen
```

| | Holds | May require | Imported by |
|---|---|---|---|
| `github.com/metacensus/api` | the contract: types, manifest, wire encoder, server | exactly two things, and they are checked | consumers |
| `github.com/metacensus/api/routegen` | generation, and everything that tests generation | anything — chi, buf, protoc-gen-go | nobody, ever |

The split has one reason. A `tool` directive is a real module requirement and a test-only import is indistinguishable from a runtime one, so buf's ~90 transitive requirements and chi would both land in what every consumer of the contract resolves. Both are generation, so both live in the second module, and it is one module rather than two because that is one reason. `routegen` is `replace`d onto the working tree, so its tests run against what is in front of you rather than against a published version.

**The Go packages sit under `go/`, so imports read `github.com/metacensus/api/go/metacensus/v1`.** The alternative — hoisting `metacensus/` to the root for `github.com/metacensus/api/metacensus/v1` — reads better at the call site, and `go.mod` at the root already breaks the `proto/ go/ ts/` symmetry. The reason it stays is that the root is shared with `ts/`, `proto/`, `internal/` and `scripts/`, and a generated `metacensus/` tree alongside them would be the only directory whose name says nothing about which language reads it. Both are defensible; this one is a one-way door once a tag exists, because the import path is the module's public surface.

**`go.mod` is at the repository root, not in `go/`, and must stay there.** A module whose `go.mod` sits in a subdirectory `go/` is versioned by tags of the form `go/v1.2.3`. A plain `v1.2.3` tag would then publish nothing and `go get ...@v1.2.3` would fail with "no matching versions", while a `go/v1.2.3` tag would not match the release workflow's tag filter, so nothing would run and no failure would be reported. Rooted here, one plain semver tag does every job — and only one module is released, because `routegen` is deliberately unversioned: no `routegen/v*` tag is ever cut, so the release workflow needs no rule for it.

## Working on it

`make` on its own lists every target. From a fresh clone nothing has to be installed first — `gen` and `check` build the pinned generators and install the npm toolchain as prerequisites.

```bash
make gen     # regenerate Go, TypeScript, the route manifest, the server and the client
make check   # everything CI runs, bar the freshness diff
make hooks   # optional: lint, format and freshness checks on commit
```

**Generated code is committed, and `make gen` is the only way to write it.** CI regenerates and fails on any diff, and on any file generation produced that is not committed. The paths `gen` writes are named once, in the Makefile's `GENERATED`; `make clean` removes exactly those and `make generated-paths` prints them, which is how the pre-commit hook asks git the same question CI asks. `routegen` refuses to run outside the repository root: its output paths are relative, so a wrong working directory would quietly write the manifests elsewhere and leave the committed ones stale — which the freshness check cannot see, because nothing in the tree changed.

`make hooks` is opt-in and does nothing unless a `.proto` or a file under `routegen/` is staged, in which case it lints, format-checks and regenerates, and refuses the commit if regeneration produced anything unstaged. Warm, that is about a second; it is the failure this repository actually has.

### Where each version is pinned, and where it is read

| Thing | Pinned in | Read by |
|---|---|---|
| Go, for the module, for building the generators, and as the floor a consumer must meet | the `go` directive in `go.mod`; `routegen/go.mod` matches it | CI's `setup-go` (`go-version-file`), and the Makefile, which derives `GOTOOLCHAIN_PIN` from the same line with `awk` rather than repeating it |
| `buf`, `protoc-gen-go` | `routegen/go.mod` `tool` directives | `make tools`, which rebuilds whenever that module's `go.mod` or `go.sum` moves |
| `ts-proto`, `typescript` | `ts/package.json` + `ts/package-lock.json` | `npm ci`, which `make gen` runs as a prerequisite when the lockfile is newer than the installed plugin |
| Node | `ts/.nvmrc` (and a floor in `engines`) | `nvm use`, and CI's `setup-node` (`node-version-file`) |
| `chi`, for the conformance test only | `routegen/go.mod` | `make test`; never the contract's module |

`buf` and `protoc-gen-go` are `tool` dependencies of **`routegen`**, for the reason the layout section gives: left in the published module they added 90 indirect requirements — the Docker CLI, quic-go, the whole buf server graph — to everything that imported the contract. Do not `go mod tidy` that module. It moves the pins, and it cannot complete anyway: something in buf's graph reaches a version of grpc-gateway that wants a newer Go than the generators are pinned to. Add a requirement by hand.

`make gen` and `make test` both run the second module with `GOWORK=off` and the pinned `GOTOOLCHAIN`, derived from the `go` directive. Both are load-bearing, and both are lessons from [metacensus/infra#52](https://github.com/metacensus/infra/pull/52): a `go.work` above the checkout resolves tool versions against the union of its members and silently lifts the pins, and the `go` directive is a floor rather than a ceiling, so an unpinned toolchain builds the plugins against whatever stdlib the developer has. `protoc-gen-go` stamps its own version into every `.pb.go`, so either one surfaces as generated-code drift in a pull request that never touched a `.proto`. The binaries land in `bin/`, and **buf runs from the repository root**, so every relative path in `buf.gen.yaml` and in `routegen` is relative to the root. `routegen` finds the root by walking up for the contract's `go.mod`, so it writes the same four files whether it is run from the root or from its own module directory, and refuses to run outside the repository at all.

## Consuming it

```bash
go get github.com/metacensus/api          # import github.com/metacensus/api/go/...
npm install @metacensus/api               # types, a route manifest, a client per surface
```

```go
import v1 "github.com/metacensus/api/go/metacensus/v1"               // authenticated
import publicv1 "github.com/metacensus/api/go/metacensus/public/v1"  // public
import "github.com/metacensus/api/go/routes"                         // both, one table
```

```ts
import type { Topic } from "@metacensus/api";                    // authenticated
import type { PartnerSubmission } from "@metacensus/api/public"; // public
```

**One package, two entry points.** The SPA calls both surfaces from one build and takes one dependency; a consumer of only the public surface — a third party integrating against `/metacensus/public/*`, who never had a session — imports `@metacensus/api/public` and does not acquire the authenticated types. Go needed no equivalent: its packages were already separate. The route manifest is exported from both, deliberately, because "every MetaCensus route on one screen" is the reason the contract lives in one repository, and the manifest is string literals rather than a type surface.

**A consumer's Go must satisfy the `go` directive in the root `go.mod`**, which is a build error below it rather than a fallback. It is the one thing here that constrains another repository's toolchain, so a consumer still on an older Go has to move first. The directive is the language version the contract is developed and generated against; nothing in the generated types needs anything newer than the module system.

Neither is ready to depend on: `go get` resolves the tag that exists to test the release path, and `npm install` resolves `ts/package.json`'s placeholder rather than any released contract. See "Releasing", below.

## What "breaking" is measured against

There are two comparisons, over two baselines, and only one of them runs in CI. This section is about the authenticated one; see "Versioning the public surface" above for why `make breaking-public` compares `metacensus.public.v1` against `origin/main` instead, and why that is a fact about callers rather than a preference.

**CI does not enforce the authenticated check.** The step is commented out in `.github/workflows/ci.yml`, and `make breaking` is intact for running by hand. Nothing consumes this contract and the only tag, `v0.1.0`, exists to exercise the release path rather than to promise anything — so the check was measuring every branch against a throwaway baseline, failing on the removal of `paper.proto`, and taking the rest of the suite down with it before the tests ever ran. [#14](https://github.com/metacensus/api/issues/14) records the trigger for turning it back on and the release-workflow bug that has to be fixed alongside it.

`make breaking-public` is unaffected by that and stays enforcing in CI: its baseline is `origin/main` rather than a tag, so it never inherits a stale release's breakage, and the population it protects — browsers holding whatever build they loaded — never consumed a tag in the first place.

What the authenticated target is, by hand now and in CI again later:

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

Each resource file declares its own routes: `topic.proto` has `TopicRoutes`, `prop.proto` has `PropRoutes`, and the public package's `partner.proto` has `PartnerRoutes`. How many there are of each is not written here — `go/routes/manifest.go` is the table, and a count beside it is a copy that goes stale on the next `.proto` change without anything noticing.

**The prefixes are part of the contract, and they are generated too.** `routes.Prefix` / `apiPrefix` carries `/metacensus/api/v1` and `routes.PublicPrefix` / `publicPrefix` carries `/metacensus/public`, emitted by `routegen` from `model.Packages`, the one table it holds. Every `path` in the manifest is relative to one of them, so join the two to get what a client requests. They had been written out by hand in every repository that needed them, with nothing making the copies agree; that is the reason they are generated here rather than left to each consumer.

**Each route carries its own `prefix`.** With one prefix a consumer could hard-code it; with two, a consumer holding a `Route` has no other way to know which to join, and guessing from the service name is exactly the hand-mirroring this repository exists to stop. That is also what keeps one manifest workable instead of one per surface — and one is the point, since the client that sees every surface is why the contract lives here.

`routegen` fails on a `metacensus.*` package declared under `proto/` that `model.Packages` does not name, rather than generating a manifest, a server and a client silently missing that package's routes. It reads the `.proto` tree rather than `protoregistry`, which holds only what the generator imported — so a package nobody imported cannot be absent from the output *and* from the check that would have caught it.

The prefixes are not expressed in the `.proto`: `google.api.http` carries a path per route and protobuf has no string constant, so putting them there would mean a custom `FileOptions` extension and a non-resource `.proto` inside a schema whose tests assert every file is a resource. `TestPrefix` pins the invariants instead — each prefix is absolute, has no trailing slash, is one a route actually hangs off, and no route path already contains its own prefix. It also refuses a prefix nested inside another, because the reverse proxy routes by longest prefix match and a nested pair moves that decision into a config file in a third repository.

**To read the whole route table at once, read the generated manifest** — `go/routes` or `ts/src/route-manifest.ts`. `params`, `query` and `body` between them account for every field of the request message, so **a request message models the whole request**, not only its body: `PropCreateRequest` carries `topic_id` although `topic_id` never travels in a body.

**They are a route declaration, not a gRPC commitment.** Nothing generates or serves gRPC: no `protoc-gen-go-grpc`, no grpc-gateway, no Connect. ts-proto is given `outputServices=none`, without which it emits service interfaces of `Promise`-returning methods.

**Every route conforms to the conventions.** It did not always: three routes on `Paper` broke them — a read over POST, and `create` and `lookup` as verbs in the path — and `Paper` has since left the contract, because those routes could not be fixed without first settling whether a paper is one resource or two ([#8](https://github.com/metacensus/api/issues/8)). `TestNonConformingRoutes` still runs, pinning the set of deliberate exceptions at empty, so a route that starts breaking a convention fails the build.

## The generated server

`routegen` walks the same descriptors that build the manifest and, per service, writes to `go/server/routes_gen.go`: a handler interface with one method per rpc — `GetTopic(context.Context, *v1.TopicGetRequest) (*v1.Topic, error)`, path, query and body already bound — an `Unimplemented<Service>` answering 501, and a `Register<Service>(mux Mux, rt *Runtime, impl <Service>)` that binds and dispatches. The hand-written half beside it is organized by concern, one file each: `binding.go` (request binding), `errors.go` (the error model), `mux.go` (the mux and path-value seam), `response.go` (response encoding), `runtime.go` (the `Runtime` struct and its options).

**`go/server`'s own comments are the account of itself**: `runtime.go`'s package comment says what the package is, `Mux`'s doc comment says what it leaves to the router and why, and every other constraint — `Unimplemented<Service>`, the error envelope, `Prefix` — is a comment beside the declaration it constrains. It is not repeated here. Two facts belong in a README because they decide how you mount:

- **`Runtime.Prefix` is literal, and `""` means no prefix.** A router already mounted at the contract's prefix — chi's `Route`, `http.StripPrefix` — wants the zero value. A router at the origin root wants `routes.Prefix`.
- **Any `Mux` that is not a `StdMux` must set `PathValue`**, and a `chi.Router` wants `server.EscapedPathValue`. `net/http`'s `ServeMux` percent-decodes a path segment and chi does not, and the generated client percent-encodes every one of them, so either wrong answer binds a wrong id and answers 200. `Register<Service>` therefore refuses to guess: it panics at registration until the field is set. `routegen/chitest` pins both directions against a real `chi.Router`, in the module where chi may be required.

- **A service whose rpcs do not all sit behind the same middleware needs `server.Except`.** `Register<Service>` registers a whole service on one `Mux`, and the auth boundary does not always follow the service boundary — `metacensus/infra` mounts `AuthRoutes.Login` and `AuthRoutes.SignUp` publicly and `AuthRoutes.Logout` behind its JWT check. `Except` sends the named rpcs to a second router and keeps the patterns in the manifest, so a renamed rpc panics at startup instead of mounting on the wrong side.

**Deliberately excluded:** persistence or a store of any kind. `Unimplemented<Service>` is the whole default implementation; wiring a real one to a database, a cache, or another service is entirely the implementer's, and nothing here assumes a shape for it. The contract's messages are `v1.*`; a backend whose store speaks its own types — infra's `core/shared/types` today — writes that conversion itself, and nothing here generates it.

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
rt := &server.Runtime{
	PathValue:  server.EscapedPathValue, // chi leaves segments escaped
	VerifyBody: verifyXSignature,
}

r.Route(routes.Prefix, func(v1 chi.Router) {
	v1.Group(func(authed chi.Router) {
		authed.Use(jwtMiddleware)

		server.RegisterTopicRoutes(authed, rt, topics{})

		// Login and SignUp are public; the rest of AuthRoutes is not.
		server.RegisterAuthRoutes(
			server.Except(authed, v1, rt, "AuthRoutes.Login", "AuthRoutes.SignUp"),
			rt, auth{})
	})
})
```

The public surface mounts the same way, against its own `Runtime`, because `Prefix` is the one field that is per surface:

```go
pub := &server.Runtime{Prefix: routes.PublicPrefix} // everything else as above
server.RegisterPartnerRoutes(server.StdMux{ServeMux: mux}, pub, partner{})
```

Sharing one `Runtime` between them would mount one surface's routes under the other's prefix — silently, since both register without complaint and only the URLs come out wrong.

## The generated client

The same walk writes `ts/src/client.ts`: one class per surface — `Client` for the authenticated API, `PublicClient` for the public one — each with one method per rpc, typed against ts-proto's generated types (`import type` only, so it costs nothing at runtime). Each method builds the path from the request's path fields (percent-encoded per segment), the query string from its query fields, serialises the body exactly once, and hands `{method, path, body?}` to a caller-supplied `Transport`. A 2xx response is `JSON.parse`d and cast to the response type — with `onlyTypes`, there is no runtime schema to validate a response against; a non-2xx throws `ApiError`, carrying the status, the path requested and the raw response text unparsed. Parse that text defensively: a 404 or 405 comes from the router before any handler runs, so it is often not JSON at all.

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

The public surface's client comes from `@metacensus/api/public` and takes a transport of its own — it sends no credentials, and the `Authorization` and `X-Signature` headers above have no counterpart on it:

```ts
import { PublicClient, PartnerSubmission_Interest } from "@metacensus/api/public";

const pub = new PublicClient(async ({ method, path, body }) => {
  const r = await fetch(origin + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body,
  });
  return { status: r.status, body: await r.text() };
});

await pub.submitPartnerInterest({
  name,
  email,
  interests: [PartnerSubmission_Interest.FundTheWork],
  message: "",
  website: "", // honeypot; a filled one gets an ordinary receipt and is dropped
});
```

`ApiError` is the same class from either entry point, so one `catch` handles both surfaces — which is why both classes are generated into one `ts/src/client.ts` rather than a file each. `routegen/internal/clientgen` states the constraint at the seam that has to honour it.

## What the tests check

`go test ./...` reads the compiled descriptors, so every check is a property of the schema: JSON name and enum casing, `Unspecified` zero values, string ids, no proto3 `optional` scalars, `{items}` on every list, no pagination fields, no message field typed from another resource's file or from the other surface, and a route manifest that covers every rpc with no two routes sharing a method and full path. `routegen/internal/model/naming_test.go` independently reconstructs a `CodeGeneratorRequest` and cross-checks every proto→Go field mapping the server renderer reads off `protobuf:"...,name=..."` struct tags against `compiler/protogen`, the public package `protoc-gen-go` itself is built on — the naming rule that produces those tags is in an internal, unimportable package, so this is read off the generated code rather than re-derived. `go/server/*_test.go` exercises the runtime: path/body precedence, the body size cap, the `X-Signature` seam seeing raw octets, the error model, unknown query parameters rejected on every route, and that every manifest route is actually served. `routegen/chitest` re-runs the routes on real chi, which is where every claim `server.Mux`'s doc comment makes about router-owned behaviour is checked rather than asserted.

**The schema checks run over every package, and nothing can drop out of that set quietly.** Each one iterates `contractPackages`; a package missing from it would not be exempt but invisible, and the suite would pass over a schema smaller than the one that ships. Two checks close that:

- `TestEveryPackageIsGoverned` reads the `.proto` tree — not `protoregistry`, which holds only what the test binary imported — and fails on any package on disk that `contractPackages` does not name. `routegen` checks `model.Packages` the same way, against the same scan in `internal/protoscan`, so a new package fails `make gen` before it fails anything else.
- `forEachContractFile` fails **per package** when one registers no files, so a typo or a missing blank import in `go/registered_test.go` cannot leave a package vacuously green while the other keeps the count non-zero. `model.Walk` fails per package for the same reason.

Route identity is the **full** path, prefix included: a public `/partner` and an authenticated `/partner` are different URLs and must not be reported as a clash, while two routes that genuinely resolve to one URL must be.

Two scripts guard the npm package, both run by `npm run check`:

- `check-no-runtime.mjs` asserts it reaches for nothing at runtime: empty `dependencies`, and no value import in anything that ships — the files under `src/` and both entry points.
- `check-entry-points.mjs` asserts every generated module is exported by an entry point, and that no module under `src/metacensus/` is exported by both. `index.ts` and `public.ts` list their exports by hand, so without it a new `.proto` file generates a module that ships in `dist/` and that no consumer can import — an absence, not a failure, and the same shape of hole as a proto package missing from `contractPackages`. Both entry points do share `src/client.ts`, which holds a class per surface: `ApiError` has to be one class, or catching it would depend on which entry point the catch block imported from.

`npm test` (`ts/test/*.test.mjs`, plain `node --test` against the built `dist/`, no test-runner dependency) exercises the clients: every method the manifest declares, on the client for that route's surface, compared against the route it says it is; and `wire.test.mjs`, which builds `routegen/wireserver` and drives the generated client against the generated server over HTTP, so the `protojson` / ts-proto pairing is a check rather than a configuration nobody has run. That one needs Go on `PATH`, which `make check` and CI have.

## Open questions

Written down, not tracked — the four ui issues that held this work were closed as not-planned when the contract moved here, and nothing has replaced them. Each of these is a decision nobody has standing to take yet because the consumer that would settle it does not exist.

- **Should the contract declare an `Error` message?** Today the server emits an ad hoc `{"error","code"}` envelope and the clients hand back the response text unparsed, on both surfaces. A real message (an error-code enum, field-level validation errors) is the obvious next step and is exactly the kind of schema that goes wrong when it is invented before a second consumer exists. See "Errors on the two surfaces" above for the trigger. Refs [#3](https://github.com/metacensus/api/issues/3).
- **Where does `Runtime.VerifyBody` sit relative to infra's JWT middleware?** infra has no `X-Signature` verification today, so this seam has no existing behaviour to match — whether it composes with the bearer check or replaces it for these routes is open, as is whether `Register<Service>` mounts at the root or under a sub-`Route`. It is also the seam the public surface does *not* want: its rejections are `Origin` and rate limit, neither of which is a body signature. Refs [#3](https://github.com/metacensus/api/issues/3).
- **Should the client be a third entry point?** `@metacensus/api` and `@metacensus/api/public` split by *surface*; neither splits the client away from the types, so a consumer that only wants `Topic` still resolves `src/client.ts`. `sideEffects: false` lets a bundler drop it, which is why this is not urgent, but a `@metacensus/api/client` export would make it unconditional. Refs [#4](https://github.com/metacensus/api/issues/4).
- **Should the generated client emit a query parameter holding its zero value?** No route declares a query field, so both answers are untested against a real caller, and `EmitDefaultValues` on the response side argues one way while URL length argues the other. Refs [#4](https://github.com/metacensus/api/issues/4).
- **Cross-language wire agreement is checked for the shapes the contract has, not for the ones it could grow.** `ts/test/wire.test.mjs` drives the generated client against the generated server over real HTTP and pins the `protojson` / ts-proto pairing: an escaped path segment, a `Timestamp` as an RFC 3339 string, an enum as its value name, `EmitDefaultValues` against `useOptionals=messages`, the error envelope, and an unknown field refused. This was [metacensus/ui#49](https://github.com/metacensus/ui/issues/49). What it does not cover is a shape nobody has written yet — a 64-bit integer (`forceLong=string`), a map, a `oneof` — so the pairing in `proto/buf.gen.yaml` is still five options that have to agree with `go/wire.go` and are only checked where a route exercises them.
