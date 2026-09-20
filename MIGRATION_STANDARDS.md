# Standards for migrating requests into this contract

Derived from the requests already migrated into `metacensus.v1` and
`metacensus.public.v1` (auth, topic, prop/vote, user, partner) — not aspirational,
only what's actually established and, where noted, mechanically enforced. Use this
to reshape an un-migrated request before it lands here; don't carry its old shape
over on the assumption it was already fine.

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

## Applying this to the migration

Any un-migrated request that currently uses PUT/DELETE, verb-suffixed paths,
numeric ids, ad hoc per-endpoint error shapes, or an id embedded only in the path
needs reshaping to match the above — that reshaping is the actual migration work,
independent of whatever the new signing mechanism ends up being.
