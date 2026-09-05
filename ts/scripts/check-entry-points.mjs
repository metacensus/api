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

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = join(here, "..");
const srcRoot = join(pkgRoot, "src");
const entryPoints = ["index.ts", "public.ts"];

// A module under this prefix belongs to exactly one surface, so exactly one
// entry point may export it. Everything else in src/ — the route manifest —
// is shared on purpose.
const perSurface = "src/metacensus/";

const failures = [];

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) yield* walk(path);
    else if (path.endsWith(".ts")) yield path;
  }
}

// `export * from "./src/a/b.js"` and `export { x } from "./src/a/b.js"`.
const exportFrom = /^\s*export\s+(?:\*|\{[^}]*\})\s+from\s+"(\.[^"]+)";/gm;

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

console.log("ts/: every generated module is exported by exactly one surface.");
