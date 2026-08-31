# How this contract was derived

The `.proto` files describe the contract as it is. This file records how it got
that way: which sources were weighed, what was left out, what is deferred and to
whom, and the questions still open. It is here rather than in proto comments so
that the schema reads as a schema.

Nothing here is required to *use* the contract. It is required to *change* it.

## Sources, and how they are weighted

| source | weight |
| --- | --- |
| **infra** — `core/api/server/*.go`, `core/chaincode/**` | Design authority. Written deliberately, largely by hand. Implements less. |
| **demo** — `api/src/**` (Bun + Elysia) | Authoritative about *which features exist* — papers, protocols and extraction exist nowhere else. **Not** authoritative about their shape: written rapidly, with AI. |
| **`types/*.ts`** — the SPA's declarations | What the client believes. Sometimes neither of the above, and occasionally a field no backend sends. |

Where demo deviates from infra the question is *"what is the right
representation?"*, not *"which is deployed?"*. Two of those sources agreeing is
often one source counted twice: the client was largely written against demo's
ORM output.

## The bar for inclusion

**Only what we are confident we want.** A field is easy to add and very hard to
remove once something depends on it, so uncertainty resolves to leaving it out.

Two tests decide most cases:

1. **Does anything maintain this field?** A field nothing keeps current is worse
   than a missing field, because it lies. This applies regardless of which
   backend authored it — infra is not trusted by default.
2. **Is this the domain, or one implementation's artifact?** ORM junction
   wrappers, denormalised convenience lists, and surrogate ids on records with
   natural keys are artifacts.

## Conventions taken as settled

These are not conflicts and are not recorded per-message anywhere:

- Every response is a JSON object at the root. Both backends return bare arrays
  from their list endpoints today; every list here wraps them in `items`.
- Create is `POST /resource`; edit is `POST /resource/{id}`. No `/create` or
  `/edit` suffixes.
- Single-resource reads return the resource bare. infra wraps `/self`,
  `/user/{id}` and `/prop/{id}` in `{"user": …}` / `{"prop": …}`.
- All ids are strings. demo mints integer serials, infra prefixed UUIDs
  (`user:0192a642-…`), and infra's vote id is a composite object.
- Writes return the affected resource.
- `created`, not `createdAt`, on every resource that has one. The one exception
  is `Member.joined`, which names the event rather than the record.
- Enum zero values are `Unspecified`. infra emits `Undefined`.

## Endpoints excluded

| endpoint | why |
| --- | --- |
| `POST /lit-search` | NCBI `esummary` passed through verbatim. Its `result` is a map keyed by PMID whose values are heterogeneous — every key is an article object except `uids`, an array of strings — so no `map<>` holds it. Its field names (`sorttitle`, `nlmuniqueid`) would be misspelled by protojson's lowerCamelCase. And it is NCBI's schema: a change there is not a change here. |
| `GET /lit-search/{id}/abstract` | Modellable alone, but it is half of a feature whose other half is not. Literature search wants a MetaCensus-shaped projection of NCBI — the SPA reads about eight of NCBI's forty fields — and both endpoints should be designed together then. |
| `POST /domain`, `POST /category` | Flat `{id, name}` vocabularies with no infra counterpart, not even a stub route. Removed together with the `Topic` fields that referenced them; taxonomy is one design question, not three. |
| `POST /protocol` | Declared in `src/lib/routes.ts`, served by demo, called by nothing. Its handler queries the *topic* table with a protocol join, so "list protocols" returns topics. `GET /topic/{topicId}/protocol` covers the real need. |
| `GET /topic/{topicId}/my-votes` | Returns a bare array of prop ids the caller has voted on, to save the SPA a request per prop. The need is real, the shape is a denormalised projection with no infra counterpart. Better answered by carrying the caller's own vote on each `Prop`, or by a filter on the vote collection. |
| `/fabric/execute/…`, `/fabric/metadata/…` | Raw Hyperledger Fabric passthrough returning opaque chaincode bytes, gated behind `ENABLE_FABRIC_DEBUG_ENDPOINTS`. infra's own comment: it "should never be exposed on a public internet-facing API". |
| multipart branch of `POST /paper/create` | A PDF part is not a JSON document. Including it would mean base64ing a PDF into a string or pretending the file part does not exist. The upload wants its own flow — mint an upload URL, PUT to it, then create the paper. |
| `/auth`, `/org` | infra declares path constants for both (`routeAuth`, carrying `// TODO, do we need this?`) and mounts neither. `Org` is a chaincode type with working Create and Get and no HTTP surface. Worth settling before anything grows a grouping concept — demo's unreachable `group` tables already overlap it. |

Out of scope by instruction: the public backend in `server/`, at
`/metacensus/public/*`.

## Fields removed, and the question each becomes

### Nothing maintains them

| removed | finding | question |
| --- | --- | --- |
| `updatedAt` (all resources) | demo-only; infra has no concept, so a client cannot distinguish "never modified" from "not tracked" | **Deferred to the all-Go backend migration.** Revisit when both backends share an implementation. |
| `Paper.status` | Five-value enum, **one reachable value**. Both create branches hardcode `"Pending Review"` (`paper.ts:220,295`); no handler transitions it | What advances a paper through screening — stored, or derived from approvals? |
| `Paper.url` | A presigned S3 URL cached in a column; expired for most of its life. `GET /paper/{id}/presigned-url` mints a fresh one | — |
| `DataExtractionReview.userId`, `DataExtraction.userId` | Column exists, create handler never sets it, so **every review is anonymous**. Nothing stops one user satisfying a topic's minimum-reviews threshold alone | Who owns a review, and what enforces independence between reviews of one paper? |
| `User.lastActive`, `User.lastCredits`, same on `Member` | infra's own. `types.NewUser` sets `LastActive: created, LastCredits: 0`; nothing ever updates either | **Deferred, not rejected.** Tracked as [metacensus/infra#54](https://github.com/metacensus/infra/issues/54) — credit-gate user actions with global reach. Add back to `User` when that lands. |
| `Prop.conclusion`, `Prop.concluded` | demo stores `status` defaulting to `"open"` and never changes it; `concluded` is emitted as `""`, not a parseable timestamp. infra has neither, which looks deliberate | How does a proposition conclude — quorum, threshold, expiry? |
| `Topic.status`, `Topic.statusDescription` | Free text neither backend validates. The five-value enum in the first draft was inferred from the **badge-colour map** in `types/enums.ts`, a styling table mixing topic, paper and prop vocabularies | What is a topic's lifecycle, and is its state stored or derived? |

### One implementation's artifact

| removed | finding | question |
| --- | --- | --- |
| `Topic.topicCategories/Reviewers/Admins/Contributors` | ORM junction wrappers (`[{category: {…}}]`). **Only two of four are ever written** — no handler inserts into `topic_reviewers` or `topic_contributors`, so those always return `[]` | Is topic membership one relation with a role (→ `Member`), or four lists? |
| `Topic.protocol` | demo embeds the whole form in every topic, on list *and* detail. `GET /topic/{id}/protocol` serves it | — |
| `Prop.votes` | infra embeds them; demo does not. Votes are an unbounded, separately-addressable collection, so embedding makes `GET /topic/{id}/prop` carry every vote in the topic. What the UI renders is a tally | Should a prop carry a vote *tally* plus the caller's own vote? That would also subsume `/my-votes`. |
| `Prop.topicId` | infra omits it; the only route to a prop already names its topic | — |
| `Session.user` | demo returns it, infra does not. Duplicates `GET /self`; two representations that can disagree | Is the extra round trip after login worth avoiding? |
| `Vote.id` | Natural key is `(propId, userId)` — what infra's `NewVoteId` builds and demo's unique index enforces. demo exposes a serial, infra a composite object | — |
| `Paper.s3Key` | Storage plumbing; binds the contract to one deployment's bucket layout | — |
| `PaperAuthor` message | demo's JSON create branch wants `[{name}]`, its multipart branch wants `[string]`, storage holds strings. A bug, not a distinction — both sides are now `repeated string` | — |
| `ProtocolElement.conditionallyShown` | Stored by demo, read by no renderer; the SPA has no conditional-display logic | What expresses the condition? A boolean cannot. |
| `User.groups`, `GroupMembership` | Dead at both ends. demo's `group`/`user_groups` tables are referenced only by `db/seed.ts`; no route handler touches them and `relations.ts` defines no relation, so nothing is reachable. Nothing in the SPA reads `.groups`. Competes with infra's `Org` | Is the grouping concept `Org` or `Group`? Settle before either gets a surface. |
| `User.role` | Vestigial. The only consumer in the SPA is `GET /user?role=admin&limit=100` (`CreateTopic.tsx:106`); the one place a role would be *displayed* is commented out (`topic-contributors/page.tsx:62,68`). demo's README: "the `user.role` column is stored and filterable but never enforced". Note `types/Contributor.ts`'s `role` is a different field, on `Group` | Does MetaCensus want global user roles, or only per-topic standing? |
| `User.topicContributors` | demo-only, never written, and the wrong direction of a relation infra routes as `/topic/{id}/member` | Readable from the user side at all? |

### Under-determined

| removed | finding | question |
| --- | --- | --- |
| `User.jobTitle`, `User.bio` | demo-only profile fields, no infra counterpart | What is a MetaCensus user profile — part of this resource, or its own? |
| `Topic.question` | demo stores it; the SPA's create form has no input that sends it | Distinct from `description`? |
| `Topic.domain`, `Topic.minimumExtractionReviews` | Taxonomy and extraction config, deferred with their features | How is a topic classified? Is the review threshold per topic or per protocol? |
| `TopicCreateRequest.id` | demo lets a client choose the primary key; infra always mints its own | — |
| `PaperLookupResponse.publishedDate` | Write-only: the SPA posts it, no create branch has a column. `YYYY-MM-DD`, so not a `Timestamp` | Should `Paper` record a publication date? Needs a date type this contract lacks. |
| `Paper.description`, `Paper.fullTextUrl` | `description` is undistinguished from `abstract`; `fullTextUrl` is derivable from `pmid`/`doi` | Is a stored canonical link needed for a paper with neither identifier? |
| sign-up `key` (ECDSA public JWK) | JWK member names are fixed by RFC 7517 and one is `key_ops`, which protojson would spell `keyOps`. Nothing consumes it: demo accepts it, has no column, and never verifies the `X-Signature` the SPA derives from the private half | Request signing needs a design before it needs a wire shape. |

### Pagination — defined, not adopted

`ListMetadata` carries `page`, `limit` and `total`, so the shape is agreed in
advance — but no response references it and no request carries a pagination
parameter. Wiring it into a route is a separate, deliberate decision per route.

This drops behaviour demo serves today:

| endpoint | parameters | where |
| --- | --- | --- |
| `GET /topic`, `GET /user` | `page`, `limit` (clamped 1..100, default 50) | query string |
| `POST /paper`, `POST /extraction-review`, `POST /protocol-template` | `page`, `limit` (default 10) | JSON body |

infra paginates nothing and carries `// TODO Paginate this` on every `GetAll`.

**Question:** offset or cursor, query string or body, per-route or uniform?

`TestNoPaginationFields` and `TestListMetadataIsUnreferenced` enforce this.

### Removed in the route-convention pass

| removed | finding | question |
| --- | --- | --- |
| `GET /paper/{paperId}/presigned-url`, `PaperPresignedUrl{Request,Response}` | A presigned URL is a standard, correct S3 mechanism — a time-limited signed URL for one object, so a browser fetches a PDF without the API proxying bytes and without a credential reaching the client. It is dropped because **infra has no object storage at all**, so the contract would be describing one backend's feature | Where does paper-PDF storage live once both backends share an implementation? |
| `DataExtractionReview` and its list endpoints, `DataExtractionInput`, `DataExtraction{Create,Edit}Request`, `DataExtractionList{,Request}`, `DataExtractionReviewList{,Request}` | Replaced by the flat upsert (see below). The review entity keyed the old read endpoints, and it is exactly what the restructure questions | [metacensus/ui#42](https://github.com/metacensus/ui/issues/42) |
| `ProtocolCreateRequest.Draft`, the `{"protocol": …}` body envelope | demo's wrapper; flattened with the route convention | — |

## What this contract requires of implementers

- **infra must implement `GET /topic/{topicId}/prop/{propId}/vote`.** It does
  not route that method today — `VoteGet` and `VoteGetAll` are declared on its
  `DataSource` and both are commented out — because a prop's votes were
  reachable through the embedded `votes` field. With that field removed there is
  no other way to read them. This is the one place the contract asks for new
  work rather than describing what exists.
- infra: `Session` sheds `user`; enum zero values become `Unspecified`.
- demo: `Topic.createdAt` → `created`; lists wrap their arrays in `items`; writes
  return resources instead of `{"message": …}`; sign-up moves from `POST /user`
  to `POST /signup`; `/protocol/create` and `/protocol/edit` become
  `POST /protocol` and `POST /protocol/{protocolId}`; the extraction write
  endpoints collapse into one upsert.

## Behaviour found in the sources, not ratified here

Recorded because a reader of either backend will meet them:

- `GET /topic/{id}/protocol` (demo) binds its path parameter to `const _topicId`
  and calls `findFirst` with no `where`, returning whichever protocol the
  database hands back first — for every topic.
- `POST /extraction` (demo) returns **HTTP 200** with `{"error": …}` when
  required fields are missing, so a client checking `response.ok` sails past it.
- `topicId` is run through `decodeURIComponent()` despite arriving in a JSON
  body, in `POST /paper`, `POST /extraction-review` and `POST /extraction/create`.
- infra's `writeError` returns HTTP 500 for every failure, including bad input.
- demo's logout is stateless: the token stays valid until it expires. infra
  revokes it.
- demo accepts three spellings for a prop's text (`description`, `statement`,
  `text`) and two vocabularies for a vote (`position` / `value`).
- The SPA calls `GET`/`POST /topic/{id}/prop` and
  `GET`/`POST /topic/{id}/prop/{propId}/vote` with hardcoded paths, which
  `src/lib/routes.ts` explicitly forbids.
- The SPA branches on `response?.message === "Protocol created successfully"`
  and on `response?.status === 200` — English prose and a body-duplicated HTTP
  status, respectively.

## Passwords on this boundary

Passwords cross `/metacensus/api/v1` **in plaintext, under TLS**, in both
`POST /login` and `POST /user`. Both backends hash on receipt: demo bcrypts in
its sign-up handler and compares in its login handler; infra bcrypts in
`parseUserPost` before calling chaincode, and chaincode compares. No hash
crosses this boundary in either direction, and no password is ever returned.

A likely source of confusion: infra's `types.UserCreateRequest` carries
`PasswordHashB64` with a `json:"passwordHash"` tag and its name reads like an
API request type — but it is the *chaincode* request, one hop further in. The
HTTP-facing struct is a separate anonymous one in `parseUserPost` with a
plaintext `Password`.

Open question: should the contract require a client-side hash instead? That is a
behaviour change for the SPA and both backends, not a rename.
