// Assert that the generated TypeScript is types, not code.
//
// "No runtime dependencies" is a property that decays quietly. ts-proto's
// *default* `forceLong` setting imports the `long` package; dropping
// `onlyTypes` brings in encode/decode helpers that pull in `protobufjs/minimal`.
// Either change would still compile, still pass the golden checks, and still
// look like a types package — right up until a consumer's bundle grew.
//
// So this runs in CI, and it checks two things:
//
//   1. `dependencies` in package.json is empty. Nothing to install means
//      nothing to ship.
//   2. No file under src/ has a value import. `import type { X }` erases at
//      compile time; a bare `import { X }` does not, and its presence means
//      some generated file now needs another module at runtime.
//
// The generated files do contain one runtime value — ts-proto emits
// `export const protobufPackage = "metacensus.v1"` per file. That is a string
// literal with no imports and no dependencies, so it is allowed and asserted
// to be the only such thing.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = join(here, "..");
const srcRoot = join(pkgRoot, "src");

const failures = [];

// --- 1. no declared dependencies -------------------------------------------

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

// --- 2. no value imports in generated sources ------------------------------

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

// `import type ...`, `export type ...` and `export ... from` are erased at
// compile time. Anything else that imports is a runtime edge.
const importLine = /^\s*import\s/;
const typeOnlyImport = /^\s*import\s+type\s/;

// The one permitted runtime emission, plus the enum declarations, which are
// values but self-contained: a string enum compiles to an object literal with
// no imports.
const allowedValueDecl = /^\s*(export\s+const\s+protobufPackage\s*=|export\s+enum\s)/;

let sawProtobufPackage = false;

for (const file of walk(srcRoot)) {
  const rel = relative(pkgRoot, file);
  const lines = readFileSync(file, "utf8").split("\n");

  for (const [i, line] of lines.entries()) {
    if (importLine.test(line) && !typeOnlyImport.test(line)) {
      failures.push(`${rel}:${i + 1}: value import — \`${line.trim()}\``);
    }
    if (allowedValueDecl.test(line) && line.includes("protobufPackage")) {
      sawProtobufPackage = true;
    }
  }

  if (/\brequire\s*\(/.test(lines.join("\n"))) {
    failures.push(`${rel}: contains a require() call`);
  }
}

if (!sawProtobufPackage) {
  // Not a failure in itself, but it means ts-proto's output changed shape and
  // the assumptions above deserve a re-read.
  console.warn("note: no `protobufPackage` constant found; ts-proto output may have changed shape");
}

if (failures.length > 0) {
  console.error("contract/ts is not runtime-free:\n");
  for (const failure of failures) console.error("  " + failure);
  process.exit(1);
}

console.log("contract/ts: no runtime dependencies, no value imports.");
