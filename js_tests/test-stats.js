"use strict";
// test-stats.js — stats.js render/copy/load (Wave 2, todo 7).
// Loads core.js + stats.js only via js_tests/harness.js.
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-stats.js
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function freshStats(overrides) {
  return h.load(["core.js", "stats.js"], overrides);
}

function fullData() {
  return {
    version: 3,
    fetches: 10,
    exposures: 100,
    perVariant: { control: 60, treatment: 40 },
    perVersion: { 1: 30, 2: 70 },
    echo: { flag: "launch_flag", since: "7d", cutoff: "2026-09-01" },
    flagFound: true,
    approximate: false,
  };
}

function statsHTML(ctx) {
  return ctx.elementsById["stats-view"].innerHTML;
}

// --- renderStats populated rendering (stats.js:7-103) ---

test("renderStats bars/donut math sums to exposures and stores lastStatsText", function () {
  var ctx = freshStats();
  var data = fullData();
  ctx.CW.renderStats(data);
  assert.equal(ctx.CW.state.lastStats, data);
  assert.equal(ctx.CW.state.lastStatsText, JSON.stringify(data, null, 2));
  var html = statsHTML(ctx);
  // perVariant bar widths pinned (stats.js:37-43): 60/100 -> 60.0%, 40/100 -> 40.0%.
  assert.ok(html.indexOf("width: 60.0%") !== -1, "missing 60% bar, got: " + html);
  assert.ok(html.indexOf("width: 40.0%") !== -1, "missing 40% bar, got: " + html);
  // Donut stops accumulate to 100 (stats.js:44-49): accent 0.0-60.0, ok 60.0-100.0.
  assert.ok(
    html.indexOf("var(--accent) 0.0% 60.0%, var(--ok) 60.0% 100.0%") !== -1,
    "donut stops wrong, got: " + html
  );
  assert.ok(
    html.indexOf('aria-label="Variant split: control 60.0%, treatment 40.0%"') !== -1,
    "donut label wrong, got: " + html
  );
  // Split line (stats.js:26-34) and perVersion line (stats.js:81-85).
  assert.ok(html.indexOf("control: 60 (60.0%), treatment: 40 (40.0%)") !== -1, "split wrong: " + html);
  assert.ok(html.indexOf("v1: 30, v2: 70") !== -1, "versions wrong: " + html);
  // Echo line (stats.js:57-61).
  assert.ok(
    html.indexOf("flag launch_flag · since 7d · cutoff 2026-09-01") !== -1,
    "echo wrong: " + html
  );
});

test("renderStats pins approximate:true marker", function () {
  var ctx = freshStats();
  var data = fullData();
  data.approximate = true;
  ctx.CW.renderStats(data);
  assert.ok(statsHTML(ctx).indexOf('<p class="muted">approximate</p>') !== -1);
  var plain = fullData();
  var ctx2 = freshStats();
  ctx2.CW.renderStats(plain);
  assert.ok(statsHTML(ctx2).indexOf("approximate") === -1);
});

test("renderStats flagFound:false keeps zero wall plus warning", function () {
  var ctx = freshStats();
  ctx.CW.renderStats({
    version: 1, fetches: 0, exposures: 0,
    perVariant: {}, perVersion: {}, echo: { flag: "nope", since: "7d" },
    flagFound: false,
  });
  var html = statsHTML(ctx);
  // stats.js:69: flagFound:false forces the populated branch even at zero.
  assert.ok(html.indexOf("stats-count") !== -1, "expected populated zero wall: " + html);
  assert.ok(
    html.indexOf("<p role=\"alert\">warning: unknown flag — showing zeros (flagFound:false).</p>") !== -1,
    "missing unknown-flag warning: " + html
  );
});

test("renderStats echoes lastStatsError as escaped alert", function () {
  var ctx = freshStats();
  ctx.CW.state.lastStatsError = "boom <b>failed</b>";
  ctx.CW.renderStats(fullData());
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("boom &lt;b&gt;failed&lt;/b&gt;") !== -1, "error not echoed escaped: " + html);
  assert.ok(html.indexOf("boom <b>") === -1, "raw HTML leaked into error echo");
});

test("renderStats true-empty onboarding state and empty chart", function () {
  var ctx = freshStats();
  ctx.CW.renderStats({
    version: 0, fetches: 0, exposures: 0,
    perVariant: {}, perVersion: {}, echo: {}, flagFound: true,
  });
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("No stats yet") !== -1, "missing onboarding guide: " + html);
  assert.ok(html.indexOf("stats-echo-quiet") !== -1, "missing quiet echo: " + html);
  var ctx2 = freshStats();
  ctx2.CW.renderStats({
    version: 1, fetches: 5, exposures: 0,
    perVariant: {}, perVersion: {}, echo: {}, flagFound: true,
  });
  assert.ok(
    statsHTML(ctx2).indexOf("No exposures yet") !== -1,
    "missing empty-chart line: " + statsHTML(ctx2)
  );
});

// --- copyStatsJson branches (stats.js:105-125) ---
// stats.js:113 reads the BARE `navigator` global (= sandbox.navigator);
// the harness leaves it as {} (harness.js:216), so tests set
// context.navigator.clipboard post-load for the clipboard branch.

test("copyStatsJson clipboard branch writes lastStatsText and says copied", async function () {
  var ctx = freshStats();
  ctx.CW.state.lastStatsText = '{"exposures":3}';
  var written = null;
  ctx.context.navigator.clipboard = {
    writeText: function (t) {
      written = t;
      return Promise.resolve();
    },
  };
  ctx.CW.copyStatsJson();
  await new Promise(function (r) { setTimeout(r, 20); });
  assert.equal(written, '{"exposures":3}');
  assert.equal(ctx.elementsById["stats-copy-status"].textContent, "copied");
});

test("copyStatsJson clipboard rejection says copy failed", async function () {
  var ctx = freshStats();
  ctx.CW.state.lastStatsText = '{"exposures":3}';
  ctx.context.navigator.clipboard = {
    writeText: function () { return Promise.reject(new Error("denied")); },
  };
  ctx.CW.copyStatsJson();
  await new Promise(function (r) { setTimeout(r, 20); });
  assert.equal(
    ctx.elementsById["stats-copy-status"].textContent,
    "copy failed — select and copy manually"
  );
});

test("copyStatsJson fallback textarea+execCommand path copies and cleans up", function () {
  var ctx = freshStats();
  ctx.CW.state.lastStatsText = '{"exposures":7}';
  delete ctx.context.navigator.clipboard;
  var created = null;
  var origCreate = ctx.context.document.createElement;
  ctx.context.document.createElement = function (tag) {
    var el = origCreate.call(this, tag);
    if (tag === "textarea") created = el;
    return el;
  };
  var before = ctx.context.document.body.children.length;
  ctx.CW.copyStatsJson();
  assert.ok(created !== null, "fallback must create a textarea");
  assert.equal(created.value, '{"exposures":7}');
  assert.equal(ctx.context.document.body.children.length, before, "textarea must be removed");
  assert.equal(ctx.elementsById["stats-copy-status"].textContent, "copied");
});

test("copyStatsJson fallback execCommand throw says copy failed", function () {
  var ctx = freshStats();
  ctx.CW.state.lastStatsText = '{"exposures":7}';
  delete ctx.context.navigator.clipboard;
  ctx.context.document.execCommand = function () { throw new Error("nope"); };
  ctx.CW.copyStatsJson();
  assert.equal(
    ctx.elementsById["stats-copy-status"].textContent,
    "copy failed — select and copy manually"
  );
});

test("copyStatsJson with empty text says nothing to copy; missing status falls back to toast", function () {
  var ctx = freshStats();
  ctx.CW.state.lastStatsText = "";
  ctx.context.navigator.clipboard = {
    writeText: function () { return Promise.resolve(); },
  };
  ctx.CW.copyStatsJson();
  assert.equal(ctx.elementsById["stats-copy-status"].textContent, "nothing to copy");
  var ctx2 = freshStats();
  ctx2.CW.state.lastStatsText = '{"a":1}';
  delete ctx2.context.navigator.clipboard;
  ctx2.CW.$ = function () { return null; }; // stats.js:106 status null -> say() toasts
  var toasted = [];
  ctx2.CW.toast = function (m) { toasted.push(m); };
  ctx2.CW.copyStatsJson();
  assert.deepEqual(toasted, ["copied"]);
});

// --- loadStats (stats.js:148-171) ---

test("loadStats shows Loading placeholder then stores lastStatsText on success", async function () {
  var ctx = freshStats();
  ctx.CW.$("stats-flag").value = "launch_flag";
  ctx.CW.$("stats-since").value = "30d";
  ctx.CW.state.projectId = "p1";
  var data = fullData();
  var seenUrl = null;
  var release;
  var gate = new Promise(function (r) { release = r; });
  ctx.CW.api = function (url) {
    seenUrl = url;
    return gate.then(function () { return data; });
  };
  var p = ctx.CW.loadStats();
  assert.equal(ctx.elementsById["stats-view"].textContent, "Loading…");
  release();
  var out = await p;
  assert.equal(out, data);
  assert.equal(ctx.CW.state.lastStats, data);
  assert.equal(ctx.CW.state.lastStatsText, JSON.stringify(data, null, 2));
  assert.equal(ctx.CW.state.lastStatsError, "");
  assert.ok(
    seenUrl === "/api/v1/admin/env/dev/stats?since=30d&flag=launch_flag&project=p1",
    "unexpected stats url: " + seenUrl
  );
});

test("loadStats defaults empty since to 7d", async function () {
  var ctx = freshStats();
  ctx.CW.$("stats-flag").value = "";
  ctx.CW.$("stats-since").value = "";
  var seenUrl = null;
  ctx.CW.api = function (url) {
    seenUrl = url;
    return Promise.resolve(fullData());
  };
  await ctx.CW.loadStats();
  assert.ok(seenUrl.indexOf("since=7d") !== -1, "unexpected stats url: " + seenUrl);
  assert.ok(seenUrl.indexOf("flag=") === -1, "empty flag must be omitted: " + seenUrl);
});

test("loadStats 500 keeps previous lastStats rendered with first-line error", async function () {
  var ctx = freshStats();
  var prev = fullData();
  ctx.CW.renderStats(prev);
  var prevText = ctx.CW.state.lastStatsText;
  ctx.CW.api = function () { return Promise.reject(new Error("request failed (500): /stats\nsecond line")); };
  var err = null;
  try {
    await ctx.CW.loadStats();
  } catch (e) {
    err = e;
  }
  assert.ok(err instanceof Error, "loadStats must rethrow");
  assert.equal(ctx.CW.state.lastStats, prev);
  assert.equal(ctx.CW.state.lastStatsText, prevText);
  assert.equal(ctx.CW.state.lastStatsError, "request failed (500): /stats");
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("exposures: <strong>100</strong>") !== -1, "previous stats lost: " + html);
  assert.ok(html.indexOf("request failed (500): /stats") !== -1, "first-line error missing: " + html);
  assert.ok(html.indexOf("second line") === -1, "error must be first line only");
});

test("loadStats error with no prior stats renders Loading failed wall", async function () {
  var ctx = freshStats();
  ctx.CW.api = function () { return Promise.reject(new Error("down")); };
  var err = null;
  try {
    await ctx.CW.loadStats();
  } catch (e) {
    err = e;
  }
  assert.ok(err instanceof Error, "loadStats must rethrow");
  assert.equal(ctx.CW.state.lastStats, null);
  assert.equal(ctx.CW.state.lastStatsError, "down");
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("Loading… failed.") !== -1, "missing failed wall: " + html);
  assert.ok(html.indexOf("down") !== -1, "missing error text: " + html);
});

// --- renderStatsFlagOptions (stats.js:129-146; no CW.drafts loaded -> state.flags) ---

test("renderStatsFlagOptions lists sorted flags; stale selection round-trips", function () {
  var ctx = freshStats();
  ctx.CW.state.flags = [{ key: "b" }, { key: "a" }, { key: "" }, null, { key: "c", _draftDeleted: true }];
  ctx.CW.renderStatsFlagOptions();
  assert.equal(
    ctx.CW.$("stats-flag").innerHTML,
    '<option value="">All flags</option>' +
      '<option value="a">a</option><option value="b">b</option>'
  );
  ctx.CW.$("stats-flag").value = "zzz";
  ctx.CW.renderStatsFlagOptions();
  var html = ctx.CW.$("stats-flag").innerHTML;
  assert.ok(html.indexOf('<option value="zzz" selected>zzz</option>') !== -1, "stale kept: " + html);
});
