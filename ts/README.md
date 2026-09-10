# @metacensus/api

TypeScript interfaces for the MetaCensus API contract — every request and
response under `/metacensus/api/v1`, generated from the `.proto` sources that
also generate the Go side.

**Types only.** There are no runtime dependencies and no value imports; a build
step enforces both. What ships is the generated interfaces, the route manifest,
and `protobufPackage`.

```bash
npm install @metacensus/api
```

```ts
import type { Topic, CreatePropRequest } from "@metacensus/api";
import { routes } from "@metacensus/api";
```

The wire format is JSON, not protobuf binary. Field names are `lowerCamelCase`
on the wire, enum values are PascalCase, and all ids are strings.

Source, the derivation of every field, and the Go module:
<https://github.com/metacensus/api>
