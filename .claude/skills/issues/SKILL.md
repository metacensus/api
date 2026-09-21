---
name: issues
description: The issue ladder in this repo — how work is typed (Epic/Task/Bug), composed, filed, picked up, and closed, and which standards apply to a pull request by area. Trigger whenever filing, triaging, composing, picking up, reviewing, or closing an issue, and whenever asking what work is ready to start.
---

Forward work in this repo is [GitHub issues](https://github.com/metacensus/api/issues). This skill is the convention in force. It fires at the moment work is typed, filed, picked up, or closed — which is why it lives here rather than in `AGENTS.md`, where it would be read on every unrelated pass.

Quarter-scale outcomes are not issues. They live as Effort prose in the strategy repo's [`README.md` § Efforts](https://github.com/metacensus/strategy/blob/main/README.md#efforts) — cross-repo work is tracked centrally there, not reinvented per-repo.

## Jira → GitHub (minimal)

| Jira | GitHub | Notes |
|---|---|---|
| **Initiative** | `strategy` repo [`README.md` § Efforts](https://github.com/metacensus/strategy/blob/main/README.md#efforts) (and design docs) | **Not** an issue type. Lives in `strategy`, not `api`. |
| **Epic** | Issue `type: Epic` | Weeks. Holds Tasks. Cross-repo under `metacensus`. |
| **Story / Task** | Issue `type: Task` | Minutes to days. One pass, one diff, one PR. **What agents pick up.** |
| **Bug** | Issue `type: Bug` | Same size as Task. |
| **Sub-task** | *none* | A Task that wants children is an Epic with sibling Tasks. |

Bars and checks for Epic and Task come from the `elegance` skill: [4A plan](../elegance/references/cells/4A-plan.md), [5A criteria](../elegance/references/cells/5A-criteria.md). Effort, the work object at tier 3, stays out of the tracker.

**Prefer Epic → Task.** Nest Epic under Epic only when one root must group cross-repo epics.

## Mechanisms (GitHub's words)

| Word | Carries |
|---|---|
| **Type** | Epic / Task / Bug (org-wide). Size lives on Epic vs Task. |
| **Parent / sub-issue** | Composition + rollup. Cross-repo under one owner. |
| **Dependency** | `blocked by` / `blocking` — what can start today. |
| **Label** | Intake (`needs-triage`, `agent-ready`), routing (`area:*`), drift (`deviated`, `reached-up`). Nothing else. |

No status labels, no milestones. Status is derived: open/closed, assignee, linked PR, sub-issue rollup, close reason. A Project board is optional later for humans; agents must not need `read:project`.

## Filing

Forms live in `.github/ISSUE_TEMPLATE/`, one per type. Blank issues off. Every form applies `needs-triage`. The forms carry this convention inline, which is how a person filing from the GitHub UI gets it without loading this skill. **Not yet created in this repo** — see below.

- **Epic** requires goal, children, non-goals, decider, decide-by.
- **Task** requires goal, binary acceptance criteria, and a **Verification** command (exit code settles done).
- **Bug** requires observed + expected; verification optional.

Leads file Epics; engineers file Tasks under them. Executable work lives in the repo where the PR lands. Example:

```
strategy README §Efforts  Effort: "Unified request contract" · appetite: one quarter · owner: jmbarzee
├── strategy#101  [Epic]  "The story we tell about it"  decider: ryan · by 2026-10-31
│   └── strategy#103  [Task]  "The one slide that carries it"
├── api#40       [Epic]  "Establish system-wide request standards"  blocked by strategy#101
│   ├── api#36   [Task]  "Migrate legacy requests to the established request standards"
│   └── api#37   [Task]  "Inventory the un-migrated requests against the established standards"
└── ui#108       [Task]  "Adopt the request contract in the client"  (parent: api#40 or own Epic)
```

## Picking up work

**Ready** (pick-up query):

```
is:issue is:open type:Task label:agent-ready -label:needs-triage no:assignee
```

Then check: no open `blocked by` (`gh issue view N --json issueDependencies`), and body has criteria + verification. Fail either → comment, do not start.

Hygiene: `label:needs-triage` · orphans: `is:issue is:open type:Task no:parent`

| Action | Task / Bug | Epic |
|---|---|---|
| Type, `area:*`, `needs-triage` / `agent-ready` | may | propose |
| Parent or stated `blocked by` | may | propose |
| Comment | may | may |
| Assign, close, edit others' body | propose | propose |

## Closing

| Outcome | Close |
|---|---|
| Done as planned | **completed** |
| Dropped | **not planned** |
| Done differently | **completed** + label `deviated` + comment |
| Child needed a parent rewrite | label `reached-up` on the Task |

## Linking an issue to its branch and PR

- `Closes #N` / `Fixes #N` in the PR body.
- `gh issue develop ISSUE --name <your-name>/<topic>` — GitHub's default `123-title` conflicts with the branch naming the [`checkin`](../checkin/SKILL.md) skill holds; **`--name` is required**.

## What is not configured yet

The convention above is written; the repo is not yet set up for it, so read every mechanism as intended rather than available. Checked against `metacensus/api` on 2026-09-20:

- **Labels do not exist.** None of `needs-triage`, `agent-ready`, `deviated`, `reached-up`, or `area:*` are created; only GitHub's stock labels (`bug`, `documentation`, `duplicate`, `enhancement`, `good first issue`, `help wanted`, `invalid`, `question`, `wontfix`) are present. The ready query returns nothing because no issue carries `agent-ready`.
- **No issue carries a type.** Epic/Task/Bug are unset on every open issue, so type filters and the Epic→Task ladder do not select anything yet.
- **No parent links or dependencies are in use**, so rollup and `blocked by` are untested here.
- **The ready gate is not written.** Nothing enforces that a Task body carries criteria and a Verification command; only a reader checks.
- **`.github/ISSUE_TEMPLATE/` does not exist.** Filing goes through blank issues today; port the forms from `strategy/.github/ISSUE_TEMPLATE/` (`epic.yml`, `task.yml`, `bug.yml`, `config.yml`) to make the Filing section above actionable.
- **`elegance`'s `check:cells` and its `elegance-review` agent's path-mapping are not built for this repo.** Both live only in `strategy` (`web/scripts/check-cells.mts`); this repo's cells are matched by hand against [elegance's `instance.md`](../elegance/instance.md) until it gets an equivalent.

Until that lands: file plainly, state the type and the parent in the body where the field cannot carry it, and do not rely on a label query to find work.

## Standards map (review a PR)

`area:*` selects which prose applies:

| Area | Touches | Read |
|---|---|---|
| `area:contract` | `proto/*.proto`, generated `go/metacensus/`, `ts/src/metacensus/` | [AGENTS.md § Compatibility and versioning](../../../AGENTS.md), [README.md § JSON is the wire / Routes](../../../README.md) |
| `area:signing` | `go/signing/`, `ts/src/signing.ts` | [README.md § The signing chain](../../../README.md) |
| `area:server` | `go/server/`, `go/service/` | [README.md § The generated server / The service layer](../../../README.md) |
| `area:store` | `go/store/`, `go/auth/` | [AGENTS.md § What the public contract does and does not mechanise](../../../AGENTS.md) |
| `area:client` | `ts/` (the npm package) | [ts/README.md](../../../ts/README.md), [README.md § The generated client](../../../README.md) |
| `area:routegen` | `routegen/`, `internal/protoscan/` | [README.md § Layout](../../../README.md), [AGENTS.md § Two one-way doors in the layout](../../../AGENTS.md) |
| `area:migration` | request-standard adoption work | [MIGRATION_STANDARDS.md](../../../MIGRATION_STANDARDS.md) |
| `area:repo` | hygiene, agents, direction | [AGENTS.md](../../../AGENTS.md), `.claude/skills/`, [README.md](../../../README.md) |
