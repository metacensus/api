# Scope: the tier ladder

Scope is a node's position on the ladder of feedback latency ([theory §1.2](theory.md#12-elegance)): the time until reality reports it wrong. A tier is one row of the grid, read as `cells/<tier>?-*.md`.

## Table 1 — Scope (the tier ladder)

**A tier is named by the chunk of world its nodes are about,** what stays the same across its three stations.

| Tier | Name | Feedback latency | Cadence of change | Work object | Who reads it | Drift instrument |
|---|---|---|---|---|---|---|
| 0 | Principles | a decade or more; learned only through the failure of tiers derived from it | almost never, deliberately | none | everyone | generativity: do the tiers below keep passing their own checks |
| 1 | Field | years to decades | rarely | none | the field | contact with the world: stakeholders, literature, users |
| 2 | Organization | years | yearly | none | the organization | periodic review of the solution against the problem |
| 3 | System | a quarter to a year | quarterly | Effort: a chunk of the solution with an owner and an appetite | a team, and whoever is on the other side of a seam | design-versus-system reconciliation; boundary and contract review |
| 4 | Module | weeks | weekly | Epic: a parent work item holding children | a pair, and the module's callers | the plan closed at checkin as done, not done, or deviated |
| 5 | File | minutes to days | daily | Issue: a leaf work item and its change | one individual, increasingly an agent | continuous integration; the acceptance criteria; the diff |

*Pair means a lead and an individual contributor working one module together.*

Each chunk is a piece of the one above. At tiers 3–5 the organization creates a work object that owns the chunk while it changes, the parent link between them being the primary relation. Tiers 0–2 have no work object; the organization inhabits those chunks.

## Which tier is this?

Ask what chunk of the world the node is about and how long reality would take to report it wrong. The hard cases are the bottom three, so **ask who has to agree.** A change inside one file needs nobody: tier 5. A change to what a module exports needs its callers: tier 4. A change to what a system exposes needs the other side of a seam — another repo, team, or published package: tier 3. Publishing is the tell: a change forcing a version bump, a coordinated deploy, or an edit in a repo you are not in is tier 3 however few lines it touched.

Two signals correct the guess. **Connectedness** ([theory §2.3](theory.md#23-the-graph)): an abstract-sounding node with few dependents is a proposal, 0A not 0B; a concrete node with many dependents is higher than it reads. **Misplacement** ([theory §3.2](theory.md#32-how-drift-compounds)): a node explaining a tier above the one it sits in belongs at that tier, and the move is up.

## Along scope

Down the ladder **the check changes kind:** direct at the bottom (run the test), indirect one tier up (a plan judged by whether its children closed), reconciliation higher (a design against the system), and at the top none — a principle is correct only if what derives from it keeps passing. **The cost changes shape:** at the bottom a crash, cheap but dangerous in volume; in the middle rework, a quarter pointed wrong; at the top a bias, nothing failing while everything derived leans the same way. **Excess changes shape too:** at the bottom a comment restating its code; in the middle a design restating its contract; at the top a rule nobody can name a violation of.
