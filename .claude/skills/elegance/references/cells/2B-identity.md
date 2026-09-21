# 2B · Identity

Tier 2 [**Organization**](../scope.md) · [**Current**](../time.md) · above [1B problem](1B-problem.md) · below [3B contract](3B-contract.md) · across [2A roadmap](2A-roadmap.md), [2C org history](2C-org-history.md).

## Example

What the organization is today, and who it is talking to.

## Location

**Org Docs**, in the [location key](../grid.md#location-key).

## Clear when

One source, fresh, matching how the field sees the organization and reflecting what the last things shipped changed; a build gate holds it where the fact is data, review for freshness otherwise.

## Excess

- **Two descriptions of what the organization is.** Retire one; do not reconcile. Test: grep for where the org describes itself — a second source is one to retire, not merge.
- **Identity written as aspiration.** What it will be is [1A](1A-vision.md) and [2A](2A-roadmap.md); this is present tense. Test: read each sentence in the present tense — one that holds only in the future is 1A or 2A.
- **Identity written as a change record.** What moved is in [2C](2C-org-history.md) and the commits; this is present tense. Test: read each sentence — one that says what changed rather than what the organization is belongs in 2C.
- **Prose where data would do.** Who it is talking to is a record the system reads and validates, not a paragraph.
- **The brand explained in words beside the tokens that define it.** The tokens are the source.

## Cost

A roadmap derived from a stale self-image.

## Moves

Rewrite in the present tense; retire a second description. The trigger comes from [3C](3C-release.md). Where the missing fact is data, strengthen the artifact and hold it as a validated record.
