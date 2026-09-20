// The clients and the route manifest are two renderings of one descriptor walk,
// so they cannot disagree at generation time — but nothing said they agree
// after it. A bug inside the client renderer (a dropped parameter, a mangled
// path template) produces a self-consistent wrong client that the freshness
// diff happily pins, because "generated" only means "committed == generated".
//
// This drives every method the manifest declares over a fake fetch and compares
// the request that reaches it with what the manifest says the route is. Each
// route has a flat method; a signed-envelope read has a getXSigned method too,
// and that pairing is checked to be coherent rather than re-derived here.
import { test } from "node:test";
import assert from "node:assert/strict";
import { Client, PublicClient } from "../dist/src/client.js";
import { routes, apiPrefix, publicPrefix } from "../dist/src/route-manifest.js";

const BASE = "https://api.test";
const methodName = (rpc) => rpc[0].toLowerCase() + rpc.slice(1);

// A fake fetch that records calls and always replies with an empty list, which
// every method's response handling (flatten, .items, .token) tolerates.
function fetcher() {
  const calls = [];
  const fetch = async (url, init) => {
    calls.push({ url, method: init.method, body: init.body });
    return { status: 200, text: async () => '{"items":[]}' };
  };
  return { calls, fetch };
}

// A signer so writes and sign-up can run; the value is irrelevant here.
const signer = async () => ({ signerId: "", keyId: "", alg: "Unspecified", publicKey: "", signingTime: "", spec: "", contentType: "", value: "" });

const surfaces = {
  [apiPrefix]: () => {
    const { calls, fetch } = fetcher();
    return { calls, client: new Client({ baseUrl: BASE, fetch, signer }) };
  },
  [publicPrefix]: () => {
    const { calls, fetch } = fetcher();
    return { calls, client: new PublicClient({ baseUrl: BASE, fetch }) };
  },
};

// The request a method needs: path parameters filled with a value that must be
// escaped, so a dropped parameter shows up as a literal "{name}".
function reqFor(route) {
  const req = {};
  for (const p of route.params) req[p] = `v/${p}`;
  if (route.body === "*") req.content = {}; // writes read req.content
  return req;
}

const wantPath = (route) =>
  BASE + route.prefix + route.path.replace(/\{(\w+)\}/g, (_, p) => encodeURIComponent(`v/${p}`));

test("every manifest route is sent by its flat method", async () => {
  assert.ok(routes.length > 0, "empty manifest");

  for (const route of routes) {
    const make = surfaces[route.prefix];
    assert.ok(make, `${route.service}.${route.rpc}: prefix ${route.prefix} has no client`);

    const name = methodName(route.rpc);
    const { calls, client } = make();
    assert.equal(typeof client[name], "function", `${route.service}.${route.rpc}: no method ${name}()`);

    await client[name](reqFor(route));
    assert.equal(calls.length, 1, `${name}: fetched ${calls.length} times`);
    assert.equal(calls[0].method, route.method, `${name}: http method`);
    assert.equal(calls[0].url, wantPath(route), `${name}: url`);
    assert.equal(calls[0].body !== undefined, route.body === "*", `${name}: body presence`);
  }
});

test("each getXSigned mirrors a flat read on the same route", async () => {
  const flat = new Set(routes.filter((r) => r.prefix === apiPrefix).map((r) => methodName(r.rpc)));

  for (const name of Object.getOwnPropertyNames(Client.prototype)) {
    if (!name.endsWith("Signed")) continue;
    const base = name.slice(0, -"Signed".length);
    assert.ok(flat.has(base), `${name}: no flat method ${base}() and no route it belongs to`);

    const route = routes.find((r) => r.prefix === apiPrefix && methodName(r.rpc) === base);
    const { calls, client } = surfaces[apiPrefix]();
    await client[name](reqFor(route));
    assert.equal(calls[0].url, wantPath(route), `${name}: url differs from ${base}`);
    assert.equal(calls[0].method, "GET", `${name}: a signed read is a GET`);
  }
});

test("no client method exists beyond a route's flat method or its signed variant", () => {
  const ignore = new Set(["constructor", "token"]);
  for (const [prefix, make] of Object.entries(surfaces)) {
    const { client } = make();
    const flat = new Set(routes.filter((r) => r.prefix === prefix).map((r) => methodName(r.rpc)));
    for (const name of Object.getOwnPropertyNames(Object.getPrototypeOf(client))) {
      if (ignore.has(name) || typeof client[name] !== "function") continue;
      const base = name.endsWith("Signed") ? name.slice(0, -"Signed".length) : name;
      assert.ok(flat.has(base), `${client.constructor.name}.${name}: no route on ${prefix}`);
    }
  }
});
