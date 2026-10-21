"use strict";
// test-swallowed-warn.js — P2 console.warn pass (production-hardening §3.3).
// Every empty catch in pb_public/js now warns with a '<module>: <context>'
// tag plus the error object instead of vanishing. This suite guards the
// representative swallowed-refresh path with FLOOR (subset) semantics:
// boot restores the persisted scope then calls CW.refreshAll()
// (boot.js:55-58); a rejected home-stats refresh must warn (not vanish).
// node:test + node:assert/strict only. Run from configwire/:
//   node --test js_tests/test-swallowed-warn.js
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function sleep(ms) {
  return new Promise(function (res) { setTimeout(res, ms); });
}

test("boot scope-restore refresh warns instead of swallowing home-stats failure", async function () {
  // boot.js parked on DOMContentLoaded (harness default readyState
  // "loading"): loaded for surface fidelity, its wiring never runs.
  var ctx = h.load(["core.js", "router.js", "scope.js", "boot.js"]);
  // The harness installs the HOST console by reference (harness.js:198), so
  // replace the binding with a recorder rather than mutating console.warn.
  var warnings = [];
  ctx.context.console = {
    log: function () {},
    info: function () {},
    warn: function (msg, err) { warnings.push({ msg: msg, err: err }); },
    error: function () {},
  };
  ctx.CW.state.token = "t"; // refreshAll/showHome only refresh when logged in
  var boom = new Error("home-stats down");
  ctx.CW.apiAll = function (url) {
    if (url.indexOf("/api/collections/projects/records") === 0) {
      return Promise.resolve([{ id: "p1", name: "P" }]);
    }
    if (url.indexOf("/api/collections/flags/records") === 0) {
      return Promise.reject(boom); // loadHomeStats fan-out fails here
    }
    return Promise.resolve([]);
  };
  await ctx.CW.refreshAll();
  await sleep(0);
  await sleep(0);
  await sleep(0);
  // Two distinct swallowed-refresh sites observe the same failure: the
  // refreshAll home fan-out (scope.js:455) and showHome itself
  // (router.js:514), reached via CW.route(). Both must warn, not vanish.
  // FLOOR-NOT-PIN contract: the old exact-2 pin (length === 2 + exact tag
  // set) was intentionally brittle — a third legitimate warn site in the
  // refresh path would fail this suite spuriously. Floor semantics keep
  // the regression guard (both expected tags + error identity) while
  // tolerating unrelated extra warns.
  function hasTaggedWarn(tag) {
    return warnings.some(function (w) { return w.msg === tag && w.err === boom; });
  }
  assert.ok(hasTaggedWarn("scope: home refresh"), "missing 'scope: home refresh' warn with original error");
  assert.ok(hasTaggedWarn("router: home stats"), "missing 'router: home stats' warn with original error");
  // Prove extra-warn tolerance INSIDE the test: an unrelated third warn
  // must not break the floor checks above.
  ctx.context.console.warn("boot: unrelated", new Error("other"));
  assert.ok(hasTaggedWarn("scope: home refresh"), "floor check broke after unrelated extra warn");
  assert.ok(hasTaggedWarn("router: home stats"), "floor check broke after unrelated extra warn");
});
