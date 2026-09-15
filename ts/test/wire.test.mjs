// The generated client against the generated server, over real HTTP.
//
// Everything else here checks one half against itself: ts/test/manifest-agreement
// drives the client against the manifest, go/server's tests drive the server
// against the manifest, and both halves come from one descriptor walk so they
// cannot disagree about the route table. None of that says the two agree about
// the *document*. protojson and ts-proto are separate projects configured to
// meet in the middle — buf.gen.yaml's useDate=string, stringEnums,
// forceLong=string and unrecognizedEnum=false against go/wire.go's
// EmitDefaultValues — and nothing checked the pairing. This is metacensus/ui#49,
// closed for the shapes below.
//
// The server is routegen/wireserver, compiled to a temporary directory and
// run: it lives in the routegen module, with everything else that exercises
// what the generator emits, so a fixture never reaches the published go.mod.
// Compiled rather than `go run`, because `go run` runs
// the binary as a child of its own and killing the parent leaves the listener
// holding the pipe this process is reading.
//
// So this file needs Go on PATH, which `make check` and CI both have and a
// bare `npm test` in a checkout without Go does not — the failure says so.

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { Client, ApiError } from "../dist/src/client.js";

const routegen = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "routegen");

let server;
let workdir;
let api;
/** Every ClientRequest the client handed the transport, for the assertions below. */
const sent = [];

before(async () => {
  workdir = mkdtempSync(join(tmpdir(), "mc-wiretest-"));
  const bin = join(workdir, "wireserver");
  try {
    // GOWORK=off for the same reason the Makefile sets it: a go.work above the
    // checkout would resolve this module against the union of its members.
    execFileSync("go", ["build", "-o", bin, "./wireserver"], {
      cwd: routegen,
      env: { ...process.env, GOWORK: "off" },
      stdio: "pipe",
    });
  } catch (e) {
    throw new Error(
      `could not build routegen/wireserver (is Go on PATH?): ` +
        `${e.stderr?.toString() || e.message}`,
    );
  }

  server = spawn(bin, [], { stdio: ["ignore", "pipe", "pipe"] });
  let stderr = "";
  server.stderr.on("data", (b) => (stderr += b));

  const addr = await new Promise((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error(`wireserver did not listen within 30s. stderr:\n${stderr}`)),
      30_000,
    );
    let out = "";
    server.stdout.on("data", (b) => {
      out += b;
      const m = out.match(/^listening (\S+)$/m);
      if (m) {
        clearTimeout(timer);
        resolve(m[1]);
      }
    });
    server.once("exit", (code) => {
      clearTimeout(timer);
      reject(new Error(`wireserver exited with ${code} before listening. stderr:\n${stderr}`));
    });
    server.once("error", (e) => {
      clearTimeout(timer);
      reject(new Error(`could not run wireserver: ${e.message}`));
    });
  });

  const base = `http://${addr}`;
  api = new Client(async (req) => {
    sent.push(req);
    const r = await fetch(base + req.path, {
      method: req.method,
      headers: { "Content-Type": "application/json" },
      body: req.body,
    });
    return { status: r.status, body: await r.text() };
  });
});

after(() => {
  // The pipes keep the event loop alive, so let them go before exiting.
  server?.stdout?.destroy();
  server?.stderr?.destroy();
  server?.kill("SIGKILL");
  if (workdir) rmSync(workdir, { recursive: true, force: true });
});

test("a path parameter survives encodeURIComponent and the server's decode", async () => {
  // The case the whole PathValue seam exists for: an id carrying characters
  // the client escapes per segment.
  const topic = await api.getTopic({ topicId: "a/b c" });
  assert.equal(sent.at(-1).path, "/metacensus/api/v1/topic/a%2Fb%20c");
  assert.equal(topic.id, "a/b c");
});

test("protojson's document is the shape ts-proto declares", async () => {
  const topic = await api.getTopic({ topicId: "t1" });

  // useDate=string against a google.protobuf.Timestamp: a string, not a Date
  // and not {seconds, nanos}.
  assert.equal(typeof topic.created, "string");
  assert.equal(topic.created, "2023-11-14T22:13:20Z");

  // EmitDefaultValues against useOptionals=messages: a default-valued scalar
  // is present, an absent message field is absent rather than null.
  assert.equal(topic.description, "");
  assert.ok(Object.hasOwn(topic, "name"));

  // Repeated fields arrive as arrays.
  const list = await api.listTopics({});
  assert.ok(Array.isArray(list.items));
  assert.equal(list.items[0].id, "t1");
});

test("a body the client serialised is a document the server accepts", async () => {
  const topic = await api.createTopic({ name: "n1", description: "d1" });
  assert.equal(sent.at(-1).body, '{"name":"n1","description":"d1"}');
  assert.equal(topic.name, "n1");
  assert.equal(topic.description, "d1");
});

test("stringEnums round-trips as the enum value name", async () => {
  const prop = await api.createProp({ topicId: "t9", type: "Statement", description: "d" });
  // The client stripped topicId out of the body; the server bound it from the
  // path and echoes it back as authorId.
  assert.equal(sent.at(-1).body, '{"type":"Statement","description":"d"}');
  assert.equal(prop.authorId, "t9");
  assert.equal(prop.type, "Statement");
});

test("a handler error arrives as ApiError carrying the envelope", async () => {
  await assert.rejects(api.getTopic({ topicId: "missing" }), (e) => {
    assert.ok(e instanceof ApiError);
    assert.equal(e.status, 404);
    // ApiError.body is raw text on purpose; this is what a caller parsing it
    // defensively finds.
    assert.deepEqual(JSON.parse(e.body), { code: "topic_not_found", error: 'no topic "missing"' });
    return true;
  });
});

test("an unimplemented route is 501, not 404", async () => {
  await assert.rejects(api.getUser({ userId: "u1" }), (e) => {
    assert.equal(e.status, 501);
    assert.equal(JSON.parse(e.body).code, "unimplemented");
    return true;
  });
});

test("a field the contract does not declare is refused rather than ignored", async () => {
  // Not reachable through the typed methods; a JavaScript caller can still do
  // it, and UnmarshalOptions says unknown fields are an error.
  const loose = /** @type {any} */ (api);
  await assert.rejects(loose.createTopic({ name: "z", description: "", extra: 1 }), (e) => {
    assert.equal(e.status, 400);
    assert.equal(JSON.parse(e.body).code, "body_invalid");
    return true;
  });
});
