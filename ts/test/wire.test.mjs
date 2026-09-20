// The generated client against the generated server, over real HTTP.
//
// This pins the *document* the two generators emit: protojson (Go) and ts-proto
// (TypeScript) are separate projects configured to meet in the middle —
// buf.gen.yaml's useDate=string, stringEnums, forceLong=string and
// unrecognizedEnum=false against go/wire.go's EmitDefaultValues — and nothing
// else checks the pairing. A drift there is a document that only round-trips on
// the machine that made it. The getXSigned reads expose the raw envelope, so the
// exact keys a signature would cover are what these assertions see.
//
// What this file no longer carries is the cross-language signing chain in
// conjunction — TypeScript signs, Go verifies, and the reverse, plus the write
// path. That is an integration concern, deferred to a suite over a mock
// persistence layer: https://github.com/metacensus/api/issues/31. The signing
// module's own behaviour is unit-tested in signing.test.mjs.
//
// The server is routegen/wireserver, compiled to a temporary directory and run;
// it lives in the routegen module so a fixture never reaches the published
// go.mod. Compiled rather than `go run`, whose child process outlives the
// parent and holds the pipe this process reads. So this file needs Go on PATH,
// which `make check` and CI both have and a bare `npm test` does not — the
// failure says so.

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { Client, ApiError } from "../dist/src/client.js";
import { verifyUser, participantPolicy, decodePublicKey } from "../dist/signing.js";

const routegen = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "routegen");

let server;
let workdir;
let client;
/** The wireserver's public key, read off its stdout: it mints one per run. */
let serverPublicKey;
/** The origin the wireserver's assertions carry, also read off stdout. */
let serverOrigin;
/** Every URL the client fetched, for the path-encoding assertion. */
const fetched = [];

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
      `could not build routegen/wireserver (is Go on PATH?): ${e.stderr?.toString() || e.message}`,
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
        serverPublicKey = out.match(/^signing-key (\S+)$/m)?.[1];
        serverOrigin = out.match(/^origin (\S+)$/m)?.[1];
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

  const recordingFetch = async (url, init) => {
    fetched.push(url);
    return fetch(url, init);
  };
  client = new Client({ baseUrl: `http://${addr}`, fetch: recordingFetch });
});

after(() => {
  // The pipes keep the event loop alive, so let them go before exiting.
  server?.stdout?.destroy();
  server?.stderr?.destroy();
  server?.kill("SIGKILL");
  if (workdir) rmSync(workdir, { recursive: true, force: true });
});

test("a path parameter survives encodeURIComponent and the server's decode", async () => {
  const topic = await client.getTopicSigned({ topicId: "a/b c" });
  assert.ok(fetched.at(-1).endsWith("/metacensus/api/v1/topic/a%2Fb%20c"));
  assert.equal(topic.id, "a/b c");
});

test("protojson's document is the shape ts-proto declares", async () => {
  const topic = await client.getTopicSigned({ topicId: "t1" });

  // useDate=string against a google.protobuf.Timestamp: a string, not a Date
  // and not {seconds, nanos}. recorded is the server's observation; the signer's
  // own claim is inside the signature, and they differ here on purpose.
  assert.equal(typeof topic.recorded, "string");
  assert.equal(topic.recorded, "2023-11-14T22:13:20Z");
  assert.equal(topic.userSignature.time, "2023-11-14T22:13:19Z");

  // The interpretation is a record property, sealed by the signature: the
  // scheme version and the content's full proto name.
  assert.equal(topic.interpretation.contentType, "metacensus.v1.Topic");

  // EmitDefaultValues against useOptionals=messages: a default-valued scalar is
  // present, an absent message field absent rather than null. The canonical form
  // a signature covers is built from exactly these keys.
  assert.equal(topic.content.description, "");
  assert.ok(Object.hasOwn(topic.content, "name"));

  const list = await client.listTopicsSigned({});
  assert.ok(Array.isArray(list));
  assert.equal(list[0].id, "t1");
});

test("a signature Go made verifies in TypeScript", async () => {
  const topic = await client.getTopicSigned({ topicId: "t1" });
  const pub = await decodePublicKey(serverPublicKey);
  const policy = participantPolicy(serverOrigin);

  // The whole WebAuthn assertion round-trips: Go builds a DER signature over
  // authenticatorData ‖ SHA-256(clientDataJSON), TypeScript recomputes the
  // challenge, checks the binding and policy, and verifies the DER signature.
  assert.ok(await verifyUser(pub, topic.content, topic.interpretation, topic.userSignature, policy));
  assert.ok(
    !(await verifyUser(pub, { ...topic.content, name: "something else" }, topic.interpretation, topic.userSignature, policy)),
    "an altered content verified",
  );
});

test("a handler error arrives as ApiError carrying the envelope", async () => {
  await assert.rejects(client.getTopicSigned({ topicId: "missing" }), (e) => {
    assert.ok(e instanceof ApiError);
    assert.equal(e.status, 404);
    // ApiError.body is raw text on purpose; this is what a caller parsing it
    // defensively finds.
    assert.deepEqual(JSON.parse(e.body), { code: "topic_not_found", error: 'no topic "missing"' });
    return true;
  });
});

test("an unimplemented route is 501, not 404", async () => {
  await assert.rejects(client.getUserSigned({ userId: "u1" }), (e) => {
    assert.equal(e.status, 501);
    assert.equal(JSON.parse(e.body).code, "unimplemented");
    return true;
  });
});
