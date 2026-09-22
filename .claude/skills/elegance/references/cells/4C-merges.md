# 4C · Merges

Tier 4 [**Module**](../scope.md) · [**Past**](../time.md) · above [3C release](3C-release.md) · below [5C commit](5C-commit.md) · across [4A plan](4A-plan.md), [4B surface](4B-surface.md).

## Example

The merged changes, each naming the item it closed and where it deviated from the plan.

## Location

**Git/History**, in the [location key](../grid.md#location-key).

## Clear when

The merge names the item it closed and the deviation it records is what the diff did; the record is immutable, so only the link and the closing status can be missing.

## Excess

- **A pull request body that narrates the commits.** The commits are listed beneath it. Test: hide the commit list and read the body — a line that only names what a commit already says goes.
- **A summary that restates the item.** Link the item; write only the deviation. Test: replace the summary with a link to the item — keep only the deviation the diff made.
- **A testing section reciting the gate.** The gate ran; its record is the check.
- **Screenshots and walkthroughs of what the diff shows.** Keep one where the diff cannot show it — rendered output — and none otherwise.

## Cost

The parent cannot close honestly, and learnings do not roll up.

## Moves

Immutable once merged, so alter before. Link both ways, item to merge and merge to item, and record the closing status on the parent. A choice made from experience cites the experience by name, so the reason is an edge rather than prose.
