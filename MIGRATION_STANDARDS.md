# Epic: Establish system-wide request standards and consolidate project requests

## Summary

Establish one enforceable standard for API requests across the system, then bring
requests currently defined in other projects into this contract. The result is a
single source of truth for request schemas, routes, generated clients, and server
bindings rather than project-specific definitions that can drift.

This Epic is complete only when:

1. the standard is agreed, documented, and mechanically enforced;
2. every existing request in every participating project has been inventoried;
3. each inventoried request has an explicit disposition; and
4. all requests selected for consolidation have been reshaped and merged through
   tracked GitHub sub-issues.

Here, **request** means an externally reachable API operation and its complete
contract: HTTP method and path, path/query/body fields, request and response
messages, identity rules, and compatibility expectations.

## Why this work is needed

Requests have evolved in multiple projects. Copying their current shapes into this
repository would preserve inconsistencies and leave multiple authoritative
definitions in place. The migration therefore has two equally important outcomes:

- define the system-wide target standard; and
- consolidate requests from other projects into that standard, changing their
  shape where necessary instead of treating the old implementation as canonical.

## Goals

- Publish one target standard for authenticated and public requests.
- Make protobuf the source of truth for request and response types.
- Generate route manifests, Go types, server bindings, and TypeScript clients from
  that source.
- Discover all requests still owned or duplicated by other projects.
- Decide whether each request will be merged, replaced by an existing request,
  retired, or explicitly deferred.
- Move requests selected for consolidation into this contract without carrying
  incompatible legacy shapes forward.
- Add automated checks for rules that should not depend on reviewer memory.
- Give consuming projects an explicit adoption and legacy-removal path.

## Non-goals

- Finalize the new signing or authentication design.
- Preserve legacy request shapes when coordinated consumers can migrate.
- Introduce speculative schemas that no known consumer requires.
- Type error bodies unless an implementation exposes a stable machine-readable
  discriminator not already represented by HTTP status.

## Workstreams and required GitHub sub-issues

### A. Ratify and enforce the request standard

Create focused sub-issues for any standard below that is not yet mechanically
enforced. Each issue must identify the rule, enforcement point, fixtures or tests,
and any deliberate exceptions.

### B. Inventory requests in every source project

Create one inventory sub-issue per source project. The issue must:

- name the source project and an accountable owner;
- list every externally reachable request;
- identify all callers and deployed compatibility constraints;
- link the current handler, request/response types, and tests;
- record whether an equivalent contract already exists here; and
- assign one disposition to every request: **merge**, **replace**, **retire**, or
  **defer with rationale and owner**.

An inventory issue is not complete while any reachable request has no disposition.

### C. Merge request families into this contract

Create separate implementation sub-issues for cohesive request families or
resources discovered by the inventories. Several migration issues per source
project are expected. Do not create one oversized “migrate project” issue when
request families can be reviewed and released independently.

Every merge sub-issue must include:

- source project, source route, and known callers;
- current method/path and target method/path;
- current request/response shape and target proto messages;
- differences from the target standard below;
- ID and ownership semantics, including parent/actor relationships;
- authenticated or public surface and its compatibility policy;
- rollout order for contract, service, and callers;
- data migration or translation requirements, if any;
- legacy code and duplicate schema to remove;
- tests and generated artifacts that prove conformance; and
- blockers involving signing or auth, isolated behind the seams in Section 2.

A merge issue is complete only when the contract is generated and tested, known
callers use it, and the prior authoritative definition is removed or has a
separately owned removal issue with a deadline.

### D. Adopt the contract in consuming projects

Create adoption sub-issues where a consumer cannot move in the same change as the
contract. Each must identify the version adopted, rollout ordering, compatibility
window, verification needed, and the date or condition for deleting the legacy
path.

### E. Close migration gaps and remove duplication

Track compatibility shims, old routes, hand-written request types, and duplicate
schemas explicitly. A shim is temporary migration work, not a second standard; it
must have an owner and removal condition.

## Epic acceptance criteria

- [ ] The standards in Sections 1–3 are reviewed and accepted as the system-wide
      target.
- [ ] CI enforces every rule identified as mechanically enforceable, or a linked
      issue records why enforcement is deferred.
- [ ] Every participating project has a completed request inventory sub-issue.
- [ ] Every inventoried request is marked merge, replace, retire, or defer.
- [ ] Every merge disposition has one or more linked implementation sub-issues.
- [ ] Every migrated request is represented in protobuf and generated into the
      route manifest, Go surface, server binding, and TypeScript client as
      applicable.
- [ ] Every migrated request conforms to route, ID, envelope, naming, and
      compatibility standards, with deliberate exceptions documented and tested.
- [ ] Known consumers have migrated or have linked, owned adoption issues with
      explicit compatibility windows.
- [ ] Superseded routes and duplicate request definitions are removed, or their
      time-bounded removal issues are linked.
- [ ] The final inventory contains no request with an unknown owner or unresolved
      disposition.

## Suggested sub-issue naming

- `Inventory <project> requests and assign migration dispositions`
- `Merge <resource/request family> requests from <project>`
- `Adopt generated <resource> contract in <consumer>`
- `Remove legacy <resource> request definitions from <project>`
- `Enforce <request standard> in contract tests`

## Target request standard

Derived from the requests already migrated into `metacensus.v1` and
`metacensus.public.v1` (auth, topic, prop/vote, user, partner) — not aspirational,
only what's actually established and, where noted, mechanically enforced. Use this
to reshape an un-migrated request before it lands here; don't carry its old shape
over on the assumption it was already fine. "Un-migrated" covers requests still
living in other projects and any route already in this repository that predates
these standards settling. Either way, the target shape in
[Section 1](#1-current-standards) is the same, and
[Section 4](#4-migration-backlog-and-issue-decomposition) defines how the work is
split into GitHub sub-issues.

Signing and auth are excluded on purpose — both are being redesigned, and pinning
today's mechanics here would just have to be undone. [Section 2](#2-signing--auth-kept-flexible)
says what to preserve structurally instead.

## 1. Current standards

### 1a. Route path + HTTP method

- **Two explicit prefixes, never assumed**: `routes.Prefix` (`/metacensus/api/v1`,
  authenticated) and `routes.PublicPrefix` (`/metacensus/public`). Every
  [`Route`](go/server/routes/manifest.go) carries its own prefix so nothing has to
  infer which surface it belongs to. No prefix may be a substring of another —
  checked by `TestPrefix`.
- **Resource nouns, hierarchical nesting that mirrors real ownership**: `/topic`,
  `/topic/{topicId}`, `/topic/{topicId}/prop/{propId}`, `.../prop/{propId}/vote`. A
  prop only exists under a topic; the path says so.
- **Path params are `{lowerCamelCase}`** even though the underlying proto field is
  `snake_case` (`topic_id` → `{topicId}`) — it's the wire convention (protojson),
  applied consistently to paths too.
- **GET is exclusively for reads, POST for writes — no PUT/PATCH/DELETE exist
  anywhere in the contract.** Mechanically enforced: `isRead` in
  [`go/contract/routes_test.go`](go/contract/routes_test.go) treats any RPC named
  `List*`/`Get*`/`Lookup*` as a read and fails the build if it isn't wired to `GET`.
- **No verbs in the path, ever.** `verbSegment` in the same test file fails CI on
  `create`/`edit`/`update`/`delete`/`lookup` appearing as a path segment. The verb
  belongs in the HTTP method and the RPC name, not the URL — `POST /topic`, not
  `POST /topic/create`.
- **"Set" is the verb for upsert-style writes** where there's naturally only one
  live value per (parent, actor) — `SetVote` is `POST .../vote` and replaces the
  caller's prior vote rather than appending. Carry this forward for any migrated
  endpoint that's really "the current X for this actor," rather than inventing PUT
  for it.
- **Singleton-by-context pattern**: `GET /self` returns the caller's own user
  record with no id in the path at all — a reusable shape for "resource, but
  scoped to who's asking."
- One `.proto` file per resource declares its own service (`TopicRoutes`,
  `PropRoutes`, ...); everything else — Go types, the route manifest, the
  generated server, the TS client — is generated from it. There is no
  hand-maintained routing table to keep in sync.

### 1b. IDs — existing and new

- **All ids are strings on the wire, full stop** — never numeric (README, "JSON is
  the wire"). No 64-bit int/float/double ever crosses the wire either (protojson/JS
  number-precision limits) — use `int32`/`uint32` or a string.
- **Server-minted ids are opaque, non-sequential**: 16 random bytes, base64url —
  see [`go/service/service.go`](go/service/service.go) (`NewID`). Not
  auto-increment, not derived from content. A migrated resource that currently has
  a sequential/numeric id should get a new opaque string id minted the same way,
  not a passthrough of the old numeric one.
- **Classify every id by whether it changes what the record *means*, or is merely
  its *address*.** An id that's part of the record's meaning (e.g. which topic a
  prop belongs to) belongs inside the resource's content and gets re-asserted
  there even though it's already in the path — see `PropCreateRequest`'s
  `topicId`, bound by path *and* repeated in content: "the two must agree." An id
  that's just the record's own address (the server-minted `id` on `TopicSigned`)
  sits outside content. This classification is independent of signing mechanics
  and should carry over regardless of how auth ends up working.
- **Composite/natural keys are used deliberately, not by default**: `Vote` has no
  id of its own — it's keyed by `(propId, userId)`, and a resubmission replaces
  rather than appends (see [`proto/metacensus/v1/prop.proto`](proto/metacensus/v1/prop.proto)).
  Reach for this only when identity really is "one live value per parent+actor";
  everything else gets a minted id.
- Never let a client submit or guess an id for a resource that gets one minted —
  the unsigned "content" message never contains the resource's own id (`Topic` has
  no `id` field; `TopicSigned` does).

### 1c. Body / object structure (typing)

- **One schema, generated everywhere**: protobuf is the source of truth; JSON is
  the actual wire format via `protojson` (never `encoding/json`) so Go and TS agree
  byte-for-byte. Migrated resources should be modeled as `.proto` messages from day
  one, not hand-typed JSON that gets a schema retrofitted later.
- **Field naming**: `lowerCamelCase` on the wire, no `json_name` overrides — a
  straight mechanical conversion from the proto's `snake_case`. Enum values are
  `PascalCase`, always nested in their message, and the zero value is always
  literally `Unspecified` (never a real, selectable option).
- **Presence is only ever expressed through message-typed fields.**
  Scalars/enums/repeateds are always present with their zero value; a genuinely
  optional scalar becomes a wrapper type (`google.protobuf.Int32Value`, etc.)
  rather than proto3 `optional`. This is what keeps Go and TS from disagreeing
  about `null` vs `0` vs absent.
- **Request messages model the entire request, not just the body.** `params`,
  `query`, and `body` between them account for every field of a request message —
  generation enforces this — so a client can derive full routing behavior from the
  type alone.
- **Standing envelope split: server-owned record metadata vs. author-submitted
  content.** `{id, recorded, content, userSignature, ...}` — `id` and `recorded`
  are record-level facts the server stamps; everything the caller actually asserts
  lives in `content`. Even with signing wholly redesigned, keep this separation:
  never let a server-owned field (id, timestamps, status) sit in the same object
  the client posts.
- **List responses share one shape**: `{ items: [...] }` for every list
  (`TopicList`, `PropList`, `VoteList`, `UserList`, `MemberList`) — never the
  resource's own plural key. A `ListMetadata{page, limit, total}` envelope already
  exists in [`proto/metacensus/v1/common.proto`](proto/metacensus/v1/common.proto),
  unused today but ready — reuse it rather than inventing pagination per migrated
  resource.
- **Naming pattern**: `{Resource}` (unsigned, author-submitted content) /
  `{Resource}Signed` (stored envelope) / `{Resource}{Action}Request` /
  `{Resource}List`.
- **Errors are untyped by design on both surfaces** — the HTTP status is the only
  contractual signal; body text is not guaranteed. If a migrated backend needs a
  stable machine-readable failure code, the open plan (not yet built) is one
  shared `Failure` message across both surfaces — not a bespoke error shape per
  endpoint. Don't invent a per-resource error type during migration.
- The one deliberate cross-surface difference: the public surface's success
  bodies include `ok: true` (a mechanical necessity — the strict unmarshaler
  rejects an otherwise-empty/unknown-field body — not a style choice; see
  [`proto/metacensus/public/v1/partner.proto`](proto/metacensus/public/v1/partner.proto)).
  Treat that as a narrow, understood exception, not a pattern to spread.

## 2. Signing & auth — kept flexible

Both are actively being redesigned, so nothing above leans on them. What's worth
preserving structurally, independent of whatever mechanism lands:

- Keep "what the author asserts" as a distinct sub-message, separable from
  record-level server metadata — whatever wraps it later (a signature envelope, or
  nothing) can wrap this same content unchanged.
- Keep meaning-bearing ids duplicated inside that content sub-message, not only in
  the path — this is what let the current signing scheme get bolted on without a
  wire break, and costs nothing if signing changes shape again.
- Resolve caller identity and any future verification behind a port/interface
  (mirroring `go/auth.Sessions` and the store-side verification seam in
  `go/service`), never inline in a handler — so migrated handlers don't have to be
  touched again when auth changes.
- Don't design new endpoints assuming today's specific token/session shape is
  final.

## 3. General API design principles, and this project's take on them

**General principles for well-designed APIs:**

- Nouns in URLs, not verbs; a small, consistent HTTP method vocabulary matched to
  semantics.
- One schema drives everything downstream — server, client, docs — so they can't
  drift.
- A handful of consistent envelope shapes (resource, list, error) rather than a
  bespoke shape per endpoint.
- Explicit separation between client-asserted and server-owned fields.
- Compatibility policy decided deliberately per audience, not applied uniformly by
  default.
- Errors carry a machine-readable signal, not just prose.
- Conventions are mechanically checked, not just documented, so they can't
  silently rot.
- Additive-only evolution for surfaces with consumers you can't coordinate with
  (e.g. a public form hit by a rolling deploy of unknown SPA builds).
- Prefer zero exceptions, or a small tracked list of deliberate ones, over ad hoc
  per-endpoint judgment calls.

**How this project specifically applies that** (full rationale in
[AGENTS.md](AGENTS.md) and [README.md](README.md)):

- **Contract-as-code, not contract-as-documentation**: every convention above that
  matters (GET for reads, no verbs in paths, no cross-resource-file field sharing,
  every write signed, prefixes non-nested) is pinned by an actual test in
  `go/contract/*_test.go` — a violation fails CI, it doesn't just look wrong in
  review.
- **Compatibility is decided per-surface, matched to who's actually depending on
  it**: the authenticated API owes no compatibility yet (its only consumers ship
  in lockstep); the public surface owes compatibility to whatever's deployed, so
  it evolves additively only, and a compatibility test compares against
  `origin/main`, not a stale tag.
- **Dependencies are weighed, not banned by policy or accepted by default**
  ("skeptical curiosity" in AGENTS.md) — cost of ownership vs. cost of not using
  it, evaluated per case.
- **Speculative schema is deliberately deferred**: there's no `Error` message
  today specifically *because* there's no second consumer yet to validate a
  guessed shape against — the project would rather under-build now than lock in a
  wrong shape early. Apply the same discipline to migrated endpoints: don't invent
  structure a second consumer hasn't yet demanded.

## 4. Migration backlog and issue decomposition

The Epic is the roll-up, not the route-level backlog. Record source-project
inventories and implementation work as linked GitHub sub-issues so ownership,
dependencies, rollout, and completion remain visible.

As of this writing every route in
[`go/server/routes/manifest.go`](go/server/routes/manifest.go) conforms to
Section 1: it is generated from `.proto` and mechanically checked. The migration
backlog begins with routes discovered in other projects and with any future route
in this repository that predates or bypasses these standards.

Use these issue boundaries:

- **One inventory issue per source project.** This establishes completeness and
  assigns a disposition to every request.
- **One merge issue per cohesive resource or request family.** Include all routes
  that must change together to preserve useful behavior.
- **One adoption issue per independently deployed consumer when needed.**
- **One removal issue per legacy surface when removal cannot be part of adoption.**
- **One enforcement issue per cross-cutting rule**, rather than repeating the
  same test work in every route migration.

Do not use a sub-issue merely as a checklist item. It must have an owner, testable
acceptance criteria, dependencies, and enough current/target detail to review the
contract change.

### Merge sub-issue template

**Title:** `Merge <resource/request family> requests from <project>`

**Current state**

- Source project and owner:
- Current routes and handlers:
- Current request/response definitions:
- Known callers:
- Deployment and compatibility constraints:

**Target contract**

- Authenticated or public surface:
- Target RPCs and method/path pairs:
- Target proto messages:
- ID and ownership model:
- Standards changed or newly enforced:

**Migration plan**

- Contract and generation changes:
- Service implementation changes:
- Caller adoption order:
- Data translation or migration:
- Compatibility shim, owner, and removal condition:
- Legacy routes/types/files to delete:
- Signing/auth blocker or preserved seam:

**Acceptance criteria**

- [ ] Protobuf defines the complete request and response contract.
- [ ] Generated route manifest, Go code, server binding, and TypeScript client are
      current.
- [ ] Contract and compatibility tests pass.
- [ ] All known callers have adopted the target contract or have linked adoption
      issues.
- [ ] Duplicate definitions and legacy routes are removed or have a linked,
      owned, time-bounded removal issue.

### Common focused migration issues

- `Reshape <METHOD> <old-path> as <METHOD> <new-path>`
  - Remove verb path segments and align the RPC name and HTTP method with
    [Section 1a](#1a-route-path--http-method).
- `Replace <resource> numeric ID with a minted opaque string ID`
  - Mint via `service.NewID`; apply the meaning-versus-address classification in
    [Section 1b](#1b-ids--existing-and-new).
- `Model <resource> request family in protobuf`
  - Apply the naming and envelope rules in
    [Section 1c](#1c-body--object-structure-typing), then generate all supported
    language and routing surfaces.
- `Remove duplicate <resource> request contract from <project>`
  - Delete the former source of truth after consumers adopt the generated
    contract.

## 5. Applying the standard during consolidation

Any request using PUT/DELETE, verb-suffixed paths, numeric IDs, ad hoc
per-endpoint error shapes, hand-written JSON contracts, or a meaning-bearing ID
embedded only in the path must be reshaped as part of its merge issue. That
reshaping is migration work, not optional cleanup, and is independent of the
future signing mechanism.

When two projects expose overlapping requests, the inventory must identify the
overlap and the merge issue must select one target contract. Do not preserve both
definitions under different names unless a documented compatibility constraint
requires a temporary shim with an owner and removal condition.