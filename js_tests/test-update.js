"use strict";
// test-update.js — update.js version math, staleness + hang-guard (Wave 2, todo 8).
// Loads core.js + boot.js + update.js via harness.js; node:test + node:assert/strict only.
// Run from configwire/: node --test js_tests/test-update.js
//
// Why boot.js too: update.js:102-104 exposes checkNow ONLY as
// window.cwAdmin.checkAssetUpdate, and cwAdmin is defined in boot.js:711.
// Both boot IIFEs defer to a captured DOMContentLoaded listener under the
// default harness readyState "loading" (boot.js:13, boot.js:882-886,
// update.js:96-100), so this suite NEVER calls ctx.dispatch() — it invokes
// update's own start listener directly (last DOMContentLoaded entry) and
// boot's wiring/updateVersion stay dormant, keeping fetchCalls pure-update.
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function fresh(overrides) {
  return h.load(["core.js", "boot.js", "update.js"], overrides);
}

function metaRes(data, ok) {
  return {
    ok: ok === undefined ? true : ok,
    json: function () { return Promise.resolve(data); },
  };
}

// update's start is the LAST DOMContentLoaded listener on document
// (boot.js:13 first, boot.js:883 second, update.js:97 last). Calling it
// directly tests start() deterministically without waking boot handlers.
function updateStart(ctx) {
  var fns = ctx.document.listeners
    .filter(function (l) { return l.type === "DOMContentLoaded"; })
    .map(function (l) { return l.fn; });
  assert.ok(fns.length >= 2, "expected boot+update DOMContentLoaded listeners, got " + fns.length);
  return fns[fns.length - 1];
}

function checkNow(ctx) {
  assert.equal(typeof ctx.cwAdmin.checkAssetUpdate, "function", "update.js:103 exposes checkNow");
  return ctx.cwAdmin.checkAssetUpdate;
}

// --- metaUrl (update.js:19-21) ---

test("metaUrl appends Date.now cache-buster", function () {
  var seen = [];
  var ctx = fresh({
    fetchImpl: function (u) {
      seen.push(u);
      return Promise.resolve(metaRes({ version: "v1", assets: "a1" }));
    },
  });
  var realNow = Date.now;
  ctx.context.Date.now = function () { return 1700000000000; };
  var p;
  try {
    p = checkNow(ctx)();
  } finally {
    ctx.context.Date.now = realNow;
  }
  return p.then(function () {
    assert.equal(seen.length, 1);
    assert.equal(seen[0], "/api/v1/meta?_=1700000000000");
  });
});

// --- readMeta normalization (update.js:23-33) ---

test("readMeta: !ok response normalizes to null (checkNow false, no baseline)", function () {
  var ctx = fresh({ fetchImpl: function () { return Promise.resolve(metaRes({ x: 1 }, false)); } });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
    // Baseline still unset: a later good poll is "first success", also false.
    return checkNow(ctx)().then(function (out2) {
      assert.equal(out2, false);
    });
  });
});

test("readMeta: sync fetch throw resolves false (offline/file:// silent)", function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("offline"); },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
    assert.ok(!ctx.elementsById["cw-reload-bar"], "no banner element touched on failure");
  });
});

test("readMeta: rejected json resolves false", function () {
  var ctx = fresh({
    fetchImpl: function () {
      return Promise.resolve({ ok: true, json: function () { return Promise.reject(new Error("bad json")); } });
    },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
  });
});

test("readMeta: non-object json normalizes to null", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes("just-a-string")); },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
  });
});

test("readMeta: non-string version/assets coerce to empty string", function () {
  var calls = 0;
  var ctx = fresh({
    fetchImpl: function () {
      calls++;
      return Promise.resolve(metaRes({ version: 5, assets: null }));
    },
  });
  var check = checkNow(ctx);
  return check().then(function (first) {
    assert.equal(first, false); // first success sets baseline (update.js:57-60)
    return check().then(function (second) {
      // {version:"",assets:""} vs {version:"",assets:""}: both guards in
      // isStale (update.js:43-44) skip on falsy fields, so never stale.
      assert.equal(second, false);
      assert.equal(calls, 2);
    });
  });
});

// --- isStale matrix via checkNow (update.js:41-46 + 55-66) ---

test("isStale: null baseline is false — first successful poll sets baseline, no banner", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes({ version: "v1", assets: "a1" })); },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
    assert.ok(!ctx.elementsById["cw-reload-bar"], "no banner on first poll (update.js:57-60)");
  });
});

test("isStale: null current is false", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes(null)); },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
  });
});

test("isStale: assets-differ is true + banner shown exactly once", function () {
  var polls = [{ version: "v1", assets: "aaa" }, { version: "v1", assets: "bbb" }];
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes(polls.shift() || polls[0])); },
  });
  var bar = ctx.document.getElementById("cw-reload-bar");
  bar.hidden = true;
  var removed = [];
  bar.removeAttribute = function (name) {
    removed.push(name);
    if (name === "hidden") bar.hidden = false;
  };
  var check = checkNow(ctx);
  return check().then(function (first) {
    assert.equal(first, false);
    assert.equal(bar.hidden, true);
    return check().then(function (second) {
      assert.equal(second, true);
      assert.equal(bar.hidden, false);
      assert.deepEqual(removed, ["hidden"]); // update.js:35-39 runs once per stale poll
    });
  });
});

test("isStale: version-differ is true", function () {
  var polls = [{ version: "v1", assets: "" }, { version: "v2", assets: "" }];
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes(polls.shift() || polls[0])); },
  });
  var check = checkNow(ctx);
  return check().then(function (first) {
    assert.equal(first, false);
    return check().then(function (second) {
      assert.equal(second, true);
    });
  });
});

test("isStale: equal fingerprint is false", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes({ version: "v1", assets: "aaa" })); },
  });
  var check = checkNow(ctx);
  return check()
    .then(function (a) { assert.equal(a, false); return check(); })
    .then(function (b) { assert.equal(b, false); return check(); })
    .then(function (c) { assert.equal(c, false); });
});

test("checkNow: post-mismatch failure resolves false (never throws)", function () {
  var mode = "good";
  var ctx = fresh({
    fetchImpl: function () {
      if (mode === "bad") throw new Error("went offline");
      return Promise.resolve(metaRes({ version: "v1", assets: "a1" }));
    },
  });
  var check = checkNow(ctx);
  return check().then(function (first) {
    assert.equal(first, false);
    mode = "bad";
    return check().then(function (out) {
      assert.equal(out, false); // update.js:66 catch swallows
    });
  });
});

// --- tick (update.js:69-72) ---

test("tick skips the poll when document.hidden", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes({ version: "v1", assets: "a1" })); },
  });
  updateStart(ctx)(); // arms the single interval; tick = intervals[0].fn (update.js:78)
  assert.equal(ctx.intervals.length, 1);
  var tick = ctx.intervals[0].fn;
  var base = ctx.fetchCalls.length;
  ctx.document.hidden = true;
  tick();
  assert.equal(ctx.fetchCalls.length, base, "hidden tick must not fetch (update.js:70)");
  ctx.document.hidden = false;
  tick();
  assert.equal(ctx.fetchCalls.length, base + 1, "visible tick polls");
});

// --- start (update.js:74-94) ---

test("start arms a single interval plus visibilitychange/online/reload-button listeners", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes({ version: "v1", assets: "a1" })); },
  });
  var start = updateStart(ctx);
  assert.equal(ctx.intervals.length, 0, "nothing armed before start (readyState loading defers)");
  start();
  assert.equal(ctx.intervals.length, 1);
  assert.equal(ctx.intervals[0].ms, 5 * 60 * 1000, "POLL_MS 5min (update.js:14)");
  start(); // update.js:76 guard: timer set, second start must not re-arm
  assert.equal(ctx.intervals.length, 1, "single interval only");
  var types = ctx.listeners.map(function (l) { return l.target + ":" + l.type; });
  assert.ok(types.indexOf("document:visibilitychange") >= 0, "visibilitychange armed (update.js:81), got " + JSON.stringify(types));
  assert.ok(types.indexOf("window:online") >= 0, "online armed (update.js:84), got " + JSON.stringify(types));
  var btn = ctx.elementsById["cw-reload-btn"];
  assert.ok(btn, "reload button looked up (update.js:87)");
  var clicks = btn.listeners.filter(function (l) { return l.type === "click"; });
  assert.equal(clicks.length, 1, "exactly one reload click listener (update.js:89)");
});
