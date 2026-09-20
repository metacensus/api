// Client is batteries-included: it owns fetch, holds the token, and signs
// writes through an injected signer. These drive it against a fake fetch and
// check the request that goes out, the token lifecycle, and the two shapes a
// read comes back in — flat view or signed envelope.
import { test } from "node:test";
import assert from "node:assert/strict";
import { Client, PublicClient, ApiError } from "../dist/src/client.js";

const BASE = "https://api.test";

// A fake fetch that records each call and replies with a canned response.
// reply is a {status, body} object, or a function of (url, init).
function fetcher(reply = { status: 200, body: "{}" }) {
  const calls = [];
  const fetch = async (url, init) => {
    calls.push({ url, method: init.method, headers: init.headers, body: init.body });
    const r = typeof reply === "function" ? reply(url, init) : reply;
    return { status: r.status ?? 200, text: async () => r.body ?? "" };
  };
  return { calls, fetch };
}

const json = (body) => ({ status: 200, body: JSON.stringify(body) });

// --- reads: flat view vs signed envelope --------------------------------

test("a read flattens the envelope to {id, recorded, ...content}", async () => {
  const env = { id: "t1", recorded: "2023-01-01T00:00:00Z", content: { name: "N", description: "d" }, userSignature: { signerId: "u1" } };
  const { calls, fetch } = fetcher(json(env));
  const topic = await new Client({ baseUrl: BASE, fetch }).getTopic({ topicId: "t1" });

  assert.deepEqual(topic, { id: "t1", recorded: "2023-01-01T00:00:00Z", name: "N", description: "d" });
  assert.equal("userSignature" in topic, false);
  assert.equal("content" in topic, false);
  assert.equal(calls[0].method, "GET");
  assert.equal(calls[0].url, `${BASE}/metacensus/api/v1/topic/t1`);
  assert.equal(calls[0].body, undefined);
});

test("the getXSigned variant returns the raw envelope, unchanged", async () => {
  const env = { id: "t1", recorded: "r", content: { name: "N", description: "d" }, userSignature: { signerId: "u1", value: "SIG" } };
  const signed = await new Client({ baseUrl: BASE, fetch: fetcher(json(env)).fetch }).getTopicSigned({ topicId: "t1" });
  assert.deepEqual(signed, env);
});

test("a vote envelope has no id, in either shape", async () => {
  const items = [{ recorded: "r", content: { topicId: "t1", propId: "p1", userId: "u1", position: "For", explanation: "", citations: [] }, userSignature: {} }];
  const c = new Client({ baseUrl: BASE, fetch: fetcher(json({ items })).fetch });

  const flat = await c.listVotes({ topicId: "t1", propId: "p1" });
  assert.equal("id" in flat[0], false);
  assert.equal(flat[0].position, "For");

  const signed = await c.listVotesSigned({ topicId: "t1", propId: "p1" });
  assert.deepEqual(signed, items);
});

test("a Member is already flat and has no signed variant", () => {
  assert.equal(typeof Client.prototype.getMember, "function");
  assert.equal(Client.prototype.getMemberSigned, undefined);
});

test("getMember returns the record unchanged", async () => {
  const member = { id: "u1", joined: "j" };
  const got = await new Client({ baseUrl: BASE, fetch: fetcher(json(member)).fetch }).getMember({ topicId: "t1", userId: "u1" });
  assert.deepEqual(got, member);
});

test("a list flattens per item; the signed list keeps the envelopes", async () => {
  const items = [
    { id: "t1", recorded: "r1", content: { name: "A", description: "" }, userSignature: {} },
    { id: "t2", recorded: "r2", content: { name: "B", description: "" }, userSignature: {} },
  ];
  const c = new Client({ baseUrl: BASE, fetch: fetcher(json({ items })).fetch });
  assert.deepEqual(await c.listTopics({}), [
    { id: "t1", recorded: "r1", name: "A", description: "" },
    { id: "t2", recorded: "r2", name: "B", description: "" },
  ]);
  assert.deepEqual(await c.listTopicsSigned({}), items);
});

// --- writes: flat content in, signer drives the envelope ----------------

test("createTopic signs under its contentType, sends the envelope, returns the flat view", async () => {
  let signedWith;
  const signer = async (content, contentType) => {
    signedWith = { content, contentType };
    return { signerId: "u1", value: "SIG" };
  };
  const created = { id: "t9", recorded: "r", content: { name: "N", description: "" }, userSignature: { signerId: "u1", value: "SIG" } };
  const { calls, fetch } = fetcher(json(created));

  const view = await new Client({ baseUrl: BASE, fetch, signer }).createTopic({ content: { name: "N", description: "" } });

  assert.deepEqual(signedWith, { content: { name: "N", description: "" }, contentType: "metacensus.v1.Topic" });
  const body = JSON.parse(calls[0].body);
  assert.deepEqual(body.content, { name: "N", description: "" });
  assert.deepEqual(body.userSignature, { signerId: "u1", value: "SIG" });
  assert.equal(calls[0].url, `${BASE}/metacensus/api/v1/topic`);
  assert.deepEqual(view, { id: "t9", recorded: "r", name: "N", description: "" });
});

test("createProp keeps the path id out of the body and signs under Prop", async () => {
  let contentType;
  const signer = async (_c, ct) => {
    contentType = ct;
    return { value: "S" };
  };
  const created = { id: "p1", recorded: "r", content: { topicId: "t1", type: "Statement", description: "d" }, userSignature: {} };
  const { calls, fetch } = fetcher(json(created));

  const view = await new Client({ baseUrl: BASE, fetch, signer }).createProp({
    topicId: "t1",
    content: { topicId: "t1", type: "Statement", description: "d" },
  });

  assert.equal(contentType, "metacensus.v1.Prop");
  assert.equal(calls[0].url, `${BASE}/metacensus/api/v1/topic/t1/prop`);
  const body = JSON.parse(calls[0].body);
  assert.equal(body.topicId, undefined);
  assert.deepEqual(body.content, { topicId: "t1", type: "Statement", description: "d" });
  assert.ok(body.userSignature);
  assert.deepEqual(view, { id: "p1", recorded: "r", topicId: "t1", type: "Statement", description: "d" });
});

test("setVote binds both path ids and signs under Vote", async () => {
  let contentType;
  const signer = async (_c, ct) => {
    contentType = ct;
    return { value: "S" };
  };
  const set = { recorded: "r", content: { topicId: "t1", propId: "p1", userId: "u1", position: "Against", explanation: "", citations: [] }, userSignature: {} };
  const { calls, fetch } = fetcher(json(set));

  const view = await new Client({ baseUrl: BASE, fetch, signer }).setVote({ topicId: "t1", propId: "p1", content: set.content });

  assert.equal(contentType, "metacensus.v1.Vote");
  assert.equal(calls[0].url, `${BASE}/metacensus/api/v1/topic/t1/prop/p1/vote`);
  assert.equal("id" in view, false);
  assert.equal(view.position, "Against");
});

test("a write with no signer throws, naming the fix", async () => {
  const { fetch } = fetcher(json({}));
  await assert.rejects(
    new Client({ baseUrl: BASE, fetch }).createTopic({ content: { name: "N", description: "" } }),
    /needs a signer/,
  );
});

// --- auth: the token lifecycle ------------------------------------------

test("login stores the token, and later requests carry it", async () => {
  const { calls, fetch } = fetcher((url) =>
    url.endsWith("/login") ? json({ token: "TK" }) : json({ id: "t1", content: { name: "", description: "" }, userSignature: {} }),
  );
  const client = new Client({ baseUrl: BASE, fetch });

  const session = await client.login({ email: "a@b.c", password: "x" });
  assert.equal(session.token, "TK");
  assert.equal(client.token, "TK");
  assert.equal(calls[0].headers["Authorization"], undefined); // none before login resolves

  await client.getTopic({ topicId: "t1" });
  assert.equal(calls[1].headers["Authorization"], "Bearer TK");
});

test("signUp signs the User content, then stores the token", async () => {
  let signedWith;
  const signer = async (content, contentType) => {
    signedWith = { content, contentType };
    return { publicKey: "PK", value: "SIG" };
  };
  const { calls, fetch } = fetcher(json({ token: "TK2" }));
  const client = new Client({ baseUrl: BASE, fetch, signer });

  const session = await client.signUp({ content: { name: "Ada" }, password: "hunter2" });

  assert.deepEqual(signedWith, { content: { name: "Ada" }, contentType: "metacensus.v1.User" });
  const body = JSON.parse(calls[0].body);
  assert.deepEqual(body.content, { name: "Ada" });
  assert.equal(body.password, "hunter2");
  assert.ok(body.userSignature);
  assert.equal(session.token, "TK2");
  assert.equal(client.token, "TK2");
});

test("logout clears the token", async () => {
  const { fetch } = fetcher({ status: 200, body: "" });
  const client = new Client({ baseUrl: BASE, fetch, token: "TK" });
  assert.equal(client.token, "TK");
  await client.logout({});
  assert.equal(client.token, undefined);
});

// --- transport concerns Client now owns ---------------------------------

test("a non-2xx throws ApiError carrying status, url and raw body", async () => {
  const { fetch } = fetcher({ status: 404, body: '{"error":"no such topic"}' });
  await assert.rejects(new Client({ baseUrl: BASE, fetch }).getTopic({ topicId: "x" }), (e) => {
    assert.ok(e instanceof ApiError);
    assert.equal(e.status, 404);
    assert.equal(e.body, '{"error":"no such topic"}');
    assert.equal(e.url, `${BASE}/metacensus/api/v1/topic/x`);
    return true;
  });
});

test("path params are percent-encoded per segment", async () => {
  const { calls, fetch } = fetcher(json({ id: "u1", joined: "j" }));
  await new Client({ baseUrl: BASE, fetch }).getMember({ topicId: "a/b c", userId: "ü?#&" });
  assert.equal(calls[0].url, `${BASE}/metacensus/api/v1/topic/a%2Fb%20c/member/%C3%BC%3F%23%26`);
});

test("an empty 2xx body becomes {}", async () => {
  const { fetch } = fetcher({ status: 200, body: "" });
  assert.deepEqual(await new Client({ baseUrl: BASE, fetch }).logout({}), {});
});

test("Content-Type is set only when there is a body", async () => {
  const { calls, fetch } = fetcher((url) =>
    url.endsWith("/login") ? json({ token: "TK" }) : json({ id: "t", content: {}, userSignature: {} }),
  );
  const client = new Client({ baseUrl: BASE, fetch });
  await client.login({ email: "a", password: "b" });
  await client.getTopic({ topicId: "t" });
  assert.equal(calls[0].headers["Content-Type"], "application/json"); // POST body
  assert.equal(calls[1].headers["Content-Type"], undefined); // GET, no body
});

test("prefix can be overridden", async () => {
  const { calls, fetch } = fetcher(json({ id: "t", content: {}, userSignature: {} }));
  await new Client({ baseUrl: BASE, fetch, prefix: "/other" }).getTopic({ topicId: "t" });
  assert.equal(calls[0].url, `${BASE}/other/topic/t`);
});

// --- the public surface -------------------------------------------------

test("PublicClient posts to the public surface without credentials", async () => {
  const { calls, fetch } = fetcher(json({ ok: true }));
  await new PublicClient({ baseUrl: BASE, fetch }).submitPartnerInterest({ name: "n", email: "e", interests: [], message: "", website: "" });
  assert.equal(calls[0].method, "POST");
  assert.equal(calls[0].url, `${BASE}/metacensus/public/partner`);
  assert.equal(calls[0].headers["Authorization"], undefined);
  assert.ok(calls[0].body);
});
