# Where MetaCensus keeps each location

The one file in this skill that names an organization. Everything else is a blueprint; this binds the location types in the [grid](references/grid.md#location-key) to the real places that play them across MetaCensus. A cell needs no binding of its own: it maps to a location type, and the type maps to a place here. This is a map — it names places and links to them, and describes nothing, because anything that describes a place belongs in that place, and `check:cells` caps its length. Porting the skill means replacing this file.

This is `api`'s copy, ported from `strategy`'s. **`check:cells` and the `elegance-review` agent's own path-mapping step are not built for this repo** — both live only in `strategy` (`web/scripts/check-cells.mts`). Until this repo gets an equivalent, place a file by hand against the table below, and skip straight to the `elegance-review` agent's other steps.

## Locations

**Paths here** are globs against this repo's root — the index a `check:cells`-equivalent would read to answer *which cell is this file in*, if this repo had one. Longest matching glob wins.

| Location | Paths here | Elsewhere in the org | Worth pointing at |
|---|---|---|---|
| Org Docs | `AGENTS.md` `.claude/skills/*/SKILL.md` `.claude/skills/elegance/references/theory.md` | strategy's [AGENTS.md](https://github.com/metacensus/strategy/blob/main/AGENTS.md), [infra's AGENTS.md](https://github.com/metacensus/infra/blob/main/AGENTS.md), ui's `AGENTS.md`, comparanda's `CLAUDE.md`. **Hole:** no rules for the org as a whole | this repo's own [AGENTS.md](../../../AGENTS.md); strategy's "Everything here describes the present" |
| Proposals | — | **Hole.** Issues and pull-request discussion in whichever repo a rule touches; one person's per-user agent memory | [strategy#32](https://github.com/metacensus/strategy/issues/32); infra's decision framework |
| Roadmap | — | **Hole.** strategy's `README.md` § Where it is going, for that tool only | this repo's own [README.md § Open questions](../../../README.md#open-questions); strategy's "none are written yet" under Efforts |
| Design Docs | `proto/**` | strategy's `content/studio/**`; infra's `docs/strategy/`, `docs/fabric/`, `core/README.md`; service-public-api's [README](https://github.com/metacensus/service-public-api#readme); comparanda's `docs/specs/` and `docs/decisions/` | infra's `PFI_PATTERN.md`; service-public-api's "must never" list; [strategy#33](https://github.com/metacensus/strategy/issues/33) |
| Project API/Docs | `README.md` `ts/README.md` `.claude/skills/elegance/instance.md` | strategy's `README.md`, `data/README.md`, `web/mcp/**`; each repo's README and agent entry point; comparanda-dpyd-data's layout | this repo's own [AGENTS.md § Two one-way doors in the layout](../../../AGENTS.md) on where `go.mod` sits; infra's documentation map |
| Package API | `go/**` `ts/**` `.claude/skills/*/references/**` `.claude/skills/*/README.md` `.claude/agents/*.md` | strategy's `web/lib/**`; infra's `core/` layers and `core/shared/`; ui's `src/components/ui`; comparanda's `.claude/agents/` | strategy's `web/lib/schema.ts` as the field contract, `web/lib/docs/read.ts`; infra's [layer placement](https://github.com/metacensus/infra/blob/main/core/README.md) |
| Code | `proto/**` `routegen/**` `internal/**` `.github/workflows/**` `.gitignore` `Makefile` `scripts/**` | every repo's source, tests, schemas, and data | this repo's own generated-code freshness check (`.githooks/pre-commit`, `make generated-paths`); strategy's `web/scripts/check-links.mts`; comparanda's provenance-quote hook |
| Git/Issues | `.github/ISSUE_TEMPLATE/**` (not yet created — see the [issues skill](../issues/SKILL.md)) | parent-child issues, dependencies, and types are enabled on the org and unused | types Epic, Task, Bug and the convention in the [issues skill](../issues/SKILL.md); the Task form's Verification field |
| Git/History | — | commits and merged pull requests per repo; pull-request templates in infra and demo; comparanda-dpyd-data's export runs | commit conventions in the [checkin skill](../checkin/SKILL.md); [strategy#22](https://github.com/metacensus/strategy/pull/22) |
| Changelog | — | **Hole.** Release tags in ui, service-public-api, demo, and this repo | this repo's own [AGENTS.md § Releasing](../../../AGENTS.md); strategy's "not in scope" sections such as [strategy#4](https://github.com/metacensus/strategy/pull/4) |
| Archive | — | **Hole.** Version control only | — |

## The systems

What each repository is and where its gate is defined. The gate column links; the workflow files describe.

| Repository | What it is | Gate |
|---|---|---|
| [strategy](https://github.com/metacensus/strategy) | The guiding brain: vision, canon, who the org talks to, and the cockpit that renders and pitches it | [ci.yml](https://github.com/metacensus/strategy/blob/main/.github/workflows/ci.yml), described in [AGENTS.md](https://github.com/metacensus/strategy/blob/main/AGENTS.md) |
| [infra](https://github.com/metacensus/infra) | The production platform: Terraform, Temporal workflows, `core/` for API, chaincode, and on-chain types | [workflows](https://github.com/metacensus/infra/tree/main/.github/workflows) |
| [api](https://github.com/metacensus/api) | The contract, defined once in Protocol Buffers and generated into Go and TypeScript | [workflows](../../../.github/workflows), described in [AGENTS.md](../../../AGENTS.md) |
| [ui](https://github.com/metacensus/ui) | The web client | [workflows](https://github.com/metacensus/ui/tree/main/.github/workflows) |
| [service-public-api](https://github.com/metacensus/service-public-api) | Public, unauthenticated endpoints, with a stated boundary | [workflows](https://github.com/metacensus/service-public-api/tree/main/.github/workflows) |
| [demo](https://github.com/metacensus/demo) | A self-contained runnable picture of the platform | [workflows](https://github.com/metacensus/demo/tree/main/.github/workflows) |
| [comparanda](https://github.com/metacensus/comparanda) | The agentic engine for schema discovery and extraction, with an evaluation harness | a write-time hook; no CI |
| [comparanda-dpyd](https://github.com/metacensus/comparanda-dpyd) | The one domain superproject | none |
| [comparanda-dpyd-data](https://github.com/metacensus/comparanda-dpyd-data) | An artifact contract of immutable exports | none |

Strategy is the vehicle for tiers 0–2; the north-star diagram, in infra's `docs/` today, is moving to it. `network` and the vendored Fabric forks are not live locations.

## Observed drift

This repo has not run its own drift pass. See strategy's [instance.md § Observed drift](https://github.com/metacensus/strategy/blob/main/.claude/skills/elegance/instance.md#observed-drift) for the last org-wide reading (2026-09-14) rather than growing a second, un-synced list here; file anything found from this repo as an issue instead.
