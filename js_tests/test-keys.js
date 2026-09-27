"use strict";
// test-keys.js — keys.js crypto matrix + render/load/create/revoke (Wave 2, todo 7).
// Loads core.js + keys.js only via js_tests/harness.js.
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-keys.js
var test = require("node:test");
var assert = require("node:assert/strict");
var nodeCrypto = require("node:crypto");
var h = require("./harness.js");

var KEY_RE = /^cw-[A-Za-z0-9]{24}$/;
var NO_SUBTLE_MSG = "WebCrypto unavailable — cannot hash key"; // keys.js:54 exact text
var SHA256_ABC = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad";
var SHA256_EMPTY = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";

// Real SHA-256 subtle stub injected via the harness `subtle` override.
// keys.js:45-55 calls window.crypto.subtle.digest("SHA-256", bytes) at CALL
// time (never at load), so the override is picked up per call.
function realSubtle() {
  return {
    digest: function (alg, data) {
      assert.equal(alg, "SHA-256");
      var d = nodeCrypto.createHash("sha256").update(Buffer.from(data)).digest();
      return Promise.resolve(new Uint8Array(d).buffer);
    },
  };
}

// Seeded deterministic getRandomValues override: constant fill, so every
// randomKey() call — and every fresh load with this same override — is identical.
function seededGRV(arr) {
  for (var i = 0; i < arr.length; i++) arr[i] = (i * 31 + 7) & 0xff;
  return arr;
}

function freshKeys(overrides) {
  return h.load(["core.js", "keys.js"], overrides);
}

// NOTE on Math.random fallback tests (keys.js:36-37): harness.js:200 installs
// the HOST Math object into the vm sandbox by reference (Math: Math), so
// `context.Math.random = stub` mutates the shared host Math. Every test below
// that stubs it saves Math.random first and restores it in a finally block.

// --- quadrant 1: getRandomValues present x subtle present ---

test("q1 randomKey: seeded getRandomValues yields valid cw-+24, deterministic in- and across-context", function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  var k1 = ctx.CW.randomKey();
  var k2 = ctx.CW.randomKey();
  assert.match(k1, KEY_RE);
  assert.equal(k1, k2);
  var ctx2 = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  assert.equal(ctx2.CW.randomKey(), k1);
});

test("q1 sha256Hex known vectors with real subtle", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  assert.equal(await ctx.CW.sha256Hex("abc"), SHA256_ABC);
  assert.equal(await ctx.CW.sha256Hex(""), SHA256_EMPTY);
});

// --- quadrant 2: getRandomValues present x subtle absent ---

test("q2 absent subtle rejects exact text; randomKey still valid", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  ctx.context.crypto.subtle = undefined; // keys.js:46 branches on window.crypto.subtle
  assert.match(ctx.CW.randomKey(), KEY_RE);
  var err = null;
  try {
    await ctx.CW.sha256Hex("abc");
  } catch (e) {
    err = e;
  }
  assert.ok(err instanceof Error, "sha256Hex must reject without subtle");
  assert.equal(err.message, NO_SUBTLE_MSG);
});

// --- quadrant 3: getRandomValues absent x subtle present ---

test("q3 Math.random fallback yields valid cw-+24; sha256Hex('') known vector", async function () {
  var ctx = freshKeys({ subtle: realSubtle() });
  ctx.context.crypto.getRandomValues = undefined; // keys.js:34 falls to Math.random loop
  var orig = Math.random;
  ctx.context.Math.random = function () { return 0.5; };
  try {
    // keys.js:37: floor(0.5*256)=128, 128%62=4 -> chars[4]='e', pinned exactly.
    assert.equal(ctx.CW.randomKey(), "cw-" + "eeeeeeeeeeeeeeeeeeeeeeee");
  } finally {
    ctx.context.Math.random = orig;
  }
  assert.equal(await ctx.CW.sha256Hex(""), SHA256_EMPTY);
});

test("q3 seeded Math.random fallback is deterministic across fresh contexts", function () {
  function lcg(seed) {
    var s = seed >>> 0;
    return function () {
      s = (Math.imul(s, 1664525) + 1013904223) >>> 0;
      return s / 4294967296;
    };
  }
  function oneKey() {
    var ctx = freshKeys({ subtle: realSubtle() });
    ctx.context.crypto.getRandomValues = undefined;
    var orig = Math.random;
    ctx.context.Math.random = lcg(12345);
    try {
      return ctx.CW.randomKey();
    } finally {
      ctx.context.Math.random = orig;
    }
  }
  var a = oneKey();
  var b = oneKey();
  assert.match(a, KEY_RE);
  assert.equal(a, b);
});

// --- quadrant 4: getRandomValues absent x subtle absent ---

test("q4 both absent: randomKey valid via fallback, sha256Hex rejects exact text", async function () {
  var ctx = freshKeys({ subtle: realSubtle() });
  ctx.context.crypto.getRandomValues = undefined;
  ctx.context.crypto.subtle = undefined;
  var orig = Math.random;
  ctx.context.Math.random = function () { return 0.25; };
  try {
    assert.match(ctx.CW.randomKey(), KEY_RE);
  } finally {
    ctx.context.Math.random = orig;
  }
  var err = null;
  try {
    await ctx.CW.sha256Hex("abc");
  } catch (e) {
    err = e;
  }
  assert.ok(err instanceof Error, "sha256Hex must reject without subtle");
  assert.equal(err.message, NO_SUBTLE_MSG);
});

// --- harness default subtle documents the zeroed-digest vector ---

test("default harness subtle resolves zeroed 32-byte digest (explicit zeroed vector)", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV }); // default subtle: harness.js:182-188
  assert.equal(await ctx.CW.sha256Hex("abc"), "00".repeat(32)); // 32 zero bytes -> 64 hex chars
});

// --- renderKeys branches (keys.js:7-16) ---

test("renderKeys empty state", function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  ctx.CW.state.keys = [];
  ctx.CW.renderKeys();
  assert.equal(ctx.elementsById["key-list"].innerHTML, "<li>No keys for this env.</li>");
});

test("renderKeys active vs revoked vs disabled branches pinned exactly", function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  ctx.CW.state.keys = [{ id: "k1", prefix: "cw-abc", revoked: false }];
  ctx.CW.renderKeys();
  assert.equal(
    ctx.elementsById["key-list"].innerHTML,
    '<li><code>cw-abc…</code> active <button type="button" data-revoke-key="k1">revoke</button></li>'
  );
  ctx.CW.state.keys = [{ id: "k2", prefix: "cw-xyz", revoked: true }];
  ctx.CW.renderKeys();
  assert.equal(
    ctx.elementsById["key-list"].innerHTML,
    '<li><code>cw-xyz…</code> revoked <button type="button" data-revoke-key="k2" disabled>revoke</button></li>'
  );
});

test("renderKeys escapes prefix and id", function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  ctx.CW.state.keys = [{ id: 'k"<b>', prefix: "<b>", revoked: false }];
  ctx.CW.renderKeys();
  var html = ctx.elementsById["key-list"].innerHTML;
  assert.ok(html.indexOf("&lt;b&gt;") !== -1, "prefix escaped, got: " + html);
  assert.ok(html.indexOf('data-revoke-key="k&quot;&lt;b&gt;"') !== -1, "id escaped, got: " + html);
});

// --- loadKeys filters (keys.js:18-30) ---

test("loadKeys filters by envId", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  var seenUrl = null;
  ctx.CW.state.envId = "env-1";
  ctx.CW.state.projectId = null;
  ctx.CW.state.envs = [];
  ctx.CW.apiAll = function (url) {
    seenUrl = url;
    return Promise.resolve([
      { id: "a", env: "env-1" },
      { id: "b", env: "env-2" },
    ]);
  };
  await ctx.CW.loadKeys();
  assert.ok(seenUrl.indexOf("/api/collections/sdk_keys/records") === 0, "unexpected url: " + seenUrl);
  assert.deepEqual(ctx.CW.state.keys.map(function (k) { return k.id; }), ["a"]);
});

test("loadKeys falls back to project env filter when no envId", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  ctx.CW.state.envId = null;
  ctx.CW.state.projectId = "p1";
  ctx.CW.state.envs = [{ id: "e1" }, { id: "e2" }];
  ctx.CW.apiAll = function () {
    return Promise.resolve([
      { id: "a", env: "e2" },
      { id: "b", env: "e9" },
    ]);
  };
  await ctx.CW.loadKeys();
  assert.deepEqual(ctx.CW.state.keys.map(function (k) { return k.id; }), ["a"]);
});

test("loadKeys keeps all items with no scope; null items renders empty", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  ctx.CW.state.envId = null;
  ctx.CW.state.projectId = null;
  ctx.CW.state.envs = [];
  ctx.CW.apiAll = function () {
    return Promise.resolve([
      { id: "a", env: "e1" },
      { id: "b", env: "e2" },
    ]);
  };
  await ctx.CW.loadKeys();
  assert.deepEqual(ctx.CW.state.keys.map(function (k) { return k.id; }), ["a", "b"]);
  ctx.CW.apiAll = function () { return Promise.resolve(null); };
  await ctx.CW.loadKeys();
  assert.equal(ctx.CW.state.keys.length, 0); // length-check: state.keys is a vm-realm array
  assert.equal(ctx.elementsById["key-list"].innerHTML, "<li>No keys for this env.</li>");
});

// --- createKey / revokeKey (keys.js:57-95) ---

test("createKey without envId sets guard text and performs no fetch", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  ctx.CW.state.envId = null;
  await ctx.CW.createKey();
  assert.equal(ctx.elementsById["key-result"].textContent, "pick an environment first");
  assert.equal(ctx.fetchCalls.length, 0);
});

test("createKey success writes prefix result and one-time key value", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  ctx.CW.state.envId = "env-1";
  ctx.CW.$("key-ratelimit").value = "100";
  var mutCalls = [];
  ctx.CW.apiMut = function (m, u, b) {
    mutCalls.push({ method: m, url: u, body: b });
    return Promise.resolve({ status: 200, data: {} });
  };
  ctx.CW.apiAll = function () { return Promise.resolve([]); }; // trailing loadKeys()
  await ctx.CW.createKey();
  assert.equal(mutCalls.length, 1);
  assert.equal(mutCalls[0].method, "POST");
  assert.equal(mutCalls[0].url, "/api/collections/sdk_keys/records");
  assert.equal(mutCalls[0].body.env, "env-1");
  assert.equal(mutCalls[0].body.rateLimit, 100);
  assert.equal(mutCalls[0].body.prefix, ctx.CW.randomKey().slice(0, 8));
  var full = ctx.elementsById["key-once-value"].textContent;
  assert.match(full, KEY_RE);
  assert.equal(full, ctx.CW.randomKey()); // seeded fill: created key equals regenerated key
  assert.equal(
    ctx.elementsById["key-result"].textContent,
    "key created (prefix " + mutCalls[0].body.prefix + ")"
  );
  assert.equal(ctx.elementsById["key-once"].hidden, false);
});

test("revokeKey PATCHes revoked:true and toasts on 200", async function () {
  var ctx = freshKeys({ getRandomValues: seededGRV, subtle: realSubtle() });
  var mutCalls = [];
  ctx.CW.apiMut = function (m, u, b) {
    mutCalls.push([m, u, b]);
    return Promise.resolve({ status: 200, data: {} });
  };
  ctx.CW.apiAll = function () { return Promise.resolve([]); }; // trailing loadKeys()
  var toasted = [];
  ctx.CW.toast = function (msg, ok) { toasted.push([msg, ok]); };
  await ctx.CW.revokeKey("k 1");
  assert.equal(mutCalls.length, 1);
  assert.equal(mutCalls[0][0], "PATCH");
  assert.equal(mutCalls[0][1], "/api/collections/sdk_keys/records/" + encodeURIComponent("k 1"));
  assert.equal(mutCalls[0][2].revoked, true); // field-wise: body is a vm-realm object
  assert.deepEqual(toasted, [["key revoked", true]]);
});
