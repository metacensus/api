# The MetaCensus API contract, in Protocol Buffers

Every request and response under `/metacensus/api/v1` and `/metacensus/public`, defined once in `.proto` and generated into Go and TypeScript.

Split out of [`metacensus/ui`](https://github.com/metacensus/ui) (`contract/`); `git log` carries the derivation history for why each field is in or out.

**Nothing consumes this yet.** infra, demo and the SPA still carry their own hand-maintained types. `v0.1.0` was tagged only to exercise the release path: the Go module is live on proxy.golang.org, but npm still serves the placeholder version in `ts/package.json`.

`routegen` also generates a Go server (`go/server`) and a TypeScript client per surface (`ts/src/client.ts`) from the same route table, so adopting the contract doesn't mean hand-writing the HTTP binding. Neither has a consumer yet, and neither touches persistence — see "The generated server" and "The generated client" below.

The four `ui` issues that tracked this work (adoption, scope, cross-language wire agreement, route conventions) were closed as not-planned on the move. Nothing here replaces them, so "Open questions" below is written down, not tracked.

## The two surfaces

Two proto packages live here, because two surfaces do, and they differ in almost every property a contract encodes:

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

Every row is a genuine difference, not just a difference in how it was written down; where one turned out to be only the latter, the public surface follows `metacensus.v1` (see "Errors on the two surfaces").

They are separate packages rather than a corner of one, and a schema test (`TestNoMessageFieldCrossesResourceFiles`) refuses a field typed across the boundary — the compatibility policies below differ, and a message shared between the two would put the laxer policy in charge of the stricter one's wire shape.

**One repository, though, and one of each generated artifact.** The SPA calls both surfaces from one build on one origin, and it is the only party that sees every surface — the same argument that put the authenticated contract here. It takes one Go module and one npm package, but not one *namespace*: Go already separates them (`go/metacensus/public/v1` pulls nothing else in), and npm separates them with a subpath export.

## Errors on the two surfaces

**Neither surface types its failures.** On both, the HTTP status is the machine-readable signal; the body around it is not in the contract.

The public surface briefly had a `Failure` message (`{ok: false, error}`), matching what `metacensus/service-public-api` actually sends, but it was dropped to match `metacensus.v1`, which [removed its own `Error{string}`](https://github.com/metacensus/api/commit/da9db64) on the argument that a free-text string buys a client nothing actionable — the status already carries the category. That argument doesn't stop being true because the surface is public. `error` here is one sentence from a fixed vocabulary, never formatted from an error value.

What the public surface does keep is **prose**, in `public/v1/common.proto`: the statuses it answers with, on every route and on paths no route claims. `google.api.http` can't express responses, so that list has nowhere else to live, and a browser client has to code against it. Documenting a status vocabulary and typing a body are different commitments; only the second was the divergence.

**The trigger for typing failures is an implementation emitting a `code`** — a stable, machine-readable discriminator the status doesn't already carry. When one does, both surfaces should gain the same message at the same time:

- **Call it `Failure`, not `Error`.** `Error` shadows the TypeScript global, so `import type { Error }` breaks `throw new Error(...)` in the importing module.
- **`ok` is not part of it.** See below.

### The one shape that still differs

`PartnerReceipt` carries `ok: true`; `metacensus.v1` returns bare resources. That's not a design position — `contract.UnmarshalOptions` rejects unknown fields, and the service sends `ok` on the 200. Removing it is a service change first, a contract change second.

## Versioning the public surface

The release tag and the wire version are different questions with different answers.

**The wire version is independent.** `metacensus.public.v1` carries its own `v1`, so the public schema can reach v2 while the authenticated one stays at v1, or the reverse.

**The release tag is shared.** One repository, one `v*` tag, one npm version, one Go module version. Splitting the Go side would mean a `go.mod` under a subdirectory, versioned by `go/v1.2.3` tags — the exact trap "Releasing" below exists to avoid, paid twice.

**The compatibility policy is independent, and it's the part that matters.** The org's standing position — [infra's `AGENTS.md`](https://github.com/metacensus/infra/blob/main/AGENTS.md); this repository has none of its own — is that no backwards compatibility is owed yet. True of the authenticated API, whose consumers are two backends and one SPA that ship together. Not true of the public surface:

- its callers are browsers running whatever build of the SPA they loaded, which nobody can redeploy, plus whatever a CDN is still serving
- there's no auth handshake, no version negotiation and no client registry, so there's no way to find out who would break
- a rolling deploy, serving the old SPA for a few more minutes, is on its own enough to break submissions mid-window

So `make breaking` and `make breaking-public` ask different questions: the first, whether the schema breaks **what was released** (against the latest tag — what a module consumer holds); the second, whether it breaks **what is deployed** (against `origin/main` — what a browser talks to). Only the second runs in CI today; see "What \"breaking\" is measured against" for why.

One honest limit: the *rule set* is `FILE` for both — buf's strictest category, nothing stricter to switch on. Things breaking on this surface that are invisible to any schema (a narrowed length cap, a newly-required field, a value that quietly changes meaning) are held by review, not tooling.

**The path carries no version segment**, unlike `/metacensus/api/v1`. Form intake commits to additive evolution instead: values get added to `Interest`, never removed or renumbered. If read-only public data ever arrives — the category [service-public-api#2](https://github.com/metacensus/service-public-api/issues/2) deliberately leaves open — it should get its own versioned prefix rather than retrofitting one here; the proto package is already `metacensus.public.v1`, and a prefix is one entry in `routegen`'s `model.Packages`. `TestPrefix` will fail the day a nested prefix appears, which is the point: the reverse proxy routes by longest prefix match, and how it tells `/metacensus/public` from `/metacensus/public/v1` apart is a decision to take deliberately.

## Why `/healthz` is not in the contract

`metacensus/service-public-api` also answers `GET /healthz`, and it is not here.

The case for including it: it's part of the service's observable surface, and a contract that omits it isn't complete.

The case against, which won: **every route in this manifest is relative to a prefix, and `/healthz` is relative to nothing.** It sits outside `/metacensus/public` on purpose — the container runtime probes the service directly rather than through the proxy, which is exactly why it must not move. Putting it in would mean either an absolute path in an otherwise-relative manifest, or a third, empty prefix that makes "prefix" stop meaning anything. `TestEveryRouteHangsOffADeclaredPrefix` records that.

And nothing would consume it — its caller is a container runtime, not an importer of Go types or npm packages. Note `metacensus.v1` already declares `HealthRoutes` at `/healthcheck`, under a prefix and the API's own; adding a second, prefixless health shape would leave two meaning different things in one manifest.

If the answer ever changes, it's because something started consuming it programmatically.

## What the public contract does and does not mechanise

**The interest set is mechanised, and it is the reason this exists.** `PartnerSubmission.Interest` is a proto enum: adding a checkbox is adding a value, both the service and the SPA regenerate from it, and `buf breaking` refuses a removal. Before this, `PartnerInterests` in the service had to equal `partnerInterests` in the SPA by a test needing both trees checked out at once — a test the repository split destroyed. Getting it wrong now fails in CI; getting it wrong then 400'd every submission carrying the new checkbox, at runtime, for users.

**The display wording is deliberately not mechanised.** The enum carries the set; the label ("Fund the work") stays in the SPA as copy. Putting labels in the schema would make a copy edit into a schema change, a regeneration, a release and a deploy — and the two fail differently: a stale label renders an odd string, a stale set rejects every submission. Only the second is worth a build failure.

**The validation limits are documented, not enforced.** `name` ≤ 120, `email` ≤ 254, `message` ≤ 2000, body ≤ 16 KiB — comments on the fields rather than `protovalidate` constraints, because the constraint couldn't reach the half that needs it: ts-proto runs with `onlyTypes=true` and drops field options entirely, so `protovalidate-es` on the TypeScript side would have no constraint to enforce. A mismatched cap degrades a form; a mismatched interest set breaks it — only one was worth new machinery.

**The server and the client are generated, at the same depth as the authenticated surface.** `routegen` renders every surface in `model.Packages`, so the public routes get `PartnerRoutes`, `UnimplementedPartnerRoutes` and `RegisterPartnerRoutes` in `go/server`, and a `PublicClient` in `ts/src/client.ts`. Generating less for one surface than the other is the absence this contract exists to refuse.

They're still route declarations rather than a gRPC commitment — the transport is HTTP+JSON, nothing here serves gRPC. Two things are per surface rather than shared, both falling out of the prefix:

- **A `Runtime` each.** `Runtime.Prefix` is the path a service's routes hang off, so a process serving both mounts `RegisterPartnerRoutes` against a `Runtime` holding `routes.PublicPrefix` and the rest against one holding `routes.Prefix`. Everything else can be shared.
- **A transport each.** `PublicClient` sends no credentials — the bearer token the authenticated transport carries has no counterpart, and the public surface isn't signed either: it has no identities at all, so there's nobody to sign and no key to verify against.

Adopting any of this in `metacensus/service-public-api` or `metacensus/ui` is separate work; nothing in either repository is touched here. `metacensus/ui` signs a request body into an `X-Signature` header today, which nothing has ever verified and which this contract no longer has a reader for.

## The signing chain

Two identities, answering different questions.

A **login token** — username and password, sent on every request, verified by the API server — says who is *connected*. It grants access and, on its own, permits no write. A **signing keypair** says who *authored* a record. `Prop.author_id` and `Prop.created` used to be the first identity pretending to be the second: the server's assertion about a caller, indistinguishable from the server's assertion about anyone. They're gone; the signature carries both facts instead.

Every write on `metacensus.v1` carries two fields — `content` and a `userSignature` over it — and `TestEveryWriteCarriesASignature` makes that a property rather than a habit. Its exception list is `unsignedWrites` in `go/signing_test.go`, each entry carrying the reason it moves no content. The public surface is exempt because it has no identities at all.

### The stored shape

```json
{
  "id": "...",
  "recorded": "2023-11-14T22:13:20Z",
  "content": { "topicId": "...", "type": "Statement", "description": "..." },
  "userSignature": {
    "signerId": "...", "keyId": "...", "alg": "Es384", "publicKey": "",
    "signingTime": "...", "spec": "metacensus.sig/1",
    "contentType": "metacensus.v1.Prop", "value": "..."
  }
}
```

**The server wraps, it never modifies.** Everything it adds — the minted id, the recording time — sits around the signed content, never inside it, so a reader checking one record against another never has to unwrap or cast.

A stored record carries an `institutional_signature` and reserves the field number after it, so the next signature layer is again a pure field addition. Which numbers these are differs per message (`VoteSigned` mints no id, so its fields stop earlier); `reserved` holds the next one and `TestSignedRecordsReserveTheNextField` holds that. The institutional signature covers the *user's signature value*, not the content — it endorses the author, not the data. What computes and checks it is the persistence layer's, below the seam; the contract carries only its shape.

### Which ids are inside the signature

Every id the path binds is inside the content and covered by the signature. Server-minted ids are not, and cannot be — a client that could choose its own key could file a record anywhere.

The rule: an id that changes what the content *means* must be covered; one that is merely the content's *address* need not be. A prop's `topicId` decides who votes on it (meaning); the prop's own id is where it lives (address).

That puts a path-bound id in two places on a write — at the top level, where the route binds it, and inside `content`, where it's signed. `routegen` refuses a signed route whose content omits one, and generates the comparison. **That comparison is a better error message, not a control** — `contentParam` in `go/server/binding.go` says why.

### Where verification happens

**Not at the API edge.** The signature travels in the body so it reaches persistence intact and is checked inside the chaincode boundary, where the record is written. `Runtime.VerifyBody` and the `X-Signature` header it existed for are both gone.

`signing.Verify` is here and is the one to call: **do not write a second implementation of this digest.** What no repository yet does is wire it into a write path and resolve a `keyId` to an enrolled identity — see "Open questions".

### The digest

```
digest = SHA-384( JCS( {"content": C, "signature": S} ) )
```

`C` is the content message as the contract's JSON; `S` is `userSignature` with `value` emptied. Why canonical JSON of the *decoded* message rather than raw octets, why `value` is emptied rather than dropped, and why the signed attributes sit on the signature rather than every content type: see the `go/signing` package comment.

One consequence reaches the schema: JCS serialises numbers as ECMAScript would, and reproducing that printing in Go is where two implementations quietly disagree. The contract avoids the problem rather than solving it — `TestNoWideNumbersCrossJCS` refuses every 64-bit integer, float and double, leaving `int32`/`uint32`, which both languages print identically. Both canonicalisers refuse anything else at runtime too.

### Enrolling the key

Sign-up is where the keypair arrives, and it's the one signature carrying its own public key inline — the signer has no id yet, so there's nothing to look one up by. `signerId` is empty on exactly that record. What the service concedes by taking the key it's handed is on `SignUpRequest`, in `auth.proto`.

`password` sits outside `content` deliberately: content is what gets persisted, and a password must never be inside a signed, stored document.

Reading a user is how a verifier resolves a key: `GET /user/{userId}` returns the record whose signature enrolled it.

### The two times

`userSignature.signingTime` is the participant's claim, inside the digest; `recorded` is the server's observation, outside it. **Ordering and conflict resolution use `recorded`, never `signingTime`** — otherwise a participant would order their own writes. What the pair does and doesn't bound is on `UserSignature.signing_time`.

### Computing it

`go/signing` and `@metacensus/api/signing` are the two halves, and neither may be edited alone.

```go
sig := &v1.UserSignature{
	SignerId: userID, KeyId: keyID, Alg: v1.UserSignature_Es384,
	SigningTime: timestamppb.Now(),
	Spec:        signing.Spec,
	ContentType: "metacensus.v1.Topic",
}
if err := signing.Sign(priv, content, sig); err != nil { ... }
// and, behind persistence:
err := signing.Verify(pub, record.Content, record.UserSignature)
```

```ts
import { SPEC, sign, keyId, encodePublicKey } from "@metacensus/api/signing";

const signature = {
  signerId, keyId: await keyId(publicKey), alg: "Es384", publicKey: "",
  signingTime: new Date().toISOString(),
  spec: SPEC, contentType: "metacensus.v1.Topic", value: "",
};
signature.value = await sign(privateKey, content, signature);
await api.createTopic({ content, userSignature: signature });
```

**`ts/test/wire.test.mjs` is the gate on all of this.** It pins the protojson / ts-proto pairing the digest now depends on: a drift in `proto/buf.gen.yaml` or `go/wire.go` is no longer just a readability problem but a signature that verifies only on the machine that made it — `useOptionals=messages` included. The file has each language verify what the other signed, in both directions, including a document that's been through protojson's decoder and encoder on the way.

## Layout

**Two Go modules.** The contract, which consumers import, and the generator, which nobody does.

```
proto/            .proto sources and buf config — the definition, both surfaces
go/               the contract in Go: generated types, the route manifest,
                  the wire encoder, the server, and the schema tests
go/metacensus/v1/ the authenticated surface; go/metacensus/public/v1/ the public one
go/server/        generated handler interfaces + registration, and the
                  hand-written runtime beside them
go/signing/       the signing chain: JCS, the digest, sign and verify
ts/               the contract in TypeScript: generated types, the route
                  manifest, a client per surface — the npm package
ts/index.ts       the authenticated entry point; ts/public.ts the public one,
                  ts/signing.ts the signing chain — the other half of go/signing
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
| `github.com/metacensus/api` | the contract: types, manifest, wire encoder, server, signing | little, and each addition is weighed — see "Dependencies" | consumers |
| `github.com/metacensus/api/routegen` | generation, and everything that tests generation | anything — chi, buf, protoc-gen-go | nobody, ever |

The split has one reason: a `tool` directive is a real module requirement, and a test-only import is indistinguishable from a runtime one, so buf's ~90 transitive requirements and chi would both land on every consumer of the contract. Both are generation, so both live in the second module. `routegen` is `replace`d onto the working tree, so its tests run against what's in front of you rather than a published version.

**The Go packages sit under `go/`, so imports read `github.com/metacensus/api/go/metacensus/v1`.** Hoisting `metacensus/` to the root would read better at the call site, but the root is shared with `ts/`, `proto/`, `internal/` and `scripts/`, and a generated `metacensus/` tree there would be the only directory whose name says nothing about which language reads it. This is a one-way door once a tag exists — the import path is the module's public surface.

**`go.mod` is at the repository root, not in `go/`, and must stay there.** A module whose `go.mod` sits in `go/` is versioned by `go/v1.2.3` tags: a plain `v1.2.3` tag would publish nothing, and `go/v1.2.3` wouldn't match the release workflow's tag filter — nothing would run and no failure would be reported. Rooted here, one plain semver tag does the job, and `routegen` is deliberately unversioned: no `routegen/v*` tag is ever cut.

## Dependencies

The stance is **skeptical curiosity**: a dependency is weighed, not banned — the enforced no-runtime check is gone, and no test pins a count. This module already rests on protobuf and genproto, so "zero" was never the real position, and reimplementing a well-scoped library from scratch is often the *lower*-quality choice. When weighing one, ask:

1. the cost of ownership it incurs, now and later;
2. the opportunity cost of *not* using it — reimplementation, bugs, missed maintenance;
3. whether, if a library is wanted here, this is the best one available;
4. what makes a library worthwhile in the abstract, and whether this candidate clears that bar.

Two costs weigh heavier here than any count did: a requirement in the root `go.mod` reaches every consumer (why generation's heavy graph lives in `routegen` — see "Layout"), and a runtime dependency in the npm package ships to the browser. **Importing the package's own modules is neither, and always fine** — it is how `client.ts` takes its route prefixes from `route-manifest.ts`.

## Working on it

`make` on its own lists every target. From a fresh clone nothing has to be installed first — `gen` and `check` build the pinned generators and install the npm toolchain as prerequisites.

```bash
make gen     # regenerate Go, TypeScript, the route manifest, the server and the client
make check   # everything CI runs, bar the freshness diff
make hooks   # optional: lint, format and freshness checks on commit
```

**Generated code is committed, and `make gen` is the only way to write it.** CI regenerates and fails on any diff or any file produced but not committed. The paths `gen` writes are named once in the Makefile's `GENERATED`; `make clean` removes exactly those and `make generated-paths` prints them, which is how the pre-commit hook asks git the same question CI asks. `routegen` refuses to run outside the repository root, since its output paths are relative and a wrong working directory would quietly write the manifests elsewhere.

`make hooks` is opt-in and does nothing unless a `.proto` or a file under `routegen/` is staged, in which case it lints, format-checks, regenerates, and refuses the commit if regeneration left anything unstaged. Warm, that's about a second.

### Where each version is pinned, and where it is read

| Thing | Pinned in | Read by |
|---|---|---|
| Go, for the module, for building the generators, and as the floor a consumer must meet | the `go` directive in `go.mod`; `routegen/go.mod` matches it | CI's `setup-go` (`go-version-file`), and the Makefile's `GOTOOLCHAIN_PIN`, derived from the same line |
| `buf`, `protoc-gen-go` | `routegen/go.mod` `tool` directives | `make tools`, which rebuilds whenever that module's `go.mod` or `go.sum` moves |
| `ts-proto`, `typescript` | `ts/package.json` + `ts/package-lock.json` | `npm ci`, which `make gen` runs as a prerequisite when the lockfile is newer than the installed plugin |
| Node | `ts/.nvmrc` (and a floor in `engines`) | `nvm use`, and CI's `setup-node` (`node-version-file`) |
| `chi`, for the conformance test only | `routegen/go.mod` | `make test`; never the contract's module |

`buf` and `protoc-gen-go` are `tool` dependencies of `routegen` rather than the root module: left in the published module they'd add 90 indirect requirements — the Docker CLI, quic-go, the whole buf server graph — to everything that imports the contract. Do not `go mod tidy` that module: it moves the pins, and can't complete anyway, since something in buf's graph wants a newer Go than the generators are pinned to. Add a requirement by hand.

`make gen` and `make test` both run the second module with `GOWORK=off` and the pinned `GOTOOLCHAIN`, both lessons from [metacensus/infra#52](https://github.com/metacensus/infra/pull/52): a `go.work` above the checkout resolves tool versions against the union of its members and silently lifts the pins, and the `go` directive is a floor rather than a ceiling, so an unpinned toolchain builds the plugins against whatever stdlib the developer has. `protoc-gen-go` stamps its own version into every `.pb.go`, so either one surfaces as generated-code drift in a PR that never touched a `.proto`. The binaries land in `bin/`, and **buf runs from the repository root**, so every relative path in `buf.gen.yaml` and `routegen` is relative to it. `routegen` finds the root by walking up for the contract's `go.mod`, so it writes the same files regardless of where it's run from, and refuses to run outside the repository entirely.

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
import { sign } from "@metacensus/api/signing";                  // the signing chain
```

```go
import "github.com/metacensus/api/go/signing" // the same chain, the same digest
```

**One package, one entry point per concern**, the set derived by `check-entry-points.mjs` from `package.json` rather than listed here. The SPA calls both surfaces from one build and takes one dependency; a consumer of only the public surface — a third party integrating against `/metacensus/public/*`, never having had a session — imports `@metacensus/api/public` and does not acquire the authenticated types. Go needed no equivalent: its packages were already separate.

`@metacensus/api/signing` splits by *concern* rather than by surface: it's a canonicaliser and a pile of WebCrypto calls, and a consumer who only wants `Topic` shouldn't have to resolve it. The route manifest is exported from both, deliberately, because "every MetaCensus route on one screen" is the reason the contract lives in one repository.

**A consumer's Go must satisfy the `go` directive in the root `go.mod`** — a build error below it, not a fallback. It's the one thing here that constrains another repository's toolchain, so a consumer still on an older Go has to move first.

Neither is ready to depend on: `go get` resolves the tag that exists only to test the release path, and `npm install` resolves `ts/package.json`'s placeholder rather than any released contract. See "Releasing", below.

## What "breaking" is measured against

Two comparisons, two baselines, only one runs in CI. This section is about the authenticated one; see "Versioning the public surface" above for why `make breaking-public` compares against `origin/main` instead.

**CI does not enforce the authenticated check.** The step is commented out in `.github/workflows/ci.yml`, and `make breaking` is intact for running by hand. Nothing consumes this contract, and the only tag, `v0.1.0`, exists only to test the release path — so the check was measuring every branch against a throwaway baseline, failing on the removal of `paper.proto`, and taking the rest of the suite down with it before the tests ever ran. [#14](https://github.com/metacensus/api/issues/14) records the trigger for turning it back on.

`make breaking-public` is unaffected: its baseline is `origin/main` rather than a tag, so it never inherits a stale release's breakage, and its population — browsers holding whatever build they loaded — never consumed a tag in the first place.

`buf breaking` compares the schema against **the latest release tag**, not the PR's merge base — the published contract is what a breaking change breaks, and unreleased `main` is not a promise to anyone. That makes it a question about state rather than a diff, so the answer doesn't depend on which base a PR happens to have.

Two consequences:

- Before the first release there are no tags, so nothing can be broken, and the check no-ops.
- After a release, an *intentional* break stays red until the version carrying it ships — accurate, not annoying, but it does mean a deliberate break and its release want to be close together.

On a tag push the comparison is against the tag immediately preceding it, so the release itself gets checked — also the trap in [#14](https://github.com/metacensus/api/issues/14): a release carrying a deliberate break fails its own gate, while the Go module publishes from the tag regardless.

## Releasing

Tags are minted, never typed:

```bash
make release-patch          # or release-minor, release-major
make release VERSION=1.4.0  # or an explicit version
make latest                 # the current version tag
```

**There is no LICENSE yet.** Deliberate, not an oversight — a licence review is planned separately, and it's worth settling before this is widely depended on.

`scripts/version.sh` validates semver, the target refuses a tag that already exists, and it prompts before pushing — a release can't be withdrawn (npm unpublish is limited to 72 hours, and the Go module proxy is an immutable cache). Pass `YES=1` to skip the prompt; without a TTY it refuses unless you do. Pushing the tag is the whole release: `.github/workflows/release.yml` runs the full CI suite first and only then publishes npm; the Go module needs nothing but the tag for proxy.golang.org to serve it.

## JSON is the wire

Protobuf binary is not used, not supported and not a fallback. Protobuf is here for the schema, the code generation and the breaking-change detection.

- **Marshal with `protojson`, never `encoding/json`.** `go/` exports `Marshal`, `Unmarshal` and `MarshalOptions`. Under `encoding/json` the generated types produce enums as integers, timestamps as `{"seconds":…,"nanos":…}` and 64-bit integers as unquoted numbers.
- **Field names are `snake_case` in proto, `lowerCamelCase` on the wire.** protojson converts; there are no `json_name` overrides.
- **Enum values are PascalCase** and nested in the message that owns them, because protojson serialises an enum as its value name. Zero values are `Unspecified`.
- **All ids are strings.** demo mints integer serials, infra prefixed UUIDs.
- **No 64-bit integer, no float, no double.** `TestNoWideNumbersCrossJCS` refuses them; use `int32`/`uint32`, or a string — see "The digest" above.
- **Presence is expressed only through message-typed fields.** `EmitDefaultValues` plus ts-proto's `useOptionals=messages` makes scalars, enums and repeated fields always present, and message fields `?: T | undefined`. A proto3 `optional` scalar is a third case the two sides would disagree about; use `google.protobuf.Int32Value` and friends instead.

Agreement between the JSON Go emits and the TypeScript generated from the same `.proto` is not checked — that needs real documents from a backend actually serving the contract, and nothing serves it yet.

## Routes

Each resource file declares its own routes: `topic.proto` has `TopicRoutes`, `prop.proto` has `PropRoutes`, and the public package's `partner.proto` has `PartnerRoutes`. How many there are of each isn't written here — `go/routes/manifest.go` is the table, and a count beside it would go stale on the next `.proto` change without anything noticing.

**The prefixes are part of the contract, and they are generated too.** `routes.Prefix` / `apiPrefix` carries `/metacensus/api/v1` and `routes.PublicPrefix` / `publicPrefix` carries `/metacensus/public`, emitted by `routegen` from `model.Packages`. Every `path` in the manifest is relative to one of them. They used to be written out by hand in every repository that needed them, with nothing keeping the copies in agreement.

**Each route carries its own `prefix`.** With one prefix a consumer could hard-code it; with two, a consumer holding a `Route` has no other way to know which to join, and guessing from the service name is exactly the hand-mirroring this repository exists to stop.

`routegen` fails on a `metacensus.*` package declared under `proto/` that `model.Packages` doesn't name, rather than silently generating a manifest, server and client missing that package's routes. It reads the `.proto` tree rather than `protoregistry`, which holds only what the generator imported, so a package nobody imported can't be absent from both the output *and* the check that would have caught it.

The prefixes aren't expressed in the `.proto` itself: `google.api.http` carries a path per route, protobuf has no string constant, and putting them there would mean a custom `FileOptions` extension in a schema whose tests assert every file is a resource. `TestPrefix` pins the invariants instead — each prefix is absolute, has no trailing slash, is one a route actually hangs off, and no route path already contains its own prefix. It also refuses a prefix nested inside another, since the reverse proxy routes by longest prefix match and a nested pair moves that decision into a config file in a third repository.

**To read the whole route table at once, read the generated manifest** — `go/routes` or `ts/src/route-manifest.ts`. `params`, `query` and `body` between them account for every field of the request message, so **a request message models the whole request**, not only its body: `PropCreateRequest` carries `topic_id` although it never travels in a body.

**They are a route declaration, not a gRPC commitment.** Nothing generates or serves gRPC: no `protoc-gen-go-grpc`, no grpc-gateway, no Connect. ts-proto is given `outputServices=none`, without which it emits service interfaces of `Promise`-returning methods.

**Every route conforms to the conventions.** It didn't always: three routes on `Paper` broke them (a read over POST, `create` and `lookup` as verbs in the path), and `Paper` has since left the contract, since those routes couldn't be fixed without first settling whether a paper is one resource or two ([#8](https://github.com/metacensus/api/issues/8)). `TestNonConformingRoutes` still runs, pinning the set of exceptions at empty.

**`Protocol` has left the contract the same way, and for the same kind of reason.** `ProtocolRoutes.CreateProtocol` and `TopicRoutes.GetProtocol` were never checked against the conventions above, and sat in the path of a larger change to the write path, so they came out rather than being carried along unreviewed. They'll be reintroduced once there's time to settle what a protocol is. Nothing consumes this contract, so the removal costs no consumer.

## The generated server

`routegen` walks the same descriptors that build the manifest and, per service, writes to `go/server/routes_gen.go`: a handler interface with one method per rpc — `GetTopic(context.Context, *v1.TopicGetRequest) (*v1.Topic, error)`, path, query and body already bound — an `Unimplemented<Service>` answering 501, and a `Register<Service>(mux Mux, rt *Runtime, impl <Service>)` that binds and dispatches. The hand-written half beside it is organized by concern, one file each: `binding.go` (request binding), `errors.go` (the error model), `mux.go` (the mux and path-value seam), `response.go` (response encoding), `runtime.go` (the `Runtime` struct and its options).

**`go/server`'s own comments are the account of itself** — `runtime.go`'s package comment says what the package is, `Mux`'s doc comment says what it leaves to the router and why, and every other constraint is a comment beside the declaration it constrains, not repeated here. Two facts belong in a README because they decide how you mount:

- **`Runtime.Prefix` is literal, and `""` means no prefix.** A router already mounted at the contract's prefix (chi's `Route`, `http.StripPrefix`) wants the zero value; a router at the origin root wants `routes.Prefix`.
- **Any `Mux` that is not a `StdMux` must set `PathValue`**, and a `chi.Router` wants `server.EscapedPathValue`. `net/http`'s `ServeMux` percent-decodes a path segment and chi does not, and the generated client percent-encodes every one, so either wrong answer binds a wrong id and answers 200. `Register<Service>` refuses to guess — it panics at registration until the field is set. `routegen/chitest` pins both directions against a real `chi.Router`.
- **A service whose rpcs don't all sit behind the same middleware needs `server.Except`.** `Register<Service>` registers a whole service on one `Mux`, and the auth boundary doesn't always follow the service boundary — `metacensus/infra` mounts `AuthRoutes.Login` and `AuthRoutes.SignUp` publicly and `AuthRoutes.Logout` behind its JWT check. `Except` sends the named rpcs to a second router, keeping the patterns in the manifest, so a renamed rpc panics at startup instead of mounting on the wrong side.

**Deliberately excluded: persistence or a store of any kind.** `Unimplemented<Service>` is the whole default implementation; wiring a real one to a database, cache or other service is entirely the implementer's. The contract's messages are `v1.*`; a backend whose store speaks its own types — infra's `core/shared/types` today — writes that conversion itself.

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
	PathValue: server.EscapedPathValue, // chi leaves segments escaped
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

The public surface mounts the same way, against its own `Runtime`, since `Prefix` is the one field that's per surface:

```go
pub := &server.Runtime{Prefix: routes.PublicPrefix} // everything else as above
server.RegisterPartnerRoutes(server.StdMux{ServeMux: mux}, pub, partner{})
```

Sharing one `Runtime` between them would mount one surface's routes under the other's prefix — silently, since both register without complaint and only the URLs come out wrong.

## The generated client

The same walk writes `ts/src/client.ts`: one class per surface — `ClientSigned` for the authenticated API, `PublicClient` for the public one — each with one method per rpc, typed against ts-proto's generated types (`import type` only, so it costs nothing at runtime). `ClientSigned` returns the `…Signed` envelopes (`getTopic` → `TopicSigned`). Each method builds the path from the request's path fields (percent-encoded per segment), the query string from its query fields, serialises the body exactly once, and hands `{method, path, body?}` to a caller-supplied `Transport`. A 2xx response is `JSON.parse`d and cast to the response type — with `onlyTypes`, there's no runtime schema to validate against; a non-2xx throws `ApiError`, carrying the status, the path requested and the raw response text unparsed. Parse that text defensively: a 404 or 405 comes from the router before any handler runs, so it's often not JSON at all.

**The `Transport` is where the caller's own concerns live**, deliberately: auth headers, and status policy beyond "2xx parses, the rest throws" (the SPA's 401-clears-the-token-and-redirects behavior, or retries). The client doesn't call `fetch` itself and carries no cache of its own.

It is not where signing happens. A participant's signature is part of the request *message*, built and signed before the client is called — see "The signing chain" above. The transport used to be where an `X-Signature` header was computed over `req.body`, which is why the client still serialises the body exactly once and hands over the string, but nothing in the contract depends on that any more.

A caller writes:

```ts
import { ClientSigned, ApiError, type Transport } from "@metacensus/api";

const transport: Transport = async ({ method, path, body }) => {
  const headers = new Headers({ "Content-Type": "application/json" });
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const r = await fetch(baseUrl + path, { method, headers, body });
  return { status: r.status, body: await r.text() };
};

const api = new ClientSigned(transport);
const topic = await api.getTopic({ topicId });
const created = await api.createTopic({ name, description: "" });
```

A `bytes` field anywhere in a request **or** response tree is refused at generation time: ts-proto types it `Uint8Array`, which `JSON.stringify` renders as `{"0":1,"1":2}` rather than the base64 protojson expects. There is no such field in the contract today.

`useOptionals=messages` makes every scalar required, pairing with `EmitDefaultValues` on the Go side: `api.createTopic({ name })` doesn't type-check, `{ name, description: "" }` does.

The public surface's client comes from `@metacensus/api/public` and takes a transport of its own — it sends no credentials, and the `Authorization` header above has no counterpart:

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

`go test ./...` reads the compiled descriptors, so every check is a property of the schema: JSON name and enum casing, `Unspecified` zero values, string ids, no proto3 `optional` scalars, `{items}` on every list, no pagination fields, no message field typed from another resource's file or from the other surface, a route manifest covering every rpc with no two routes sharing a method and full path, and what the signing chain needs — every write signed, content and signature paired, no number JCS can't carry across the two languages, no presence case inside signed content, and the institutional signature's field number reserved on every stored record. `routegen/internal/model/naming_test.go` independently reconstructs a `CodeGeneratorRequest` and cross-checks every proto→Go field mapping the server renderer reads off struct tags against `compiler/protogen`, the package `protoc-gen-go` itself is built on — read off the generated code rather than re-derived, since the naming rule lives in an internal, unimportable package. `go/server/*_test.go` exercises the runtime: path/body precedence, the body size cap, both halves of a signed request being required and agreeing with each other, the error model, unknown query parameters rejected on every route, and that every manifest route is actually served. `routegen/chitest` re-runs the routes on real chi, checking every claim `server.Mux`'s doc comment makes about router-owned behaviour.

**The schema checks run over every package, and nothing can drop out of that set quietly.** Each iterates `contractPackages`; a package missing from it would be invisible, not exempt, and the suite would pass over a schema smaller than the one that ships. Two checks close that:

- `TestEveryPackageIsGoverned` reads the `.proto` tree — not `protoregistry`, which holds only what the test binary imported — and fails on any package on disk `contractPackages` doesn't name. `routegen` checks `model.Packages` the same way, against the same scan in `internal/protoscan`, so a new package fails `make gen` before it fails anything else.
- `forEachContractFile` fails **per package** when one registers no files, so a typo or missing blank import in `go/registered_test.go` can't leave one package vacuously green while the other keeps the count non-zero. `model.Walk` fails per package for the same reason.

Route identity is the **full** path, prefix included: a public `/partner` and an authenticated `/partner` are different URLs and must not be reported as a clash, while two routes that genuinely resolve to one URL must be.

`go/signing` has a suite of its own: the canonicaliser against the cases RFC 8785 exists for — key ordering by code unit, the escapes `encoding/json` would have got wrong, the numbers both languages refuse — and then the chain, where an altered content, an altered signed attribute, a substituted `contentType` and a key that doesn't thumbprint to its `keyId` each have to fail.

`check-entry-points.mjs` guards the npm package, run by `npm run check`. It asserts what `package.json` publishes, what the entry points export and what the tsconfigs compile are the same set. `index.ts` and `public.ts` list their exports by hand, and both tsconfigs list the entry points by hand, so without it a new `.proto` file generates a module no consumer can import, and a new subpath export emits no `dist/` file at all — the same shape of hole as a proto package missing from `contractPackages`. Both entry points share `src/client.ts`, which holds a class per surface: `ApiError` has to be one class, or catching it would depend on which entry point the catch block imported from.

`npm test` (`ts/test/*.test.mjs`, plain `node --test` against the built `dist/`) exercises the clients — every method the manifest declares, on the client for that route's surface, compared against the route it says it is — and `wire.test.mjs`, which builds `routegen/wireserver` and drives the generated client against the generated server over HTTP, so the `protojson` / ts-proto pairing is a check rather than a configuration nobody has run. That one needs Go on `PATH`, which `make check` and CI have. It gates signature validity as well as readability, for the reason given under "The digest" above.

## Open questions

Written down, not tracked — the four `ui` issues that held this work were closed as not-planned when the contract moved here, and nothing has replaced them. Each of these is a decision nobody has standing to take yet, because the consumer that would settle it doesn't exist.

- **Should the contract declare an `Error` message?** Today the server emits an ad hoc `{"error","code"}` envelope and the clients hand back the response text unparsed, on both surfaces. A real message (an error-code enum, field-level validation errors) is the obvious next step, and exactly the kind of schema that goes wrong when invented before a second consumer exists. See "Errors on the two surfaces" above. Refs [#3](https://github.com/metacensus/api/issues/3).
- **No write path verifies a signature yet, and what persistence owes is stated only in comments beside the fields it constrains, nowhere as a suite.** `signing.Verify` checks a digest against a key it's handed; resolving a `keyId` to an enrolled identity and deciding who may sign what are persistence's, deliberately. What persistence owes, as of this contract: verify the digest under the key `keyId` resolves to; refuse a `signerId` that disagrees with the `userId` its content carries; check at sign-up that `keyId` is the thumbprint of the `publicKey` handed over; and hold a `signingTime`/`recorded` tolerance in chaincode configuration, since a number written into the contract would be a wire break to change. No repository implements any of it — `metacensus/infra` has no signature handling and still carries `AuthorId` — so the above is the only statement of the work, and it's prose rather than a conformance suite.
- **Key rotation.** `userSignature.keyId` exists from the first release so a second key is a lookup rather than a reshaping, but no route enrols one and nothing says what happens to records signed by a key that's been retired. Sign-up is the only enrolment today.
- **Should the client be an entry point of its own?** `@metacensus/api` and `@metacensus/api/public` split by *surface*; neither splits the client away from the types, so a consumer that only wants `Topic` still resolves `src/client.ts`. `sideEffects: false` lets a bundler drop it, which is why this isn't urgent, but a `@metacensus/api/client` export would make it unconditional — `@metacensus/api/signing` has since made the same split for the same reason. Adding one is now gated rather than remembered: `check-entry-points.mjs` fails an entry point missing from either tsconfig. Refs [#4](https://github.com/metacensus/api/issues/4).
- **Should the generated client emit a query parameter holding its zero value?** No route declares a query field, so both answers are untested against a real caller, and `EmitDefaultValues` on the response side argues one way while URL length argues the other. Refs [#4](https://github.com/metacensus/api/issues/4).
- **Cross-language wire agreement is checked for the shapes the contract has, not for the ones it could grow.** `ts/test/wire.test.mjs` drives the generated client against the generated server over real HTTP and pins the `protojson` / ts-proto pairing: an escaped path segment, a `Timestamp` as an RFC 3339 string, an enum as its value name, `EmitDefaultValues` against `useOptionals=messages`, the error envelope, and an unknown field refused. This was [metacensus/ui#49](https://github.com/metacensus/ui/issues/49). What it doesn't cover is a shape nobody has written yet — a 64-bit integer (`forceLong=string`), a map, a `oneof` — so the pairing in `proto/buf.gen.yaml` is still five options that have to agree with `go/wire.go` and are only checked where a route exercises them.
