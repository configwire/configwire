"use strict";
// test-boot-pure.js — boot.js version IIFE pure logic + cwAdmin surface (Wave 2, todo 8).
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-boot-pure.js
//
// isDevTag (boot.js:791), parseVer (boot.js:795), isNewer (boot.js:808) live
// inside a DOM/fetch IIFE with no exports, so this suite extracts their
// verbatim source from pb_public/js/boot.js by brace-matching and evaluates
// just those three functions — no DOM, no fetch, no timers. updateVersion
// (boot.js:823) is intentionally never invoked here.
//
// Hang-guard: this suite NEVER calls ctx.dispatch('DOMContentLoaded') —
// boot's wiring (boot.js:13) and updateVersion (boot.js:882-886) stay dormant
// and fetchCalls must stay empty after load. Exits with NO --test-force-exit.
var test = require("node:test");
var assert = require("node:assert/strict");
var fs = require("node:fs");
var path = require("node:path");
var h = require("./harness.js");

var BOOT_SRC = fs.readFileSync(path.join(__dirname, "..", "pb_public", "js", "boot.js"), "utf8");

// Slice a top-level `function name(...) {...}` out of BOOT_SRC by brace matching.
function extractFn(src, name) {
  var idx = src.indexOf("function " + name + "(");
  assert.ok(idx >= 0, "found function " + name + " in boot.js");
  var brace = src.indexOf("{", idx);
  var depth = 0;
  for (var i = brace; i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") {
      depth--;
      if (depth === 0) return src.slice(idx, i + 1);
    }
  }
  throw new Error("unbalanced braces extracting " + name);
}

// Evaluate ONLY the three pure helpers (String/Math only — boot.js:791-822).
var ver = new Function(
  extractFn(BOOT_SRC, "isDevTag") + "\n" +
  extractFn(BOOT_SRC, "parseVer") + "\n" +
  extractFn(BOOT_SRC, "isNewer") + "\n" +
  "return { isDevTag: isDevTag, parseVer: parseVer, isNewer: isNewer };"
)();

// --- isDevTag (boot.js:791-794) ---

test("isDevTag: empty/dev/vdev match case-insensitively", function () {
  assert.equal(ver.isDevTag(""), true);
  assert.equal(ver.isDevTag("dev"), true);
  assert.equal(ver.isDevTag("DEV"), true);
  assert.equal(ver.isDevTag("vdev"), true);
  assert.equal(ver.isDevTag("VDev"), true);
  assert.equal(ver.isDevTag("  vdev  "), true);
  assert.equal(ver.isDevTag(null), true);
  assert.equal(ver.isDevTag(undefined), true);
});

test("isDevTag: real tags never match", function () {
  assert.equal(ver.isDevTag("v1.2.3"), false);
  assert.equal(ver.isDevTag("v0.0.5"), false);
  assert.equal(ver.isDevTag("latest"), false);
  assert.equal(ver.isDevTag("1.0.0"), false);
});

// --- parseVer (boot.js:795-807) ---

test("parseVer: strips leading v and splits core", function () {
  assert.deepEqual(ver.parseVer("v1.2.3"), { core: [1, 2, 3], pre: "" });
  assert.deepEqual(ver.parseVer("V2.0"), { core: [2, 0], pre: "" });
  assert.deepEqual(ver.parseVer("1.2.3"), { core: [1, 2, 3], pre: "" });
});

test("parseVer: splits pre-release at first dash", function () {
  assert.deepEqual(ver.parseVer("1.2.3-rc.1"), { core: [1, 2, 3], pre: "rc.1" });
  assert.deepEqual(ver.parseVer("v1.2.3-beta"), { core: [1, 2, 3], pre: "beta" });
});

test("parseVer: non-numeric segments coerce to 0, never throws", function () {
  assert.deepEqual(ver.parseVer("dev"), { core: [0], pre: "" });
  assert.deepEqual(ver.parseVer("a.b"), { core: [0, 0], pre: "" });
  assert.deepEqual(ver.parseVer(""), { core: [0], pre: "" });
  assert.deepEqual(ver.parseVer(null), { core: [0], pre: "" });
});

// --- isNewer (boot.js:808-822) ---

test("isNewer: core compare, v1.2.4 newer than v1.2.3", function () {
  assert.equal(ver.isNewer("v1.2.4", "v1.2.3"), true);
  assert.equal(ver.isNewer("v1.2.3", "v1.2.4"), false);
  assert.equal(ver.isNewer("v1.2.3", "v1.2.3"), false);
  assert.equal(ver.isNewer("1.2.3", "v1.2.3"), false);
  assert.equal(ver.isNewer("v2.0.0", "v1.9.9"), true);
});

test("isNewer: longer version is newer when prefix is equal", function () {
  assert.equal(ver.isNewer("1.2.3.1", "1.2.3"), true);
  assert.equal(ver.isNewer("1.2.3", "1.2.3.1"), false);
});

test("isNewer: stable beats same-core pre-release (boot.js:820)", function () {
  assert.equal(ver.isNewer("1.2.3", "1.2.3-rc"), true);
  assert.equal(ver.isNewer("1.2.3-rc", "1.2.3"), false);
});

test("isNewer: parseVer('dev') is safe — dev current never crashes compare", function () {
  assert.equal(ver.isNewer("v1.2.4", "dev"), true);
  assert.equal(ver.isNewer("v0.0.1", "dev"), true);
});

// --- window.cwAdmin getter surface (boot.js:711-784) ---

test("window.cwAdmin exposes the QA/contract getter surface", function () {
  var ctx = h.load(["core.js", "boot.js"]); // DOMContentLoaded suppressed: never dispatched
  assert.ok(ctx.cwAdmin, "window.cwAdmin defined (boot.js:711)");
  var expected = [
    "state", "login", "logout",
    "loadFlags", "loadReleases", "loadStats", "renderStats",
    "loadProjects", "loadEnvs", "loadRules", "loadExperiments", "loadKeys",
    "saveFlag", "deleteFlag", "publish", "rollback",
    "route", "parseHash", "checkSetup",
    "createKey", "revokeKey",
  ];
  expected.forEach(function (name) {
    assert.ok(name in ctx.cwAdmin, "cwAdmin exposes ." + name);
  });
  var desc = Object.getOwnPropertyDescriptor(ctx.cwAdmin, "state");
  assert.equal(typeof desc.get, "function", "cwAdmin.state is a lazy getter (boot.js:712)");
});

// --- zero-fetch proof (DOMContentLoaded suppressed) ---

test("boot load with DOMContentLoaded suppressed records zero fetch", function () {
  var ctx = h.load(["core.js", "boot.js"]);
  // Wiring is parked, not run: DOMContentLoaded listeners captured but never fired.
  var parked = ctx.document.listeners.filter(function (l) { return l.type === "DOMContentLoaded"; });
  assert.ok(parked.length >= 2, "boot wiring + updateVersion parked, got " + parked.length);
  // NOTE: no ctx.dispatch('DOMContentLoaded') anywhere in this file by design.
  assert.equal(ctx.fetchCalls.length, 0, "zero fetch without DOMContentLoaded (boot.js:850,862 unfired)");
  assert.equal(ctx.intervals.length, 0, "no timers armed without DOMContentLoaded");
});
