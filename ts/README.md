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
against the signer's `time`, or re-checking the assertion — each signed-envelope
read has a **`getXSigned`** twin that returns the raw envelope:

```ts
import { verifyUser, participantPolicy } from "@metacensus/api/signing";

const signed = await client.getTopicSigned({ topicId }); // { id, recorded, content, interpretation, userSignature }
const pub = await resolveEnrolledKey(signed.userSignature.keyId); // your key directory
await verifyUser(pub, signed.content, signed.interpretation, signed.userSignature, participantPolicy(origin));
```

`getXSigned` exists only where there's a signature to see; `Member` has none.

## Writes and the signer

A write takes **flat content** — never the envelope — and the `signer` returns
the record's `interpretation`, the `Signature` over it, and (sign-up only) the
enrolling `publicKey`; the client assembles the body. The signer is set once on
the constructor, because the key it signs with is identical for a session. The
signing mechanism is a **passkey (WebAuthn)**: the signer runs the ceremony and
packages its assertion — the private key never enters the client, and cannot,
because a passkey's key is non-extractable in the authenticator.

```ts
import {
  interpretation, userChallenge, keyId, participantPolicy,
} from "@metacensus/api/signing";

const signer: Signer = async (content, contentType) => {
  const interp = interpretation(contentType); // { spec, contentType } — the client names contentType
  const kid = await keyId(publicKey);
  const time = new Date().toISOString();

  // The digest the assertion must commit to, carried as the WebAuthn challenge.
  const challenge = await userChallenge(content, interp, kid, time);
  const cred = await navigator.credentials.get({
    publicKey: { challenge, allowCredentials: [/* this device's passkey */] },
  }); // ui#55 owns provisioning and RP-ID scoping

  const r = cred.response as AuthenticatorAssertionResponse;
  return {
    interpretation: interp,
    signature: {
      keyId: kid,
      time,
      assertion: {
        authenticatorData: toBase64Url(r.authenticatorData),
        clientDataJson: toBase64Url(r.clientDataJSON), // its challenge === base64url(challenge)
        signature: toBase64Url(r.signature),           // DER, verified as-is
      },
    },
  };
};

const client = new Client({ baseUrl, signer });
const created = await client.createTopic({ content: { name: "A review", description: "" } });
// TopicRecord, flattened like a read
```

The client supplies `content` and the exact `contentType` (the one thing left to
the caller, and the easiest to get wrong); the signer builds the interpretation
and the assertion. A headless signer (a server or a test) builds the same shape
with `assert()` instead of a browser ceremony — one format, both layers. A write
on a client built without a signer throws. **The browser ceremony above is
sketch, not shipped: provisioning a passkey to a device, RP-ID scoping across
environments, and recovery are [ui#55](https://github.com/metacensus/ui/issues/55).**

## Auth and the session token

`login` and `signUp` store the returned token; the client adds
`Authorization: Bearer <token>` to every later request, and `logout` clears it.
`signUp` signs its `User` content through the same signer; the signer returns the
enrolling `publicKey`, which the client sends on the request (not on the
signature — no record carries its own verification key) and the store binds under
the signature's `keyId`. Keep the password out of `content`, since content is what
gets stored. `token` (a getter) exposes the
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

Every write on the authenticated surface carries a `content` message, an
`interpretation`, and a `userSignature` over both. A session token says who is
connected; the signature says who authored the record, and the API server does
not check it — it is verified by the persistence layer, behind the edge.
`@metacensus/api/signing` is what your `Signer` uses, and what a verifier calls:

```ts
import {
  SPEC, interpretation, userChallenge, assert, verifyUser,
  authenticatorData, participantPolicy, keyId, generateKeyPair, FLAG_UP, FLAG_UV,
} from "@metacensus/api/signing";

const { privateKey, publicKey } = await generateKeyPair(); // ECDSA P-256 (ES256)
```

The mechanism is **WebAuthn/passkeys**, one format for both the participant and
the institution (ECDSA P-256 / ES256). This module is the toolkit: `interpretation`
and `userChallenge` build the digest, `assert` makes a headless assertion,
`verifyUser`/`verifyCountersign` check one under a `participantPolicy` or
`institutionPolicy`. `routes` carries a `signed` flag per route, so "which routes
need a key?" is a lookup rather than a guess.

The scheme, the digest, and the verify procedure are the repository README's
"The signing chain"; `go/signing` is the other half that computes the same digest
and whose DER assertions this module verifies. The browser passkey ceremony
itself is [ui#55](https://github.com/metacensus/ui/issues/55).

## Both surfaces

The wire format is JSON, not protobuf binary. Field names are `lowerCamelCase`
on the wire, enum values are PascalCase, and all ids are strings.

Source, the derivation of every field, and the Go module:
<https://github.com/metacensus/api>
