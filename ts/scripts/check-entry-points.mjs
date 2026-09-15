// Assert the entry points export every generated module, and that the two
// surfaces stay apart.
//
// index.ts and public.ts list their exports by hand, so a new .proto file
// generates a module that ships in dist/ and that no consumer can import —
// silently, because tsc only type-checks what is reachable and nothing else
// looks. This is the same shape of hole as a proto package missing from
// contractPackages: not a failure, an absence.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

import { entryPoints as publishedEntryPoints } from "./entry-points.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = join(here, "..");
const srcRoot = join(pkgRoot, "src");

// The generated *types* are what the split is for: a consumer of only the
// public surface must not acquire the authenticated messages. A module under
// this prefix is therefore one surface's, and exactly one entry point may
// export it.
//
// Everything else under src/ is shared on purpose and both entry points may
// name it: the route manifest, because "every MetaCensus route on one screen"
// is why the contract is one repository; and the client, for the reason its
// own generated header gives.
const perSurface = "src/metacensus/";

const failures = [];

// What package.json publishes, not a list beside it: a subpath export added
// without touching this file would otherwise ship uncovered by this check.
const published = publishedEntryPoints(pkgRoot);
failures.push(...published.failures);
const entryPoints = published.entryPoints;

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) yield* walk(path);
    else if (path.endsWith(".ts")) yield path;
  }
}

// `export * from "./src/a/b.js"` and `export { x, type Y } from "./src/a/b.js"`,
// the latter possibly spread over several lines, which is how prettier leaves a
// named list of any length.
const exportFrom = /^[ \t]*export\s+(?:\*|\{[\s\S]*?\})\s+from\s+"(\.[^"]+)";/gm;

// entry point -> the src/ modules it exports
const exported = new Map();
for (const entry of entryPoints) {
  const source = readFileSync(join(pkgRoot, entry), "utf8");
  const paths = new Set();
  for (const [, spec] of source.matchAll(exportFrom)) {
    const path = spec.replace(/^\.\//, "").replace(/\.js$/, ".ts");
    try {
      statSync(join(pkgRoot, path));
    } catch {
      failures.push(`${entry}: exports ${spec}, which does not exist`);
      continue;
    }
    paths.add(path);
  }
  exported.set(entry, paths);
}

const allExported = new Set([...exported.values()].flatMap((s) => [...s]));

for (const file of walk(srcRoot)) {
  const rel = relative(pkgRoot, file);
  if (!allExported.has(rel)) {
    failures.push(
      `${rel} is generated but no entry point exports it, so it ships in ` +
        `dist/ and no consumer can import it. Add a line to ` +
        `${rel.startsWith(perSurface + "public/") ? "public.ts" : "index.ts"}.`,
    );
  }
}

for (const rel of allExported) {
  if (!rel.startsWith(perSurface)) continue;
  const owners = entryPoints.filter((e) => exported.get(e).has(rel));
  if (owners.length > 1) {
    failures.push(
      `${rel} is exported by ${owners.join(" and ")}. A module belongs to one ` +
        `surface; two entry points exist so a consumer of one does not acquire ` +
        `the other.`,
    );
  }
}

if (failures.length > 0) {
  console.error("ts/ entry points do not cover what is generated:\n");
  for (const failure of failures) console.error("  " + failure);
  console.error();
  process.exit(1);
}

console.log(
  "ts/: every generated module is exported, and no surface's types by more than one entry point.",
);
