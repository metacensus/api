# The elegance skill

Every act of writing into a project alters its representation of reality, and nearly every alteration adds more than the reader needs. This skill exists so an AI altering it leans toward fewer, clearer elements. [SKILL.md](SKILL.md) is the entry for using it; this file is for changing it.

Read [theory.md](references/theory.md) sections 1–4 in order, then [scope.md](references/scope.md) and [time.md](references/time.md), then [grid.md](references/grid.md) and the cells — [5B](references/cells/5B-code.md) and [4B](references/cells/4B-surface.md) first — then [instance.md](instance.md).

## Changing the skill

The skill is applied to itself. Its files sit in cells and are held to the same test: what does the reader lose if this goes?

- **Name the cell.** `check:cells --diff` prints it.
- **Name what it retires.** A change is finished when nothing left describes what it replaced. A new rule that retires nothing is suspect; a new sentence that removes none is more so.
- **Keep the edges.** Theory's section numbers are the anchors every other file links through, so `grep -r "theory.md#"` is what a move owes first.
- **One table, one file.** Table 1 lives in scope.md, Table 2 in time.md, Table 3 and the location key in grid.md. `check:cells` fails when a cell and Table 3 disagree.
- **The writer is not the reviewer.** A change made in the conversation that asked for it carries that momentum. The check is the [`elegance-review`](../../agents/elegance-review.md) agent, holding the skill and the diff; `check:cells --diff` says when it is owed.
- **Prescription lives in cells.** SKILL.md and theory.md carry principles, moves, and questions; a rule that wants to be steps belongs in the cell with the check and cost that make it specific.

## The gradient

The skill is a tool, and each file reads top to bottom as abstract/poetic → tangible action. The two ends are cut by different rules, and confusing them is the standing failure of an edit to this skill.

- **An element's warrant can be behavioral, not only informational.** "What does the reader lose if this goes" reads two ways — the claim that disappears, and the act the model stops taking — and they diverge only at reformulation. A question near the action floor that restates a rule from the abstract ceiling is not a duplicate edge; it is the claim converted to an act at the moment of acting, and for a tool that conversion is the product.
- **The floor is the instrument against inertia.** The enemy is the author's and the model's own sycophancy and inertia; the ceiling is memorable but nebulous, and the floor's questions are what move a model to act on it. Deduplicating them against the abstract rules — the content test a fresh reviewer and `elegance-review` both reach for first — removes the instrument and hands the model back its inertia. Hold the line here, and do not let a review cut the floor as excess.
- **The boundary is a trust decision.** Where instruction stops and model judgement begins is set by how far each file reformulates toward action: how little we trust the model to derive the act from the rule. Push it down only as far as the model needs; below it the skill depends on intelligence and says nothing.

## Instruments

- **The skeleton gate.** `check:cells`, the one instrument at gate confidence.
- **Review yield.** A pass that reports no findings and no failed attempts is a failed pass.

## Holes

- Net element change per pass and review yield are the readings the skill needs, and nothing records them.
- Nothing refuses to proceed without the review; every gate in this org is advisory.
- The review trigger cannot exempt a tier-5-only change: a commit message is never a changed path.
- `changedPaths()` in `check-cells.mts` treats a failing `git merge-base` as no committed paths, so a shallow or detached checkout under-reports.
- The gate runs behind one repo's toolchain while the skill claims the org. Portable inputs are git plus the two tables; the cost of a port is untested.
- A response to review findings is an unreviewed alteration, made by the party with the most momentum, and nothing re-fires.
- The scope of an elegance pass is unsolved: when everything in range is poor, where the bound comes from. [strategy#47](https://github.com/metacensus/strategy/issues/47).
