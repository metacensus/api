// The entry points package.json actually publishes, derived from its `exports`
// rather than listed beside them.
//
// Both guards here used to hold the literal ["index.ts", "public.ts"] while
// their comments claimed they covered what package.json points at. A third
// subpath export would have shipped unchecked by either of them, which is the
// shape of hole they exist to close — and `@metacensus/api/signing` is now
// that third export, added without either guard being touched.

import { readFileSync, statSync } from "node:fs";
import { join } from "node:path";

// A published path is "./dist/<name>.js"; the source beside package.json is
// "<name>.ts". tsconfig.build.json's rootDir is the package root, so that
// mapping is the build's, not a guess.
const DIST = "./dist/";

export function entryPoints(pkgRoot) {
  const pkg = JSON.parse(readFileSync(join(pkgRoot, "package.json"), "utf8"));
  const out = [];
  const failures = [];

  for (const [subpath, entry] of Object.entries(pkg.exports ?? {})) {
    // "./package.json": "./package.json" is a file, not a module.
    if (typeof entry === "string") continue;
    const published = entry.default ?? entry.types;
    if (!published?.startsWith(DIST) || !published.endsWith(".js")) {
      failures.push(
        `package.json exports ${subpath} as ${JSON.stringify(published)}, ` +
          `which is not a ${DIST}*.js path; these guards cannot find its source.`,
      );
      continue;
    }
    const source = published.slice(DIST.length).replace(/\.js$/, ".ts");
    try {
      statSync(join(pkgRoot, source));
    } catch {
      failures.push(`package.json exports ${subpath}, whose source ${source} does not exist.`);
      continue;
    }
    out.push(source);
  }

  if (out.length === 0) {
    failures.push("package.json declares no module entry points; both guards would check nothing.");
  }
  return { entryPoints: out, failures };
}
