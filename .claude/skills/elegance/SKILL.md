---
name: elegance
description: Elegance is the presentation of data or concepts with maximum clarity using the fewest possible elements. Trigger whenever an AI is about to write, rewrite, review, or remove anything persistent — code, comments, docs, plans, issues, data files, memory, commit and PR text — and whenever the user asks whether something should be written, shortened, moved, refactored, or exist at all.
---

**Elegance is the presentation of data or concepts with maximum clarity using the fewest possible elements.** Clarity is the constraint; fewest is the direction. Nothing is elegant, only nearer to it, so "fine as is" is never a finding — it is the absence of one.

> Perfection is finally attained not when there is no longer anything to add but when there is no longer anything to take away, when a body has been stripped down to its nakedness.
>
> — Antoine de Saint-Exupéry

## The test

For every element — a word, sentence, paragraph, section, file; a token, expression, function, signature, package — ask **what the reader loses if it goes**. Nothing: it goes. Something: ask whether a smaller element, or one placed where the reader already looks, carries it. Only then does it stay.

The burden is on existence. An element is not kept because it is true, because it was written, or because removing it might lose something. It is kept because you named what it carries that nothing else does.

## The scope of an alteration

If you are swapping words, you are at the wrong scope. Elegance is won where an element can be removed, merged, moved up, or replaced by structure — a sentence for a paragraph, a name for a comment, a signature for a pattern repeated at every call site. Work down: retire the section before polishing its sentences.

The signal you are too low: preserving meaning makes every sentence feel critical. Go up one element and ask what the paragraph is for. The observed failure of this skill is saying too much, never too little; treat any urge to add as evidence you have not yet found what to remove.

## Prose about code

Text that is not code serves two purposes and no third:

1. **Summary** — a reduction, placed one scope above what it summarizes. Almost never a comment.
2. **Illustration** — of code that is complex or obscured, and shorter than the code it illustrates.

Code documents itself nearly always. A comment on a ten-line private function has two readings: the code is self-documenting and the comment goes, or the code should be refactored and the comment was hiding it. A comment explaining why a function exists is a smell about the function; a shape repeated at every call site is a smell about the signature. Refactors that leave the observable behavior at the package and feature boundary unchanged are in scope, and a comment is never the cheap way out. [5B code](references/cells/5B-code.md) and [4B surface](references/cells/4B-surface.md) carry the smells.

## Moves

In order of preference, and the order is the point.

- **Retire.** Delete what nothing loses. Low connectedness at assertion confidence marks the prune set. [§4.3](references/theory.md#43-keeping-the-loop-closed)
- **Merge.** Two nodes saying one thing become one; a duplicate is a missing edge, and the only question is which copy is the source. [§2.3](references/theory.md#23-the-graph)
- **Move up.** Summarize upward, reference sideways, never explain downward. A node explaining a scope above its own belongs at that scope. [§4.2](references/theory.md#42-where-information-goes)
- **Replace with structure.** A name, type, signature, test, or gate carries what a sentence would, and cannot drift from it; a pointer at a node that already passed carries a shape better than a description of one. [§4.2](references/theory.md#42-where-information-goes)
- **Derive.** What is computable is not written: counts, lists of contents, claims over a set. The thing that computes it prints it. [§2.3](references/theory.md#23-the-graph)
- **Rewrite whole.** When an element stays, replace it entirely so nothing left describes what it replaced. [§4.3](references/theory.md#43-keeping-the-loop-closed)
- **Mark the hole.** An open question is a named absence at a known scope, never prose filling the space. [§3.4](references/theory.md#34-holes)
- **Add, last.** A new element names its scope and what it retires. Restraint is reported, never silently applied: non-goals, what was left alone and why. [§4.2](references/theory.md#42-where-information-goes)

## The model

The grid says where an element belongs and what settles it; the reader is who decides whether it is clear. [theory.md](references/theory.md) builds the terms.

- **Scope** is feedback latency, how long reality takes to say a node is wrong: 0 principles, 1 field, 2 organization, 3 system, 4 module, 5 file. Between the bottom three, ask who has to agree. [scope.md](references/scope.md)
- **Time** is the station in the loop: **A** future, read to act; **B** current, written from observation; **C** past, written from action. Same words at another station, opposite correction. [time.md](references/time.md)
- **A cell** is scope by time, `3A`, one file each under [cells/](references/cells/), and every cell names what excess looks like there. [grid.md](references/grid.md)
- **Confidence is the check that was passed**, never fluency. Agent output enters at assertion. Risk is one minus confidence, times how much depends on the node. [§3.3](references/theory.md#33-pricing-drift)

## Questions worth asking

- **Is this a summary or an illustration, and is it shorter than what it describes?**
- **Which element is this, and is there a larger one I should be asking about?**
- **Where would the reader look for this, and is that here?**
- **What already holds this?**
- **Could the artifact carry this instead of a sentence?**
- **If the thing this describes changed, would anyone fix this?**

## Before you finish

- **Which cells did you touch?** `pnpm --dir web check:cells --diff` names them from your paths. An unexpected one is an alteration to undo or a cell to open.
- **Hand the diff to the `elegance-review` agent.** It holds this skill and nothing of your reasons. Give it no account of why and name no cell. It is instructed to find what you did not, and a review that finds nothing owes its attempts.
- **What points at what you altered?** Grep the path, symbol, and name you changed, and read what comes back as its reader. A pointer whose target moved, a list you grew, a claim over a set you changed: the defect in a file your diff does not contain.

## Reading the references

Locate the cell — tier by how long reality takes to say the node is wrong, station by how a disagreement is settled — and open `references/cells/<tier><station>-*.md`. Its location type binds to a real place in [instance.md](instance.md), the one file that names an organization. [README.md](README.md) is for changing this skill.
