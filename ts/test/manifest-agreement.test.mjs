// The clients and the route manifest are two renderings of one descriptor walk,
// so they cannot disagree at generation time — but nothing said they agree
// after it. A bug inside the client renderer (a dropped parameter, a mangled
// path template) produces a self-consistent wrong client that the freshness
// diff happily pins, because "generated" only means "committed == generated".
//
// This drives every method the manifest declares and compares what reaches the
// transport with what the manifest says the route is.
import { test } from "node:test";
import assert from "node:assert/strict";
import { ClientSigned, PublicClient } from "../dist/src/client.js";
import { routes, apiPrefix, publicPrefix } from "../dist/src/route-manifest.js";

// One client class per surface, keyed by the prefix its routes hang off. This
// pairing is the one thing about the split that nothing else states at
// runtime: routegen emits the classes and the prefixes from one table, and
// this is the assertion that the table was read the same way twice.
const surfaces = [
  { prefix: apiPrefix, name: "ClientSigned", ctor: ClientSigned },
  { prefix: publicPrefix, name: "PublicClient", ctor: PublicClient },
];

// routegen names a method lowerFirst(rpc); nothing else about the mapping is
// generated, so this is the one place it is restated, and it is checked.
const methodName = (rpc) => rpc[0].toLowerCase() + rpc.slice(1);

test("every manifest route is a client method that sends that route", async () => {
  assert.ok(routes.length > 0, "empty manifest");

  for (const route of routes) {
    const surface = surfaces.find((s) => s.prefix === route.prefix);
    assert.ok(
      surface,
      `${route.service}.${route.rpc}: prefix ${route.prefix} has no client class`,
    );

    const name = methodName(route.rpc);
    const fn = surface.ctor.prototype[name];
    assert.equal(
      typeof fn,
      "function",
      `${route.service}.${route.rpc}: no ${surface.name} method ${name}()`,
    );

    const calls = [];
    const api = new surface.ctor(async (req) => {
      calls.push(req);
      return { status: 200, body: "{}" };
    });

    // Path params get a value that needs escaping, so a template that dropped
    // one shows up as a literal "{name}" rather than as a coincidence.
    const req = {};
    for (const p of route.params) req[p] = `v/${p}`;
    await api[name](req);

    assert.equal(calls.length, 1, `${name}: transport called ${calls.length} times`);
    const sent = calls[0];
    assert.equal(sent.method, route.method, `${name}: http method`);

    // route.prefix, not apiPrefix: a route joined to the wrong surface's
    // prefix is a URL nothing serves, and it is the mistake carrying a prefix
    // per route exists to make impossible.
    const want =
      route.prefix +
      route.path.replace(/\{(\w+)\}/g, (_, p) => encodeURIComponent(`v/${p}`));
    assert.equal(sent.path, want, `${name}: path`);

    // body is "*" or "" — routegen rejects a named body — so its presence is
    // fully determined by the manifest.
    assert.equal(
      sent.body !== undefined,
      route.body === "*",
      `${name}: body present=${sent.body !== undefined}, manifest body=${JSON.stringify(route.body)}`,
    );
  }
});

test("no client has a method the manifest does not declare on its surface", () => {
  for (const surface of surfaces) {
    const mine = routes.filter((r) => r.prefix === surface.prefix);

    // A surface with no routes would make every assertion below vacuous, and
    // a generated class with nothing to call is a table entry nobody noticed
    // was wrong.
    assert.ok(mine.length > 0, `${surface.name}: no route declares ${surface.prefix}`);

    const declared = new Set(mine.map((r) => methodName(r.rpc)));
    const own = Object.getOwnPropertyNames(surface.ctor.prototype).filter(
      (n) => n !== "constructor" && typeof surface.ctor.prototype[n] === "function",
    );
    const extra = own.filter((n) => !declared.has(n) && !n.startsWith("#") && n !== "call");
    assert.deepEqual(
      extra,
      [],
      `${surface.name} methods with no route on ${surface.prefix}: ${extra.join(", ")}`,
    );
  }
});
