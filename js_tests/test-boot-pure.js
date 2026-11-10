"use strict";
// test-boot-pure.js — boot.js cwAdmin surface + zero-fetch load contract.
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-boot-pure.js
//
// Phase 5 removed boot's GitHub update-check helpers (isDevTag/parseVer/
// isNewer/tryShow); the same-origin meta path and the no-third-party contract
// are pinned behaviorally in test-update.js and test-full-order.js.
//
// Hang-guard: this suite NEVER calls ctx.dispatch('DOMContentLoaded') —
// boot's wiring stays dormant and fetchCalls must stay empty after load.
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

// --- window.cwAdmin getter surface ---

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
  assert.equal(ctx.fetchCalls.length, 0, "zero fetch without DOMContentLoaded");
  assert.equal(ctx.intervals.length, 0, "no timers armed without DOMContentLoaded");
});
