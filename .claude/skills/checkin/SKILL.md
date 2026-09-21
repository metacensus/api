---
name: checkin
description: Pre-flight checks before putting anything back into GitHub from this repo — branch, precedent-matching, the generated/hand-written boundary, the build gate, collateral-truth review, and PR framing. Trigger before any commit, push, or pull request, and whenever the user says they want to check something in, commit, push, open a PR, or "put this back".
---

This repo publishes a versioned contract two other repos import. The cost of a sloppy check-in is not a broken build — `make check` catches that. It is a diff that hand-edits generated output, breaks the published contract without meaning to, or invents a route shape the rest of the contract does not share. That work lands on whoever maintains the repo next, or on a consumer pinned to a bad tag. Do not create it.

Work through these in order. Do not skip to the commit.

## 1. Branch — never main

```bash
git rev-parse --abbrev-ref HEAD
```

If it says `main`, stop and branch. This convention is unwritten in `AGENTS.md` — read it from precedent instead: branch names observed in this repo are **`<your-name>/<topic>`**, lowercase kebab-case (`ptmo516/request-standards`, `claude/metacensus-server-layer-33976e`). The owner prefix says who to ask about a stale branch; keep `<topic>` about the change's *subject*, not its issue number or the tool that made it. Branching from an issue needs `gh issue develop ISSUE --name <your-name>/<topic>`, because GitHub's default `123-title` does not match.

If you are already on a branch that does not match that shape, rename it before pushing:

```bash
git branch -m <your-name>/<topic>
```

## 2. Match precedent before you invent

**The single highest-value habit in this repo.** Before adding a route, a field, or a store method, find 2–3 sibling examples that already solved the same problem and copy their exact shape.

Observed conventions, verified by reading `go/service/*_routes.go` and `proto/metacensus/v1/*.proto` — verify rather than trust this list, it will age:

- A proto service is named `<Noun>Routes`; its RPCs are `List`/`Get`/`Create`-style verbs mapped to REST via `option (google.api.http)`, and a sub-resource nests under its parent's path (`/topic/{topic_id}/member`).
- A handler lives on `*Handlers` in `go/service/`, one file per resource, and returns `mapErr(err)` rather than the store's raw error.
- IDs and recorded timestamps are minted in the handler (`h.newID()`, `h.now()`), never accepted from the client on create.

If your new route's shape differs from every sibling, that is a signal to change *your* route — not a licence to introduce a variant. Passing `make check` is not the same as conventional.

## 3. Never hand-edit generated state

[`README.md` § Layout](../../../README.md#layout) and `make generated-paths` are the source of truth for what is generated: `go/metacensus/`, `go/server/routes_gen.go`, `ts/src/metacensus/`, and the rest of `$(GENERATED)`. Editing one by hand is invisible until the next `make gen` silently reverts it.

- Change `proto/*.proto`, then run `make gen` — never edit a `.pb.go` or `ts/src/metacensus/*.ts` directly.
- If `make gen` produces a diff you did not expect (a toolchain version stamped into every file, for instance), that is drift in the pinned toolchain, not something to hand-fix — see [AGENTS.md § Toolchain, pinning, and generation traps](../../../AGENTS.md).
- The pre-commit hook (`make hooks` to opt in) runs `make lint format-check` and a freshness check on any staged `proto/*` or `routegen/*` change; CI is the enforcement boundary if you skip it.

## 4. Check what else your edit made false

Changing a proto field or route can silently leave other things stale: the route list a doc references, an open question in [README.md § Open questions](../../../README.md#open-questions) that your change answers, a hand-maintained copy of the contract in a consuming repo. `make gen` refreshes the generated code; it does not refresh prose about it.

After any contract change, grep for the route or message name outside `proto/`, `go/`, and `ts/`, and re-read what comes back.

## 5. Keep the diff surgical

`make format-check` enforces canonical `.proto` formatting (`buf format`); run `make format` if it fails, and let it rewrite only the files you touched. For Go and TypeScript, `go build ./... && go vet ./...` and `npm run check` (in `ts/`) are the checked bar — there is no separate byte-hygiene rule beyond it. Never reformat or re-serialize a whole file to land a few-line change; if your editor rewrites more than you touched, undo the rest.

## 6. Run the gate

```bash
make check
```

Runs `buf lint`, `buf format --diff`, `breaking-public`, `go test`, `go build`, `go vet`, and the TypeScript package's own `check`/`test`. Not optional. CI runs the same commands on every pull request and on `main`, so a skipped local check surfaces as a red check rather than a broken checkout — run it here anyway; finding the break before you push is the whole point.

If your change touches `proto/*.proto`, also run `make breaking` by hand: it is **not enforced in CI** yet ([#14](https://github.com/metacensus/api/issues/14)), so it is the only thing standing between a merge and an accidental break of what is already tagged.

## 7. Ask what settles what you altered

The [elegance skill](../elegance/SKILL.md) names the cell your change belongs to and what excess looks like there. This repo has no `check:cells` yet — see [elegance's `instance.md`](../elegance/instance.md) — so map your changed paths against its location table by hand rather than running a command for it, then open the matching cell under `../elegance/references/cells/`.

## 8. Read your own diff before committing

```bash
git --no-pager diff -U1
```

Read every line as if reviewing someone else. Ask:

- Is the line count proportional to the change described? A one-route fix should be a handful of lines, not a reformatted file.
- Did anything get reordered that did not need to be?
- Is there a generated file in the diff you edited by hand instead of through `make gen`?
- Is there any file in the diff you did not intend to touch?

## 9. Commit and PR

Match the repo's observed commit style — lowercase, imperative, describing the *substance* of the change rather than the mechanics.

End commit messages with:

```
Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
```

End PR bodies with:

```
🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

**Name any convention you knowingly bent.** If a documented rule says one thing and you did another for good reason, say so in the PR body. A deliberate, stated exception is fine; the same choice undocumented reads as drift and costs the next maintainer time.

## 10. Do not commit without being asked

Make the change, verify it, show the diff, and stop. Committing and pushing are the user's call.
