# @metacensus/api

TypeScript interfaces for the MetaCensus API contract — every request and
response under `/metacensus/api/v1` and `/metacensus/public`, generated from the
`.proto` sources that also generate the Go side.

What ships is the generated interfaces, the route manifest, `protobufPackage`, a
batteries-included client per surface, and the signing chain. How dependencies
are weighed is in the repository's AGENTS.md, under "Dependencies".

```bash
npm install @metacensus/api
```

```ts
import { Client, ApiError, type Signer } from "@metacensus/api";

const client = new Client({ baseUrl: "https://api.example.com", signer });
await client.login({ email, password });          // token stored internally
const topic = await client.getTopic({ topicId }); // TopicRecord: { id, recorded, name, description }
```

`Client` is batteries-included: it owns its `fetch`, holds the session token
across `login`/`logout`, and signs writes with the `signer` you inject. It is the
client to reach for. Construct it with:

- `baseUrl` — the origin; the client adds `/metacensus/api/v1` and the route.
- `fetch?` — a `fetch` to use instead of the global, for tests or instrumentation
  (wrap the global to add retries, a 401 policy, or logging).
- `signer?` — signs writes; see "Writes", below. Omit it for a read-only client.
- `token?` — an initial bearer token, to resume a session without logging in.

A non-2xx response throws `ApiError` (`method`, `url`, `status`, and the raw
`body` — parse it defensively, since a 404 or 405 comes from the router, not a
handler). Every scalar is required, which is what makes Go's `EmitDefaultValues`
and these types describe the same document: `client.createTopic({ content: { name } })`
does not type-check, `{ content: { name, description: "" } }` does.

## Reads: flat record or signed envelope

A read returns the **flat record** — the domain object with the server's `id`
and `recorded` folded in, and the `{content, userSignature}` envelope gone:

```ts
const topic = await client.getTopic({ topicId }); // { id, recorded, name, description }
topic.name; // not topic.content.name
```

The envelope isn't uniform, so the record isn't either:

| Response shape | `getX` / `listX` returns |
| --- | --- |
| id-bearing envelope (topic, prop, user) | `TopicRecord` — `{ id, recorded, ...content }` — / `TopicRecord[]` |
| a vote — envelope with **no `id`** | `VoteRecord` (no `id`) / `VoteRecord[]` |
| a member — already flat | `Member`, unchanged / `Member[]` |

When you need the signature itself — verifying authorship, comparing `recorded`
against the signer's `signingTime`, or re-hashing the document — each
signed-envelope read has a **`getXSigned`** twin that returns the raw envelope:

```ts
const signed = await client.getTopicSigned({ topicId }); // TopicSigned: { id, recorded, content, userSignature }
await verify(pub, signed.content, signed.userSignature);
```

`getXSigned` exists only where there's a signature to see; `Member` has none.

## Writes and the signer

A write takes **flat content** — never the envelope — and the `signer` assembles
the rest. The signer is set once on the constructor, because the key it signs
with is identical for a session:

```ts
import { SPEC, sign, keyId } from "@metacensus/api/signing";

const signer: Signer = async (content, contentType) => {
  const s = {
    signerId,
    keyId: await keyId(publicKey),
    alg: "Es384",
    publicKey: "",
    signingTime: new Date().toISOString(),
    spec: SPEC,
    contentType, // the client names it — you don't
    value: "",
  };
  s.value = await sign(privateKey, content, s);
  return s;
};

const client = new Client({ baseUrl, signer });
const created = await client.createTopic({ content: { name: "A review", description: "" } });
// TopicRecord, flattened like a read
```

The client supplies `content` and the exact `contentType` (the one thing
`sign`/`verify` leave to the caller, and the easiest to get wrong); your closure
holds the key and does the crypto — the key never enters the client. A write on
a client built without a signer throws.

## Auth and the session token

`login` and `signUp` store the returned token; the client adds
`Authorization: Bearer <token>` to every later request, and `logout` clears it.
`signUp` signs its `User` content through the same signer (that signature enrols
the key — inline `publicKey`, empty `signerId`; keep the password out of
`content`, since content is what gets stored). `token` (a getter) exposes the
current token, to persist and later restore a session via the `token` option.

## The public surface

The public, unauthenticated surface is an entry point of its own in the same
package, with a client to match — no signer, no token:

```ts
import type { PartnerSubmission } from "@metacensus/api/public";
import { PartnerSubmission_Interest, PublicClient } from "@metacensus/api/public";

const pub = new PublicClient({ baseUrl });
await pub.submitPartnerInterest({ name, email, interests, message, website: "" });
```

One package because the SPA calls both surfaces from one build; an entry point
per surface because a consumer of only the public surface should not acquire the
authenticated types. `@metacensus/api/signing` is a third, splitting by concern
rather than by surface — nothing on the public surface is ever signed.

`ApiError` is the same class from either entry point, so `instanceof ApiError`
holds for a failure from either surface. Importing it twice is harmless.

`PartnerSubmission_Interest` is a string enum, so the checkbox list is generated
from the contract rather than mirrored by hand — which is the coupling this
package exists to make mechanical. `Unspecified` is the proto3 zero value, not
an offered choice, so filter it out:

```ts
const choices = Object.values(PartnerSubmission_Interest).filter(
  (i) => i !== PartnerSubmission_Interest.Unspecified,
);
```

The display labels are not in the contract; they stay in the SPA as copy. See
the enum's comment for why.

## Signing

Every write on the authenticated surface carries a `content` message and a
`userSignature` over it. A session token says who is connected; the signature
says who authored the record, and the API server does not check it — it is
verified by the persistence layer, behind the edge. `@metacensus/api/signing` is
what your `Signer` uses to make one:

```ts
import { SPEC, sign, verify, keyId, generateKeyPair, encodePublicKey } from "@metacensus/api/signing";

const { privateKey, publicKey } = await generateKeyPair(); // ECDSA P-384
```

The digest is SHA-384 over RFC 8785 canonical JSON of `{content, signature}`,
with `value` emptied. Every field but `value` is inside it, so a signature cannot
be re-attributed or re-dated — which is why `contentType` has to name the content
you actually signed and `signingTime` has to be set. `routes` carries a `signed`
flag per route, so "which routes need a key?" is a lookup rather than a guess.

The full account of the chain, and `go/signing`, the other half that has to
compute the same digest, are in the repository README.

## Both surfaces

The wire format is JSON, not protobuf binary. Field names are `lowerCamelCase`
on the wire, enum values are PascalCase, and all ids are strings.

Source, the derivation of every field, and the Go module:
<https://github.com/metacensus/api>
