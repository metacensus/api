# 5B · Code

Tier 5 [**File**](../scope.md) · [**Current**](../time.md) · above [4B surface](4B-surface.md) · across [5A criteria](5A-criteria.md), [5C commit](5C-commit.md).

## Example

The file as it is; the test that passes or fails against it.

## Location

**Code**, in the [location key](../grid.md#location-key).

## Clear when

A reader who knows the language can confirm what a function does from its name and a glance at its body, without a comment, and the checks pass — the gate of build, lint, and tests is the strongest rung in the graph.

## Excess

The tangible form of every excess above it, and the cell where elegance is most measurable: the code either needs the comment or it does not.

- **A comment restating its code.** The reader has the code; the comment costs a second read and drifts on the first edit. Test: cover the comment and read the function — if nothing was lost, delete it. A thirty-line function with a descriptive name and no comment is the norm, not the exception; if that one needs none, a ten-line one needs less.
- **A comment longer than what it describes.** If the comment takes as many tokens as the function, the function was never the hard part. Delete the comment, or find the thing that actually needed explaining and see whether it is a design smell.
- **A comment explaining why the function exists.** Justification is a smell about the function, not a reason for a comment. A private ten-line helper with a rationale has two readings: it is self-documenting and the comment goes, or it should not exist in this shape. Look at the call sites before deciding.
- **A shape repeated at every call site.** Every caller passing `x != nil` as the first argument means the signature is wrong, not the callers; the helper should take `x`. Every caller wrapping the result the same way means the wrap belongs inside. The refactor is in scope whenever it leaves the package's observable behavior unchanged.
- **A comment explaining a scope above the file.** Package behavior in a function comment, architecture in a file header: it goes stale each time the higher scope moves and nobody looks for it here. Move it up ([theory §3.2](../theory.md#32-how-drift-compounds)).
- **A number or list a check computes, transcribed beside it.** Counts, totals, coverage, what is registered or mounted: the script prints it, or the set is derived where it is defined so adding to the set is what registers it ([theory §2.3](../theory.md#23-the-graph)).
- **A file header narrating the file.** A header earns one or two lines of summary for a reader choosing whether to open this file, and nothing a reader who has opened it can see for themselves.
- **Dead paths, defensive branches for states the types exclude, flags nothing sets.** Elements with no reader.

## Cost

Minutes to fix, and the way the tier above accumulates debt in volume. A comment kept is a comment maintained by everyone who edits the function after you.

## Moves

Retire the comment first; if something is lost, strengthen the artifact — rename, retype, extract, add the assertion or test — until it is not. Refactor the signature when call sites repeat a shape. Move upward what explains a higher scope. Rewrite whole so nothing describes what was replaced ([theory §4.3](../theory.md#43-keeping-the-loop-closed)). Where a comment survives, it is shorter than the code, and the change that added it says why.
