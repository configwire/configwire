"use strict";
// test-sso.js — cloud SSO bridge tenant UI (plan cloud-sso-bridge P1).
// account.js SSO helpers (getSsoTicket/stripSsoParam/getAuthMode/
// redeemSsoTicket/showSsoNotice) + boot.js mode-gated normalBoot.
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-sso.js
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function plain(o) {
  return JSON.parse(JSON.stringify(o));
}

function jsonRes(status, data, ok) {
  return {
    status: status,
    ok: ok === undefined ? status >= 200 && status < 300 : ok,
    json: function () { return Promise.resolve(data); },
  };
}

function $(ctx, id) {
  return ctx.document.getElementById(id);
}

// Minimal load for helper unit tests (never boot.js: CW.refreshAll is
// stubbed since redeem calls it — account.js redeem path).
function freshUnit(overrides) {
  var ctx = h.load(["core.js", "auth.js", "account.js"], overrides);
  var refreshCalls = [];
  ctx.CW.refreshAll = function () {
    refreshCalls.push(Array.prototype.slice.call(arguments));
    return Promise.resolve();
  };
  ctx.refreshCalls = refreshCalls;
  return ctx;
}

function tick(ms) {
  return new Promise(function (r) { setTimeout(r, ms || 10); });
}

async function flush(n) {
  for (var i = 0; i < (n || 6); i++) await tick();
}

// --- getSsoTicket (account.js) ---

test("getSsoTicket: absent without query", function () {
  var ctx = freshUnit();
  ctx.context.location.href = "http://127.0.0.1:8090/";
  assert.equal(ctx.CW.getSsoTicket(), "");
});

test("getSsoTicket: parses ?sso= alongside other params and hash", function () {
  var ctx = freshUnit();
  ctx.context.location.href = "http://127.0.0.1:8090/?foo=1&sso=TICKET.abc&bar=2#/p/x";
  assert.equal(ctx.CW.getSsoTicket(), "TICKET.abc");
});

test("getSsoTicket: empty sso reads as absent", function () {
  var ctx = freshUnit();
  ctx.context.location.href = "http://127.0.0.1:8090/?sso=";
  assert.equal(ctx.CW.getSsoTicket(), "");
});

// --- stripSsoParam (account.js) ---

test("stripSsoParam: drops sso, keeps others and hash", function () {
  var ctx = freshUnit();
  assert.equal(
    ctx.CW.stripSsoParam("http://h/?foo=1&sso=T&bar=2#/x"),
    "http://h/?foo=1&bar=2#/x"
  );
  assert.equal(ctx.CW.stripSsoParam("http://h/?sso=T"), "http://h/");
  assert.equal(ctx.CW.stripSsoParam("http://h/path"), "http://h/path");
});

// --- getAuthMode (account.js) ---

test("getAuthMode: sso response cached, cloudUrl stored", async function () {
  var calls = 0;
  var ctx = freshUnit({
    fetchImpl: function (url) {
      assert.equal(url, "/api/v1/admin/auth/mode");
      calls++;
      return Promise.resolve(jsonRes(200, { mode: "sso", cloudUrl: "https://cloud.example.com" }));
    },
  });
  assert.equal(await ctx.CW.getAuthMode(), "sso");
  assert.equal(await ctx.CW.getAuthMode(), "sso");
  assert.equal(calls, 1, "mode cached after first fetch");
  assert.equal(ctx.CW.state.cloudUrl, "https://cloud.example.com");
});

test("getAuthMode: unknown mode normalizes to standard", async function () {
  var ctx = freshUnit({
    fetchImpl: function () {
      return Promise.resolve(jsonRes(200, { mode: "weird" }));
    },
  });
  assert.equal(await ctx.CW.getAuthMode(), "standard");
});

test("getAuthMode: fetch failure falls back to standard (contract predates)", async function () {
  var ctx = freshUnit({
    fetchImpl: function () {
      return Promise.reject(new Error("harness fetch denied"));
    },
  });
  assert.equal(await ctx.CW.getAuthMode(), "standard");
});

// --- redeemSsoTicket (account.js) ---

test("redeemSsoTicket success stores token like login and strips ?sso=", async function () {
  var seen = null;
  var ctx = freshUnit({
    fetchImpl: function (url, opts) {
      assert.equal(url, "/api/v1/admin/sso/redeem");
      seen = plain(opts);
      return Promise.resolve(jsonRes(200, { token: "sso-tok-1" }));
    },
  });
  ctx.context.location.href = "http://127.0.0.1:8090/?sso=TICKET123";
  var replaced = [];
  ctx.context.history = {
    replaceState: function (a, b, url) { replaced.push(String(url)); },
  };
  var data = await ctx.CW.redeemSsoTicket("TICKET123");
  assert.deepEqual(plain(data), { token: "sso-tok-1" });
  assert.equal(seen.method, "POST");
  assert.deepEqual(plain(JSON.parse(seen.body)), { ticket: "TICKET123" });
  // Exactly like CW.login success: bare token memory-first + localStorage.
  assert.equal(ctx.CW.state.token, "sso-tok-1");
  assert.equal(ctx.store["cw_admin_token"], "sso-tok-1");
  assert.equal($(ctx, "login-section").hidden, true);
  assert.equal($(ctx, "admin-section").hidden, false);
  assert.equal(ctx.refreshCalls.length, 1);
  assert.deepEqual(replaced, ["http://127.0.0.1:8090/"]);
});

test("redeemSsoTicket failure rejects without storing a token", async function () {
  var ctx = freshUnit({
    fetchImpl: function () {
      return Promise.resolve(jsonRes(400, { message: "invalid ticket: expired." }));
    },
  });
  await assert.rejects(ctx.CW.redeemSsoTicket("STALE"), /sso redeem failed: HTTP 400/);
  assert.equal(ctx.CW.state.token, null);
  assert.ok(!Object.prototype.hasOwnProperty.call(ctx.store, "cw_admin_token"));
});

// --- showSsoNotice (account.js) ---

test("showSsoNotice hides login+setup, shows notice, hides link without cloudUrl", function () {
  var ctx = freshUnit();
  ctx.CW.showSsoNotice("");
  assert.equal($(ctx, "login-section").hidden, true);
  assert.equal($(ctx, "setup-section").hidden, true);
  assert.equal($(ctx, "sso-section").hidden, false);
  assert.equal($(ctx, "sso-cloud-link").hidden, true);
  assert.equal($(ctx, "sso-error").hidden, true);
});

test("showSsoNotice sets dashboard link when cloudUrl known, renders error", function () {
  var ctx = freshUnit();
  ctx.CW.state.cloudUrl = "https://cloud.example.com";
  ctx.CW.showSsoNotice("sso redeem failed: HTTP 409");
  assert.equal($(ctx, "sso-cloud-link").hidden, false);
  assert.equal($(ctx, "sso-cloud-link").href, "https://cloud.example.com");
  assert.equal($(ctx, "sso-error").textContent, "sso redeem failed: HTTP 409");
  assert.equal($(ctx, "sso-error").hidden, false);
});

// --- boot gating (full order, dispatched) ---

function bootFetch(map) {
  return function (url) {
    if (Object.prototype.hasOwnProperty.call(map, String(url))) {
      return Promise.resolve(jsonRes(200, map[String(url)]));
    }
    return Promise.reject(new Error("harness fetch denied: " + url));
  };
}

test("boot standard mode with no token keeps login intact", async function () {
  var ctx = h.load(undefined, {
    fetchImpl: bootFetch({
      "/api/v1/admin/auth/mode": { mode: "standard" },
      "/api/v1/admin/setup/status": { needsSetup: false },
      "/api/v1/meta": { version: "v0.0.0", assets: "a" },
    }),
  });
  ctx.dispatch("DOMContentLoaded");
  await flush();
  assert.equal($(ctx, "login-section").hidden, false, "login intact in standard mode");
  assert.equal($(ctx, "setup-section").hidden, true);
  assert.equal($(ctx, "sso-section").hidden, true, "no sso notice in standard mode");
});

test("boot sso mode with no token hides login+setup, shows notice", async function () {
  var ctx = h.load(undefined, {
    fetchImpl: bootFetch({
      "/api/v1/admin/auth/mode": { mode: "sso", cloudUrl: "https://cloud.example.com" },
      "/api/v1/meta": { version: "v0.0.0", assets: "a" },
    }),
  });
  ctx.dispatch("DOMContentLoaded");
  await flush();
  assert.equal($(ctx, "login-section").hidden, true, "login hidden in sso mode");
  assert.equal($(ctx, "setup-section").hidden, true, "setup hidden in sso mode");
  assert.equal($(ctx, "sso-section").hidden, false, "notice shown in sso mode");
  var urls = ctx.fetchCalls.map(function (c) { return String(c.url); });
  assert.ok(urls.indexOf("/api/v1/admin/setup/status") === -1, "setup not probed in sso mode");
});

test("boot ?sso= auto-redeems, stores token, strips param", async function () {
  var posts = [];
  var ctx = h.load(undefined, {
    fetchImpl: function (url, opts) {
      if (String(url) === "/api/v1/admin/sso/redeem") {
        posts.push(plain(JSON.parse(opts.body)));
        return Promise.resolve(jsonRes(200, { token: "boot-sso-tok" }));
      }
      if (String(url) === "/api/v1/meta") return Promise.resolve(jsonRes(200, { version: "v0.0.0", assets: "a" }));
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
  });
  ctx.context.location.href = "http://127.0.0.1:8090/?sso=BOOT.tik";
  var replaced = [];
  ctx.context.history = {
    replaceState: function (a, b, url) { replaced.push(String(url)); },
  };
  ctx.dispatch("DOMContentLoaded");
  await flush(10);
  assert.deepEqual(posts, [{ ticket: "BOOT.tik" }], "same-origin redeem posted");
  assert.equal(ctx.CW.state.token, "boot-sso-tok");
  assert.equal(ctx.store["cw_admin_token"], "boot-sso-tok");
  assert.deepEqual(replaced, ["http://127.0.0.1:8090/"], "?sso= stripped");
});

test("boot ?sso= failure in standard mode falls back to login", async function () {
  var ctx = h.load(undefined, {
    fetchImpl: bootFetch({
      "/api/v1/admin/auth/mode": { mode: "standard" },
      "/api/v1/admin/setup/status": { needsSetup: false },
      "/api/v1/meta": { version: "v0.0.0", assets: "a" },
    }),
  });
  // Redeem endpoint answers 400 in this scenario (bad ticket on a
  // standard backend): override just that URL to fail.
  var base = ctx.context.fetch;
  ctx.context.fetch = function (url, opts) {
    if (String(url) === "/api/v1/admin/sso/redeem") {
      return Promise.resolve(jsonRes(400, { message: "invalid ticket: malformed or tampered." }));
    }
    return base(url, opts);
  };
  ctx.context.location.href = "http://127.0.0.1:8090/?sso=BAD";
  ctx.dispatch("DOMContentLoaded");
  await flush(10);
  assert.equal($(ctx, "login-section").hidden, false, "login intact after failed redeem in standard mode");
  assert.equal($(ctx, "sso-section").hidden, true);
});
