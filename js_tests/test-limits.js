"use strict";
// test-limits.js — limits.js validators, current-IP render, and limits
// form/API flows. Loads core.js + limits.js only via js_tests/harness.js.
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-limits.js
var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

var DB = { limits: null };
var failGet = false;

var h = harness.load(["core.js", "limits.js"], {
  fetchImpl: async function (url, opts) {
    var method = (opts && opts.method) || "GET";
    if (url.indexOf("/api/v1/admin/limits") === 0 && method === "GET") {
      if (failGet) throw new Error("boom (forced failure)");
      return { status: 200, ok: true, json: function () { return Promise.resolve(DB.limits); } };
    }
    return { status: 200, ok: true, json: function () { return Promise.resolve({}); } };
  },
});

var INPUT_IDS = ["limit-global", "limit-burst", "limit-fetch", "limit-ingest",
  "limit-ip-headers", "admin-allowed-ips", "limits-result", "settings-limits-result"];

function setVal(id, v) { h.CW.$(id).value = v; }
function textOf(id) { return h.CW.$(id).textContent; }
function plain(o) { return JSON.parse(JSON.stringify(o)); } // cross-realm compare

function reset() {
  var s = h.CW.state;
  s.limits = null;
  s.token = "tok";
  INPUT_IDS.forEach(function (id) { h.CW.$(id).value = ""; h.CW.$(id).textContent = ""; });
  h.CW.$("admin-current-ip-text").textContent = "";
  h.fetchCalls.length = 0;
  failGet = false;
  DB.limits = null;
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

test("isValidIP accepts v4/v6, rejects empties and out-of-range", function () {
  var C = h.CW;
  assert.equal(C.isValidIP("1.2.3.4"), true);
  assert.equal(C.isValidIP("  10.0.0.1  "), true);
  assert.equal(C.isValidIP("::1"), true);
  assert.equal(C.isValidIP("fe80::1%1"), true);
  assert.equal(C.isValidIP("fe80::1%eth0"), false); // charset is hex+zones only, no interface names
  assert.equal(C.isValidIP(""), false);
  assert.equal(C.isValidIP(null), false);
  assert.equal(C.isValidIP("1.2.3.256"), false);
  assert.equal(C.isValidIP("not-an-ip"), false);
  assert.equal(C.isValidIP("1.2.3"), false);
});

test("ipv4CidrContains honors subnet boundaries", function () {
  var C = h.CW;
  assert.equal(C.ipv4CidrContains("10.0.0.0/8", "10.7.8.9"), true);
  assert.equal(C.ipv4CidrContains("192.168.1.0/24", "192.168.2.1"), false);
  assert.equal(C.ipv4CidrContains("0.0.0.0/0", "203.0.113.9"), true);
  assert.equal(C.ipv4CidrContains("1.2.3.4/32", "1.2.3.4"), true);
  assert.equal(C.ipv4CidrContains("1.2.3.4/32", "1.2.3.5"), false);
  assert.equal(C.ipv4CidrContains("1.2.3.4/33", "1.2.3.4"), false);
  assert.equal(C.ipv4CidrContains("garbage", "1.2.3.4"), false);
  assert.equal(C.ipv4CidrContains("10.0.0.0/8", "not-an-ip"), false);
});

test("isAllowedByList: empty allows all, exact/CIDR match, blanks skipped", function () {
  var C = h.CW;
  assert.equal(C.isAllowedByList("9.9.9.9", []), true);
  assert.equal(C.isAllowedByList("9.9.9.9", null), true);
  assert.equal(C.isAllowedByList("", ["1.2.3.4"]), false);
  assert.equal(C.isAllowedByList("1.2.3.4", ["1.2.3.4"]), true);
  assert.equal(C.isAllowedByList("FE80::1", ["fe80::1"]), true);
  assert.equal(C.isAllowedByList("10.7.8.9", ["", "10.0.0.0/8"]), true);
  assert.equal(C.isAllowedByList("203.0.113.9", ["1.2.3.4", "10.0.0.0/8"]), false);
});

test("stringToIPs/ipsToString and header pair round-trip", function () {
  var C = h.CW;
  assert.deepEqual(plain(C.stringToIPs("a, b ,,c")), ["a", "b", "c"]);
  assert.deepEqual(plain(C.stringToIPs(null)), []);
  assert.deepEqual(plain(C.stringToIPs("")), []);
  assert.equal(C.ipsToString(["a", "b"]), "a, b");
  assert.equal(C.ipsToString(null), "");
  assert.deepEqual(plain(C.stringToHeaders("X-A, X-B")), ["X-A", "X-B"]);
  assert.equal(C.headersToString(["X-A", "X-B"]), "X-A, X-B");
});

// --- current-IP render ---

test("renderCurrentIP states: unknown, allowed, blocked, remote suffix", function () {
  reset();
  var C = h.CW;
  C.renderCurrentIP(C.$("admin-current-ip"), "", null, []);
  assert.equal(textOf("admin-current-ip-text"), "Your IP: …");
  C.renderCurrentIP(C.$("admin-current-ip"), "1.2.3.4", null, ["1.2.3.4"]);
  assert.equal(textOf("admin-current-ip-text"), "Your IP: 1.2.3.4");
  C.renderCurrentIP(C.$("admin-current-ip"), "9.9.9.9", "9.9.9.9", ["1.2.3.4"]);
  assert.ok(textOf("admin-current-ip-text").indexOf("blocked by allowlist") >= 0);
  C.renderCurrentIP(C.$("admin-current-ip"), "1.2.3.4", "5.6.7.8", []);
  assert.equal(textOf("admin-current-ip-text"), "Your IP: 1.2.3.4 (remote 5.6.7.8)");
});

// --- saveLimits validation (no fetch on rejection) ---

test("saveLimits rejects out-of-range numerics without network", async function () {
  reset();
  setVal("limit-global", "0");
  setVal("limit-burst", "400");
  setVal("limit-fetch", "100");
  setVal("limit-ingest", "50");
  await h.CW.saveLimits(null);
  assert.equal(textOf("limits-result"), "Limits must be 1–10000.");
  assert.equal(h.fetchCalls.length, 0);
});

test("saveLimits rejects >10 headers and bad header names without network", async function () {
  reset();
  setVal("limit-global", "200");
  setVal("limit-burst", "400");
  setVal("limit-fetch", "100");
  setVal("limit-ingest", "50");
  setVal("limit-ip-headers", "H1,H2,H3,H4,H5,H6,H7,H8,H9,H10,H11");
  await h.CW.saveLimits(null);
  assert.equal(textOf("limits-result"), "Max 10 headers.");
  assert.equal(h.fetchCalls.length, 0);
  setVal("limit-ip-headers", "bad header!");
  await h.CW.saveLimits(null);
  assert.ok(textOf("limits-result").indexOf("Bad header:") === 0);
  assert.equal(h.fetchCalls.length, 0);
});

test("saveLimits success PUTs parsed body", async function () {
  reset();
  setVal("limit-global", "200");
  setVal("limit-burst", "400");
  setVal("limit-fetch", "100");
  setVal("limit-ingest", "50");
  setVal("limit-ip-headers", "X-Foo");
  setVal("admin-allowed-ips", "10.0.0.1, 192.168.0.0/16");
  DB.limits = { globalRps: 200, burst: 400, fetchRps: 100, ingestRps: 50, adminAllowedIPs: [], ipHeaders: [] };
  var calls = [];
  var restore = stubApiMut(calls, 200, { globalRps: 200 });
  try {
    await h.CW.saveLimits(null);
  } finally {
    restore();
  }
  assert.equal(calls.length, 1);
  assert.equal(calls[0].method + " " + calls[0].path, "PUT /api/v1/admin/limits");
  assert.deepEqual(plain(calls[0].body.adminAllowedIPs), ["10.0.0.1", "192.168.0.0/16"]);
  assert.deepEqual(plain(calls[0].body.ipHeaders), ["X-Foo"]);
  assert.equal(textOf("limits-result"), "Saved.");
});

// --- saveAdminLimit validation ---

test("saveAdminLimit rejects >32 IPs and non-printable entries", async function () {
  reset();
  var many = [];
  for (var i = 0; i < 33; i++) many.push("10.0.0." + i);
  setVal("admin-allowed-ips", many.join(","));
  await h.CW.saveAdminLimit(null);
  assert.equal(textOf("settings-limits-result"), "Max 32 IPs.");
  assert.equal(h.fetchCalls.length, 0);
  setVal("admin-allowed-ips", "ab");
  await h.CW.saveAdminLimit(null);
  assert.equal(textOf("settings-limits-result"), "adminAllowedIPs entry must be non-empty printable");
  assert.equal(h.fetchCalls.length, 0);
});

test("saveAdminLimit sends allowlist via PUT", async function () {
  reset();
  h.CW.state.limits = { globalRps: 200, burst: 400, fetchRps: 100, ingestRps: 50 };
  setVal("admin-allowed-ips", "10.0.0.1, 192.168.0.0/16");
  setVal("limit-ip-headers", "");
  DB.limits = { globalRps: 200, burst: 400, fetchRps: 100, ingestRps: 50, adminAllowedIPs: [], ipHeaders: [] };
  var calls = [];
  var restore = stubApiMut(calls, 200, { globalRps: 200 });
  try {
    await h.CW.saveAdminLimit(null);
  } finally {
    restore();
  }
  assert.equal(calls.length, 1);
  assert.deepEqual(plain(calls[0].body.adminAllowedIPs), ["10.0.0.1", "192.168.0.0/16"]);
});

// --- putLimits/loadLimits flows ---

test("putLimits failure renders status shape", async function () {
  reset();
  h.CW.state.limits = { globalRps: 200, burst: 400, fetchRps: 100, ingestRps: 50 };
  setVal("admin-allowed-ips", "");
  setVal("limit-ip-headers", "");
  var calls = [];
  var restore = stubApiMut(calls, 400, { message: "nope" });
  try {
    await h.CW.saveAdminLimit(null);
  } finally {
    restore();
  }
  assert.ok(textOf("settings-limits-result").indexOf("Save failed (400)") === 0);
});

test("loadLimits fills state and inputs; failure surfaces inline", async function () {
  reset();
  DB.limits = { globalRps: 200, burst: 400, fetchRps: 100, ingestRps: 50,
    adminAllowedIPs: ["10.0.0.1"], ipHeaders: ["X-Foo"], clientIp: "10.0.0.1", remoteAddr: "" };
  var data = await h.CW.loadLimits();
  assert.equal(data.globalRps, 200);
  assert.equal(h.CW.state.limits.globalRps, 200);
  assert.equal(h.CW.$("limit-global").value, 200);
  assert.equal(h.CW.$("admin-allowed-ips").value, "10.0.0.1");
  failGet = true;
  var err = null;
  try { await h.CW.loadLimits(); } catch (e) { err = e; }
  assert.ok(err instanceof Error);
  assert.ok(textOf("limits-result").length > 0);
});
