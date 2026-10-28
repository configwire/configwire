"use strict";
// test-logs-env.js — logs viewer ?env= filter wiring (env chip + pill).
// Loads core.js + logs.js only via js_tests/harness.js.
// Covers the project-chip precedent applied to environments: per-row env
// chip (data-f-kind="env", slug text with raw-ID fallback, is-missing when
// the record is gone, absent when the item carries no env), ?env= in
// buildLogsPath, "Environment: <slug>" pill label, and pill/toggle/clear
// removal. node:test + node:assert/strict only. Run from configwire/:
// node --test js_tests/test-logs-env.js
var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

function freshLogs(overrides) {
  return harness.load(["core.js", "logs.js"], overrides);
}

function seedScope(ctx) {
  ctx.CW.state.projects = [{ id: "p1", name: "Main" }];
  ctx.CW.state.logEnvs = [{ id: "e1", slug: "prod" }];
}

var ROW = "POST /api/v1/env/prod/events reason=accepted status=202 project=p1";

function okRes(data) {
  return { ok: true, status: 200, json: function () { return Promise.resolve(data); } };
}

function logEnvFetch(logItems) {
  return function (url) {
    var u = String(url);
    if (u.indexOf("/api/collections/environments/records") === 0) {
      return Promise.resolve(okRes({ items: [{ id: "e1", slug: "prod" }], perPage: 200, totalPages: 1 }));
    }
    if (u.indexOf("/api/collections/projects/records") === 0) {
      return Promise.resolve(okRes({ items: [{ id: "p1", name: "Main" }], perPage: 200, totalPages: 1 }));
    }
    if (u.indexOf("/api/v1/admin/logs") === 0) {
      return Promise.resolve(okRes({ items: logItems, page: 1, perPage: 10, total: logItems.length }));
    }
    throw new Error("harness fetch denied: " + url);
  };
}

function logUrls(ctx) {
  return ctx.fetchCalls.map(function (c) { return String(c.url); })
    .filter(function (u) { return u.indexOf("/api/v1/admin/logs") === 0; });
}

// --- labels ---

test("env kind label is Environment", function () {
  var ctx = freshLogs();
  assert.equal(ctx.CW.logFilterKindLabel("env"), "Environment");
});

test("env value label resolves slug, falls back to raw id", function () {
  var ctx = freshLogs();
  seedScope(ctx);
  assert.equal(ctx.CW.logFilterLabel("env", "e1"), "prod");
  assert.equal(ctx.CW.logFilterLabel("env", "ghost"), "ghost");
});

// --- path building ---

function baseFilters(env) {
  return { level: "", src: "", project: "", env: env, key: "",
    status: "", reason: "", limiter: "", page: 1, perPage: 10 };
}

test("buildLogsPath includes ?env= when set, omits when empty", function () {
  var ctx = freshLogs();
  assert.ok(ctx.CW.buildLogsPath(baseFilters("e1")).indexOf("env=e1") !== -1);
  assert.ok(ctx.CW.buildLogsPath(baseFilters("")).indexOf("env=") === -1);
});

// --- row chips ---

test("messageCellHtml renders env chip with slug next to project chip", function () {
  var ctx = freshLogs();
  seedScope(ctx);
  var html = ctx.CW.messageCellHtml(ROW, "sdk", "e1");
  assert.ok(html.indexOf('data-f-kind="env"') !== -1);
  assert.ok(html.indexOf('data-f-val="e1"') !== -1);
  assert.ok(html.indexOf(">prod<") !== -1);
  assert.ok(html.indexOf("log-env") !== -1);
  assert.ok(html.indexOf("is-missing") === -1);
  assert.ok(html.indexOf("log-project") !== -1, "project chip still rendered");
  assert.ok(html.indexOf("log-project") < html.indexOf("log-env"), "env chip sits next to project chip");
});

test("messageCellHtml falls back to raw id with missing styling when record gone", function () {
  var ctx = freshLogs();
  seedScope(ctx);
  var html = ctx.CW.messageCellHtml(ROW, "sdk", "gone");
  assert.ok(html.indexOf('data-f-val="gone"') !== -1);
  assert.ok(html.indexOf(">gone<") !== -1);
  assert.ok(html.indexOf("log-env is-missing") !== -1);
});

test("messageCellHtml renders no env chip when env empty", function () {
  var ctx = freshLogs();
  seedScope(ctx);
  assert.ok(ctx.CW.messageCellHtml(ROW, "sdk", "").indexOf("log-env") === -1);
  assert.ok(ctx.CW.messageCellHtml(ROW, "sdk", undefined).indexOf("log-env") === -1);
});

// --- end-to-end through loadLogs ---

test("loadLogs sends ?env=, renders chip + pill; clearLogFilter removes", async function () {
  var items = [{ id: "r1", level: 1, message: ROW, src: "sdk", env: "e1", created: "2026-10-28 12:00:00.000Z" }];
  var ctx = freshLogs({ fetchImpl: logEnvFetch(items) });
  ctx.CW.state.logFilters = { level: "", src: "", project: "", env: "e1",
    key: "", status: "", reason: "", limiter: "", page: 1, perPage: 10 };
  await ctx.CW.loadLogs();
  var urls = logUrls(ctx);
  assert.equal(urls.length, 1);
  assert.ok(urls[0].indexOf("env=e1") !== -1, "got " + urls[0]);
  var listHtml = String(ctx.CW.$("logs-list").innerHTML);
  assert.ok(listHtml.indexOf('data-f-kind="env"') !== -1);
  assert.ok(listHtml.indexOf(">prod<") !== -1);
  var pills = String(ctx.CW.$("logs-active-filters").innerHTML);
  assert.ok(pills.indexOf("Environment: prod") !== -1, "got " + pills);

  await ctx.CW.clearLogFilter("env");
  urls = logUrls(ctx);
  assert.equal(urls.length, 2);
  assert.ok(urls[1].indexOf("env=") === -1, "got " + urls[1]);
  pills = String(ctx.CW.$("logs-active-filters").innerHTML);
  assert.ok(pills.indexOf("Environment") === -1, "got " + pills);
});

test("toggleLogFilter toggles env on/off; clearAllLogFilters resets env keeping perPage", async function () {
  var items = [{ id: "r1", level: 1, message: ROW, src: "sdk", env: "", created: "2026-10-28 12:00:00.000Z" }];
  var ctx = freshLogs({ fetchImpl: logEnvFetch(items) });
  ctx.CW.state.logFilters = { level: "", src: "", project: "", env: "",
    key: "", status: "", reason: "", limiter: "", page: 1, perPage: 25 };
  await ctx.CW.toggleLogFilter("env", "e1");
  var urls = logUrls(ctx);
  assert.equal(urls.length, 1);
  assert.ok(urls[0].indexOf("env=e1") !== -1, "got " + urls[0]);
  assert.ok(String(ctx.CW.$("logs-active-filters").innerHTML).indexOf("Environment: prod") !== -1);

  await ctx.CW.toggleLogFilter("env", "e1");
  urls = logUrls(ctx);
  assert.equal(urls.length, 2);
  assert.ok(urls[1].indexOf("env=") === -1, "got " + urls[1]);

  ctx.CW.state.logFilters.env = "e1";
  ctx.CW.state.logFilters.project = "p1";
  await ctx.CW.clearAllLogFilters();
  urls = logUrls(ctx);
  assert.equal(urls.length, 3);
  assert.ok(urls[2].indexOf("env=") === -1 && urls[2].indexOf("project=") === -1, "got " + urls[2]);
  assert.ok(urls[2].indexOf("perPage=25") !== -1, "perPage kept, got " + urls[2]);
  assert.equal(String(ctx.CW.$("logs-active-filters").innerHTML), "");
});
