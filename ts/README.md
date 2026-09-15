# @metacensus/api

TypeScript interfaces for the MetaCensus API contract — every request and
response under `/metacensus/api/v1`, generated from the `.proto` sources that
also generate the Go side.

**No runtime dependencies.** `dependencies` is empty and nothing under `src/`
holds a value import of a third-party module; a build step enforces both. What
ships is the generated interfaces, the route manifest, `protobufPackage`, and
a typed `Client` — the one piece here with actual logic, over a transport you
supply.

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

The wire format is JSON, not protobuf binary. Field names are `lowerCamelCase`
on the wire, enum values are PascalCase, and all ids are strings.

Source, the derivation of every field, and the Go module:
<https://github.com/metacensus/api>
