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
