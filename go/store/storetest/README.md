# The conformance suite

`store.Store` is one definition two backends implement. Structural agreement is
free — a compile-time assertion gets it — and behavioural agreement is not.
This is the instrument for the second, and the only one:

> Two implementations of one definition drift behaviourally even while agreeing
> structurally. The only instrument that catches it is a suite every
> implementer runs.
> — [metacensus/strategy#33](https://github.com/metacensus/strategy/issues/33)

That is rung 5 of #33's enforcement ladder, and the rung nothing else reaches.
There are currently **zero tests under infra's `core/`**, so this is also the
first behavioural check either backend will have.

## Running it

Two pieces, and both are wanted. The first makes a class of drift
*impossible*; the second makes a different class *detectable*. Claiming the
first while only having the second is worse than having neither, because it
displaces a review that was doing real work.

```go
// In the backend's own package: shape divergence becomes a build failure.
var _ store.Store = (*fabricStore)(nil)

// In the backend's tests: behavioural divergence becomes a test failure.
func TestConformance(t *testing.T) {
	storetest.Run(t, storetest.Config{
		New: func(t *testing.T) store.Store {
			s := newFabricStore(t)          // or newPostgresStore(t)
			t.Cleanup(s.Close)
			return s
		},
		// Optional. Only if this backend validates id shape.
		NewID: func(k store.Kind) store.ID { return mintPrefixedUUID(k) },
		// Optional. Supplying it is how a backend claims CodeConflict is reachable.
		Contend: func(t *testing.T, s store.Store) error { return forceMVCCConflict(t, s) },
	})
}
```

`Config.New` must return genuinely independent state each call — the
determinism case builds two stores and replays one set of requests into both.

`storetest.Cases()` lists every check, each with a `Why`. A backend that
legitimately cannot satisfy one says so, by name, in its own test file. It does
not quietly not run it.

## What it catches

**Determinism.** That what the caller minted is what comes back: ids,
timestamps, hashes, unchanged through a write and a read. And that two
independent stores fed one set of requests produce the same records — the
closest a black-box suite gets to replicated execution.

**Attribution.** That the author of a prop, the voter on a vote, and the
reviewer of an extraction all come from `Caller` and never from a request
field. Two callers writing the same shape must produce two records, not one.

**Idempotency and uniqueness.** That a recast replaces rather than adds; that a
create with a taken id is `AlreadyExists` rather than a silent overwrite or a
duplicate; that a duplicate email is refused.

**Fan-out atomicity.** That a single malformed element rejects the whole
`ExtractionUpsert` and leaves *nothing* — no partially applied elements, and no
advanced `Recorded`. This is the suite's one piece of fault injection, and it
is deliberately the kind that needs no hook: a datum the store itself must
refuse. It is exactly the bug demo has today, where a review row is inserted
and then N elements are looped in unbatched with no transaction
(`api/src/routes/extraction.ts:56-76`).

**Merge semantics.** That absent elements are untouched and only an explicit
`Retract` removes one.

**The error taxonomy.** That every failure is a `*store.Error` with a code from
the vocabulary and a non-empty `Op`; that a malformed request is `Invalid`
rather than `Internal`; that a missing record is `NotFound` rather than a zero
value and a nil error; that a zero `Caller` is `Unauthenticated` rather than an
unattributed write. A bare driver error fails these cases as firmly as a wrong
code, because it forces the shared layer to string-match.

**Cancellation.** That a cancelled context produces `DeadlineExceeded`. Every
method takes a context precisely so this is reachable.

**Attestation.** That `Level` is always populated; that `Author` is the writing
caller; that `TxID` accompanies `Committed` and endorsers accompany
`Endorsed`; that a caller signature comes back verbatim; that a later action in
another capacity does not restamp an earlier record.

**Contention.** That concurrent recasts either commit or return `CodeConflict`
— never a third outcome, a duplicate row, or a code outside the taxonomy — and
that exactly one vote survives.

**Reachability of `CodeConflict`.** Only if `Config.Contend` is supplied. When
it is not, the case skips and says what went unchecked, which is the honest
answer: the code is reachable from Fabric structurally and from Postgres at
`READ COMMITTED` never, and an unreachable code means callers write a retry
path that is exercised on one backend and untested on the other.

## What it does not catch

The behavioural remainder, written down as the remainder rather than assumed
away.

- **Anything about a store nobody ran it against.** The suite is imported and
  run by an implementation. `go test ./...` in this repository runs it against
  an in-memory double under `internal/`, which proves the cases are mutually
  satisfiable — not that Postgres or Fabric satisfies them. A green run here is
  necessary and nowhere near sufficient.
- **Real atomicity under a real fault.** The malformed-datum case proves the
  implementation validates before it writes. It does not prove the store rolls
  back a crash, a lost connection, or a peer failure mid-commit. That needs
  fault injection at the driver, and it is the largest gap in this file.
- **Endorsement.** Nothing here executes a chaincode invocation on more than
  one peer, so nothing here catches a determinism violation that only two peers
  disagreeing would reveal. `Determinism/TwoFreshStoresAgree` is a proxy, and
  it passes for an implementation that reads a clock at exactly the same
  wall-clock instant twice, or that draws from a seeded generator.
- **Ordering.** The suite compares lists as sets, deliberately: no ordering is
  promised, so none is checked. A backend that starts returning key order and a
  caller that starts depending on it will not be caught here.
- **Scale.** `Read/ListsReturnEverythingWritten` uses 25 records. It catches a
  page size of 10 or 20 and would not catch one of 1000.
- **Isolation between concurrent operations of different kinds.** Only
  contended `VoteSet` is exercised. A read that observes a half-applied
  extraction from another transaction would pass.
- **Anything about the wire.** Whether a store record maps correctly to
  `metacensus.v1` types is the shared layer's problem and is not tested here or
  anywhere else yet.
- **The suite itself.** These cases are not mutation-tested: nothing proves a
  case would go red against an implementation that violated the property it
  names. A case that asserts nothing looks exactly like a case that passes.
- **Performance.** An implementation that satisfies every case with an N+1 per
  element passes.

## Adding a case

A case earns its place by naming a way two implementations could disagree while
both looking correct. Fill in `Why` with what a failure means — a conformance
failure is read by someone who did not write the case, on a PR that did not
intend to touch it, and a red assertion with no stated purpose gets worked
around rather than fixed.

Do not add a case that only one backend can pass. That is the same failure the
interface exists to avoid, arriving through the test suite instead of through
the definition.
