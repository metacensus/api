// The sugar Client is generated from the same descriptor walk as ClientSigned,
// so what it flattens has to match the four response shapes the envelope comes
// in — and the write path has to assemble an envelope the signing chain would
// accept. These drive the generated Client against a fake transport and check
// both: the unwrapping per entity shape, and that writes sign the right content
// under the right contentType.
import { test } from "node:test";
import assert from "node:assert/strict";
import { Client } from "../dist/src/client.js";
import { routes, apiPrefix } from "../dist/src/route-manifest.js";

// A transport that records what it was handed and replies with a canned body.
// bodyFor is the response document (object serialised, or a string sent raw).
function fake(bodyFor) {
  const calls = [];
  const transport = async (req) => {
    calls.push(req);
    const body = typeof bodyFor === "function" ? bodyFor(req) : bodyFor;
    return { status: 200, body: typeof body === "string" ? body : JSON.stringify(body) };
  };
  return { calls, transport };
}

// --- reads: the four response shapes ------------------------------------

test("an id-bearing envelope (Topic) flattens to {id, recorded, ...content}", async () => {
  const env = {
    id: "t1",
    recorded: "2023-01-01T00:00:00Z",
    content: { name: "Nutrition", description: "d" },
    userSignature: { signerId: "u1", value: "SIG" },
  };
  const { transport, calls } = fake(env);
  const topic = await new Client(transport).getTopic({ topicId: "t1" });

  assert.deepEqual(topic, {
    id: "t1",
    recorded: "2023-01-01T00:00:00Z",
    name: "Nutrition",
    description: "d",
  });
  // The envelope overhead is gone, not merely hidden.
  assert.equal("userSignature" in topic, false);
  assert.equal("content" in topic, false);
  // A read needs no signer, and the path is the raw client's.
  assert.equal(calls[0].method, "GET");
  assert.equal(calls[0].path, "/metacensus/api/v1/topic/t1");
});

test("Prop and User flatten the same way (id-bearing envelopes)", async () => {
  const prop = await new Client(
    fake({ id: "p1", recorded: "r", content: { topicId: "t1", type: "Statement", description: "d" }, userSignature: {} }).transport,
  ).getProp({ topicId: "t1", propId: "p1" });
  assert.deepEqual(prop, { id: "p1", recorded: "r", topicId: "t1", type: "Statement", description: "d" });

  const user = await new Client(
    fake({ id: "u1", recorded: "r", content: { name: "Ada" }, userSignature: {} }).transport,
  ).getUser({ userId: "u1" });
  assert.deepEqual(user, { id: "u1", recorded: "r", name: "Ada" });
});

test("a Vote envelope has no id, and the flat view has none either", async () => {
  const list = {
    items: [
      {
        recorded: "2023-01-02T00:00:00Z",
        content: { topicId: "t1", propId: "p1", userId: "u1", position: "For", explanation: "", citations: [] },
        userSignature: {},
      },
    ],
  };
  const votes = await new Client(fake(list).transport).listVotes({ topicId: "t1", propId: "p1" });

  assert.equal(votes.length, 1);
  assert.equal("id" in votes[0], false); // the shape the sugar must not invent
  assert.equal(votes[0].recorded, "2023-01-02T00:00:00Z");
  assert.equal(votes[0].position, "For");
  assert.equal("content" in votes[0], false);
});

test("a Member is already flat, so it is returned unchanged", async () => {
  const member = { id: "u1", joined: "2023-01-03T00:00:00Z" };
  const got = await new Client(fake(member).transport).getMember({ topicId: "t1", userId: "u1" });
  assert.deepEqual(got, member);
});

test("a list response is unwrapped to an array of flat views", async () => {
  const list = {
    items: [
      { id: "t1", recorded: "r1", content: { name: "A", description: "" }, userSignature: {} },
      { id: "t2", recorded: "r2", content: { name: "B", description: "" }, userSignature: {} },
    ],
  };
  const topics = await new Client(fake(list).transport).listTopics({});
  assert.deepEqual(topics, [
    { id: "t1", recorded: "r1", name: "A", description: "" },
    { id: "t2", recorded: "r2", name: "B", description: "" },
  ]);
});

test("listMembers unwraps {items} without flattening (already flat)", async () => {
  const list = { items: [{ id: "u1", joined: "j1" }, { id: "u2", joined: "j2" }] };
  const members = await new Client(fake(list).transport).listMembers({ topicId: "t1" });
  assert.deepEqual(members, list.items);
});

// --- writes: flat content in, signer drives the envelope ----------------

test("createTopic signs the content under its contentType and returns the flat view", async () => {
  let signedWith;
  const signer = async (content, contentType) => {
    signedWith = { content, contentType };
    return { signerId: "u1", value: "SIG" };
  };
  const created = { id: "t9", recorded: "2023-01-04T00:00:00Z", content: { name: "N", description: "" }, userSignature: { signerId: "u1", value: "SIG" } };
  const { transport, calls } = fake(created);

  const view = await new Client(transport, signer).createTopic({ content: { name: "N", description: "" } });

  // The sugar named the contentType, not the caller — the footgun sign/verify
  // leave to the caller.
  assert.deepEqual(signedWith, { content: { name: "N", description: "" }, contentType: "metacensus.v1.Topic" });
  // What went out is the full envelope the raw client expects.
  const sent = JSON.parse(calls[0].body);
  assert.deepEqual(sent.content, { name: "N", description: "" });
  assert.deepEqual(sent.userSignature, { signerId: "u1", value: "SIG" });
  assert.equal(calls[0].path, "/metacensus/api/v1/topic");
  // And the reply is flattened like a read.
  assert.deepEqual(view, { id: "t9", recorded: "2023-01-04T00:00:00Z", name: "N", description: "" });
});

test("createProp keeps the path id out of the body and signs under Prop", async () => {
  let contentType;
  const signer = async (_content, ct) => {
    contentType = ct;
    return { value: "S" };
  };
  const created = { id: "p1", recorded: "r", content: { topicId: "t1", type: "Statement", description: "d" }, userSignature: {} };
  const { transport, calls } = fake(created);

  const view = await new Client(transport, signer).createProp({
    topicId: "t1",
    content: { topicId: "t1", type: "Statement", description: "d" },
  });

  assert.equal(contentType, "metacensus.v1.Prop");
  assert.equal(calls[0].path, "/metacensus/api/v1/topic/t1/prop");
  const sent = JSON.parse(calls[0].body);
  assert.equal(sent.topicId, undefined); // path-bound, stripped from the body
  assert.deepEqual(sent.content, { topicId: "t1", type: "Statement", description: "d" });
  assert.ok(sent.userSignature);
  assert.deepEqual(view, { id: "p1", recorded: "r", topicId: "t1", type: "Statement", description: "d" });
});

test("setVote signs under Vote and binds both path ids", async () => {
  let contentType;
  const signer = async (_content, ct) => {
    contentType = ct;
    return { value: "S" };
  };
  const set = { recorded: "r", content: { topicId: "t1", propId: "p1", userId: "u1", position: "Against", explanation: "", citations: [] }, userSignature: {} };
  const { transport, calls } = fake(set);

  const view = await new Client(transport, signer).setVote({
    topicId: "t1",
    propId: "p1",
    content: set.content,
  });

  assert.equal(contentType, "metacensus.v1.Vote");
  assert.equal(calls[0].path, "/metacensus/api/v1/topic/t1/prop/p1/vote");
  assert.equal("id" in view, false);
  assert.equal(view.position, "Against");
});

test("a write with no signer throws, naming the fix", async () => {
  const { transport } = fake({});
  await assert.rejects(
    new Client(transport).createTopic({ content: { name: "N", description: "" } }),
    /needs a signer/,
  );
});

// --- drift guard --------------------------------------------------------

// The sugar covers every authenticated record route and only those: a read or
// signed write whose response is an envelope, a list of envelopes, or an
// already-flat record. The four plain-response routes stay on ClientSigned.
// This is the one place that set is restated, so a new record route that the
// generator failed to sugar (or a plain one it wrongly did) fails here.
test("Client sugars every record route and excludes the plain ones", () => {
  const plain = new Set(["login", "signUp", "logout", "healthcheck"]);
  const authRoutes = routes.filter((r) => r.prefix === apiPrefix);
  assert.ok(authRoutes.length > plain.size, "no authenticated routes to check");

  for (const r of authRoutes) {
    const name = r.rpc[0].toLowerCase() + r.rpc.slice(1);
    const has = typeof Client.prototype[name] === "function";
    if (plain.has(name)) {
      assert.equal(has, false, `${name} returns a plain response and must stay off the sugar Client`);
    } else {
      assert.equal(has, true, `${name} is a record route with no sugar Client method`);
    }
  }
});
