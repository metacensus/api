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

Two identities answer different questions. A **login token** says who is *connected* — it grants access and permits no write on its own. A **signing keypair** says who *authored* a record. `Prop.author_id`/`created` were the first pretending to be the second; they're gone, and the signature carries both facts.

Every write on `metacensus.v1` carries `content` and a `userSignature` over it (`TestEveryWriteCarriesASignature` makes that a property; exceptions are `unsignedWrites` in `go/signing_test.go`). The public surface is exempt — it has no identities. A stored record is the signed content wrapped, never modified:

```json
{
  "id": "...", "recorded": "2023-11-14T22:13:20Z",
  "content": { "topicId": "...", "type": "Statement", "description": "..." },
  "userSignature": {
    "signerId": "...", "keyId": "...", "alg": "Es384", "publicKey": "",
    "signingTime": "...", "spec": "metacensus.sig/1",
    "contentType": "metacensus.v1.Prop", "value": "..."
  }
}
```

```
digest = SHA-384( JCS( {"content": C, "signature": S} ) )
```

`C` is the content as the contract's JSON; `S` is `userSignature` with `value` emptied. **`signing.Verify` is the one to call — do not write a second implementation of this digest.** Why canonical JSON of the decoded message, why `value` is emptied, why the signed attributes sit on the signature: the [`go/signing`](go/signing/signing.go) package comment. How to sign in each language, and enrolment at sign-up: [ts/README.md](ts/README.md), "Signing", and the same `go/signing` package.

Two facts reach the schema. **Every id the path binds is inside the signed content**; server-minted ids are not, and can't be — an id that changes what the content *means* is covered, one that is merely its *address* need not be, and `routegen` refuses a signed route that omits a path-bound id. And **no 64-bit number crosses the digest**: JCS prints numbers as ECMAScript would, so `TestNoWideNumbersCrossJCS` holds the "JSON is the wire" rule below to what both languages print identically.

**Verification happens behind persistence, not at the API edge** — the signature travels in the body so it reaches the chaincode boundary intact; `Runtime.VerifyBody` and the `X-Signature` header are gone. **Ordering uses `recorded` (the server's observation), never `signingTime` (the participant's claim)**, or a participant could order their own writes. A stored record carries an `institutional_signature` and reserves the field after it, so the next signature layer is a pure addition (`TestSignedRecordsReserveTheNextField`).

**`ts/test/wire.test.mjs` pins the wire:** protojson and ts-proto must emit the same document, or a signature verifies only on the machine that made it, not just a readability problem. It checks that pairing over reads; the two languages signing in conjunction across the write path waits on an integration suite over mock persistence ([#31](https://github.com/metacensus/api/issues/31)), and the signing module's own behaviour is unit-tested in `ts/test/signing.test.mjs`.

## JSON is the wire

Protobuf binary is not used, supported, or a fallback — protobuf is here for the schema, code generation and breaking-change detection.

- **Marshal with `protojson`, never `encoding/json`** — `go/` exports `Marshal`, `Unmarshal`, `MarshalOptions`. Under `encoding/json` the generated types produce enums as integers and timestamps as `{"seconds","nanos"}`.
- **Field names are `lowerCamelCase` on the wire** (protojson converts; no `json_name` overrides). **Enum values are PascalCase**, nested in their message, zero value `Unspecified`. **All ids are strings.**
- **No 64-bit integer, float or double** — use `int32`/`uint32` or a string (see the signing chain).
- **Presence is expressed only through message-typed fields.** `EmitDefaultValues` + ts-proto's `useOptionals=messages` makes scalars/enums/repeated always present and message fields `?: T | undefined`; a proto3 `optional` scalar is a third case the two sides would disagree about — use `google.protobuf.Int32Value` and friends.

## Routes

Each resource file declares its routes (`topic.proto` → `TopicRoutes`); **to read the whole table, read the generated manifest** — [`go/routes`](go/routes/manifest.go) or `ts/src/route-manifest.ts`. `params`, `query` and `body` account for every field of a request message, so a request message models the whole request, not only its body.

The prefixes (`routes.Prefix`, `routes.PublicPrefix`) are part of the contract and generated from `model.Packages`; every `path` is relative to one, and each route carries its own `prefix` so a consumer holding a `Route` needn't guess which to join. `TestPrefix` pins the invariants (each absolute, no trailing slash, none nested); `TestNonConformingRoutes` pins the conventions at zero exceptions. They are route declarations, not a gRPC commitment — nothing generates or serves gRPC.

## Layout

**Two Go modules.** The contract, which consumers import, and the generator, which nobody does.

```
proto/            .proto sources and buf config — both surfaces
go/               the contract in Go: types, route manifest, wire encoder,
                  server, schema tests (go/metacensus/{v1,public/v1})
go/server/        generated handler interfaces + registration, hand-written runtime
go/signing/       the signing chain: JCS, the digest, sign and verify
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
import "github.com/metacensus/api/go/routes"                         // both, one table
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

## The generated client

The same walk writes `ts/src/client.ts`: one **batteries-included** client per surface — `Client` for the authenticated API, `PublicClient` for the public one. Constructed with `{ baseUrl, fetch?, signer?, token? }`, `Client` owns its HTTP — it builds the path, holds the session token across `login`/`logout` (adding `Authorization` itself), and signs writes through the injected `signer`. Reads return flat views (`getTopic` → `TopicView`, `{id, recorded, ...content}`); a signed-envelope read also gets a `getTopicSigned` twin returning the raw envelope, for verifying authorship. Writes take flat content, never an envelope. A 2xx is parsed and cast; a non-2xx throws `ApiError` (status, url, raw text). `ApiError` is one class across both entry points, which is why both classes share one file. Usage and the public-surface variant are in [ts/README.md](ts/README.md).

## Open questions

Written down, not tracked — the four `ui` issues that held this work were closed as not-planned on the move, and nothing replaced them. Each is a decision nobody has standing to take until the consumer that would settle it exists.

- **Should the contract declare an `Error` message?** Today the server emits an ad hoc `{"error","code"}` and clients hand back the text unparsed. Exactly the schema that goes wrong when invented before a second consumer. Refs [#3](https://github.com/metacensus/api/issues/3).
- **No write path verifies a signature yet.** What persistence owes is stated only in field comments: verify the digest under the key `keyId` resolves to, refuse a `signerId` disagreeing with its content's `userId`, check at sign-up that `keyId` thumbprints the `publicKey`, and hold a `signingTime`/`recorded` tolerance in chaincode config. No repository implements any of it.
- **Key rotation.** `keyId` exists so a second key is a lookup, but no route enrols one and nothing says what happens to records signed by a retired key. Sign-up is the only enrolment.
- **Should the client, or a zero-valued query parameter, be split out / emitted?** No route declares a query field, and `sideEffects: false` already lets a bundler drop the client, so neither is urgent. Refs [#4](https://github.com/metacensus/api/issues/4).
- **Cross-language wire agreement is checked for the shapes the contract has, not the ones it could grow** — a 64-bit integer, a map, a `oneof` are untested, so the pairing in `proto/buf.gen.yaml` is only checked where a route exercises it.
