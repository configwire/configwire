/* test-router-scope.js — pure tests for router.js parseHash/projectById
 * and scope.js defaultEnvId. Loads core.js + router.js + scope.js only
 * (never boot.js — avoids wiring). No fetch/timers used by these paths.
 * node:test + node:assert/strict only. Run from configwire/:
 *   node --test js_tests/test-router-scope.js
 */
"use strict";

var test = require("node:test").test;
var assert = require("node:assert/strict");
var h = require("./harness.js");

function fresh() {
  return h.load(["core.js", "router.js", "scope.js"]);
}

function hashCtx(hash) {
  var ctx = fresh();
  ctx.context.location.hash = hash;
  return ctx;
}

// parseHash returns a vm-realm object (different prototype), so
// deepStrictEqual rejects it; normalize through JSON first.
function dj(o) {
  return JSON.parse(JSON.stringify(o));
}

// ---- parseHash (router.js:9-29) ----

test("parseHash: empty hash is home", function () {
  var ctx = hashCtx("");
  assert.deepEqual(dj(ctx.CW.parseHash()), { view: "home", id: "", anchor: "" });
});

test("parseHash: bare #/ is home", function () {
  var ctx = hashCtx("#/");
  assert.deepEqual(dj(ctx.CW.parseHash()), { view: "home", id: "", anchor: "" });
});

test("parseHash: unknown shape falls back home", function () {
  var ctx = hashCtx("#/nope");
  assert.deepEqual(dj(ctx.CW.parseHash()), { view: "home", id: "", anchor: "" });
});

test("parseHash: detail without anchor", function () {
  var ctx = hashCtx("#/p/abc");
  assert.deepEqual(dj(ctx.CW.parseHash()), { view: "detail", id: "abc", anchor: "" });
});

test("parseHash: slash-anchor form extracts anchor (router.js:24-25)", function () {
  var ctx = hashCtx("#/p/abc/flags");
  assert.deepEqual(dj(ctx.CW.parseHash()), { view: "detail", id: "abc", anchor: "flags" });
});

test("parseHash: hash-anchor form extracts anchor (router.js:21-22)", function () {
  var ctx = hashCtx("#/p/abc#rules");
  assert.deepEqual(dj(ctx.CW.parseHash()), { view: "detail", id: "abc", anchor: "rules" });
});

test("parseHash: percent-encoded id decodes", function () {
  var ctx = hashCtx("#/p/a%20b");
  assert.deepEqual(dj(ctx.CW.parseHash()), { view: "detail", id: "a b", anchor: "" });
});

test("parseHash: account view plus anchor variants (router.js:11-17)", function () {
  assert.deepEqual(dj(hashCtx("#/p/account").CW.parseHash()), { view: "account", id: "", anchor: "" });
  assert.deepEqual(dj(hashCtx("#/p/account#keys").CW.parseHash()), { view: "account", id: "", anchor: "keys" });
  assert.deepEqual(dj(hashCtx("#/p/account/keys").CW.parseHash()), { view: "account", id: "", anchor: "keys" });
});

test("parseHash: malformed escape throws at decodeURIComponent (router.js:26)", function () {
  // Source calls decodeURIComponent(rest) unguarded, so '#/p/%ZZ' raises
  // URIError — pinned here as the real per-source behavior.
  var ctx = hashCtx("#/p/%ZZ");
  assert.throws(function () { ctx.CW.parseHash(); }, /URI/);
});

// ---- projectById (router.js:31-36) ----

test("projectById: hit returns the project object", function () {
  var ctx = fresh();
  var a = { id: "a", name: "alpha" };
  var b = { id: "b", name: "beta" };
  ctx.CW.state.projects = [a, b];
  assert.equal(ctx.CW.projectById("b"), b);
  assert.equal(ctx.CW.projectById("a"), a);
});

test("projectById: miss returns null", function () {
  var ctx = fresh();
  ctx.CW.state.projects = [{ id: "a", name: "alpha" }];
  assert.equal(ctx.CW.projectById("zzz"), null);
});

test("projectById: empty list returns null", function () {
  var ctx = fresh();
  ctx.CW.state.projects = [];
  assert.equal(ctx.CW.projectById("a"), null);
});

// ---- defaultEnvId (scope.js:35-40) ----

test("defaultEnvId: prefers the dev slug", function () {
  var ctx = fresh();
  ctx.CW.state.envs = [
    { id: "e-prod", slug: "prod" },
    { id: "e-dev", slug: "dev" },
  ];
  assert.equal(ctx.CW.defaultEnvId(), "e-dev");
});

test("defaultEnvId: falls back to first env when no dev slug", function () {
  var ctx = fresh();
  ctx.CW.state.envs = [
    { id: "e-staging", slug: "staging" },
    { id: "e-prod", slug: "prod" },
  ];
  assert.equal(ctx.CW.defaultEnvId(), "e-staging");
});

test("defaultEnvId: empty env list yields empty string", function () {
  var ctx = fresh();
  ctx.CW.state.envs = [];
  assert.equal(ctx.CW.defaultEnvId(), "");
});

// ---- getHomeSince / setHomeSince (router.js home range selector) ----

test("getHomeSince: defaults to 7d", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.getHomeSince(), "7d");
});

test("getHomeSince: invalid state falls back to 7d", function () {
  ["400d", "abc", "", null, undefined, "1m", "90"].forEach(function (bad) {
    var ctx = fresh();
    ctx.CW.state.homeSince = bad;
    assert.equal(ctx.CW.getHomeSince(), "7d");
  });
});

test("getHomeSince: whitelisted values pass through", function () {
  ["1d", "2d", "7d", "30d", "90d"].forEach(function (good) {
    var ctx = fresh();
    ctx.CW.state.homeSince = good;
    assert.equal(ctx.CW.getHomeSince(), good);
  });
});

test("setHomeSince: rejects non-whitelisted values, keeps 7d", function () {
  ["400d", "abc", "", "1m", "all"].forEach(function (bad) {
    var ctx = fresh();
    assert.equal(ctx.CW.setHomeSince(bad), "7d");
    assert.equal(ctx.CW.state.homeSince, "7d");
  });
});

test("setHomeSince: accepts whitelisted value and persists it", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.setHomeSince("30d"), "30d");
  assert.equal(ctx.CW.state.homeSince, "30d");
  assert.equal(ctx.CW.getHomeSince(), "30d");
  assert.equal(ctx.store["cw_home_since"], "30d");
});

test("homeSince: persisted value round-trips through loadPersistedScope", function () {
  var ctx = h.load(["core.js", "router.js", "scope.js"], {
    localStorageSeed: { cw_home_since: "90d" },
  });
  ctx.CW.loadPersistedScope();
  assert.equal(ctx.CW.getHomeSince(), "90d");
});

// ---- homeDailyAverage (router.js) ----

function zeros(n) {
  var out = [];
  for (var i = 0; i < n; i++) out.push(0);
  return out;
}

function rep(n, v) {
  var out = [];
  for (var i = 0; i < n; i++) out.push(v);
  return out;
}

test("homeDailyAverage: mean over complete days, rounded", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.homeDailyAverage(zeros(16).concat(rep(7, 5)).concat(rep(7, 10))), 4);
  assert.equal(ctx.CW.homeDailyAverage([1, 2, 3, 4, 5, 6, 7]), 4);
  assert.equal(ctx.CW.homeDailyAverage([3, 9]), 6);
  assert.equal(ctx.CW.homeDailyAverage([5]), 5);
  assert.equal(ctx.CW.homeDailyAverage([1, 2]), 2);
});

test("homeDailyAverage: empty input -> 0", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.homeDailyAverage([]), 0);
  assert.equal(ctx.CW.homeDailyAverage(null), 0);
});

// ---- homeTrimPartialDay (router.js) ----

function pts(days, vals) {
  return days.map(function (d, i) {
    return { day: d, fetches: vals[i], exposures: 0, versions: null };
  });
}

function utcToday() {
  return new Date().toISOString().slice(0, 10);
}

test("homeTrimPartialDay: drops trailing current-UTC-day bucket", function () {
  var ctx = fresh();
  var out = ctx.CW.homeTrimPartialDay(pts(["2026-01-01", "2026-01-02", utcToday()], [4, 6, 99]));
  assert.equal(out.length, 2);
  assert.equal(out[1].day, "2026-01-02");
  assert.equal(ctx.CW.homeDailyAverage(ctx.CW.homePointsTotals(out)), 5);
});

test("homeTrimPartialDay: keeps trailing complete day untouched", function () {
  var ctx = fresh();
  var input = pts(["2026-01-01", "2026-01-02"], [4, 6]);
  var out = ctx.CW.homeTrimPartialDay(input);
  assert.equal(out.length, 2);
  assert.equal(out[1].fetches, 6);
});

test("homeTrimPartialDay: only-today series trims to empty (average 0)", function () {
  var ctx = fresh();
  var out = ctx.CW.homeTrimPartialDay(pts([utcToday()], [99]));
  assert.equal(out.length, 0);
  assert.equal(ctx.CW.homeDailyAverage(ctx.CW.homePointsTotals(out)), 0);
});

test("homeTrimPartialDay: empty and day-less inputs pass through safely", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.homeTrimPartialDay([]).length, 0);
  assert.equal(ctx.CW.homeTrimPartialDay(null).length, 0);
});
