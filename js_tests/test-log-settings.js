"use strict";
// test-log-settings.js — logs.js settings switches + save body shape.
// Loads core.js + logs.js only via js_tests/harness.js. The harness
// element stub has no attribute storage, so each test installs a tiny
// get/setAttribute pair on the switch element first.
// Covers the admin-log area switches (enable, success lines, health
// checks), inputs apply, and that saveLogSettings carries the nested
// adminLogs object in the PUT body. node:test + node:assert/strict
// only. Run from configwire/:
// node --test js_tests/test-log-settings.js
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function fresh(overrides) {
  return h.load(["core.js", "logs.js"], overrides);
}

function $(ctx, id) {
  return ctx.document.getElementById(id);
}

// withAttrs gives a stub element working aria-checked storage.
function withAttrs(el, initial) {
  var attrs = { "aria-checked": initial };
  el.getAttribute = function (k) { return attrs[k] == null ? null : attrs[k]; };
  el.setAttribute = function (k, v) { attrs[k] = String(v); };
  return el;
}

// withClasses gives a stub element working classList storage (the
// harness stub's classList is a no-op).
function withClasses(el) {
  var set = {};
  el.classList = {
    add: function (c) { set[c] = true; },
    remove: function (c) { delete set[c]; },
    toggle: function (c, force) {
      var on = force === undefined ? !set[c] : !!force;
      if (on) set[c] = true;
      else delete set[c];
      return on;
    },
    contains: function (c) { return !!set[c]; },
  };
  return el;
}

function plain(o) {
  return JSON.parse(JSON.stringify(o));
}

function okRes(data) {
  return { ok: true, status: 200, json: function () { return Promise.resolve(data); } };
}

var SETTINGS = { enabled: true, successLogs: true, maxDays: 30,
  adminLogs: { enabled: false, successLines: true, healthChecks: false } };

// --- admin-log area switches ---

test("admin switches toggle aria-checked independently", function () {
  var ctx = fresh();
  var req = withAttrs($(ctx, "log-admin-requests"), "false");
  var suc = withAttrs($(ctx, "log-admin-success"), "true");
  var hea = withAttrs($(ctx, "log-admin-health"), "false");
  ctx.CW.onLogAdminRequestsSwitch();
  ctx.CW.onLogAdminSuccessSwitch();
  ctx.CW.onLogAdminHealthSwitch();
  assert.equal(req.getAttribute("aria-checked"), "true");
  assert.equal(suc.getAttribute("aria-checked"), "false");
  assert.equal(hea.getAttribute("aria-checked"), "true");
});

test("setSettingsInputs applies the adminLogs object", function () {
  var ctx = fresh();
  var req = withAttrs($(ctx, "log-admin-requests"), "false");
  var suc = withAttrs($(ctx, "log-admin-success"), "false");
  var hea = withAttrs($(ctx, "log-admin-health"), "false");
  ctx.CW.setLogAdminRequestsSwitch(true);
  ctx.CW.setLogAdminSuccessSwitch(true);
  ctx.CW.setLogAdminHealthSwitch(true);
  assert.equal(req.getAttribute("aria-checked"), "true");
  assert.equal(suc.getAttribute("aria-checked"), "true");
  assert.equal(hea.getAttribute("aria-checked"), "true");
  ctx.CW.setLogAdminRequestsSwitch(false);
  assert.equal(req.getAttribute("aria-checked"), "false");
});

// --- card visibility ---

test("master switch collapses and expands all cards together", function () {
  var ctx = fresh();
  var wrap = withClasses($(ctx, "log-settings-cards"));
  withAttrs($(ctx, "log-enabled"), "true");
  ctx.CW.onLogEnabledSwitch();
  assert.equal(wrap.classList.contains("is-collapsed"), true);
  ctx.CW.onLogEnabledSwitch();
  assert.equal(wrap.classList.contains("is-collapsed"), false);
});

test("admin switch expands and collapses its sub-settings", function () {
  var ctx = fresh();
  var body = withClasses($(ctx, "log-admin-settings-body"));
  withAttrs($(ctx, "log-admin-requests"), "false");
  ctx.CW.onLogAdminRequestsSwitch();
  assert.equal(body.classList.contains("is-collapsed"), false);
  ctx.CW.onLogAdminRequestsSwitch();
  assert.equal(body.classList.contains("is-collapsed"), true);
});

test("opening the dialog collapses sub-settings when the area is off", function () {
  var ctx = fresh();
  var body = withClasses($(ctx, "log-admin-settings-body"));
  withAttrs($(ctx, "log-admin-requests"), "true");
  ctx.CW.state.logSettings = { enabled: true, adminLogs: { enabled: false, successLines: true, healthChecks: false } };
  ctx.CW.openLogSettingsDialog();
  assert.equal($(ctx, "log-admin-requests").getAttribute("aria-checked"), "false");
  assert.equal(body.classList.contains("is-collapsed"), true);
});

// --- save body shape ---

test("saveLogSettings sends nested adminLogs in the PUT body", async function () {
  var puts = [];
  var ctx = fresh({
    fetchImpl: function (url, opts) {
      var u = String(url);
      if (u === "/api/v1/admin/log-settings" && opts && opts.method === "PUT") {
        puts.push(plain(JSON.parse(opts.body)));
        return Promise.resolve(okRes({ enabled: true, successLogs: true, maxDays: 30,
          adminLogs: { enabled: true, successLines: false, healthChecks: true } }));
      }
      if (u === "/api/v1/admin/log-settings") {
        return Promise.resolve(okRes(plain(SETTINGS)));
      }
      if (u.indexOf("/api/v1/admin/logs") === 0) {
        return Promise.resolve(okRes({ items: [], page: 1, perPage: 10, total: 0 }));
      }
      throw new Error("harness fetch denied: " + url);
    },
  });
  withAttrs($(ctx, "log-admin-requests"), "true");
  withAttrs($(ctx, "log-admin-success"), "false");
  withAttrs($(ctx, "log-admin-health"), "true");
  withAttrs($(ctx, "log-success"), "true");
  $(ctx, "log-maxdays").value = "30";
  await ctx.CW.saveLogSettings();
  assert.equal(puts.length, 1);
  assert.deepEqual(puts[0].adminLogs, { enabled: true, successLines: false, healthChecks: true });
  assert.equal(puts[0].successLogs, true);
  assert.equal(puts[0].maxDays, 30);
});
