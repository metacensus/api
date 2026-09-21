# The grid

Scope and time are independent, so every node sits in one cell of the six-by-three grid they form. A cell's coordinate is its tier digit and station letter, `3A`, and its file is `cells/3A-design.md`. **A cell is named for what it holds, not its coordinate,** so it answers to the artifact it governs rather than to the symmetry of the grid.

## Reading the grid

Open one cell at `cells/<tier><station>-*.md`; a row at `cells/3?-*.md`, summarized in [scope.md](scope.md); a column at `cells/?A-*.md`, summarized in [time.md](time.md). One tier up in the same station holds a cell's summary, one tier down derives from it, the other two stations are what it is read from and what it leaves behind. From a file you are editing, `pnpm --dir web check:cells --diff` maps its path through the location table below to the cell — the question an author actually has.

Every cell file carries the same six headings in order: **Example**, **Location**, **Clear when**, **Excess**, **Cost**, **Moves**. Clear when is the reader's test — what must hold for someone here to act without more. Excess is what saying too much looks like in this cell, the hunt list for author and reviewer alike, because the observed failure at every scope is addition ([theory §3.1](theory.md#31-drift)). A cell holds only what is its own; its confidence rung and drift instrument derive from the row, the column, and the location key, so it links rather than copies. `check:cells` holds every cell to this skeleton.

## Table 3 — The grid, with examples and locations

Each entry is an example at that scope and time, tagged with the type of location where that information belongs, and linked to its cell.

| Scope \ Time | A · Future | B · Current | C · Past |
|---|---|---|---|
| 0 Principles | [0A proposal](cells/0A-proposal.md) A proposed writing rule: the case for it, the alternatives rejected, and who has to say yes [Proposals] | [0B rules](cells/0B-rules.md) The rules the organization lives by: how it writes, what counts as the source of truth, how it decides [Org Docs] | [0C retired rules](cells/0C-retired-rules.md) A rule the organization once lived by, and the handbook version that carried it [Archive] |
| 1 Field | [1A vision](cells/1A-vision.md) The world the organization is trying to bring about, and the wider movement it belongs to [Org Docs] | [1B problem](cells/1B-problem.md) The problem as it stands in the world, and why it persists [Org Docs] | [1C field history](cells/1C-field-history.md) How the field got here [Archive] |
| 2 Organization | [2A roadmap](cells/2A-roadmap.md) The next efforts in order, each with an appetite and a decider, and the outcome they add up to [Roadmap] | [2B identity](cells/2B-identity.md) What the organization is today, and who it is talking to [Org Docs] | [2C org history](cells/2C-org-history.md) Earlier versions of the vision, and the annual reports that show how it moved [Archive] |
| 3 System | [3A design](cells/3A-design.md) The target architecture, and the plan to reach it within appetite, with non-goals stated [Design Docs] | [3B contract](cells/3B-contract.md) The contract the system exposes, and how to run it and contribute to it [Project API/Docs] | [3C release](cells/3C-release.md) What the effort landed this quarter, and what it dropped [Changelog] |
| 4 Module | [4A plan](cells/4A-plan.md) A parent item with its children, its non-goals, a decider and a decide-by date [Git/Issues] | [4B surface](cells/4B-surface.md) One module's exports: its types, its functions, and the invariants callers can rely on [Package API] | [4C merges](cells/4C-merges.md) The merged changes, each naming the item it closed and where it deviated from the plan [Git/History] |
| 5 File | [5A criteria](cells/5A-criteria.md) What the file should contain, and the acceptance criteria that say when it is done [Git/Issues] | [5B code](cells/5B-code.md) The file as it is; the test that passes or fails against it [Code] | [5C commit](cells/5C-commit.md) The commit and its message [Git/History] |

*A cell maps to a location type here, and the type maps to a place in [instance.md](../instance.md), the one file that names an organization.*

## Where the grid collapses

At tier 5 latency is near zero, so a commitment becomes a fact within minutes and the future and current cells are rarely written apart; at tier 0 a value describes and prescribes the same entity in one sentence. The grid is fully two-dimensional only in the middle, where organizations most often have a gap.

## Location key

The type of place where a cell's information belongs, and the strongest confidence rung it offers by itself. **Enforcement** is what the tooling reads: `gate` (the location refuses a wrong state on its own), `partial` (it gates one half and describes the other), or `described` (nothing refuses anything). All three grade incorrectness only ([§3.1](theory.md#31-drift)); nothing anywhere refuses excess, so every location is owed a reader for that.

**Where a location does not gate, a second reader is the check;** `check:cells --diff` names those locations. A path settles a location but not a station, so the predicate over-fires by design: over-firing costs a fresh context, under-firing costs the defect the reader exists to catch.

| Location | Enforcement | What it is | How it is enforced |
|---|---|---|---|
| Org Docs | `described` | organization-level documents: mission, principles, vision, the handbook | at best, structure and cross-references checked at build |
| Proposals | `described` | candidate principles and policies awaiting a decision: RFCs, proposal documents, discussion threads | a decision step, or none |
| Roadmap | `described` | planning documents that sequence efforts: roadmaps, objectives, milestones | a review cadence at best |
| Design Docs | `described` | design documents, architecture decision records, target-state descriptions | reconciled against the system by review |
| Project API/Docs | `partial` | a system's public surface and its own docs: the contract it exposes, its readme and contributor guide, what that system is now | the API is checked where it is generated or typed; the docs are described |
| Package API | `partial` | the surface a module or package exports, with whatever is colocated to explain it: what that chunk is now | checked by the type system and tests where they exist; otherwise described |
| Code | `gate` | source, tests, schemas, lint, continuous integration | impossible or detectable, depending on the check |
| Git/Issues | `described` | the work tracker: issues, child items, dependencies, open changes | structure only; no required fields, no transitions |
| Git/History | `partial` | version control history: commits, merged changes, releases | the record is immutable; what the message claims about the diff is described |
| Changelog | `described` | release notes, status reports, what an effort landed | written at release or review time |
| Archive | `described` | superseded documents, retired policies, annual reports, kept out of the working set | none; read only when history is the question |
