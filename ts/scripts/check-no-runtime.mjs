// Assert that the generated TypeScript is types, not code: no declared
// dependencies, and no value imports under src/. ts-proto's default forceLong
// would pull in `long`; dropping onlyTypes would pull in protobufjs.
//
// `export const protobufPackage` is the one permitted runtime emission — a
// string literal with no imports.

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

// Type-only imports are erased at compile time; anything else is a runtime edge.
const importLine = /^\s*import\s/;
const typeOnlyImport = /^\s*import\s+type\s/;

// Permitted: a string enum compiles to a self-contained object literal.
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
  // Not a failure, but ts-proto's output changed shape.
  console.warn("note: no `protobufPackage` constant found; ts-proto output may have changed shape");
}

if (failures.length > 0) {
  console.error("contract/ts is not runtime-free:\n");
  for (const failure of failures) console.error("  " + failure);
  process.exit(1);
}

console.log("contract/ts: no runtime dependencies, no value imports.");
