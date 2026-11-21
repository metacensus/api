# Working on the contract

Cross-cutting decisions with no single declaration to sit beside. Per-declaration detail is a comment beside the code; the [README](README.md) orients a consumer.

## Compatibility and versioning

**`metacensus.v1` owes no compatibility yet** — its consumers ship together. The public surface, which owes compatibility to whatever is deployed, is library-api-public (README).

**Wire version and release tag are independent.** The `v1` in `metacensus.v1` and its prefix versions the schema; the release tag versions the repository — one `v*` tag, one npm version, one Go module version.

**`make breaking`** asks whether the schema breaks **what was released**, against the latest tag (what a module consumer holds), not the PR's merge base. The rule set is buf's `FILE` — the strictest there is; breaks invisible to any schema (a narrowed cap, a newly-required field, a value that changes meaning) are held by review. **Not enforced in CI** ([#14](https://github.com/metacensus/api/issues/14)); intact for running by hand. An intentional break stays red until the version carrying it ships. On a tag push it compares against the immediately preceding tag, so a release carrying a deliberate break fails its own gate while the Go module publishes from the tag regardless.

## How a store.Store change reaches the services

**service-api-chain implements first** and merges: a method `store.Store` does not declare yet is harmless there, and chain's implementation passing `storetest` is the only proof chain can support it. Write it against the unmerged api with `go mod edit -replace github.com/metacensus/api=<your api checkout>`, locally.

**Then api adds it to `Store`** with its `storetest` cases. The **Chain** check builds chain's `main` against the pull request and runs its `make api-check` ([#94](https://github.com/metacensus/api/issues/94)); the merge queue reruns it against chain's `main` as it stands at merge time ([#95](https://github.com/metacensus/api/issues/95)). Chain's own pull requests run against api's `main` the same way, so neither `main` breaks the other. Both repositories merge only through a merge queue ("Merge when ready"), with no bypass, admins included.

**A method that needs new types takes two api pull requests.** Chain builds against api's release and `main`, so it cannot write against a message neither holds: the first adds the protos and routes (served 501) and is released; chain then implements against that release; the second adds the methods to `Store`, as above.

**service-api-standard trails**: it implements the method at the bump pull request that brings the release carrying it ([#97](https://github.com/metacensus/api/issues/97)), and gates nothing here. Changing an existing method's signature stays expand/contract by hand ([#92](https://github.com/metacensus/api/issues/92)).

## The request standard

Every route in `metacensus.v1` is measured against this table. It stands with the README's "Routes", "JSON is the wire" and "The signing chain", and this file's "Errors" and "Compatibility and versioning". A rule marked review has no test yet; [#72](https://github.com/metacensus/api/issues/72) tracks each that could have one.

| Rule | Enforced by |
|---|---|
| `List*`/`Get*`/`Lookup*` and `HealthRoutes.Healthcheck` use `GET`; every other RPC uses `POST`, so a read must carry one of those names. | `TestNonConformingRoutes` |
| Paths are resource nouns nested by ownership (`/topic/{topicId}/prop/{propId}/vote`), params `{lowerCamelCase}`, no verbs (`POST /topic`, not `POST /topic/create`); a resource scoped to the caller takes no id (`GET /self`). The session verbs on `AuthRoutes` (`/login`, `/signup`, `/refresh`, `/logout`) are the one exception. | `TestNonConformingRoutes` (verbs); review |
| Lists are `{ items: [...] }`. No route paginates; `ListMetadata` stays unused until one does, and then that route uses it. | `TestListResponsesWrapItems`, `TestNoPaginationFields`, `TestListMetadataIsUnreferenced` |
| Server-minted ids are `<kind>:<UUIDv7>` (`go/store`). | `TestJourney` |
| A path-bound id repeated in `content` equals the one in the path. | `TestPathAndSignedContentMustAgree` |
| A composite key, like `Vote`'s `(propId, userId)`, addresses one live value per parent and actor, written with `Set*`; any other writable record gets a minted id. | review |
| Fields the server owns (`id`, `recorded`) never go in the object the client posts. | `TestContentHasNoServerOwnedFields` |
| Handlers resolve the caller through `go/auth.Sessions`, never inline. | review |
| Record messages are `{Resource}`, `{Resource}Signed`, `{Resource}{Action}Request` and `{Resource}List`; the session and health routes are the exceptions. | `TestRecordMessageNames` |
| A search at an external provider is a `List*` read of hits nested under the resource it is matched against (`GET /topic/{topicId}/hit?term=`), its input scalar query fields. It is typed, never a passthrough, records nothing, and answers a capped list, not a page; each hit names the stored records it matches. | review |

**Extending it.** A shape with no precedent here — bulk writes, filtering, async work — starts from the closest existing pattern and adds no structure until a second consumer needs it. The same PR adds the rule to this table, with a test where one is possible. Uploads are outside it: file transfer may be a surface of its own rather than a place in `metacensus.v1` ([#98](https://github.com/metacensus/api/issues/98)).

## Why /healthz is not in the contract

`Serve` answers `GET /healthz`, and the manifest does not declare it: every route in the manifest is relative to a prefix, and `/healthz` is relative to nothing. A container runtime probes a service directly rather than through a proxy, which is why it must not move. Including it would mean an absolute path in an otherwise-relative manifest, or a second, empty prefix that makes "prefix" meaningless — and nothing would consume it (its caller is a runtime, not a type importer). `metacensus.v1` already declares `HealthRoutes` at `/healthcheck`, under a prefix; a second prefixless shape would leave two meaning different things. `TestEveryRouteHangsOffADeclaredPrefix` records this. Revisit only if something starts consuming it programmatically.

## Errors

The contract does not type its failures: the HTTP status is the machine-readable signal, the body around it is not in the contract. `metacensus.v1` dropped its `Error{string}` on the argument that free text buys a client nothing the status doesn't ([da9db64](https://github.com/metacensus/api/commit/da9db64)).

**The trigger for typing failures is an implementation emitting a `code`** — a stable discriminator the status doesn't carry. When one does, the contract gains a message for it: call it **`Failure`, not `Error`** (`Error` shadows the TS global and breaks `throw new Error(...)` in an importing module), and `ok` is not part of it.

## Dependencies

**Skeptical curiosity**: a dependency is weighed, not banned. No test pins a count — this module already rests on protobuf and genproto, so "zero" was never the position, and reimplementing a well-scoped library is often the lower-quality choice. Weigh: cost of ownership; opportunity cost of *not* using it; whether this is the best one available; whether it clears the bar in the abstract.

Two costs weigh heaviest: a requirement in the root `go.mod` reaches every consumer (why generation's heavy graph lives in `routegen`), and a runtime dependency in the npm package ships to the browser. Importing the package's own modules is neither, and always fine.

## Toolchain, pinning, and generation traps

- **buf and protoc-gen-go are `tool` deps of `routegen`, not the root** — in the published module they'd add ~90 indirect requirements (Docker CLI, quic-go, the buf server graph) to every consumer. **Do not `go mod tidy` `routegen`**: it moves the pins and can't complete anyway (something in buf's graph wants a newer Go than the generators are pinned to). Add a requirement by hand.
- **`make gen` and `make test` run `routegen` with `GOWORK=off` and the pinned `GOTOOLCHAIN`** — a `go.work` above the checkout resolves tool versions against the union of its members and lifts the pins; the `go` directive is a floor not a ceiling, so an unpinned toolchain builds plugins against whatever stdlib is present. `protoc-gen-go` stamps its version into every `.pb.go`, so either surfaces as generated-code drift in a PR that never touched a `.proto`.
- **buf runs from the repository root** — every relative path in `buf.gen.yaml` and `routegen` is relative to it. `routegen` finds the root by walking up for the contract's `go.mod` and refuses to run outside the repository.

| Thing | Pinned in | Read by |
|---|---|---|
| Go (module, generator build, consumer floor) | `go` directive in `go.mod`; `routegen/go.mod` matches | CI `setup-go`, Makefile `GOTOOLCHAIN_PIN` |
| `buf`, `protoc-gen-go` | `routegen/go.mod` `tool` directives | `make tools` |
| `ts-proto`, `typescript` | `ts/package.json` + lockfile | `npm ci` |
| Node | `ts/.nvmrc` + `engines` floor | `nvm use`, CI `setup-node` |
| `chi` (conformance test only) | `routegen/go.mod` | `make test`; never the contract module |

## Two one-way doors in the layout

Both are part of the module's API once a tag exists, so both are settled deliberately (README, "Layout").

- **The Go packages sit under `go/`**, so imports read `github.com/metacensus/api/go/metacensus/v1`. Hoisting `metacensus/` to the root would read better at the call site, but the root is shared with `ts/`, `proto/`, `internal/` and `scripts/`, and a generated `metacensus/` tree there would be the only directory whose name says nothing about which language reads it.
- **`go.mod` is at the repository root, not in `go/`.** A module whose `go.mod` sits in `go/` is versioned by `go/v1.2.3` tags: a plain `v1.2.3` would publish nothing, and `go/v1.2.3` isn't a version `make release` can mint. Rooted here, one plain semver tag does the job, and `routegen` is deliberately unversioned.

## Releasing

A release is dispatched, not a tag push: `make release TYPE=patch|minor|major` or `VERSION=1.4.0` (`DRY_RUN=1` checks without tagging), `make latest`. It validates semver with `scripts/version.sh`, prompts (`YES=1` skips; no TTY refuses without it), dispatches `release.yml` for `HEAD` and exits with the run's result. The workflow refuses a commit not on `main` or an existing tag, re-runs `ci.yml` at the commit, then the release-bot App tags it and npm publishes; the Go module needs nothing but the tag. Last, it opens a `deps/api-vX.Y.Z` bump pull request in each service, as the App with a token scoped to that service, and turns on auto-merge; a service already at or past the version gets none, and a bump that fails CI waits for a person ([#97](https://github.com/metacensus/api/issues/97)). Only the App can create a `v*` tag (a tag ruleset), so a pushed tag is refused. There is no conformance gate and no approval step: every commit on `main` already passes service-api-chain ([#92](https://github.com/metacensus/api/issues/92)), so a release needs no one's sign-off beyond the merge that put the commit there.

**Retracting a release.** No version can be deleted — the Go module proxy keeps it for good — only marked bad and superseded:

1. Open a pull request that reverts the bad change and adds `retract vX.Y.Z // <reason>` to `go.mod`; merge it through the queue.
2. `make release TYPE=patch`. The new version's `go.mod` carries the retraction, so `go get` skips vX.Y.Z for `@latest` and `go list -m -u` flags it, and its bump pull requests move both services past it (an open bump to vX.Y.Z is closed as superseded).
3. npm: within 72h of publishing, `npm unpublish @metacensus/api@X.Y.Z`; after that, `npm deprecate @metacensus/api@X.Y.Z "<reason>"`. Either needs an npm owner of the package, and the number can never be reused.
4. Leave the tag: deleting it removes nothing from the proxy, and only the App can recreate it. Edit the GitHub release's notes to say it is retracted.

**Licensing** is in the README ("License"). v0.6.0 and earlier shipped unlicensed, and the Go proxy and npm keep them so.
