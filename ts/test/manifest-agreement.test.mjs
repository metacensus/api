// The client and the route manifest are two renderings of one descriptor walk,
// so they cannot disagree at generation time — but nothing said they agree
// after it. A bug inside the client renderer (a dropped parameter, a mangled
// path template) produces a self-consistent wrong client that the freshness
// diff happily pins, because "generated" only means "committed == generated".
//
// This drives every method the manifest declares and compares what reaches the
// transport with what the manifest says the route is.
import { test } from "node:test";
import assert from "node:assert/strict";
import { Client } from "../dist/src/client.js";
import { routes, apiPrefix } from "../dist/src/route-manifest.js";

// routegen names a method lowerFirst(rpc); nothing else about the mapping is
// generated, so this is the one place it is restated, and it is checked.
const methodName = (rpc) => rpc[0].toLowerCase() + rpc.slice(1);

test("every manifest route is a client method that sends that route", async () => {
  assert.ok(routes.length > 0, "empty manifest");

  for (const route of routes) {
    const name = methodName(route.rpc);
    const fn = Client.prototype[name];
    assert.equal(typeof fn, "function", `${route.service}.${route.rpc}: no client method ${name}()`);

    const calls = [];
    const api = new Client(async (req) => {
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

    const want =
      apiPrefix +
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

test("the client has no method the manifest does not declare", () => {
  const declared = new Set(routes.map((r) => methodName(r.rpc)));
  const own = Object.getOwnPropertyNames(Client.prototype).filter(
    (n) => n !== "constructor" && typeof Client.prototype[n] === "function",
  );
  const extra = own.filter((n) => !declared.has(n) && !n.startsWith("#") && n !== "call");
  assert.deepEqual(extra, [], `client methods with no route: ${extra.join(", ")}`);
});
