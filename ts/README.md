# @metacensus/api

TypeScript interfaces for the MetaCensus API contract — every request and
response under `/metacensus/api/v1` and `/metacensus/public`, generated from the
`.proto` sources that also generate the Go side.

**Types only.** There are no runtime dependencies and no value imports; a build
step enforces both. What ships is the generated interfaces, the route manifest,
and `protobufPackage`.

```bash
npm install @metacensus/api
```

```ts
import type { Topic, PropCreateRequest } from "@metacensus/api";
import { routes, apiPrefix } from "@metacensus/api";
```

The public, unauthenticated surface is a second entry point in the same package:

```ts
import type { PartnerSubmission } from "@metacensus/api/public";
import { PartnerSubmission_Interest, publicPrefix } from "@metacensus/api/public";
```

One package because the SPA calls both surfaces from one build; two entry points
because a consumer of only the public surface should not acquire the
authenticated types.

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

The wire format is JSON, not protobuf binary. Field names are `lowerCamelCase`
on the wire, enum values are PascalCase, and all ids are strings.

Source, the derivation of every field, and the Go module:
<https://github.com/metacensus/api>
