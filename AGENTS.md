# Working on the contract

Cross-cutting decisions with no single declaration to sit beside. Per-declaration detail is a comment beside the code; the [README](README.md) orients a consumer. Nothing consumes the contract yet, so these are stances, not obligations.

## Compatibility and versioning

The two surfaces (README, "The two surfaces") owe different compatibility:

- **`metacensus.v1`** owes none yet — its consumers are two backends and one SPA that ship together (the org's standing position, [infra's `AGENTS.md`](https://github.com/metacensus/infra/blob/main/AGENTS.md)).
- **`metacensus.public.v1`** owes it to whatever is deployed. Its callers are browsers running whatever SPA build they loaded; there is no auth handshake, version negotiation or client registry to find who would break, and a rolling deploy alone can break submissions mid-window. So form intake evolves additively — values are added to `Interest`, never removed or renumbered — and the path carries no version segment. Read-only public data, if it ever arrives ([service-public-api#2](https://github.com/metacensus/service-public-api/issues/2)), gets its own versioned prefix rather than retrofitting one; `TestPrefix` fails the day a nested prefix appears, because the reverse proxy routes by longest-prefix match and that decision must be taken deliberately.

**Wire version and release tag are independent.** `metacensus.public.v1` carries its own `v1`, so the public schema can reach v2 while the authenticated one stays at v1. The release tag is shared: one repo, one `v*` tag, one npm version, one Go module version. Splitting the Go side would mean a `go.mod` under a subdirectory versioned by `go/v1.2.3` tags — the trap "Releasing" avoids, paid twice.

**`make breaking` and `make breaking-public` ask different questions.** The rule set is buf's `FILE` for both — the strictest there is; breaks invisible to any schema (a narrowed cap, a newly-required field, a value that changes meaning) are held by review.

- `make breaking` — does the schema break **what was released**, against the latest tag (what a module consumer holds), not the PR's merge base? The published contract is what a break breaks. **Not enforced in CI** ([#14](https://github.com/metacensus/api/issues/14)): the only tag, `v0.1.0`, exists to test the release path, so the check measured every branch against a throwaway baseline and failed on `paper.proto`'s removal. Intact for running by hand. Before the first real release there are no meaningful tags, so it no-ops; after one, an intentional break stays red until the version carrying it ships. On a tag push it compares against the immediately preceding tag, so a release carrying a deliberate break fails its own gate while the Go module publishes from the tag regardless.
- `make breaking-public` — does it break **what is deployed**, against `origin/main` (what a browser talks to)? The only one in CI, and unaffected by a stale release, since its population never consumed a tag.

## Why /healthz is not in the contract

`service-public-api` answers `GET /healthz`, and it is not here: every route in the manifest is relative to a prefix, and `/healthz` is relative to nothing. The container runtime probes the service directly rather than through the proxy, which is why it must not move. Including it would mean an absolute path in an otherwise-relative manifest, or a third empty prefix that makes "prefix" meaningless — and nothing would consume it (its caller is a runtime, not a type importer). `metacensus.v1` already declares `HealthRoutes` at `/healthcheck`, under a prefix; a second prefixless shape would leave two meaning different things. `TestEveryRouteHangsOffADeclaredPrefix` records this. Revisit only if something starts consuming it programmatically.

## What the public contract does and does not mechanise

- **The interest set is mechanised, and is why this exists.** `PartnerSubmission.Interest` is a proto enum; adding a checkbox is adding a value, service and SPA regenerate, and `buf breaking` refuses a removal. Getting it wrong now fails CI; before the split it 400'd every submission carrying the new checkbox, at runtime.
- **Display wording is not.** The label ("Fund the work") stays in the SPA as copy. A stale label renders an odd string; a stale set rejects every submission — only the second is worth a build failure.
- **Validation limits are documented, not enforced** — `name` ≤ 120, `email` ≤ 254, `message` ≤ 2000, body ≤ 16 KiB, as field comments. ts-proto runs `onlyTypes=true` and drops field options, so `protovalidate-es` would have no constraint to enforce on the TS side.

## Errors

Neither surface types its failures: the HTTP status is the machine-readable signal, the body around it is not in the contract. `metacensus.v1` dropped its `Error{string}` on the argument that free text buys a client nothing the status doesn't ([da9db64](https://github.com/metacensus/api/commit/da9db64)); the public surface followed, dropping its `Failure{ok,error}`. What the public surface keeps is a documented status vocabulary in `public/v1/common.proto` — `google.api.http` can't express responses, and a browser has to code against them.

**The trigger for typing failures is an implementation emitting a `code`** — a stable discriminator the status doesn't carry. When one does, both surfaces gain the same message: call it **`Failure`, not `Error`** (`Error` shadows the TS global and breaks `throw new Error(...)` in an importing module), and `ok` is not part of it. `PartnerReceipt`'s `ok: true` is the one exception, and a service fact (`UnmarshalOptions` rejects unknown fields), not a design position.

## Dependencies

**Skeptical curiosity**: a dependency is weighed, not banned. No test pins a count — this module already rests on protobuf and genproto, so "zero" was never the position, and reimplementing a well-scoped library is often the lower-quality choice. Weigh: cost of ownership; opportunity cost of *not* using it; whether this is the best one available; whether it clears the bar in the abstract.

Two costs weigh heaviest: a requirement in the root `go.mod` reaches every consumer (why generation's heavy graph lives in `routegen`), and a runtime dependency in the npm package ships to the browser. Importing the package's own modules is neither, and always fine.

## Toolchain, pinning, and generation traps

- **buf and protoc-gen-go are `tool` deps of `routegen`, not the root** — in the published module they'd add ~90 indirect requirements (Docker CLI, quic-go, the buf server graph) to every consumer. **Do not `go mod tidy` `routegen`**: it moves the pins and can't complete anyway (something in buf's graph wants a newer Go than the generators are pinned to). Add a requirement by hand.
- **`make gen` and `make test` run `routegen` with `GOWORK=off` and the pinned `GOTOOLCHAIN`** — lessons from [infra#52](https://github.com/metacensus/infra/pull/52). A `go.work` above the checkout resolves tool versions against the union of its members and lifts the pins; the `go` directive is a floor not a ceiling, so an unpinned toolchain builds plugins against whatever stdlib is present. `protoc-gen-go` stamps its version into every `.pb.go`, so either surfaces as generated-code drift in a PR that never touched a `.proto`.
- **buf runs from the repository root** — every relative path in `buf.gen.yaml` and `routegen` is relative to it. `routegen` finds the root by walking up for the contract's `go.mod` and refuses to run outside the repository.

| Thing | Pinned in | Read by |
|---|---|---|
| Go (module, generator build, consumer floor) | `go` directive in `go.mod`; `routegen/go.mod` matches | CI `setup-go`, Makefile `GOTOOLCHAIN_PIN` |
| `buf`, `protoc-gen-go` | `routegen/go.mod` `tool` directives | `make tools` |
| `ts-proto`, `typescript` | `ts/package.json` + lockfile | `npm ci` |
| Node | `ts/.nvmrc` + `engines` floor | `nvm use`, CI `setup-node` |
| `chi` (conformance test only) | `routegen/go.mod` | `make test`; never the contract module |

## Two one-way doors in the layout

Both are the module's public surface once a tag exists, so both are settled deliberately (README, "Layout").

- **The Go packages sit under `go/`**, so imports read `github.com/metacensus/api/go/metacensus/v1`. Hoisting `metacensus/` to the root would read better at the call site, but the root is shared with `ts/`, `proto/`, `internal/` and `scripts/`, and a generated `metacensus/` tree there would be the only directory whose name says nothing about which language reads it.
- **`go.mod` is at the repository root, not in `go/`.** A module whose `go.mod` sits in `go/` is versioned by `go/v1.2.3` tags: a plain `v1.2.3` would publish nothing, and `go/v1.2.3` wouldn't match the release workflow's tag filter — nothing would run and no failure would be reported. Rooted here, one plain semver tag does the job, and `routegen` is deliberately unversioned.

## Releasing

Tags are minted, never typed: `make release-patch` / `release-minor` / `release-major`, `make release VERSION=1.4.0`, `make latest`. `scripts/version.sh` validates semver, refuses an existing tag, and prompts before pushing (`YES=1` skips; no TTY refuses without it). A release can't be withdrawn — npm unpublish is limited to 72h, the Go module proxy is an immutable cache. Pushing the tag is the whole release: `release.yml` runs full CI then publishes npm; the Go module needs nothing but the tag.

**No LICENSE yet** — deliberate; a licence review is planned before this is widely depended on.
