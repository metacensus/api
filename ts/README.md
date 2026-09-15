# @metacensus/api

TypeScript interfaces for the MetaCensus API contract — every request and
response under `/metacensus/api/v1` and `/metacensus/public`, generated from the
`.proto` sources that also generate the Go side.

What ships is the generated interfaces, the route manifest, `protobufPackage`,
and a typed client per surface — the one piece here with logic of its own, over
a transport you supply. It reaches for nothing at runtime;
`scripts/check-no-runtime.mjs` is what says so, and what fails the build.

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
string to send — sign *those* octets if you sign anything, because the client
serialises once on purpose.

**The transport is where your own concerns go**: auth headers, `X-Signature`,
a 401 policy, retries. The client calls no `fetch`, keeps no cache, and throws
`ApiError` for any non-2xx the transport did not handle itself. `ApiError.body`
is raw text — a 404 or 405 comes from the server's router rather than a
handler, so parse it defensively rather than assuming JSON.

Every scalar is required, which is what makes Go's `EmitDefaultValues` and
these types describe the same document: `api.createTopic({ name })` does not
type-check, `{ name, description: "" }` does. Message-typed fields are
optional.

## The public surface

The public, unauthenticated surface is a second entry point in the same package,
with a client of its own:

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

One package because the SPA calls both surfaces from one build; two entry points
because a consumer of only the public surface should not acquire the
authenticated types. `PublicClient` sends no credentials of its own — that, as
above, is the transport's business, and the transport you pass here is not the
one you pass `Client`.

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

## Both surfaces

The wire format is JSON, not protobuf binary. Field names are `lowerCamelCase`
on the wire, enum values are PascalCase, and all ids are strings.

Source, the derivation of every field, and the Go module:
<https://github.com/metacensus/api>
