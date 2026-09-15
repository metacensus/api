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
import { routes, Client, type Transport } from "@metacensus/api";

const transport: Transport = async ({ method, path, body }) => {
  const r = await fetch(baseUrl + path, { method, body });
  return { status: r.status, body: await r.text() };
};
const api = new Client(transport);
const topic = await api.getTopic({ topicId });
```

The wire format is JSON, not protobuf binary. Field names are `lowerCamelCase`
on the wire, enum values are PascalCase, and all ids are strings.

Source, the derivation of every field, and the Go module:
<https://github.com/metacensus/api>
