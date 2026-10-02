"use strict";
// test-auth-account.js — auth.js + account.js (Wave 3, todo 9).
// Loads core.js + auth.js + account.js only (never boot.js/scope.js: CW.refreshAll
// is stubbed with a recorder since auth login calls it — account.js:24, auth.js:24).
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-auth-account.js
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function fresh(overrides) {
  var ctx = h.load(["core.js", "auth.js", "account.js"], overrides);
  var refreshCalls = [];
  ctx.CW.refreshAll = function () {
    refreshCalls.push(Array.prototype.slice.call(arguments));
    return Promise.resolve();
  };
  ctx.refreshCalls = refreshCalls;
  return ctx;
}

// Cross-realm compare: vm realm prototypes break strict deepEqual,
// so compare via JSON round-trip.
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

var AUTH_URL = "/api/collections/_superusers/auth-with-password";

// --- login success (auth.js:7-30) ---

test("login success stores exact token, clears error, flips shell, refreshes", async function () {
  var seenOpts;
  var ctx = fresh({
    fetchImpl: function (url, opts) {
      assert.equal(url, AUTH_URL);
      seenOpts = plain(opts);
      return Promise.resolve(jsonRes(200, { token: "tok-abc-123" }));
    },
  });
  var data = await ctx.CW.login("admin@example.com", "password123");
  assert.deepEqual(plain(data), { token: "tok-abc-123" });
  // Request shape (auth.js:9-12): POST JSON identity/password.
  assert.equal(seenOpts.method, "POST");
  assert.deepEqual(plain(JSON.parse(seenOpts.body)), { identity: "admin@example.com", password: "password123" });
  // Bare token stored under CW.LS_KEY (auth.js:21-22).
  assert.equal(ctx.CW.LS_KEY, "cw_admin_token");
  assert.equal(ctx.CW.state.token, "tok-abc-123");
  assert.equal(ctx.store["cw_admin_token"], "tok-abc-123");
  // Login error cleared (auth.js:8) and shell flipped (auth.js:23-24).
  assert.equal($(ctx, "login-error").textContent, "");
  assert.equal($(ctx, "login-error").hidden, true);
  assert.equal($(ctx, "login-section").hidden, true);
  assert.equal($(ctx, "admin-section").hidden, false);
  assert.equal($(ctx, "logout-btn").hidden, false);
  assert.equal(ctx.refreshCalls.length, 1, "refreshAll called once");
  assert.equal(ctx.fetchCalls.length, 1);
});

// --- login failure 401 (auth.js:14-18, 26-29) ---

test("login 401 writes login-error text and clears token", async function () {
  var ctx = fresh({
    localStorageSeed: { cw_admin_token: "stale-token" },
    fetchImpl: function () {
      return Promise.resolve(jsonRes(401, { message: "unauth" }));
    },
  });
  ctx.CW.state.token = "stale-token";
  await assert.rejects(
    ctx.CW.login("admin@example.com", "wrong"),
    function (err) {
      assert.equal(err.message, "login failed: HTTP 401");
      return true;
    }
  );
  assert.equal(ctx.CW.state.token, null);
  assert.ok(!Object.prototype.hasOwnProperty.call(ctx.store, "cw_admin_token"));
  assert.equal($(ctx, "login-error").textContent, "login failed: HTTP 401");
  assert.equal($(ctx, "login-error").hidden, false);
  assert.equal(ctx.refreshCalls.length, 0, "no refresh on failed login");
});

// --- logout (auth.js:32-36) ---

test("logout clears storage+state and hides admin", function () {
  var ctx = fresh({ localStorageSeed: { cw_admin_token: "tok-x" } });
  ctx.CW.state.token = "tok-x";
  $(ctx, "admin-section").hidden = false; // pretend logged in
  ctx.CW.logout();
  assert.equal(ctx.CW.state.token, null);
  assert.ok(!Object.prototype.hasOwnProperty.call(ctx.store, "cw_admin_token"));
  assert.equal($(ctx, "login-section").hidden, false);
  assert.equal($(ctx, "admin-section").hidden, true);
  assert.equal($(ctx, "logout-btn").hidden, true);
});

// --- checkSetup toggles sections (account.js:18-36) ---

test("checkSetup needsSetup:true shows setup, hides login", async function () {
  var ctx = fresh({
    fetchImpl: function (url) {
      assert.equal(url, "/api/v1/admin/setup/status");
      return Promise.resolve(jsonRes(200, { needsSetup: true }));
    },
  });
  var out = await ctx.CW.checkSetup();
  assert.deepEqual(plain(out), { needsSetup: true });
  assert.equal($(ctx, "setup-section").hidden, false);
  assert.equal($(ctx, "login-section").hidden, true);
});

test("checkSetup needsSetup:false shows login, clears setup status", async function () {
  var ctx = fresh({
    fetchImpl: function () {
      return Promise.resolve(jsonRes(200, { needsSetup: false }));
    },
  });
  $(ctx, "setup-result").textContent = "stale";
  var out = await ctx.CW.checkSetup();
  assert.deepEqual(plain(out), { needsSetup: false });
  assert.equal($(ctx, "setup-section").hidden, true);
  assert.equal($(ctx, "login-section").hidden, false);
  assert.equal($(ctx, "setup-result").textContent, "");
});

test("checkSetup rejection falls back to normal login", async function () {
  var ctx = fresh({
    fetchImpl: function () {
      return Promise.resolve(jsonRes(500, { message: "kaput" }));
    },
  });
  var out = await ctx.CW.checkSetup();
  assert.deepEqual(plain(out), { needsSetup: false });
  assert.equal($(ctx, "setup-section").hidden, true);
  assert.equal($(ctx, "login-section").hidden, false);
});

test("checkSetup short-circuits when a token exists (zero fetch)", async function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("must not fetch"); },
  });
  ctx.CW.state.token = "tok-x";
  var out = await ctx.CW.checkSetup();
  assert.deepEqual(plain(out), { needsSetup: false });
  assert.equal(ctx.fetchCalls.length, 0);
});

// --- createSetup validation branches (account.js:43-45) ---

test("createSetup rejects empty email without fetching", async function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("must not fetch"); },
  });
  $(ctx, "setup-email").value = "   ";
  $(ctx, "setup-password").value = "password123";
  $(ctx, "setup-password-confirm").value = "password123";
  await ctx.CW.createSetup();
  assert.equal($(ctx, "setup-error").textContent, "Email required.");
  assert.equal(ctx.fetchCalls.length, 0);
});

test("createSetup rejects short password without fetching", async function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("must not fetch"); },
  });
  $(ctx, "setup-email").value = "a@b.c";
  $(ctx, "setup-password").value = "short";
  $(ctx, "setup-password-confirm").value = "short";
  await ctx.CW.createSetup();
  assert.equal($(ctx, "setup-error").textContent, "Password needs 8+ characters.");
  assert.equal(ctx.fetchCalls.length, 0);
});

test("createSetup rejects mismatched confirmation without fetching", async function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("must not fetch"); },
  });
  $(ctx, "setup-email").value = "a@b.c";
  $(ctx, "setup-password").value = "password123";
  $(ctx, "setup-password-confirm").value = "different1";
  await ctx.CW.createSetup();
  assert.equal($(ctx, "setup-error").textContent, "Passwords don't match.");
  assert.equal(ctx.fetchCalls.length, 0);
});

test("createSetup posts setup then auto-logs-in, storing the token", async function () {
  var urls = [];
  var ctx = fresh({
    fetchImpl: function (url, opts) {
      urls.push(url);
      if (url === "/api/v1/admin/setup") {
        assert.equal(opts.method, "POST");
        assert.deepEqual(plain(JSON.parse(opts.body)), { email: "a@b.c", password: "password123" });
        return Promise.resolve(jsonRes(201, { email: "a@b.c" }));
      }
      assert.equal(url, AUTH_URL);
      return Promise.resolve(jsonRes(200, { token: "fresh-setup-token" }));
    },
  });
  $(ctx, "setup-email").value = "a@b.c";
  $(ctx, "setup-password").value = "password123";
  $(ctx, "setup-password-confirm").value = "password123";
  var data = await ctx.CW.createSetup();
  assert.deepEqual(urls, ["/api/v1/admin/setup", AUTH_URL]);
  assert.deepEqual(plain(data), { token: "fresh-setup-token" });
  assert.equal(ctx.CW.state.token, "fresh-setup-token");
  assert.equal(ctx.store["cw_admin_token"], "fresh-setup-token");
  assert.equal($(ctx, "setup-error").textContent, "");
  assert.equal(ctx.refreshCalls.length, 1, "auto-login refreshes");
});

// --- renderAccounts escaping (account.js:66-76) ---

test("renderAccounts escapes emails and empty state", function () {
  var ctx = fresh();
  ctx.CW.state.accounts = [];
  ctx.CW.renderAccounts();
  assert.equal($(ctx, "account-list").innerHTML, "<li>No admin accounts.</li>");
  ctx.CW.state.accounts = [
    { id: '1"><script>', email: "<b>evil</b>@x.test", created: "2026<09" },
  ];
  ctx.CW.renderAccounts();
  var html = $(ctx, "account-list").innerHTML;
  assert.ok(html.indexOf("&lt;b&gt;evil&lt;/b&gt;@x.test") !== -1, "email escaped: " + html);
  assert.ok(html.indexOf("2026&lt;09") !== -1, "created escaped: " + html);
  assert.ok(html.indexOf("<b>evil</b>") === -1, "raw HTML leaked");
  assert.ok(html.indexOf('data-account-delete="1&quot;&gt;&lt;script&gt;"') !== -1, "id attr escaped: " + html);
});

// --- loadAccounts (account.js:78-88) ---

test("loadAccounts stores items and renders; failure surfaces escaped error", async function () {
  var items = [{ id: "u1", email: "boss@example.com", created: "2026-09-01" }];
  var ctx = fresh({
    fetchImpl: function (url) {
      assert.equal(url, "/api/v1/admin/account/list");
      return Promise.resolve(jsonRes(200, { items: items }));
    },
  });
  var out = await ctx.CW.loadAccounts();
  assert.deepEqual(plain(out), plain(items));
  assert.deepEqual(plain(ctx.CW.state.accounts), plain(items));
  assert.ok($(ctx, "account-list").innerHTML.indexOf("boss@example.com") !== -1);

  var ctx2 = fresh({
    fetchImpl: function () {
      return Promise.resolve(jsonRes(500, { message: "db <down>" }));
    },
  });
  await assert.rejects(ctx2.CW.loadAccounts(), /request failed \(500\)/);
  assert.ok($(ctx2, "account-list").innerHTML.indexOf("db &lt;down&gt;") !== -1);
});

// --- createAccount validation + apiMut (account.js:90-112) ---

test("createAccount validation branches write account-result without fetching", async function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("must not fetch"); },
  });
  $(ctx, "account-email").value = "";
  $(ctx, "account-password").value = "password123";
  await ctx.CW.createAccount();
  assert.equal($(ctx, "account-result").textContent, "Email required.");
  $(ctx, "account-email").value = "n@x.test";
  $(ctx, "account-password").value = "tiny";
  await ctx.CW.createAccount();
  assert.equal($(ctx, "account-result").textContent, "Password needs 8+ characters.");
  assert.equal(ctx.fetchCalls.length, 0);
});

test("createAccount posts, reports the created email, clears fields", async function () {
  var ctx = fresh({
    fetchImpl: function (url, opts) {
      if (url === "/api/v1/admin/account/create") {
        assert.equal(opts.method, "POST");
        assert.deepEqual(plain(JSON.parse(opts.body)), { email: "n@x.test", password: "password123" });
        return Promise.resolve(jsonRes(201, { email: "n@x.test", id: "u9" }));
      }
      assert.equal(url, "/api/v1/admin/account/list");
      return Promise.resolve(jsonRes(200, { items: [] }));
    },
  });
  $(ctx, "account-email").value = "n@x.test";
  $(ctx, "account-password").value = "password123";
  var out = await ctx.CW.createAccount();
  assert.equal(out.status, 201);
  assert.equal($(ctx, "account-result").textContent, "Account created: n@x.test");
  assert.equal($(ctx, "account-email").value, "");
  assert.equal($(ctx, "account-password").value, "");
});

test("createAccount failure pins status plus server message", async function () {
  var ctx = fresh({
    fetchImpl: function (url) {
      if (url === "/api/v1/admin/account/create") {
        return Promise.resolve(jsonRes(500, { message: "boom" }));
      }
      return Promise.resolve(jsonRes(200, { items: [] }));
    },
  });
  $(ctx, "account-email").value = "n@x.test";
  $(ctx, "account-password").value = "password123";
  await ctx.CW.createAccount();
  assert.equal($(ctx, "account-result").textContent, "Account create failed (500): boom");
});

// --- changeAccountPassword via scripted promptDialog (account.js:114-132) ---

test("changeAccountPassword posts encoded id with scripted password", async function () {
  var promptArgs;
  var ctx = fresh({
    fetchImpl: function (url, opts) {
      if (url === "/api/v1/admin/account/a%20b%2Fc/password") {
        assert.equal(opts.method, "POST");
        assert.deepEqual(plain(JSON.parse(opts.body)), { password: "newpass12" });
        return Promise.resolve(jsonRes(204, {}));
      }
      assert.equal(url, "/api/v1/admin/account/list");
      return Promise.resolve(jsonRes(200, { items: [] }));
    },
  });
  ctx.CW.promptDialog = function (msg, def, opts) {
    promptArgs = { msg: msg, def: def, opts: plain(opts) };
    return Promise.resolve("newpass12");
  };
  var out = await ctx.CW.changeAccountPassword("a b/c");
  assert.equal(promptArgs.msg, "New password for this admin (min 8 characters):");
  assert.deepEqual(promptArgs.opts, {
    title: "Change password",
    okText: "Change",
    inputType: "password",
    required: true,
    minLength: 8,
  });
  assert.equal(out.status, 204);
  assert.equal($(ctx, "account-result").textContent, "Password changed.");
});

test("changeAccountPassword cancel performs zero fetches", async function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("must not fetch"); },
  });
  ctx.CW.promptDialog = function () { return Promise.resolve(null); };
  var out = await ctx.CW.changeAccountPassword("u1");
  assert.strictEqual(out, undefined);
  assert.equal(ctx.fetchCalls.length, 0);
});

test("changeAccountPassword short password rejected locally, zero fetches", async function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("must not fetch"); },
  });
  ctx.CW.promptDialog = function () { return Promise.resolve("short"); };
  await ctx.CW.changeAccountPassword("u1");
  assert.equal($(ctx, "account-result").textContent, "Password needs 8+ characters.");
  assert.equal(ctx.fetchCalls.length, 0);
});

// --- deleteAccount via scripted confirmDialog (account.js:134-149) ---

test("deleteAccount confirm=true issues DELETE and reports deletion", async function () {
  var ctx = fresh({
    fetchImpl: function (url, opts) {
      if (url === "/api/v1/admin/account/u1") {
        assert.equal(opts.method, "DELETE");
        return Promise.resolve(jsonRes(204, {}));
      }
      assert.equal(url, "/api/v1/admin/account/list");
      return Promise.resolve(jsonRes(200, { items: [] }));
    },
  });
  var confirmArgs;
  ctx.CW.confirmDialog = function (msg, opts) {
    confirmArgs = { msg: msg, opts: plain(opts) };
    return Promise.resolve(true);
  };
  var out = await ctx.CW.deleteAccount("u1");
  assert.equal(confirmArgs.msg, "Delete this admin account?");
  assert.deepEqual(confirmArgs.opts, { title: "Delete admin", okText: "Delete", danger: true });
  assert.equal(out.status, 204);
  assert.equal($(ctx, "account-result").textContent, "Admin deleted.");
});

test("deleteAccount confirm=false performs zero apiMut calls", async function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("must not fetch"); },
  });
  ctx.CW.confirmDialog = function () { return Promise.resolve(false); };
  var out = await ctx.CW.deleteAccount("u1");
  assert.strictEqual(out, undefined);
  assert.equal(ctx.fetchCalls.length, 0, "deny-fetch recorded nothing");
});

test("deleteAccount failure pins status plus verbatim server message", async function () {
  var ctx = fresh({
    fetchImpl: function (url) {
      if (url === "/api/v1/admin/account/u1") {
        return Promise.resolve(jsonRes(400, { message: "cannot delete last admin" }));
      }
      return Promise.resolve(jsonRes(200, { items: [] }));
    },
  });
  ctx.CW.confirmDialog = function () { return Promise.resolve(true); };
  await ctx.CW.deleteAccount("u1");
  assert.equal($(ctx, "account-result").textContent, "Delete failed (400): cannot delete last admin");
});
