"use strict";
// test-hourly.js — hourly 24h/48h series (stats.js + router.js).
// Run from configwire/: node --test js_tests/test-hourly.js
// Pin the local zone so local-time labels are deterministic on any CI host.
process.env.TZ = "Asia/Ho_Chi_Minh";
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function freshStats() {
  return h.load(["core.js", "stats.js"]);
}

function freshRouter() {
  return h.load(["core.js", "router.js", "scope.js"]);
}

function fullData() {
  return {
    version: 3,
    fetches: 10,
    exposures: 100,
    perVariant: { control: 60, treatment: 40 },
    perVersion: { 1: 30, 2: 70 },
    approximate: false,
  };
}

function statsHTML(ctx) {
  return ctx.elementsById["stats-view"].innerHTML;
}

test("hourly renderSeries uses hour labels and Hourly aria", function () {
  var ctx = freshStats();
  var data = fullData();
  data.echo = { since: "1d", granularity: "hour" };
  data.series = [
    { day: "2026-11-14", hour: "2026-11-13T15:00:00Z", fetches: 5, exposures: 2, versions: { 1: 5 } },
    { day: "2026-11-14", hour: "2026-11-13T16:00:00Z", fetches: 7, exposures: 3, versions: { 1: 7 } },
    { day: "2026-11-14", hour: "2026-11-14T15:00:00Z", fetches: 4, exposures: 1, versions: { 1: 4 } },
  ];
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("Hourly fetches and exposures for 3 hours") !== -1, "hourly aria missing: " + html);
  assert.ok(html.indexOf(">22:00<") !== -1, "hourly x label must be local HH:MM: " + html);
  assert.ok(html.indexOf("11-13 22:00 UTC+7") !== -1, "hourly range stamp must be local: " + html);
  assert.ok(html.indexOf("15:00") === -1, "UTC hour must not leak into chart labels: " + html);
  assert.ok(html.indexOf("for 3 hours") !== -1, "hour count word wrong: " + html);
});

test("hourly seriesTipHTML heads with day+hour stamp", function () {
  var ctx = freshStats();
  var tip = ctx.CW.seriesTipHTML({ day: "2026-11-14", hour: "2026-11-14T13:00:00Z", fetches: 4, exposures: 1, versions: {} }, true);
  assert.ok(tip.indexOf("11-14 13:00 UTC") !== -1, "hourly tip heading must stay UTC: " + tip);
  var daily = ctx.CW.seriesTipHTML({ day: "2026-11-14", fetches: 4, exposures: 1, versions: {} });
  assert.ok(daily.indexOf("2026-11-14 UTC") !== -1, "daily tip must name the UTC day: " + daily);
});

test("hourly chart stays local while hover tooltip reports UTC", function () {
  var ctx = freshStats();
  var data = fullData();
  data.echo = { since: "1d", granularity: "hour" };
  data.series = [];
  for (var i = 0; i < 7; i++) {
    var hh = String(15 + i);
    data.series.push({ day: "2026-11-14", hour: "2026-11-14T" + hh + ":00:00Z", fetches: i + 1, exposures: 1 });
  }
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  assert.ok(html.indexOf(">22:00<") !== -1 && html.indexOf(">04:00<") !== -1, "x ticks must be local: " + html);
  assert.ok(html.indexOf("UTC+7") !== -1, "range must name the local zone: " + html);
  assert.ok(html.indexOf("15:00") === -1 && html.indexOf("21:00") === -1, "UTC hours must be tooltip-only: " + html);
  var tip = ctx.CW.seriesTipHTML({ day: "2026-11-14", hour: "2026-11-14T15:00:00Z", fetches: 5, exposures: 2 }, true);
  assert.ok(tip.indexOf("11-14 15:00 UTC") !== -1, "hover must show the UTC instant: " + tip);
});

test("hourly renderSeries ticks every 6h for 25 points", function () {
  var ctx = freshStats();
  var data = fullData();
  data.echo = { since: "1d", granularity: "hour" };
  data.series = [];
  for (var i = 0; i < 25; i++) {
    var hh = i < 10 ? "0" + i : "" + i;
    data.series.push({ day: "2026-11-14", hour: "2026-11-14T" + hh + ":00:00Z", fetches: 1, exposures: 0 });
  }
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  // 25 hourly points tick every 6h: 0,6,12,18,24 -> 5 labels.
  assert.equal(html.split('class="stats-x"').length - 1, 5, "hourly x labels wrong: " + html);
});

test("homeMergePoints keys hourly by hour, not by day", function () {
  var ctx = freshRouter();
  var a = [
    { day: "2026-11-14", hour: "2026-11-14T13:00:00Z", fetches: 5, exposures: 0, versions: null },
    { day: "2026-11-14", hour: "2026-11-14T14:00:00Z", fetches: 7, exposures: 0, versions: null },
  ];
  var b = [
    { day: "2026-11-14", hour: "2026-11-14T13:00:00Z", fetches: 3, exposures: 1, versions: null },
  ];
  var merged = ctx.CW.homeMergePoints([a, b]);
  assert.equal(merged.length, 2, "same-day hours must not collapse: " + JSON.stringify(merged));
  assert.equal(merged[0].hour, "2026-11-14T13:00:00Z");
  assert.equal(merged[0].fetches, 8);
  assert.equal(merged[0].exposures, 1);
  assert.equal(merged[1].hour, "2026-11-14T14:00:00Z");
});

test("homeTrimPartialDay passes hourly through; daily still trims today", function () {
  var ctx = freshRouter();
  var hourly = [
    { day: "2026-11-13", hour: "2026-11-13T23:00:00Z", fetches: 4, exposures: 0 },
    { day: "2026-11-14", hour: "2026-11-14T00:00:00Z", fetches: 6, exposures: 0 },
  ];
  assert.equal(ctx.CW.homeTrimPartialDay(hourly).length, 2, "hourly must not trim");
  var today = new Date().toISOString().slice(0, 10);
  var daily = [
    { day: "2026-01-01", fetches: 4, exposures: 0 },
    { day: today, fetches: 99, exposures: 0 },
  ];
  assert.equal(ctx.CW.homeTrimPartialDay(daily).length, 1, "daily must trim today");
});

test("homeAverageForPoints divides hourly totals by window days", function () {
  var ctx = freshRouter();
  ctx.CW.state.homeSince = "1d";
  var pts = [];
  for (var i = 0; i < 25; i++) pts.push({ day: "2026-11-14", hour: "2026-11-14T00:00:00Z", fetches: 4, exposures: 0 });
  // 25h x 4 = 100 total / 1 day = 100 per day.
  assert.equal(ctx.CW.homeAverageForPoints(pts), 100);
  ctx.CW.state.homeSince = "2d";
  assert.equal(ctx.CW.homeAverageForPoints(pts), 50);
});

test("homeAverageForPoints prefers the effective echo.sinceDays over the requested range", function () {
  // Server clamps ?since= to the rollup retention: home asks 90d, backend
  // serves an effective 2d hourly window. Dividing by the requested 90
  // would understate the daily average 45x.
  var ctx = freshRouter();
  ctx.CW.state.homeSince = "90d";
  var pts = [];
  for (var i = 0; i < 49; i++) pts.push({ day: "2026-11-14", hour: "2026-11-14T00:00:00Z", fetches: 2, exposures: 0 });
  // 49h x 2 = 98 total / 2 effective days = 49 per day.
  assert.equal(ctx.CW.homeAverageForPoints(pts, 2), 49);
  // Bad/absent effective days still fall back to the requested range.
  assert.equal(ctx.CW.homeAverageForPoints(pts, 0), 1);
});
