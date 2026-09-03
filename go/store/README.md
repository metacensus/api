# The persistence seam: placement rule

`store.Store` is one definition two backends implement — `metacensus/demo` over
Postgres, `metacensus/infra` over Hyperledger Fabric — and both serve
`/metacensus/api/v1` to the same SPA.

That makes a seam, and a seam needs a rule about what may not cross it. This
file is that rule. It is not a design proposal; it is the decision procedure a
reviewer or an implementer applies when asking *does this belong above the line
or below it?*

**It lives here rather than in either backend.** It governs a seam two
implementers must satisfy, and a rule owned by one side of a seam is that
side's convention rather than a contract. It was written as
[metacensus/infra#56](https://github.com/metacensus/infra/issues/56), where it
would have been exactly that.

The design reasoning that produced the interface is in the PR that added it.
This is the part that has to survive it.

---

## The two questions

Per [metacensus/strategy#33](https://github.com/metacensus/strategy/issues/33),
a boundary is a decision procedure or it is nothing. There are two here, both
binary, and every placement call reduces to one of them.

**Q1 — Could two endorsing peers compute this differently?**
If yes, it **must** happen above the seam, and the resulting value travels down
as request data.

**Q2 — Would a wrong answer here be a validity claim that someone relies on?**
If yes, it **must** happen below the seam, inside chaincode, where endorsement
makes it non-bypassable.

Everything else is free, and should sit above the seam by default — shared once
rather than written twice.

The two questions do not overlap and cannot both fire on the same computation.
Q1 covers things a deterministic replicated executor *cannot* do; Q2 covers
things only a replicated executor can be *trusted* to do.

### Q1 in practice — non-determinism goes up

This is already infra's convention, and the interface promotes it to a rule.
Non-deterministic values are minted in the HTTP layer and passed into the
request:

- prop id and `time.Now()` — `core/api/server/prop.go:22,42`
- vote `LastCast` — `core/api/server/vote.go:56`
- user id and the bcrypt salt — `core/api/server/user.go:30,35,47`
- the types even say so: `// Set by server (non-deterministic)` —
  `core/shared/types/types.go:172`

The rule generalizes to: **below the seam, every operation is a pure function
of (request, current state).** An implementation that generates an id, reads a
clock, or draws randomness has broken endorsement, whether or not it fails
today.

In the interface, that is why `UserCreateRequest` carries `UserID`, `Created`
and `PasswordHash`; why `PropCreateRequest` carries `PropID` and `Created`; why
`VoteSetRequest` carries `Cast`; and why `ExtractionUpsertRequest` carries
`Recorded`. Each is a value the store is forbidden to invent.

A live violation to fix separately: `Topics.InitLedger` calls
`time.LoadLocation("America/Denver")`
(`core/chaincode/contracts/contracts/topics.go:44`), which reads tzdata from
the peer image. Peers with different images — or none — disagree or error.

### Q2 in practice — validity claims go down

Anything whose correctness the ledger is being asked to vouch for:

- signature verification against a registered key
- credit balance checks ([infra#54](https://github.com/metacensus/infra/issues/54))
- topic membership and per-topic credentialing
- existence and uniqueness invariants (`AssertExists` / `AssertExistsNot`)
- any check whose failure should stop a write

The argument in infra#54 is the general form: a check above the line looks like
enforcement, is bypassable by anything talking to the ledger directly, and —
worse — makes the chaincode check look redundant to whoever later tidies it up.

**Corollary:** the shared layer may fail fast, is never the check of record,
and must never be the only place a check happens. Duplication here is
deliberate; the lower copy is authoritative.

---

## Named near-misses

#33 is explicit that this is the property most likely to decide whether the
rule is usable. Each of these looks like a violation and is not. Do not flag
them.

| Looks like | Actually |
|---|---|
| The shared layer computing a request digest | Canonicalization, not verification. It **must** be shared, or the signer and the verifier disagree on bytes and every signature fails. `store.Digest` is the type; the algorithm is deliberately not here (open decision 4). |
| The shared layer checking id shape, enum range, required fields | A courtesy check that produces a good error. Legitimate precisely because the authoritative check still runs below. |
| The shared layer comparing a bcrypt hash | Session establishment, not a validity claim about a record. A password authenticates a session; a signature attributes a record. Provenance never depends on it. This is what `CredentialGet` exists for. |
| The shared layer holding session tokens (`infra core/api/server/auth/store.go:11-24`) | Session state is not store state. It is in-process and unpersisted by design; the store never sees a token. |
| The shared layer calling an authenticated capability it does not implement | Being a client of a thing is not implementing it. |
| A Postgres backend that cannot return `LevelEndorsed` | Not a gap. The asymmetry is the return value, not a missing method — see "The asymmetry, stated" below. |
| A file that states this rule in prose | Documentation of the rule reads exactly like a violation of it. This file is itself an instance. |

---

## Depth: predictable, not uniform

Two pressures pull against each other. *Share as much as possible* pulls logic
up. *Guarantee as much as possible* pushes it down. Optimizing either alone
produces extreme depth variation, because every operation carries a different
amount of endorsement-requiring logic.

The tie-breaker is **not** uniformity. It is predictability: **depth should be
derivable from a property of the operation, never a per-method judgment call.**
Variation an implementer can compute is fine. Variation they have to guess at
is the confusing kind.

Practically, that means a small fixed number of depth classes. Two:

**Class A — full operation (default).** The store performs the entire unit of
work, including every state-dependent check. Nearly everything is Class A.

**Class B — material return (exception).** The store returns data and the
shared layer computes over it. Admissible **only** when the remaining work is
pure computation over the returned data with no state-dependent invariant —
that is, when nothing about it needs endorsing.

### The Class B roster

Maintained here as it grows. Every entry states the test it passed.

| Method | What the shared layer does with it | Why nothing needs endorsing |
|---|---|---|
| `Users.CredentialGet` | A bcrypt comparison against the returned hash | It reads no state beyond the record already returned, and no invariant depends on its outcome. A credit check would fail this test; so would a membership check. |

Left unpoliced, each exception looks locally reasonable and the seam erodes
upward one method at a time, with no diff ever showing it. A new Class B method
that is not in this table is a review finding.

Every method's doc comment states its class.

---

## What each side owns

| Concern | Above the seam (shared) | Below the seam (per backend) |
|---|---|---|
| Non-determinism — ids, timestamps, salts, nonces | All of it. Minted once, passed as request data. | Never generates any. A backend that does has broken endorsement. |
| Well-formedness — id shape, enum range, required fields | Checks it, to produce a good error. | Checks it again. This one is authoritative; the one above is a courtesy and must never be the only check. |
| State-dependent invariants — does it exist, has this user voted, is the balance sufficient | Nothing. Checking above the line is racy and unendorsed. | All of it, inside the same unit of work as the write. |
| Session authentication | All of it. Session state is not store state. | Nothing. The store never sees a token. |
| Authorization | Only what session state answers: is this token valid, is this route authenticated at all. | Anything reading persisted state: credits, topic membership, key-to-user binding. |
| Storage layout, keys, transaction mechanics, submit/evaluate | Nothing. | All of it, invisibly. |
| HTTP status mapping, response shaping | All of it, from a typed `store.Code`. | Returns typed errors, never HTTP concepts. |

The consequence to accept openly: a Postgres backend must reimplement the
state-dependent checks, and the two can drift. That is real duplication, and
`./storetest` is the only instrument against it.

---

## Two things called verification

The word covers two acts, and they land on opposite sides.

| The act | What it needs | Where it may run |
|---|---|---|
| **The crypto check** — does this signature verify over this digest with this key? | Nothing but the digest, the signature and a key. Stateless, deterministic, cheap. | Anywhere. Legitimate above the line as a fail-fast — and only ever as a fail-fast. |
| **The authoritative gate** — is that key bound to this user, and is the record now committed as attributed to them? | Ledger state (the user's registered key) and endorsement, to make the answer non-bypassable. | Chaincode only. Anywhere else it is advisory, and a caller talking to the ledger directly walks past it. |

**Prerequisite.** Chaincode cannot run the gate today. There is no user public
key on the ledger to verify against: `PublicKey []byte` is declared in both of
infra's type copies (`core/shared/types/types.go:136`,
`core/chaincode/contracts/types/user.go:9`) and used as a field in neither, and
the only ECDSA key in `core/` is the API's own JWT signing key
(`core/api/server/auth/auth.go`), which authenticates a session and attributes
nothing. Registering user public keys at user creation is a prerequisite for
verification, not a follow-up to it — and it changes `UserCreateRequest`.

### The asymmetry, stated

If the gate is chaincode-only and demo runs nothing, demo does not merely prove
less — it *rejects* less. A forged, malformed or absent signature passes
through Postgres untouched. The permissive side determines what the contract
enforces in practice; the strict side determines where production breaks first.

The practical cost is a development-loop one: anyone building the signing path
against demo never discovers their signing code is broken until Fabric
integration. Whether the Postgres backend runs a stateless crypto check it is
not allowed to trust is open decision 7.

---

## Admitting a new operation

`demo` is authoritative about **which features exist** — it is the only place
papers, protocols, extraction, literature search and PDF storage exist at all —
and authoritative about **nothing else**. It was built fast with AI assistance;
its shapes are not evidence. `infra` is the reverse: messy, but deliberate, and
it carries the constraints that actually bind. So: **take the feature list from
demo, take the shape discipline from infra, and convert deliberately.**

For each candidate operation, in order:

**1. Is it persistence at all?**
Literature search (`demo api/src/routes/litSearch.ts`) is an outbound call to
PubMed — not persistence, and non-deterministic, so it cannot go below the line
regardless. Presigned S3 URLs (`demo api/src/routes/paper.ts:152-181` for
reads, `290-334` for writes) are object storage. Neither belongs in this
interface.

**2. Is it a read wearing a write's clothes?**
`POST /extraction-review`, `POST /category`, `POST /domain`,
`POST /protocol-element` are all reads issued as POSTs
(`demo api/src/routes/extraction.ts:8-45`, `routes/lists.ts:6,15,24`).
Normalize before modelling. #33 names same-signature semantic collision as the
worst available defect precisely because it executes rather than erroring.

**3. Does it have a natural key, or a surrogate one?**
Find the natural key before admitting the operation. A surrogate id Fabric
cannot mint deterministically forces a resolve-then-read round trip — which is
the entire reason `POST /extraction-review` exists. A review *is*
`(topic, paper, protocol, reviewer)`; once that is the key, the extra endpoint
disappears and two reviewers stop colliding. (In demo the surrogate is worse
than a round trip: `data_extraction_review.id` is a `serial`
(`api/src/schema.ts:267`) with no unique constraint on the natural key, so
repeated posts mint duplicate reviews and the lookup returns a paginated list.)

**4. Can *every* implementer express it?**
#33's characteristic failure is an operation admitted because the first
implementer could do it. Offset pagination, arbitrary sorting and ad-hoc
filtering all fail this test. `ListMetadata{page, limit, total}` was the live
example, and this PR removed it from `proto/metacensus/v1/common.proto`:
Fabric offers an opaque bookmark rather than an offset, and `total` needs an
unbounded scan.

**5. What is the atomic unit?**
One interface method is one atomic unit of work in the store — a chaincode
invocation in Fabric, a transaction in Postgres. This is what pins the seam's
depth. It also means a fan-out inside one method is atomic on both sides for
free.

**6. Where does authorship come from?**
Auth, never the request body. A `userId` in a body is a client asserting who it
is. Structurally absent beats conventionally ignored:
`data_extraction_review.user_id` exists in demo's schema
(`api/src/schema.ts:274`) and no handler sets it
(`api/src/routes/extraction.ts:58-62`), so every extraction in the working
product is anonymous while topics default to requiring two reviews
(`api/src/schema.ts:70`). No request type in `store` carries a user id.

**7. Which depth class?**
Class A unless it passes the Class B test, and then it goes in the roster
above.

An operation that survives all seven is a candidate. Anything else is a
question, not an omission.

---

## Left out

Much of the product is unbuilt, so parts of this surface are not merely
unimplemented but unoutlined. **That is the expected state, not a defect to
chase.** An operation is cheap to add and very expensive to remove once
something depends on it, so uncertainty resolves to leaving things out. Each
omission is stated as the question that has to be answered before it is
admitted.

| Left out | Why | The question it becomes |
|---|---|---|
| Offset pagination — `page`, `limit`, `total` | Fabric offers an opaque bookmark, not an offset, and `total` needs an unbounded scan. Fails step 4 exactly. | When a list first hurts, does it get an opaque cursor with no total — the only shape both can honour? |
| Filtering and sorting | Fabric range scans return key order only; anything else is a full scan sorted in memory. Postgres would make it look cheap. | Which orderings matter enough to become key layouts? Sort order in a KV store is a schema decision, not a query parameter. |
| `Execute` and `GetMetadata` (`infra core/api/server/server.go:71-72`) | Fabric-only by construction; Postgres cannot express them at all. The clearest existing violation of the interface's own premise, already gated behind an env flag. | None — they belong on a Fabric-specific debug interface the shared layer does not know about. |
| PDF and blob storage | demo uses S3 with presigned URLs. A ledger must never hold a PDF, and neither should a row: different lifecycle, different failure modes. | Does the content hash go on the ledger as the provenance anchor while the bytes live in object storage? |
| Literature search | An outbound call to PubMed. Not persistence, and not deterministic, so it cannot run below the line at all. | Is it a separate service interface, or a non-persistent capability of the shared layer? |
| Update and delete on most resources | No update path exists today for users or topics, and on an append-only ledger a delete is a tombstone with different semantics from a row disappearing. | Which resources are genuinely mutable? Each answer costs a tombstone convention and a history-read decision. |
| Reference lists — categories, domains, premade elements | Static reference data with no provenance requirement, currently served as POSTs that are reads. | Does reference data belong in the store at all, or is it configuration that ships with a deployment? |
| `lastActive` / `lastCredits` | Declared in three places, written once at creation, never updated (`infra core/chaincode/contracts/types/user.go:45-46`). A field nothing maintains is worse than a missing one. | infra#54's scope. They become interface-worthy when something writes them. |
| A cross-operation transaction handle | The obvious escape hatch, and the one that would destroy the seam: Fabric cannot hold a transaction open across two client calls. | If a flow genuinely needs two operations atomically, it is one operation. Which flows those are — and today, none. |

### Partial failure that survives anyway

Atomicity within one call does not make multi-call flows atomic, and one flow
crosses calls: uploading a paper's PDF to object storage and then recording it.
Neither store can enroll an S3 write in its transaction
(`demo api/src/routes/paper.ts:290-334`, where the row is inserted, the blob is
PUT, and the row is then updated with the key — non-atomic in both directions).
The resolution is ordering rather than transactions: write the blob first,
record it second, and let an unreferenced blob be garbage rather than let a
record point at nothing. It is also a reason to keep blob storage out of this
interface entirely.

---

## Open decisions

Stated as decisions with their tradeoffs, not as tasks. None was resolvable
from the code or the strategy documents.

**1. Does the reviewer go in the key, or does a review stay an object?**
*For key:* removes the surrogate id, removes `POST /extraction-review`, makes
both backends a single prefix scan, and makes anonymous reviews structurally
impossible. This is what the interface does.
*Against:* a review acquires no identity of its own, so it can never carry
status, a submitted-at, or a completeness flag without those becoming a
separate record keyed the same way.
*Turns on:* whether a review is ever a thing with a lifecycle — submitted,
locked, superseded — or only ever a grouping of datums.

**2. Does the caller signature land now, or after the first backend ships?**
*Now:* provenance is the product's reason for using a ledger, and on an
append-only store every record written before signatures is unattributable
permanently.
*Later:* it requires user public keys on the ledger, a key custody story the
SPA does not have, and a settled canonical digest. All three are substantial
and none is started.
*Turns on:* whether pre-signature data is acceptable to keep. If it is,
`Caller.Signed` stays nilable and the interface is ready when the keys are. Key
registration is the long pole, not the interface.

**3. One channel, or one channel per topic?**
*Per topic:* what strategy states and what `channelByTopicId` implements
(`infra core/chaincode/contracts/topic.go:33,40-42`). Real isolation and
per-topic credentialing.
*Single channel:* what actually runs — every request set hardcodes
`metacensus.topics` (`core/shared/types/types.go:528` and 26 more).
Cross-topic reads like `UserList` are trivial.
*Turns on:* whether any operation must span topics. `TopicList` and `UserList`
are where it bites. Fabric does not do cross-channel queries, so if one is
needed, per-topic channels are already decided against.

**4. Where does the canonical digest live, and who owns it?**
*With the contract:* neutral authority, as #33 requires — but it needs a Go and
a TypeScript implementation kept in step.
*With the shared Go layer:* one implementation, easy to keep correct, and it
makes Go the specification, which is the "derive it from one implementation"
failure exactly.
*Turns on:* whether the SPA ever signs directly, or whether signing always
happens in a Go client. `store.Digest` is a type with no algorithm because this
is unresolved.

**5. Is the conformance suite run against a real Fabric network, and by whom?**
*For:* `./storetest` against an in-memory double proves the suite is
satisfiable, not that either backend satisfies it. Behavioural drift is only
caught where the real store runs.
*Against:* a Fabric network per commit is expensive.
*Turns on:* whether the two backends are genuinely expected to stay
behaviourally identical, or whether Postgres is a development convenience whose
divergence is tolerated. These are very different commitments and the answer
changes what the interface should promise.

**6. Can an extraction datum ever be removed?**
*Retract:* per-element keys mean omission cannot mean deletion, so retraction
must be explicit. This is `Datum.Retract`.
*Never:* on an append-only ledger a retraction is a tombstone, not a removal —
the original value stays visible in history forever. For a provenance system
that may be correct.
*Turns on:* whether a reviewer correcting a mistake should leave the mistake
legible. The product's audit posture suggests yes, which makes this a display
decision rather than a storage one.

**7. Does the Postgres backend run a stateless signature check it is not
allowed to trust?**
*Yes:* catches broken signing code in the development loop instead of at Fabric
integration. It is a lint, not a gate, so it does not weaken the chaincode-only
rule.
*No:* keeps a single unambiguous statement — verification happens in chaincode,
full stop — with no second crypto implementation to drift or be mistaken for
enforcement.
*Turns on:* whether anyone will build the signing path while developing against
demo.

---

## What this rule does not cover

- **Whether a given feature should exist.** A product question; it lives in
  `metacensus/strategy`.
- **Intent.** Whether a check is *meant* to be authoritative is not reviewable;
  whether it currently *is* the only check is.
- **The two divergent type copies.** `infra core/shared/types` and
  `core/chaincode/contracts/types` are mutually unparseable. Separate work, but
  it blocks signature verification specifically: a signature covers agreed
  bytes, and two encodings means signer and verifier compute different digests.
- **Behavioural drift between the two implementations.** Nothing in this file
  catches it. That is `./storetest`'s job, and `./storetest/README.md` states
  what it does and does not catch.
