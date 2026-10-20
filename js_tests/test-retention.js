"use strict";
// test-retention.js — retention.js validators and retention form/API flows.
// Loads core.js + retention.js only via js_tests/harness.js.
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-retention.js
var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

var DB = { retention: null };
var failGet = false;

var h = harness.load(["core.js", "retention.js"], {
  fetchImpl: async function (url, opts) {
    var method = (opts && opts.method) || "GET";
    if (url.indexOf("/api/v1/admin/retention") === 0 && method === "GET") {
      if (failGet) throw new Error("boom (forced failure)");
      return { status: 200, ok: true, json: function () { return Promise.resolve(DB.retention); } };
    }
    return { status: 200, ok: true, json: function () { return Promise.resolve({}); } };
  },
});

var INPUT_IDS = ["retention-raw", "retention-rollup", "retention-result"];

function setVal(id, v) { h.CW.$(id).value = v; }
function textOf(id) { return h.CW.$(id).textContent; }
function plain(o) { return JSON.parse(JSON.stringify(o)); } // cross-realm compare

function reset() {
  var s = h.CW.state;
  s.retention = null;
  s.token = "tok";
  INPUT_IDS.forEach(function (id) { h.CW.$(id).value = ""; h.CW.$(id).textContent = ""; });
  h.fetchCalls.length = 0;
  failGet = false;
  DB.retention = null;
}

function stubApiMut(calls, status, data) {
  var real = h.CW.apiMut;
  h.CW.apiMut = function (method, path, body) {
    calls.push({ method: method, path: path, body: body });
    return Promise.resolve({ status: status, data: data === undefined ? {} : data });
  };
  return function () { h.CW.apiMut = real; };
}

// --- pure validators ---

test("isValidRetentionDays accepts 1..1825 integers only", function () {
  var C = h.CW;
  assert.equal(C.isValidRetentionDays(1), true);
  assert.equal(C.isValidRetentionDays(30), true);
  assert.equal(C.isValidRetentionDays(1825), true);
  assert.equal(C.isValidRetentionDays(0), false);
  assert.equal(C.isValidRetentionDays(-1), false);
  assert.equal(C.isValidRetentionDays(1826), false);
  assert.equal(C.isValidRetentionDays(1.5), false);
  assert.equal(C.isValidRetentionDays(NaN), false);
  assert.equal(C.isValidRetentionDays("30"), false);
  assert.equal(C.isValidRetentionDays(null), false);
});

test("validateRetentionPair enforces bounds and rollup>=raw", function () {
  var C = h.CW;
  assert.equal(C.validateRetentionPair(30, 90), "");
  assert.equal(C.validateRetentionPair(1, 1), "");
  assert.equal(C.validateRetentionPair(1825, 1825), "");
  assert.equal(C.validateRetentionPair(0, 90), "Retention must be 1–1825 days.");
  assert.equal(C.validateRetentionPair(30, 1826), "Retention must be 1–1825 days.");
  assert.equal(C.validateRetentionPair(90, 30), "Rollup must be >= raw.");
});

// --- saveRetention validation (no fetch on rejection) ---

test("saveRetention rejects out-of-range numerics without network", async function () {
  reset();
  setVal("retention-raw", "0");
  setVal("retention-rollup", "90");
  await h.CW.saveRetention(null);
  assert.equal(textOf("retention-result"), "Retention must be 1–1825 days.");
  assert.equal(h.fetchCalls.length, 0);
});

test("saveRetention rejects rollup<raw without network", async function () {
  reset();
  setVal("retention-raw", "90");
  setVal("retention-rollup", "30");
  await h.CW.saveRetention(null);
  assert.equal(textOf("retention-result"), "Rollup must be >= raw.");
  assert.equal(h.fetchCalls.length, 0);
});

test("saveRetention success PUTs parsed body", async function () {
  reset();
  setVal("retention-raw", "45");
  setVal("retention-rollup", "120");
  DB.retention = { rawDays: 30, rollupDays: 90 };
  var calls = [];
  var restore = stubApiMut(calls, 200, { rawDays: 45, rollupDays: 120 });
  try {
    await h.CW.saveRetention(null);
  } finally {
    restore();
  }
  assert.equal(calls.length, 1);
  assert.equal(calls[0].method + " " + calls[0].path, "PUT /api/v1/admin/retention");
  assert.deepEqual(plain(calls[0].body), { rawDays: 45, rollupDays: 120 });
  assert.equal(textOf("retention-result"), "Saved.");
});

// --- put/load flows ---

test("saveRetention failure renders status shape", async function () {
  reset();
  h.CW.state.retention = { rawDays: 30, rollupDays: 90 };
  setVal("retention-raw", "45");
  setVal("retention-rollup", "120");
  var calls = [];
  var restore = stubApiMut(calls, 400, { message: "nope" });
  try {
    await h.CW.saveRetention(null);
  } finally {
    restore();
  }
  assert.ok(textOf("retention-result").indexOf("Save failed (400)") === 0);
});

test("loadRetention fills state and inputs; failure surfaces inline", async function () {
  reset();
  DB.retention = { rawDays: 45, rollupDays: 120 };
  var data = await h.CW.loadRetention();
  assert.equal(data.rawDays, 45);
  assert.equal(h.CW.state.retention.rawDays, 45);
  assert.equal(h.CW.$("retention-raw").value, 45);
  assert.equal(h.CW.$("retention-rollup").value, 120);
  failGet = true;
  var err = null;
  try { await h.CW.loadRetention(); } catch (e) { err = e; }
  assert.ok(err instanceof Error);
  assert.ok(textOf("retention-result").length > 0);
});
