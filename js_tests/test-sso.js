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

// SSO gating: login/password hidden; logout mirrors standard.
function freshGated() {
  // core (setLoggedIn/isSsoMode/applySsoUiGating) + auth + router
  // (syncSidebar/showSettings) + account (showSsoNotice/checkSetup).
  var ctx = h.load(["core.js", "auth.js", "router.js", "account.js"], {});
  // Mirror index.html defaults: login visible, setup/sso/admin hidden.
  $(ctx, "login-section").hidden = false;
  $(ctx, "setup-section").hidden = true;
  $(ctx, "sso-section").hidden = true;
  $(ctx, "admin-section").hidden = true;
  $(ctx, "logout-btn").hidden = true;
  return ctx;
}

test("sso post-login shell shows logout like standard, hides account, keeps admin", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  ctx.CW.state.token = "tok";
  ctx.CW.setLoggedIn(true);
  assert.equal($(ctx, "login-section").hidden, true, "no login dialog");
  assert.equal($(ctx, "setup-section").hidden, true, "no setup password form");
  assert.equal($(ctx, "login-error").hidden, true, "no login error surface");
  assert.equal($(ctx, "logout-btn").hidden, false, "logout shown like standard");
  assert.equal($(ctx, "logout-btn").style.display, "");
  assert.equal($(ctx, "account-card").hidden, true, "no account card");
  assert.equal($(ctx, "account-list").hidden, true, "no account list");
  assert.equal($(ctx, "account-create-form").hidden, true, "no create-account form");
  assert.equal($(ctx, "account-result").hidden, true, "no account result");
  assert.equal($(ctx, "admin-section").hidden, false, "shell works post-SSO-login");
  assert.equal($(ctx, "sso-section").hidden, true, "notice gone once logged in");
});

test("sso logged-out shell routes to the notice, never to login", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  ctx.CW.setLoggedIn(false);
  assert.equal($(ctx, "admin-section").hidden, true);
  assert.equal($(ctx, "sso-section").hidden, false, "managed notice is the only pre-auth surface");
  assert.equal($(ctx, "login-section").hidden, true, "no login fallback leak");
  assert.equal($(ctx, "setup-section").hidden, true);
  assert.equal($(ctx, "logout-btn").hidden, true);
  assert.equal($(ctx, "sso-error").hidden, true);
});

test("sso syncSidebar with token keeps the logout button like standard", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  ctx.CW.state.token = "tok";
  $(ctx, "logout-btn").hidden = true; // prove the sync shows it
  $(ctx, "logout-btn").style.display = "none";
  ctx.CW.syncSidebar();
  assert.equal($(ctx, "logout-btn").hidden, false);
  assert.equal($(ctx, "logout-btn").style.display, "");
  assert.equal($(ctx, "account-card").hidden, true);
});

test("sso showSettings/showAccount keep the account card hidden", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  ctx.CW.state.token = "tok";
  ctx.CW.loadAccounts = function () { return Promise.resolve([]); };
  ctx.CW.loadLimits = function () { return Promise.resolve(); };
  $(ctx, "account-card").hidden = false; // visible before gating
  ctx.CW.showSettings();
  assert.equal($(ctx, "account-card").hidden, true);
  $(ctx, "account-card").hidden = false;
  ctx.CW.showAccount();
  assert.equal($(ctx, "account-card").hidden, true);
});

test("sso showSsoNotice hides logout and every account control", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  $(ctx, "logout-btn").hidden = false;
  $(ctx, "account-card").hidden = false;
  $(ctx, "account-list").hidden = false;
  $(ctx, "account-create-form").hidden = false;
  $(ctx, "account-result").hidden = false;
  ctx.CW.showSsoNotice("");
  assert.equal($(ctx, "logout-btn").hidden, true);
  assert.equal($(ctx, "account-card").hidden, true);
  assert.equal($(ctx, "account-list").hidden, true);
  assert.equal($(ctx, "account-create-form").hidden, true);
  assert.equal($(ctx, "account-result").hidden, true);
  assert.equal($(ctx, "sso-section").hidden, false, "notice stays the only surface");
});

test("sso checkSetup never probes setup and never reveals login", async function () {
  var probed = 0;
  var ctx = h.load(["core.js", "auth.js", "router.js", "account.js"], {
    fetchImpl: function () {
      probed++;
      return Promise.reject(new Error("must not fetch in sso mode"));
    },
  });
  ctx.CW.state.authMode = "sso";
  var out = await ctx.CW.checkSetup();
  assert.deepEqual(plain(out), { needsSetup: false });
  assert.equal(probed, 0, "no setup probe in sso mode");
  assert.equal($(ctx, "login-section").hidden, true);
  assert.equal($(ctx, "setup-section").hidden, true);
});

test("sso helpers stay callable: CW.login/CW.logout/account APIs intact", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  assert.equal(typeof ctx.CW.login, "function");
  assert.equal(typeof ctx.CW.logout, "function");
  assert.equal(typeof ctx.CW.createAccount, "function");
  assert.equal(typeof ctx.CW.changeAccountPassword, "function");
  assert.equal(typeof ctx.CW.deleteAccount, "function");
  assert.equal(typeof ctx.CW.loadAccounts, "function");
});

test("standard mode keeps every surface: login/logout/account all present", function () {
  var ctx = freshGated();
  ctx.CW.state.token = "tok";
  ctx.CW.setLoggedIn(true);
  assert.equal($(ctx, "logout-btn").hidden, false, "logout present when logged in");
  assert.equal($(ctx, "logout-btn").style.display, "");
  assert.equal($(ctx, "account-card").hidden, false, "account card present");
  assert.equal($(ctx, "account-list").hidden, false);
  assert.equal($(ctx, "account-create-form").hidden, false);
  assert.equal($(ctx, "account-result").hidden, false);
  assert.equal($(ctx, "admin-section").hidden, false);
  assert.equal($(ctx, "login-section").hidden, true);
  ctx.CW.setLoggedIn(false);
  assert.equal($(ctx, "login-section").hidden, false, "login present when logged out");
  assert.equal($(ctx, "logout-btn").hidden, true);
  assert.equal($(ctx, "sso-section").hidden, true, "no notice in standard mode");
});

test("standard syncSidebar shows logout with a token", function () {
  var ctx = freshGated();
  ctx.CW.state.token = "tok";
  ctx.CW.syncSidebar();
  assert.equal($(ctx, "logout-btn").hidden, false);
});

test("boot sso with stored token re-applies gating after mode resolves", async function () {
  var ctx = h.load(undefined, {
    localStorageSeed: { cw_admin_token: "stored-tok" },
    fetchImpl: bootFetch({
      "/api/v1/admin/auth/mode": { mode: "sso", cloudUrl: "https://cloud.example.com" },
      "/api/v1/meta": { version: "v0.0.0", assets: "a" },
    }),
  });
  ctx.dispatch("DOMContentLoaded");
  await flush(10);
  assert.equal(ctx.CW.state.token, "stored-tok");
  assert.equal($(ctx, "login-section").hidden, true, "no login after reload");
  assert.equal($(ctx, "logout-btn").hidden, false, "logout shown like standard after reload");
  assert.equal($(ctx, "logout-btn").style.display, "");
  assert.equal($(ctx, "account-card").hidden, true, "no account card after reload");
  assert.equal($(ctx, "admin-section").hidden, false, "shell works after reload");
});

test("boot standard with stored token keeps logout and account controls", async function () {
  var ctx = h.load(undefined, {
    localStorageSeed: { cw_admin_token: "stored-tok" },
    fetchImpl: bootFetch({
      "/api/v1/admin/auth/mode": { mode: "standard" },
      "/api/v1/meta": { version: "v0.0.0", assets: "a" },
    }),
  });
  ctx.dispatch("DOMContentLoaded");
  await flush(10);
  assert.equal($(ctx, "logout-btn").hidden, false, "logout present in standard reload");
  assert.equal($(ctx, "account-card").hidden, false, "account card present in standard reload");
  assert.equal($(ctx, "admin-section").hidden, false);
});

// --- sso logout navigates home to cloud (follow-up) ---
// Every REAL logout event in sso mode (CW.logout call, 401/session-expiry
// via logoutRef) ends with location.href = cloudUrl when known; unset
// falls back to the notice; standard logout never navigates.

test("sso CW.logout navigates to the cloud dashboard URL", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  ctx.CW.state.cloudUrl = "https://cloud.example.com";
  ctx.CW.state.token = "tok";
  ctx.context.location.href = "http://127.0.0.1:8090/#/p/x";
  ctx.CW.logout();
  assert.equal(ctx.context.location.href, "https://cloud.example.com");
  assert.equal(ctx.CW.state.token, null, "token cleared before navigating");
  assert.equal($(ctx, "login-section").hidden, true, "still no login form");
  assert.equal($(ctx, "sso-section").hidden, false, "notice was the fallback under navigation");
});

test("sso CW.logout without cloudUrl falls back to the notice, no navigation", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  ctx.CW.state.token = "tok";
  ctx.context.location.href = "http://127.0.0.1:8090/#/p/x";
  ctx.CW.logout();
  assert.equal(ctx.context.location.href, "http://127.0.0.1:8090/#/p/x", "href untouched");
  assert.equal($(ctx, "sso-section").hidden, false, "managed notice shown");
  assert.equal($(ctx, "login-section").hidden, true, "never the login form");
});

test("sso 401 session-expiry navigates home to cloud", async function () {
  var ctx = h.load(["core.js", "auth.js", "router.js", "account.js"], {
    fetchImpl: function () {
      return Promise.resolve({ status: 401, ok: false, json: function () { return Promise.resolve({}); } });
    },
  });
  ctx.CW.state.authMode = "sso";
  ctx.CW.state.cloudUrl = "https://cloud.example.com";
  ctx.CW.state.token = "stale-tok";
  ctx.context.location.href = "http://127.0.0.1:8090/#/";
  await assert.rejects(ctx.CW.api("/api/collections/flags/records"), /session expired/);
  assert.equal(ctx.CW.state.token, null);
  assert.equal(ctx.context.location.href, "https://cloud.example.com");
  assert.equal($(ctx, "login-section").hidden, true, "expiry never reveals login");
});

test("standard CW.logout never navigates", function () {
  var ctx = freshGated();
  ctx.CW.state.token = "tok";
  ctx.context.location.href = "http://127.0.0.1:8090/#/";
  ctx.CW.logout();
  assert.equal(ctx.context.location.href, "http://127.0.0.1:8090/#/", "href untouched");
  assert.equal($(ctx, "login-section").hidden, false, "classic login returns");
  assert.equal($(ctx, "logout-btn").hidden, true);
  assert.equal($(ctx, "sso-section").hidden, true, "no notice in standard mode");
});

// --- logout-before-mode-resolves race (item 7) ---
// On the stored-token boot path setLoggedIn(true) + shell flip run BEFORE
// the background getAuthMode() revalidation returns, so a logout tap in
// that window must await the cached getAuthMode() promise FIRST and only
// then clear-token -> setLoggedIn(false) -> ssoLogoutHome(). Resolved mode
// stays fully synchronous (pinned below: returns undefined, not a promise).

function freshRace(fetchImpl) {
  var ctx = h.load(["core.js", "auth.js", "router.js", "account.js"], {
    fetchImpl: fetchImpl,
  });
  // Mirror index.html defaults, then the stored-token boot flip while the
  // mode is still unknown (standard branch — today's boot order).
  $(ctx, "login-section").hidden = false;
  $(ctx, "setup-section").hidden = true;
  $(ctx, "sso-section").hidden = true;
  $(ctx, "admin-section").hidden = true;
  $(ctx, "logout-btn").hidden = true;
  ctx.CW.state.token = "tok";
  try { ctx.context.localStorage.setItem("cw_admin_token", "tok"); } catch (e) { /* stub */ }
  ctx.CW.setLoggedIn(true);
  // Record every login-section visibility write from here on: "never
  // revealed" means no write of false, not just a clean final state.
  var loginEl = $(ctx, "login-section");
  var writes = [];
  var cur = loginEl.hidden;
  Object.defineProperty(loginEl, "hidden", {
    configurable: true,
    get: function () { return cur; },
    set: function (v) { writes.push(!!v); cur = !!v; },
  });
  ctx.loginWrites = writes;
  return ctx;
}

test("race: logout with mode unresolved resolving sso ends at cloud, login never revealed", async function () {
  var resolveMode;
  var gate = new Promise(function (res) { resolveMode = res; });
  var ctx = freshRace(function (url) {
    if (String(url) === "/api/v1/admin/auth/mode") {
      return gate.then(function () {
        return jsonRes(200, { mode: "sso", cloudUrl: "https://cloud.example.com" });
      });
    }
    return Promise.reject(new Error("harness fetch denied: " + url));
  });
  ctx.context.location.href = "http://127.0.0.1:8090/#/p/x";
  assert.equal($(ctx, "logout-btn").hidden, false, "logout live while mode unknown (the race window)");
  var p = ctx.CW.logout();
  assert.ok(p && typeof p.then === "function", "unresolved mode takes the async path");
  // Await-first: nothing cleared and no surface flipped while mode pending.
  assert.equal(ctx.CW.state.token, "tok", "token kept until mode resolves");
  assert.equal($(ctx, "login-section").hidden, true, "login not revealed while pending");
  assert.equal(ctx.context.location.href, "http://127.0.0.1:8090/#/p/x", "no navigation while pending");
  resolveMode();
  await p;
  await flush();
  assert.equal(ctx.CW.state.token, null, "token cleared after resolve");
  assert.equal(ctx.context.location.href, "https://cloud.example.com", "sso logout navigates home to cloud");
  assert.equal($(ctx, "login-section").hidden, true, "login never shown");
  assert.ok(ctx.loginWrites.indexOf(false) === -1, "no login-section reveal at any point");
  assert.equal($(ctx, "sso-section").hidden, false, "notice was the fallback under navigation");
});

test("race: logout with mode unresolved resolving standard returns to login, no navigation", async function () {
  var resolveMode;
  var gate = new Promise(function (res) { resolveMode = res; });
  var ctx = freshRace(function (url) {
    if (String(url) === "/api/v1/admin/auth/mode") {
      return gate.then(function () { return jsonRes(200, { mode: "standard" }); });
    }
    return Promise.reject(new Error("harness fetch denied: " + url));
  });
  ctx.context.location.href = "http://127.0.0.1:8090/#/";
  var p = ctx.CW.logout();
  assert.ok(p && typeof p.then === "function", "unresolved mode takes the async path");
  resolveMode();
  await p;
  await flush();
  assert.equal(ctx.CW.state.token, null);
  assert.equal(ctx.context.location.href, "http://127.0.0.1:8090/#/", "href untouched");
  assert.equal($(ctx, "login-section").hidden, false, "classic login returns");
  assert.equal($(ctx, "sso-section").hidden, true, "no notice in standard mode");
});

test("race: logout with mode fetch failing falls back to standard login (residual case)", async function () {
  var ctx = freshRace(function () {
    return Promise.reject(new Error("harness fetch denied"));
  });
  ctx.context.location.href = "http://127.0.0.1:8090/#/";
  await ctx.CW.logout();
  await flush();
  assert.equal(ctx.CW.state.token, null);
  assert.equal(ctx.context.location.href, "http://127.0.0.1:8090/#/", "href untouched");
  assert.equal($(ctx, "login-section").hidden, false, "failure fallback stays standard: login returns");
  assert.equal($(ctx, "sso-section").hidden, true);
});

test("race: 401 auto-logout with mode unresolved resolving sso navigates home", async function () {
  var resolveMode;
  var gate = new Promise(function (res) { resolveMode = res; });
  var ctx = freshRace(function (url) {
    if (String(url) === "/api/v1/admin/auth/mode") {
      return gate.then(function () {
        return jsonRes(200, { mode: "sso", cloudUrl: "https://cloud.example.com" });
      });
    }
    return Promise.resolve({ status: 401, ok: false, json: function () { return Promise.resolve({}); } });
  });
  ctx.context.location.href = "http://127.0.0.1:8090/#/";
  await assert.rejects(ctx.CW.api("/api/collections/flags/records"), /session expired/);
  resolveMode();
  await flush(10);
  assert.equal(ctx.CW.state.token, null);
  assert.equal(ctx.context.location.href, "https://cloud.example.com", "expiry navigates home once mode resolves");
  assert.equal($(ctx, "login-section").hidden, true, "expiry never reveals login");
  assert.ok(ctx.loginWrites.indexOf(false) === -1, "no login-section reveal at any point");
});

test("race: logoutRef fallback without auth.js awaits mode then navigates home", async function () {
  var resolveMode;
  var gate = new Promise(function (res) { resolveMode = res; });
  // No auth.js: CW.logout is undefined, so CW.api 401 reaches the core.js
  // fallback lines (setLoggedIn/showSsoNotice come from account.js).
  var ctx = h.load(["core.js", "account.js"], {
    fetchImpl: function (url) {
      if (String(url) === "/api/v1/admin/auth/mode") {
        return gate.then(function () {
          return jsonRes(200, { mode: "sso", cloudUrl: "https://cloud.example.com" });
        });
      }
      return Promise.resolve({ status: 401, ok: false, json: function () { return Promise.resolve({}); } });
    },
  });
  assert.equal(typeof ctx.CW.logout, "undefined", "fallback precondition: no CW.logout");
  ctx.CW.state.token = "stale-tok";
  ctx.context.location.href = "http://127.0.0.1:8090/#/";
  await assert.rejects(ctx.CW.api("/api/collections/flags/records"), /session expired/);
  resolveMode();
  await flush(10);
  assert.equal(ctx.CW.state.token, null);
  assert.equal(ctx.context.location.href, "https://cloud.example.com");
  assert.equal($(ctx, "login-section").hidden, true, "expiry never reveals login");
});

test("resolved-mode logout stays synchronous (no added latency on the common path)", function () {
  var ctx = freshGated();
  ctx.CW.state.authMode = "sso";
  ctx.CW.state.cloudUrl = "https://cloud.example.com";
  ctx.CW.state.token = "tok";
  ctx.context.location.href = "http://127.0.0.1:8090/#/p/x";
  var ret = ctx.CW.logout();
  assert.equal(ret, undefined, "resolved mode returns undefined, not a promise");
  assert.equal(ctx.context.location.href, "https://cloud.example.com");
  var ctx2 = freshGated();
  ctx2.CW.state.authMode = "standard";
  ctx2.CW.state.token = "tok";
  ctx2.context.location.href = "http://127.0.0.1:8090/#/";
  assert.equal(ctx2.CW.logout(), undefined, "standard resolved path also synchronous");
  assert.equal($(ctx2, "login-section").hidden, false, "login returns");
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

test("boot ?sso= garbage in sso mode shows notice with error, no login leak", async function () {
  var ctx = h.load(undefined, {
    fetchImpl: bootFetch({
      "/api/v1/admin/auth/mode": { mode: "sso", cloudUrl: "https://cloud.example.com" },
      "/api/v1/meta": { version: "v0.0.0", assets: "a" },
    }),
  });
  var base = ctx.context.fetch;
  ctx.context.fetch = function (url, opts) {
    if (String(url) === "/api/v1/admin/sso/redeem") {
      return Promise.resolve(jsonRes(400, { message: "invalid ticket: malformed or tampered." }));
    }
    return base(url, opts);
  };
  ctx.context.location.href = "http://127.0.0.1:8090/?sso=GARBAGE";
  ctx.dispatch("DOMContentLoaded");
  await flush(10);
  assert.equal($(ctx, "sso-section").hidden, false, "notice shown");
  assert.equal($(ctx, "sso-error").hidden, false, "redeem error rendered");
  assert.ok($(ctx, "sso-error").textContent.length > 0);
  assert.equal($(ctx, "login-section").hidden, true, "no login fallback leak");
  assert.equal($(ctx, "setup-section").hidden, true);
  assert.equal($(ctx, "logout-btn").hidden, true);
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
