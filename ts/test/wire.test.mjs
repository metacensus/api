// The generated client against the generated server, over real HTTP.
//
// **This is the gate on signature validity, not a nice-to-have.** The digest a
// participant signs is JCS (RFC 8785) over the contract's own JSON, so it is
// only one number in both languages if protojson and ts-proto emit the same
// document — which is exactly the pairing this file was already pinning when
// all it protected was readability. A drift in buf.gen.yaml's five options or
// in go/wire.go's EmitDefaultValues now shows up as a signature that verifies
// on the machine that made it and nowhere else, so the last tests here have
// each language verify what the other signed.
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
import {
  SPEC,
  canonicalize,
  sign,
  verify,
  generateKeyPair,
  encodePublicKey,
  decodePublicKey,
  keyId,
} from "../dist/signing.js";

const routegen = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "routegen");

let server;
let workdir;
let api;
/** The wireserver's public key, read off its stdout: it mints one per run. */
let serverPublicKey;
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
        // Printed before "listening", so by the time there is an address
        // there is a key.
        serverPublicKey = out.match(/^signing-key (\S+)$/m)?.[1];
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

/**
 * A syntactically complete signature that signs nothing.
 *
 * go/server checks that both halves of a signed request are present and that
 * the ids agree; it never looks at the value. The tests above are about
 * encoding, so they send this; the tests at the bottom of this file are about
 * signatures, and make real ones.
 */
function emptySignature() {
  return {
    signerId: "",
    keyId: "",
    alg: "Unspecified",
    publicKey: "",
    signingTime: "2024-01-01T00:00:00Z",
    spec: "",
    contentType: "",
    value: "",
  };
}

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
  // and not {seconds, nanos}. `recorded` is the server's observation; the
  // signer's own claim is inside the signature, and they differ here on
  // purpose.
  assert.equal(typeof topic.recorded, "string");
  assert.equal(topic.recorded, "2023-11-14T22:13:20Z");
  assert.equal(topic.userSignature.signingTime, "2023-11-14T22:13:19Z");

  // EmitDefaultValues against useOptionals=messages: a default-valued scalar
  // is present, an absent message field is absent rather than null. Both
  // matter more than they did: the canonical form a signature covers is built
  // from exactly these keys.
  assert.equal(topic.content.description, "");
  assert.ok(Object.hasOwn(topic.content, "name"));
  assert.equal(topic.userSignature.publicKey, "");

  // Repeated fields arrive as arrays.
  const list = await api.listTopics({});
  assert.ok(Array.isArray(list.items));
  assert.equal(list.items[0].id, "t1");
});

test("a body the client serialised is a document the server accepts", async () => {
  const topic = await api.createTopic({
    content: { name: "n1", description: "d1" },
    userSignature: emptySignature(),
  });
  assert.ok(sent.at(-1).body.startsWith('{"content":{"name":"n1","description":"d1"},"userSignature":{'));
  assert.equal(topic.content.name, "n1");
  assert.equal(topic.content.description, "d1");
});

test("stringEnums round-trips as the enum value name", async () => {
  const prop = await api.createProp({
    topicId: "t9",
    content: { topicId: "t9", type: "Statement", description: "d" },
    userSignature: emptySignature(),
  });
  // The client stripped the top-level topicId out of the body; the server
  // bound it from the path and echoes it back as the minted id. The copy
  // inside content is the signed one and travelled in the body.
  assert.ok(!JSON.parse(sent.at(-1).body).topicId);
  assert.equal(JSON.parse(sent.at(-1).body).content.topicId, "t9");
  assert.equal(prop.id, "t9");
  assert.equal(prop.content.type, "Statement");
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
  await assert.rejects(
    loose.createTopic({
      content: { name: "z", description: "" },
      userSignature: emptySignature(),
      extra: 1,
    }),
    (e) => {
      assert.equal(e.status, 400);
      assert.equal(JSON.parse(e.body).code, "body_invalid");
      return true;
    },
  );
});

// --- the signing chain, across both languages ------------------------------
//
// Everything above pins the *document* the two generators emit. These pin what
// that buys: one digest. Each direction has one language sign and the other
// verify, so a divergence in protojson's output, in ts-proto's options, or in
// either canonicaliser fails here rather than in a chaincode nobody is
// watching.

test("the canonical form sorts and escapes as RFC 8785 says, not as JSON.stringify would", () => {
  // Go's TestCanonicalOrdersAndEscapes has these same cases. Both are here so
  // that a divergence reads as a string rather than as a failed signature.
  assert.equal(canonicalize({ b: 1, a: 2, C: 3 }), '{"C":3,"a":2,"b":1}');
  assert.equal(canonicalize({ x: [3, 1, 2] }), '{"x":[3,1,2]}');
  assert.equal(canonicalize({ s: "a<b&c" }), '{"s":"a<b&c"}');
  assert.equal(canonicalize({ s: "\u0000\u001f" }), '{"s":"\\u0000\\u001f"}');
  assert.equal(canonicalize({ s: "h\u00e9llo \u2028" }), '{"s":"h\u00e9llo \u2028"}');
});

test("a signature TypeScript made verifies in Go", async () => {
  const { privateKey, publicKey } = await generateKeyPair();
  const content = { name: "Ada", email: "ada@example.org", country: "GB" };
  const signature = {
    signerId: "", // no id is minted yet: this request is what enrols the key
    keyId: await keyId(publicKey),
    alg: "Es384",
    publicKey: await encodePublicKey(publicKey),
    signingTime: "2024-01-01T00:00:00Z",
    spec: SPEC,
    contentType: "metacensus.v1.UserContent",
    value: "",
  };
  signature.value = await sign(privateKey, content, signature);

  const session = await api.signUp({ content, password: "hunter2", userSignature: signature });
  assert.equal(session.token, "signed-up:ada@example.org");
});

test("Go rejects a TypeScript signature over content that changed in flight", async () => {
  const { privateKey, publicKey } = await generateKeyPair();
  const content = { name: "Ada", email: "ada@example.org", country: "GB" };
  const signature = {
    signerId: "",
    keyId: await keyId(publicKey),
    alg: "Es384",
    publicKey: await encodePublicKey(publicKey),
    signingTime: "2024-01-01T00:00:00Z",
    spec: SPEC,
    contentType: "metacensus.v1.UserContent",
    value: "",
  };
  signature.value = await sign(privateKey, content, signature);

  await assert.rejects(
    api.signUp({
      content: { ...content, email: "mallory@example.org" },
      password: "hunter2",
      userSignature: signature,
    }),
    (e) => {
      assert.equal(e.status, 400);
      assert.equal(JSON.parse(e.body).code, "signature_invalid");
      return true;
    },
  );
});

test("Go rejects an enrolled key that is not the one keyId names", async () => {
  // Trust on first use is trust in the key offered, not in anybody: without
  // this check a participant could enrol someone else's public key and later
  // claim the signatures made with it.
  const mine = await generateKeyPair();
  const theirs = await generateKeyPair();
  const content = { name: "Ada", email: "ada@example.org", country: "GB" };
  const signature = {
    signerId: "",
    keyId: await keyId(theirs.publicKey),
    alg: "Es384",
    publicKey: await encodePublicKey(mine.publicKey),
    signingTime: "2024-01-01T00:00:00Z",
    spec: SPEC,
    contentType: "metacensus.v1.UserContent",
    value: "",
  };
  signature.value = await sign(mine.privateKey, content, signature);

  await assert.rejects(
    api.signUp({ content, password: "hunter2", userSignature: signature }),
    (e) => {
      assert.equal(JSON.parse(e.body).code, "key_not_bound");
      return true;
    },
  );
});

test("a signature Go made verifies in TypeScript", async () => {
  const topic = await api.getTopic({ topicId: "t1" });
  const pub = await decodePublicKey(serverPublicKey);

  assert.ok(await verify(pub, topic.content, topic.userSignature));
  assert.ok(
    !(await verify(pub, { ...topic.content, name: "something else" }, topic.userSignature)),
    "an altered content verified",
  );
});

test("a signature survives the server decoding and re-encoding the document", async () => {
  // The whole reason the digest is over canonical JSON rather than over the
  // octets sent: an API server decodes a request and hands the message on, so
  // whatever reaches persistence has been through protojson at least once. A
  // string carrying every escape the two canonicalisers have to agree about
  // makes that round trip visible.
  const { privateKey, publicKey } = await generateKeyPair();
  const content = {
    name: "quotes \" backslash \\ tab \t newline \n",
    description: "h\u00e9llo \u2028 \ud83d\ude00 <&> \u0001",
  };
  const signature = {
    signerId: "u1",
    keyId: await keyId(publicKey),
    alg: "Es384",
    publicKey: "",
    signingTime: "2024-01-01T00:00:00Z",
    spec: SPEC,
    contentType: "metacensus.v1.TopicContent",
    value: "",
  };
  signature.value = await sign(privateKey, content, signature);

  const topic = await api.createTopic({ content, userSignature: signature });

  // wireserver echoes both halves unchanged — the server wraps, it never
  // modifies — so what comes back has been through protojson's decoder and
  // encoder and must still verify under the key that signed the original.
  assert.notEqual(topic.recorded, undefined, "the server added its own fields");
  assert.ok(await verify(publicKey, topic.content, topic.userSignature));
});
