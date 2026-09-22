# 4B · Surface

Tier 4 [**Module**](../scope.md) · [**Current**](../time.md) · above [3B contract](3B-contract.md) · below [5B code](5B-code.md) · across [4A plan](4A-plan.md), [4C merges](4C-merges.md).

## Example

One module's exports: its types, its functions, and the invariants callers can rely on.

## Location

**Package API**, in the [location key](../grid.md#location-key).

## Clear when

A caller can use every export from its name and type alone, every stated invariant holds and is pinned by the type system or a test, and nothing is exported that nothing consumes.

## Excess

- **A doc comment that restates the signature.** `Parse parses` says nothing the name did not. The comment that stays says what the type cannot: the constraint, the invariant, the alternative rejected — and is shorter than the signature it sits on.
- **A package doc that explains the files.** One paragraph on what the package is for and where it sits in the system is the summary owed upward; a tour of each file is a copy of the directory listing.
- **A helper exported because it exists.** Every export is a promise to every caller. Unexport what nothing outside consumes; delete what nothing consumes.
- **Two helpers doing one thing.** A near-duplicate is the missing edge at this scope. Keep one, and let the callers of the other move.
- **A wrapper thinner than its name.** A function whose body is one call, one comparison, or one nil check, and whose name is longer than its body, is syntactic sugar the caller can spell. Inline it unless the name carries an invariant the call does not.
- **An option, parameter, or mode with one caller.** A degree of freedom nothing exercises is an element with no reader.
- **A README for the module that mirrors the code.** Types and signatures are read where they are typed; a readme that lists them is a second copy that drifts by the next export.

## Cost

Callers rely on invariants that do not hold; duplicated helpers multiply the fix; every export is a compatibility promise paid on the next change.

## Moves

Strengthen before describing: rename, retype, add the test. Unexport and delete before documenting. Where the surface cannot carry it, state the invariant once where callers read it, shorter than the signature. A dependency or seam decision is recorded at the seam with the cost it avoided, once ([theory §2.3](../theory.md#23-the-graph)).
