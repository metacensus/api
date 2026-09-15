// Exercises the generated client against the built dist with a fake
// transport. No devDependency added: node:test and node:assert are built
// into Node. Run: npm run build && node --test test/ (or npm test, which
// does both).
import { test } from "node:test";
import assert from "node:assert/strict";
import { Client, ApiError } from "../dist/src/client.js";

function fake(status = 200, body = "{}") {
  const calls = [];
  const transport = async (req) => {
    calls.push(req);
    return { status, body };
  };
  return { calls, transport };
}

test("path params are percent-encoded per segment; GET has no body", async () => {
  const { calls, transport } = fake();
  await new Client(transport).getMember({ topicId: "a/b c", userId: "ü?#&" });
  assert.equal(calls[0].method, "GET");
  assert.equal(calls[0].path, "/metacensus/api/v1/topic/a%2Fb%20c/member/%C3%BC%3F%23%26");
  assert.equal(calls[0].body, undefined);
  assert.equal(Object.hasOwn(calls[0], "body"), true, "key present with undefined value");
});

test("body:* excludes path-bound fields and is serialised once", async () => {
  const { calls, transport } = fake();
  await new Client(transport).createTopic({ name: "n", description: "d" });
  assert.equal(calls[0].path, "/metacensus/api/v1/topic");
  assert.equal(calls[0].body, '{"name":"n","description":"d"}');
});

test("a route with no path params sends the whole request as the body", async () => {
  const { calls, transport } = fake();
  await new Client(transport).login({ email: "a@b.com", password: "x" });
  assert.equal(calls[0].path, "/metacensus/api/v1/login");
  assert.equal(calls[0].body, '{"email":"a@b.com","password":"x"}');
});

test("a route with no body and no path params sends neither", async () => {
  const { calls, transport } = fake();
  await new Client(transport).listTopics({});
  assert.equal(calls[0].method, "GET");
  assert.equal(calls[0].path, "/metacensus/api/v1/topic");
  assert.equal(calls[0].body, undefined);
});

test("non-2xx throws ApiError carrying status, raw body and the path requested", async () => {
  const { transport } = fake(404, '{"error":"no such topic"}');
  await assert.rejects(new Client(transport).getTopic({ topicId: "x" }), (e) => {
    assert.ok(e instanceof ApiError);
    assert.equal(e.status, 404);
    assert.equal(e.body, '{"error":"no such topic"}');
    // The path the transport was given, prefix included — an error naming a
    // path nobody requested is a worse error.
    assert.equal(e.path, "/metacensus/api/v1/topic/x");
    assert.equal(e.message, "GET /metacensus/api/v1/topic/x: HTTP 404");
    return true;
  });
});

test("2xx is parsed and cast, shape unchecked", async () => {
  const { transport } = fake(200, '{"id":"t","name":"n","description":"","unexpected":1}');
  const topic = await new Client(transport).getTopic({ topicId: "t" });
  assert.equal(topic.unexpected, 1);
});

test("empty 2xx body becomes {}", async () => {
  const { transport } = fake(200, "");
  assert.deepEqual(await new Client(transport).logout({}), {});
});

test("401 reaches the transport first: the transport can act before the client sees it", async () => {
  let policyRan = false;
  const transport = async () => {
    policyRan = true;
    throw new Error("session expired");
  };
  await assert.rejects(new Client(transport).getSelf({}), /session expired/);
  assert.ok(policyRan);
});

test("prefix can be overridden, mirroring server.Runtime.Prefix", async () => {
  const { calls, transport } = fake();
  await new Client(transport, "/other/prefix").getTopic({ topicId: "t" });
  assert.equal(calls[0].path, "/other/prefix/topic/t");
});

test("body-carrying route with a path param strips the path field from the body", async () => {
  const { calls, transport } = fake();
  await new Client(transport).createProp({ topicId: "t1", type: "Statement", description: "d" });
  assert.equal(calls[0].path, "/metacensus/api/v1/topic/t1/prop");
  assert.equal(calls[0].body, '{"type":"Statement","description":"d"}');
});
