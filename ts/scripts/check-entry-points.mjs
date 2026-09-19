// Assert the entry points export every generated module and keep the two
// surfaces apart. The exports are hand-listed, so a new generated module can
// ship in dist/ that no consumer can import — an absence, not a failure, that
// nothing else catches.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

import { entryPoints as publishedEntryPoints } from "./entry-points.mjs";

// tsconfig's `include` is the other entry-point list; a subpath export missing
// from it emits no dist/<name>.js and passes every other check. Read by regex,
// not JSON.parse, because these files are JSONC.
const INCLUDE = /"include"\s*:\s*\[([^\]]*)\]/;

function includeList(pkgRoot, file) {
  const source = readFileSync(join(pkgRoot, file), "utf8");
  const m = source.match(INCLUDE);
  if (!m) return null;
  return [...m[1].matchAll(/"([^"]+)"/g)].map(([, v]) => v);
}

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = join(here, "..");
const srcRoot = join(pkgRoot, "src");

// A module under this prefix is one surface's generated types, so exactly one
// entry point may export it — that is what the split protects. Anything else
// under src/ is shared and may be exported by any number; the walk finds them.
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

for (const tsconfig of ["tsconfig.json", "tsconfig.build.json"]) {
  const include = includeList(pkgRoot, tsconfig);
  if (include === null) {
    failures.push(`${tsconfig}: no \`include\` array; this guard cannot see what tsc compiles.`);
    continue;
  }
  for (const entry of entryPoints) {
    if (!include.includes(entry)) {
      failures.push(
        `${tsconfig} does not include ${entry}, which package.json publishes. ` +
          `tsc would emit no dist/ output for it and the export would resolve to nothing.`,
      );
    }
  }
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
      `${rel} is exported by ${owners.join(" and ")}. A module under ` +
        `${perSurface} belongs to one surface; the entry points are split so ` +
        `that a consumer of one surface does not acquire another's types.`,
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
  "ts/: every generated module is exported, no surface's types by more than one entry point, " +
    "and every published entry point is compiled.",
);
