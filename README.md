# The MetaCensus API contract, in Protocol Buffers

Every request and response under `/metacensus/api/v1` and `/metacensus/public`, defined once in `.proto` and generated into Go and TypeScript — types, a route manifest, a Go server and a TypeScript client per surface, and the signing chain. Split out of [`metacensus/ui`](https://github.com/metacensus/ui) (`contract/`); `git log` carries the derivation.

**Nothing consumes this yet.** infra, demo and the SPA still carry hand-maintained types. `v0.1.0` was tagged only to exercise the release path: the Go module is live on proxy.golang.org, but `go get` resolves that throwaway tag and `npm install` resolves `ts/package.json`'s placeholder. Neither is ready to depend on.

Policy and rationale for changing the contract — compatibility, versioning, dependencies, the toolchain, releasing — are in [AGENTS.md](AGENTS.md).

## The two surfaces

Two proto packages, because two surfaces, differing in almost every property a contract encodes:

|  | `metacensus.v1` | `metacensus.public.v1` |
| --- | --- | --- |
| prefix | `/metacensus/api/v1` | `/metacensus/public` |
| caller | an authenticated session | anyone on the internet |
| implementers | two, with different persistence | one, `metacensus/service-public-api` |
| compatibility owed | none yet | to whatever is deployed |
| breaking baseline | the latest release tag | `origin/main` |
| success body | the bare resource | the bare message, plus `ok` |

They are separate packages, not a corner of one: `TestNoMessageFieldCrossesResourceFiles` refuses a field typed across the boundary, so the laxer compatibility policy can never take charge of the stricter one's wire shape. Still **one repository and one of each artifact** — the SPA calls both from one build and is the only party that sees every surface. One Go module, one npm package, but two namespaces: Go separates them by import path, npm by a subpath export. Compatibility differs per surface; see [AGENTS.md](AGENTS.md).

### Errors on the two surfaces

**Neither surface types its failures** — the HTTP status is the machine-readable signal, the body around it is not in the contract. When to type failures, why the public surface documents a status vocabulary in `public/v1/common.proto`, and why `PartnerReceipt` still carries `ok`, are in [AGENTS.md](AGENTS.md). Whether to declare an `Error` message at all is an open question, below.

## The signing chain

Two identities answer different questions. A **login token** says who is *connected* — it grants access and permits no write on its own. A **signing key** says who *authored* a record. `Prop.author_id`/`created` were the first pretending to be the second; they're gone, and the signature carries authorship.

The signing mechanism is **WebAuthn/passkeys** — the enterprise-grade, widely-adopted way to hold a non-extractable private key on a device and prove possession. The object design follows that standard rather than a hand-rolled raw-ECDSA scheme, and **one format serves both layers**: the participant signs with a passkey, the institution countersigns headless (server/HSM), and both are the same `Signature` — a WebAuthn assertion whose challenge is the record's digest. One decoder, one primitive (**ECDSA P-256 / ES256**, WebAuthn's default), the whole chain down.

**No silent assertion is needed, because MetaCensus doesn't sign every request.** The mutable working surface (data extraction, drafts) lives in a staging store off the ledger; ledger writes are deliberate, bulk, scoped attestations — submitting votes and props for a paper. A passkey gesture at the moment a participant vouches is the correct semantic for "who vouches for this content," and the assertion even carries proof a human was verified. Non-interactive participant signing (curl, CI) has no story and isn't meant to — see [Open questions](#open-questions).

Every write on `metacensus.v1` carries `content`, an `interpretation`, and a `userSignature` over them (`TestEveryWriteCarriesASignature`; exceptions are `unsignedWrites` in `go/contract/signing_test.go`). The public surface is exempt — it has no identities. A stored record is the signed content wrapped, never modified:

```json
{
  "id": "...", "recorded": "2023-11-14T22:13:20Z",
  "content": { "topicId": "...", "type": "Statement", "description": "..." },
  "interpretation": { "spec": "metacensus.sig/2", "contentType": "metacensus.v1.Prop" },
  "userSignature": {
    "keyId": "...", "time": "...",
    "assertion": { "authenticatorData": "...", "clientDataJson": "...", "signature": "..." }
  },
  "institutionalSignature": { "keyId": "...", "time": "...", "assertion": { "...": "..." } }
}
```

WebAuthn doesn't sign arbitrary bytes — it signs `authenticatorData ‖ SHA-256(clientDataJSON)`, with the content digest carried inside `clientDataJSON.challenge`. So the chain is a **digest** and a **wrapper around it**:

```
challenge = SHA-256( JCS( {"content": C, "interpretation": I, "keyId": K, "time": T} ) )
assertion.signature = ECDSA( authenticatorData ‖ SHA-256(clientDataJSON) ),  clientDataJSON.challenge = challenge
```

`C` is the content as the contract's JSON, `I` the interpretation, `K`/`T` the signature's own `keyId`/`time`. **`signing.VerifyUser` is the one to call — do not write a second implementation of this digest.** The **verify procedure** is:

1. **Resolve the key.** `keyId` selects the signer's enrolled key from persistence; that key is the trust anchor. The record never carries its own verification key (a `publicKey` on a general record is a footgun — forge content, key and signature and it "verifies"). The one exception is sign-up (below).
2. **Recompute the digest** from the record's own `content`, `interpretation`, `keyId` and `time`, and confirm it equals `clientDataJSON.challenge` (base64url). Nothing is reverse-engineered — the challenge is rebuilt from stored inputs.
3. **Verify the signature** over `authenticatorData ‖ SHA-256(clientDataJSON)` under the resolved key. `clientDataJSON` is hashed as stored and only parsed to read `challenge`/`type` — never re-serialised, so a passkey's browser-minted bytes are not a second canonicalization problem.
4. **Apply the layer's acceptance policy** — the *only* honest difference between the layers, a per-layer check, not a second code path. A participant requires `clientDataJSON.type` `webauthn.get`, a known `origin`, and the user-verified flag; the institution requires the countersign `type`, no browser origin, and honest flags. A headless signer must **never forge passkey metadata** — the flags and type are the custody signal a verifier reads (synced vs device-bound is readable from the `BE`/`BS` flags).
5. **The institution nests over the participant.** `institutionalSignature` is the same `Signature`, but its challenge is `SHA-256( JCS( {"signature": userSignature, "interpretation": I, "keyId": K, "time": T} ) )` — it vouches for the whole `userSignature`, so altering the participant's signature invalidates the countersignature.

The **interpretation** is why a record self-describes: append-only, it can be neither re-signed nor allowed to assume the surrounding schema holds still, so it carries the signing scheme (`spec`, an atomic version of canonicalization + digest + curve + encoding) and the payload type (`contentType`). Deriving the type from the current envelope would fail the moment that schema drifts. It is a record property, identical for both signatures, and sealed by both digests.

**Enrollment (sign-up) is the one trust-on-first-use write.** No key is enrolled yet, so `SignUpRequest` carries the enrolling `public_key` — on the request, not on every `Signature` — and the store binds it under `user_signature.key_id`, which must thumbprint it. The field comment and `signing.EnrolledKey` carry the rest.

Two more facts reach the schema. **Every id the path binds is inside the signed content**; server-minted ids are not, and can't be — an id that changes what the content *means* is covered, one that is merely its *address* need not be, and `routegen` refuses a signed route that omits a path-bound id. And **no 64-bit number crosses the digest**: JCS prints numbers as ECMAScript would, so `TestNoWideNumbersCrossJCS` holds the "JSON is the wire" rule below to what both languages print identically.

**Verification happens behind persistence, not at the API edge** — the signature travels in the body so it reaches the chaincode boundary intact; `Runtime.VerifyBody` and the `X-Signature` header are gone. **Authorship binds to the key, resolved below the store seam**: `signerId` is gone from the wire, since it is `keyId`'s owner, not a client claim; the service fast-fails only that a caller is present and that a vote's `user_id` is the caller. **Ordering uses `recorded` (the server's observation), never `time` (the participant's claim)**, or a participant could order their own writes. A stored record carries an `institutional_signature` and reserves the field after it, so the next signature layer is a pure addition (`TestSignedRecordsReserveTheNextField`).

**`ts/test/wire.test.mjs` pins the wire and the cross-language chain:** protojson and ts-proto must emit the same document, and a signature Go makes must verify in TypeScript — the DER assertion Go writes, TypeScript recomputes the challenge for and verifies. The two languages signing in conjunction across the full write path waits on an integration suite over mock persistence ([#31](https://github.com/metacensus/api/issues/31)); the signing module's own behaviour is unit-tested in `ts/test/signing.test.mjs`. How to sign in each language, and the browser passkey ceremony, are in [ts/README.md](ts/README.md), "Signing", and the [`go/signing`](go/signing/signing.go) package comment.

## JSON is the wire

Protobuf binary is not used, supported, or a fallback — protobuf is here for the schema, code generation and breaking-change detection.

- **Marshal with `protojson`, never `encoding/json`** — `go/` exports `Marshal`, `Unmarshal`, `MarshalOptions`. Under `encoding/json` the generated types produce enums as integers and timestamps as `{"seconds","nanos"}`.
- **Field names are `lowerCamelCase` on the wire** (protojson converts; no `json_name` overrides). **Enum values are PascalCase**, nested in their message, zero value `Unspecified`. **All ids are strings.**
- **No 64-bit integer, float or double** — use `int32`/`uint32` or a string (see the signing chain).
- **Presence is expressed only through message-typed fields.** `EmitDefaultValues` + ts-proto's `useOptionals=messages` makes scalars/enums/repeated always present and message fields `?: T | undefined`; a proto3 `optional` scalar is a third case the two sides would disagree about — use `google.protobuf.Int32Value` and friends.

## Routes

Each resource file declares its routes (`topic.proto` → `TopicRoutes`); **to read the whole table, read the generated manifest** — [`go/server/routes`](go/server/routes/manifest.go) or `ts/src/route-manifest.ts`. `params`, `query` and `body` account for every field of a request message, so a request message models the whole request, not only its body.

The prefixes (`routes.Prefix`, `routes.PublicPrefix`) are part of the contract and generated from `model.Packages`; every `path` is relative to one, and each route carries its own `prefix` so a consumer holding a `Route` needn't guess which to join. `TestPrefix` pins the invariants (each absolute, no trailing slash, none nested); `TestNonConformingRoutes` pins the conventions at zero exceptions. They are route declarations, not a gRPC commitment — nothing generates or serves gRPC.

## Layout

**Two Go modules.** The contract, which consumers import, and the generator, which nobody does.

```
proto/            .proto sources and buf config — both surfaces
go/               the contract in Go (go/metacensus/{v1,public/v1})
go/contract/      the wire encoder and the whole-contract schema tests
go/server/        generated handler interfaces + registration, hand-written runtime
go/server/routes/ the generated route manifest (routes.Prefix, routes.Routes)
go/signing/       the signing chain: JCS, the digest, sign and verify
go/store/         the persistence port the two backends implement
go/auth/          the session port + a development placeholder
go/service/       the handlers: store + auth + minting, behind the server interfaces
ts/               the contract in TypeScript — the npm package
                  (ts/index.ts authenticated, ts/public.ts public, ts/signing.ts signing)
internal/         protoscan, the .proto tree scan both modules check against
routegen/         module 2: the generator, renderers, templates, pinned tools,
                  and the suites that exercise what it emits (chitest, wireserver)
scripts/          version.sh, which make release uses to mint tags
```

| | Holds | May require | Imported by |
|---|---|---|---|
| `github.com/metacensus/api` | the contract | little, each addition weighed | consumers |
| `github.com/metacensus/api/routegen` | generation and its tests | anything — chi, buf, protoc-gen-go | nobody, ever |

The split has one reason: a `tool` directive is a real module requirement, so buf's ~90 transitive requirements and chi would land on every consumer. Both are generation, so both live in module 2, which is `replace`d onto the working tree. **The Go packages sit under `go/`** (imports read `github.com/metacensus/api/go/metacensus/v1`) and **`go.mod` is at the repository root** — both one-way doors once a tag exists, both explained in [AGENTS.md](AGENTS.md).

## Consuming it

```bash
go get github.com/metacensus/api          # import github.com/metacensus/api/go/...
npm install @metacensus/api               # types, route manifest, a client per surface
```

```go
import v1 "github.com/metacensus/api/go/metacensus/v1"               // authenticated
import publicv1 "github.com/metacensus/api/go/metacensus/public/v1"  // public
import "github.com/metacensus/api/go/server/routes"                  // both surfaces, one table
import "github.com/metacensus/api/go/signing"                        // the signing chain
```

```ts
import type { Topic } from "@metacensus/api";                    // authenticated
import type { PartnerSubmission } from "@metacensus/api/public"; // public
import { sign } from "@metacensus/api/signing";                  // the signing chain
```

**One entry point per concern**, the set derived by `check-entry-points.mjs` from `package.json`. A consumer of only the public surface imports `@metacensus/api/public` and does not acquire the authenticated types; `@metacensus/api/signing` splits by concern so a consumer who only wants `Topic` needn't resolve a pile of WebCrypto. Go needed no equivalent — its packages were already separate. A consumer's Go must satisfy the `go` directive in the root `go.mod`. Client usage is in [ts/README.md](ts/README.md).

## Working on it

```bash
make          # list every target
make gen      # regenerate Go, TypeScript, the manifest, the server and the client
make check    # everything CI runs, bar the freshness diff
make hooks    # optional: lint, format and freshness checks on commit
```

From a fresh clone nothing has to be installed first — `gen` and `check` build the pinned generators and install the npm toolchain as prerequisites. **Generated code is committed, and `make gen` is the only way to write it** — CI regenerates and fails on any diff. The written paths are named once in the Makefile's `GENERATED`; `routegen` refuses to run outside the repository root, since its output paths are relative. The toolchain pins, and the traps around them, are in [AGENTS.md](AGENTS.md).

## The generated server

`routegen` writes `go/server/routes_gen.go` per service: a handler interface with one method per rpc (path, query and body already bound), an `Unimplemented<Service>` answering 501, and a `Register<Service>` that binds and dispatches. **`go/server`'s own comments are the account of itself** — `runtime.go` for the package, `Mux`'s doc comment for what it leaves to the router. `Unimplemented<Service>` is the whole default; persistence is entirely the implementer's. Two facts decide how you mount:

- **`Runtime.Prefix` is literal, and `""` means no prefix.** A router already mounted at the contract's prefix wants the zero value; one at the origin root wants `routes.Prefix`. A process serving both surfaces needs one `Runtime` per surface — sharing one mounts a surface under the wrong prefix, silently.
- **Any `Mux` that is not a `StdMux` must set `PathValue`**, and a `chi.Router` wants `server.EscapedPathValue` — `ServeMux` percent-decodes a segment, chi does not, and the client percent-encodes every one. `Register<Service>` panics until it's set. A service whose rpcs don't share one middleware needs `server.Except`.

```go
type topics struct{ server.UnimplementedTopicRoutes }

func (topics) GetTopic(ctx context.Context, req *v1.TopicGetRequest) (*v1.Topic, error) {
	t, err := store.Lookup(ctx, req.TopicId) // your persistence, your choice
	if err != nil {
		return nil, server.Errorf(http.StatusNotFound, "not_found", "no such topic")
	}
	return t, nil
}

mux := http.NewServeMux()
server.RegisterTopicRoutes(server.StdMux{ServeMux: mux}, &server.Runtime{Prefix: routes.Prefix}, topics{})
```

On a `chi.Router`, set `PathValue: server.EscapedPathValue` and register inside whatever it's mounted under; `server.Except(authed, v1, rt, "AuthRoutes.Login", "AuthRoutes.SignUp")` sends the public rpcs to a second router. `routegen/chitest` pins both routers' behaviour.

## The service layer

`go/service` implements those handler interfaces over two ports, so a backend supplies persistence and nothing else — no routing, no request handling. `service.New(service.Config{Store: …})` returns the handlers; `h.Register(mux, rt)` mounts every service, wrapping all but the health check, login and sign-up in the session middleware.

- **`go/store`** is the persistence port: a write takes a fully-minted `*v1.XSigned` and returns only an error, in a closed vocabulary (`store.KindOf`) the service maps to an HTTP status. `metacensus/demo` (Postgres) and `metacensus/infra` (Fabric) each implement it; the service's tests run against an in-memory fake.
- **`go/auth`** is the session port — token to `callerID` — with an in-memory placeholder. Real authentication is deferred behind the interface (see [Open questions](#open-questions)); nothing in `go/auth` is a security boundary today.

Per request it resolves the caller, mints the `id` and `recorded` (record-level, outside the signed content, so a signature survives assembly), hashes the password above the seam, calls the store once, and maps its `Kind` to a status. It verifies no signature, and fails fast only on checks the store re-enforces — a caller is present, and a vote's `user_id` is that caller. Binding the author to the caller (the key `keyId` resolves to must be the caller's) needs the key history, so it is the store's, below the seam; no record carries a signer id for the edge to shortcut with. `go/service`'s package comment is the account of itself.

## The generated client

The same walk writes `ts/src/client.ts`: one **batteries-included** client per surface — `Client` for the authenticated API, `PublicClient` for the public one. Constructed with `{ baseUrl, fetch?, signer?, token? }`, `Client` owns its HTTP — it builds the path, holds the session token across `login`/`logout` (adding `Authorization` itself), and signs writes through the injected `signer`. Reads return flat records (`getTopic` → `TopicRecord`, `{id, recorded, ...content}`); a signed-envelope read also gets a `getTopicSigned` twin returning the raw envelope, for verifying authorship. Writes take flat content, never an envelope. A 2xx is parsed and cast; a non-2xx throws `ApiError` (status, url, raw text). `ApiError` is one class across both entry points, which is why both classes share one file. Usage and the public-surface variant are in [ts/README.md](ts/README.md).

## Open questions

Written down, not tracked — the four `ui` issues that held this work were closed as not-planned on the move, and nothing replaced them. Each is a decision nobody has standing to take until the consumer that would settle it exists.

- **Should the contract declare an `Error` message?** Today the server emits an ad hoc `{"error","code"}` and clients hand back the text unparsed. Exactly the schema that goes wrong when invented before a second consumer. Refs [#3](https://github.com/metacensus/api/issues/3).
- **No write path verifies a signature yet.** `signing.VerifyUser`/`VerifyCountersign` implement the verify procedure above and the acceptance policy, but no repository calls them on a write. What that leaves to a backend, beyond the procedure: refuse a vote whose `user_id` is not the resolved author, bind `authenticatorData`'s rpId hash to the expected RP per environment (left to verifier config, not done in `signing`), and hold a `time`/`recorded` tolerance in chaincode config.
- **Key rotation is anticipated; revocation is not designed.** Per-device and synced passkeys mean several keys per participant over time, which the key-history-by-timestamp model already allows — a record stays verifiable forever by resolving the key in force at signing time. Recovery is: regain account access, enrol a new key; the old one is retired in the history, never re-signed. But **revocation — a lost key must not attribute *new* records while the records it already signed stay valid — is undesigned**, and no route enrols a second key yet (sign-up is the only enrolment). Synced-vs-device-bound is a custody distinction readable from the assertion's `BE`/`BS` flags, recorded honestly rather than as a field.
- **Non-interactive participant signing has no story.** A passkey assertion needs a user gesture and a browser authenticator, so curl, CI and any headless *participant* cannot produce one — deliberately (see "The signing chain"). The institutional layer is headless by design; a headless participant would need a different enrolled key type, which nothing defines. Refs [ui#55](https://github.com/metacensus/ui/issues/55).
- **The browser passkey ceremony lives in the consumer.** `go/signing` and `ts/src/signing.ts` expose the format primitives — build the digest, verify an assertion, sign headless — but `navigator.credentials.create()/get()` is the SPA's, tracked in [ui#55](https://github.com/metacensus/ui/issues/55) (key provisioning). The generated TypeScript client's `Signer` is the seam it plugs into.
- **Authentication is undesigned.** `go/service` resolves a caller through the `go/auth` session port, but the only implementation is an in-memory placeholder: a random token mapped to a `callerID`, no expiry, no persistence, no cryptographic binding. What a token should be, where session state lives, and how any of it improves under a real "authn" story are all open; the port exists so the answer slots in without touching the service.
- **Should the client, or a zero-valued query parameter, be split out / emitted?** No route declares a query field, and `sideEffects: false` already lets a bundler drop the client, so neither is urgent. Refs [#4](https://github.com/metacensus/api/issues/4).
- **Cross-language wire agreement is checked for the shapes the contract has, not the ones it could grow** — a 64-bit integer, a map, a `oneof` are untested, so the pairing in `proto/buf.gen.yaml` is only checked where a route exercises it.
