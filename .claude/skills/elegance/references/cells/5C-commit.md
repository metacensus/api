# 5C · Commit

Tier 5 [**File**](../scope.md) · [**Past**](../time.md) · above [4C merges](4C-merges.md) · across [5A criteria](5A-criteria.md), [5B code](5B-code.md).

## Example

The commit and its message.

## Location

**Git/History**, in the [location key](../grid.md#location-key).

## Clear when

One line says what changed and the body, if any, says only why; a reader with the diff learns nothing from the message that the diff shows, and learns the one thing it cannot.

## Excess

- **A body that walks the diff.** File by file, hunk by hunk: the diff is beside it and cannot lie. Test: hide the diff and read the message — a line that only tells you what the diff shows goes.
- **A subject that summarizes and a body that summarizes again.** One goes.
- **The reasoning in full.** The alternatives weighed live in the item or the pull request; the commit names the choice and links.
- Everything [4C](4C-merges.md) names, at the commit.

## Cost

History that cannot be scanned. Blame that returns prose instead of a reason.

## Moves

Immutable once made, so alter before committing. One line for what, the why only where the diff cannot show it, a link for everything else.
