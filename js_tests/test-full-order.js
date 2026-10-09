"use strict";
// test-full-order.js — full-order integration smoke (Wave 3, todo 11).
// Loads ALL 18 files via harness FULL_ORDER (index.html:694-711 order) with
// NO files arg to h.load(); node:test + node:assert/strict only.
// Run from configwire/: node --test js_tests/test-full-order.js
//
// What this proves:
//   1. Wiring identity: window.cwAdmin.checkAssetUpdate IS update.js checkNow.
//      boot.js:1115 builds the cwAdmin object (no checkAssetUpdate there);
//      update.js:102-104 assigns checkNow onto it. checkNow is closure-private
//      so the reference is obtained behaviorally: negative control (full order
//      minus update.js -> undefined), stability across dispatch (assigned once
//      at load, not re-armed), and a checkNow-driven state change observed
//      through the cwAdmin handle (baseline set -> stale -> banner + true).
//   2. Deny-fetch: dispatching DOMContentLoaded through h.dispatch() (which
//      fires document, then body, then window listeners per harness.js) runs
//      boot's handler + updateVersion (boot.js:13,882-886) + update's start
//      (update.js:96-100); the recording stub logs the github releases URL
//      and /api/v1/meta attempts with ZERO real sockets.
//   3. Hang guard: exactly one update.js setInterval (POLL_MS 5min) is
//      captured, never scheduled — the suite exiting with NO --test-force-exit
//      IS the proof.
//
// NOTE on deny shape: the default sync-throwing deny stub CANNOT be used with
// dispatch — boot.js:18 calls CW.checkSetup().catch(...), and a synchronous
// fetch throw escapes checkSetup (core.js:135 api() throws before returning a
// promise), aborting the listener chain before updateVersion/start run
// (verified: "harness fetch denied: /api/v1/admin/setup/status"). The suite
// therefore uses async-deny (fetchImpl returning a rejected deny error): every
// attempt is still RECORDED, still denied, still zero sockets, and every
// in-product caller handles rejection (account.js:28, boot.js:859,879,
// update.js:66 silent paths).
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

var GITHUB_URL = "https://api.github.com/repos/configwire/configwire/releases?per_page=10"; // boot.js:862
var POLL_MS = 5 * 60 * 1000; // update.js:14

function urls(ctx) {
  return ctx.fetchCalls.map(function (c) { return String(c.url); });
}

function asyncDeny() {
  return function (url) {
    return Promise.reject(new Error("harness fetch denied: " + url));
  };
}

function metaRes(data, ok) {
  return {
    ok: ok === undefined ? true : ok,
    json: function () { return Promise.resolve(data); },
  };
}

// --- full-order load: silent at load, 19 files, admin surface present ---

test("FULL_ORDER loads all 18 files with zero fetch before DOMContentLoaded", function () {
assert.equal(h.FULL_ORDER.length, 18);
assert.equal(h.FULL_ORDER[0], "core.js"); // index.html:694
assert.equal(h.FULL_ORDER[12], "transfer.js"); // index.html:706
assert.equal(h.FULL_ORDER[h.FULL_ORDER.length - 1], "update.js"); // index.html:711
  var ctx = h.load(); // NO files arg: defaults to FULL_ORDER
  assert.deepEqual(JSON.parse(JSON.stringify(urls(ctx))), []);
  assert.equal(typeof ctx.CW, "object");
  assert.equal(typeof ctx.cwAdmin, "object"); // boot.js:1115
  assert.equal(typeof ctx.cwAdmin.checkAssetUpdate, "function"); // update.js:102-104
});

// --- wiring identity: boot object + update checkNow ---

test("wiring identity: checkAssetUpdate comes from update.js, not boot.js", function () {
  var names = h.FULL_ORDER.filter(function (n) { return n !== "update.js"; });
  var ctx = h.load(names, { fetchImpl: asyncDeny() });
  assert.equal(typeof ctx.cwAdmin, "object", "boot.js:1115 builds cwAdmin alone");
  assert.equal(
    ctx.cwAdmin.checkAssetUpdate,
    undefined,
    "without update.js nothing assigns checkAssetUpdate (update.js:102-104 owns it)"
  );
});

test("wiring identity: handle is stable across dispatch (assigned once at load)", function () {
  var ctx = h.load(undefined, { fetchImpl: asyncDeny() }); // NO files arg
  var before = ctx.cwAdmin.checkAssetUpdate;
  assert.equal(typeof before, "function");
  ctx.dispatch("DOMContentLoaded");
  assert.ok(
    ctx.cwAdmin.checkAssetUpdate === before,
    "update.js:102-104 assigns at load; start()/polls never re-assign"
  );
});

test("wiring identity: checkNow-driven state change through the cwAdmin handle", function () {
  var canned = { version: "v1.0.0", assets: "aaa" };
  var ctx = h.load(undefined, { // NO files arg
    fetchImpl: function (url) {
      if (String(url).indexOf("/api/v1/meta") === 0) return Promise.resolve(metaRes(canned));
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
  });
  var checkNow = ctx.cwAdmin.checkAssetUpdate;
  var bar = ctx.document.getElementById("cw-reload-bar"); // update.js:36
  var removed = [];
  bar.removeAttribute = function (name) { removed.push(String(name)); };
  return checkNow().then(function (first) {
    assert.equal(first, false, "update.js:57-60 first success sets baseline, no banner");
    canned = { version: "v1.0.0", assets: "bbb" }; // redeploy, same VERSION
    return checkNow();
  }).then(function (second) {
    assert.equal(second, true, "update.js:61-64 assets mismatch -> stale");
    assert.deepEqual(JSON.parse(JSON.stringify(removed)), ["hidden"]);
  });
});

// --- dispatch: deny-recorded github + meta, single captured interval ---

test("dispatch records github + meta attempts with zero sockets, arms one interval", function () {
  var ctx = h.load(undefined, { fetchImpl: asyncDeny() }); // NO files arg
  ctx.dispatch("DOMContentLoaded");
  var recorded = urls(ctx);
  assert.ok(
    recorded.indexOf(GITHUB_URL) !== -1,
    "boot.js:862 github releases attempt recorded, got " + JSON.stringify(recorded)
  );
  assert.ok(
    recorded.indexOf("/api/v1/meta") !== -1,
    "boot.js:850 same-origin meta attempt recorded, got " + JSON.stringify(recorded)
  );
  assert.ok(
    recorded.some(function (u) { return u.indexOf("/api/v1/meta?_=") === 0; }),
    "update.js:19-21 cache-busted meta poll recorded, got " + JSON.stringify(recorded)
  );
  // Zero real sockets: every recorded attempt either rejected (async-deny) or
  // was swallowed by a silent catch; nothing left the process.
  assert.equal(ctx.intervals.length, 1, "exactly one interval armed, got " + ctx.intervals.length);
  assert.equal(ctx.intervals[0].ms, POLL_MS, "update.js:78 5-minute poll");
  assert.equal(typeof ctx.intervals[0].fn, "function");
  assert.equal(ctx.intervals[0].cleared, false);
  // Captured, never scheduled: the harness stub records without calling the
  // real setInterval, so this process exits cleanly with NO --test-force-exit
  // (completing this run IS the hang-guard proof).
});

// --- failure micro-test: allow-list mode sanity (deny-by-default) ---

test("allow-list mode: same-origin meta succeeds while github throws deny error", function () {
  function allowMetaOnly(url) {
    if (String(url).indexOf("/api/v1/meta") === 0) {
      return Promise.resolve(metaRes({ version: "v9.9.9", assets: "zzz" }));
    }
    throw new Error("harness fetch denied: " + url);
  }
  var ctx = h.load(undefined, { fetchImpl: allowMetaOnly }); // NO files arg
  return ctx.context.fetch("/api/v1/meta").then(function (r) {
    assert.equal(r.ok, true);
    return r.json();
  }).then(function (d) {
    assert.deepEqual(JSON.parse(JSON.stringify(d)), { version: "v9.9.9", assets: "zzz" });
    assert.throws(
      function () { ctx.context.fetch(GITHUB_URL); },
      /harness fetch denied/,
      "github attempt throws deny error (deny-by-default)"
    );
    var recorded = urls(ctx);
    assert.ok(recorded.indexOf("/api/v1/meta") !== -1);
    assert.ok(recorded.indexOf(GITHUB_URL) !== -1, "denied attempt still recorded");
  });
});
