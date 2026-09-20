# @metacensus/api

TypeScript interfaces for the MetaCensus API contract — every request and
response under `/metacensus/api/v1` and `/metacensus/public`, generated from the
`.proto` sources that also generate the Go side.

What ships is the generated interfaces, the route manifest, `protobufPackage`,
a typed client per surface — over a transport you supply — and the signing
chain. How dependencies are weighed is in the repository README, under
"Dependencies".

```bash
npm install @metacensus/api
```

```ts
import type { Topic, PropCreateRequest } from "@metacensus/api";
import { routes, Client, ApiError, type Transport } from "@metacensus/api";

const transport: Transport = async ({ method, path, body }) => {
  const r = await fetch(baseUrl + path, { method, body });
  return { status: r.status, body: await r.text() };
};
const api = new Client(transport);
const topic = await api.getTopic({ topicId });
```

`path` is absolute and already carries `/metacensus/api/v1`, every segment
percent-encoded; prepend only an origin. `body`, when present, is the exact
string to send: the client serialises once, so what you log or hash is what
went out.

**The transport is where your own concerns go**: auth headers, a 401 policy,
retries. The client calls no `fetch`, keeps no cache, and throws `ApiError` for
any non-2xx the transport did not handle itself. `ApiError.body` is raw text —
a 404 or 405 comes from the server's router rather than a handler, so parse it
defensively rather than assuming JSON.

Signing is *not* a transport concern. A participant's signature is part of the
request message and is made before the client is called; see "Signing", below.

Every scalar is required, which is what makes Go's `EmitDefaultValues` and
these types describe the same document: `api.createTopic({ name })` does not
type-check, `{ name, description: "" }` does. Message-typed fields are
optional.

## The public surface

The public, unauthenticated surface is an entry point of its own in the same
package, with a client to match:

```ts
import type { PartnerSubmission } from "@metacensus/api/public";
import {
  PartnerSubmission_Interest,
  PublicClient,
  publicPrefix,
} from "@metacensus/api/public";

const pub = new PublicClient(transport);
await pub.submitPartnerInterest({ name, email, interests, message, website: "" });
```

One package because the SPA calls both surfaces from one build; an entry point
per surface because a consumer of only the public surface should not acquire the
authenticated types. `@metacensus/api/signing` is a third, splitting by concern
rather than by surface — nothing on the public surface is ever signed.
`PublicClient` sends no credentials of its own; pass it a separate transport
from the one you pass `Client`.

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
verified by the persistence layer, behind the edge.

```ts
import { SPEC, sign, keyId, generateKeyPair, encodePublicKey } from "@metacensus/api/signing";

const { privateKey, publicKey } = await generateKeyPair(); // ECDSA P-384

const content = { name: "A review", description: "" };
const userSignature = {
  signerId,
  keyId: await keyId(publicKey),
  alg: "Es384",
  publicKey: "", // inline only on sign-up, which is what enrols the key
  signingTime: new Date().toISOString(),
  spec: SPEC,
  contentType: "metacensus.v1.TopicContent",
  value: "",
};
userSignature.value = await sign(privateKey, content, userSignature);

await api.createTopic({ content, userSignature });
```

The digest is SHA-384 over RFC 8785 canonical JSON of
`{content, signature}`, with `value` emptied. Every field but `value` is inside
it, so a signature cannot be re-attributed or re-dated — which also means
`contentType` has to name the content you actually signed, and `signingTime`
has to be set.

Sign-up is the one request whose signature carries its own public key: there is
no id yet to look one up by. Set `publicKey` there, leave `signerId` empty, and
keep the password out of `content` — content is what gets stored.

`routes` carries a `signed` flag per route, so "which routes need a key?" is a
lookup rather than a guess.

The full account of the chain, and `go/signing`, the other half that has to
compute the same digest, are in the repository README.

## Both surfaces

The wire format is JSON, not protobuf binary. Field names are `lowerCamelCase`
on the wire, enum values are PascalCase, and all ids are strings.

Source, the derivation of every field, and the Go module:
<https://github.com/metacensus/api>
