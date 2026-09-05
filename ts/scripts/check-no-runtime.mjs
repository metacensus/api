// Assert the TypeScript carries no runtime: no declared dependencies, and no
// value imports in anything that ships — the generated files under src/ and the
// entry points package.json points at. ts-proto's default forceLong would pull
// in `long`; dropping onlyTypes would pull in protobufjs.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = join(here, "..");
const srcRoot = join(pkgRoot, "src");

// The entry points are hand-written and outside src/, so walking src/ alone
// would leave every published module but the generated ones unchecked.
const entryPoints = ["index.ts", "public.ts"].map((f) => join(pkgRoot, f));

const failures = [];

const pkg = JSON.parse(readFileSync(join(pkgRoot, "package.json"), "utf8"));
const deps = Object.keys(pkg.dependencies ?? {});
if (deps.length > 0) {
  failures.push(
    `package.json declares runtime dependencies: ${deps.join(", ")}. ` +
      `A types-only contract package must have none.`,
  );
}
const peers = Object.keys(pkg.peerDependencies ?? {});
if (peers.length > 0) {
  failures.push(`package.json declares peerDependencies: ${peers.join(", ")}.`);
}

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      yield* walk(path);
    } else if (path.endsWith(".ts")) {
      yield path;
    }
  }
}

// Type-only imports are erased at compile time; anything else is a runtime edge.
const importLine = /^\s*import\s/;
const typeOnlyImport = /^\s*import\s+type\s/;

for (const file of [...entryPoints, ...walk(srcRoot)]) {
  const rel = relative(pkgRoot, file);
  const source = readFileSync(file, "utf8");

  for (const [i, line] of source.split("\n").entries()) {
    if (importLine.test(line) && !typeOnlyImport.test(line)) {
      failures.push(`${rel}:${i + 1}: value import — \`${line.trim()}\``);
    }
  }

  if (/\brequire\s*\(/.test(source)) {
    failures.push(`${rel}: contains a require() call`);
  }
}

if (failures.length > 0) {
  console.error("ts/ is not runtime-free:\n");
  for (const failure of failures) console.error("  " + failure);
  process.exit(1);
}

console.log("ts/: no runtime dependencies, no value imports.");
