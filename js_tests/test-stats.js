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
  // Since-only filters: no flag echo line is ever rendered.
  assert.ok(html.indexOf("flag ") === -1, "flag echo must be gone: " + html);
  assert.ok(html.indexOf("stats-echo-quiet") === -1, "quiet echo must be gone: " + html);
});

test("renderStats pins approximate:true marker", function () {
  var ctx = freshStats();
  var data = fullData();
  data.approximate = true;
  ctx.CW.renderStats(data);
  assert.ok(statsHTML(ctx).indexOf('<p class="muted">Approximate.</p>') !== -1);
  var plain = fullData();
  var ctx2 = freshStats();
  ctx2.CW.renderStats(plain);
  assert.ok(statsHTML(ctx2).indexOf("Approximate") === -1);
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
    perVariant: {}, perVersion: {},
  });
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("Publish, fetch, then send exposure.") !== -1, "missing onboarding guide: " + html);
  assert.ok(html.indexOf("stats-echo-quiet") === -1, "quiet echo must be gone: " + html);
  assert.ok(html.indexOf("flag ") === -1, "flag echo must be gone: " + html);
  var ctx2 = freshStats();
  ctx2.CW.renderStats({
    version: 1, fetches: 5, exposures: 0,
    perVariant: {}, perVersion: {},
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
  assert.equal(ctx.elementsById["stats-copy-status"].textContent, "Copied");
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
    "Copy failed"
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
  assert.equal(ctx.elementsById["stats-copy-status"].textContent, "Copied");
});

test("copyStatsJson fallback execCommand throw says copy failed", function () {
  var ctx = freshStats();
  ctx.CW.state.lastStatsText = '{"exposures":7}';
  delete ctx.context.navigator.clipboard;
  ctx.context.document.execCommand = function () { throw new Error("nope"); };
  ctx.CW.copyStatsJson();
  assert.equal(
    ctx.elementsById["stats-copy-status"].textContent,
    "Copy failed"
  );
});

test("copyStatsJson with empty text says nothing to copy; missing status falls back to toast", function () {
  var ctx = freshStats();
  ctx.CW.state.lastStatsText = "";
  ctx.context.navigator.clipboard = {
    writeText: function () { return Promise.resolve(); },
  };
  ctx.CW.copyStatsJson();
  assert.equal(ctx.elementsById["stats-copy-status"].textContent, "Nothing to copy.");
  var ctx2 = freshStats();
  ctx2.CW.state.lastStatsText = '{"a":1}';
  delete ctx2.context.navigator.clipboard;
  ctx2.CW.$ = function () { return null; }; // stats.js:106 status null -> say() toasts
  var toasted = [];
  ctx2.CW.toast = function (m) { toasted.push(m); };
  ctx2.CW.copyStatsJson();
  assert.deepEqual(toasted, ["Copied"]);
});

// --- loadStats (stats.js:148-171) ---

test("loadStats shows Loading placeholder then stores lastStatsText on success", async function () {
  var ctx = freshStats();
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
    seenUrl === "/api/v1/admin/env/dev/stats?since=30d&project=p1",
    "unexpected stats url: " + seenUrl
  );
});

test("loadStats defaults empty since to 7d", async function () {
  var ctx = freshStats();
  ctx.CW.$("stats-since").value = "";
  var seenUrl = null;
  ctx.CW.api = function (url) {
    seenUrl = url;
    return Promise.resolve(fullData());
  };
  await ctx.CW.loadStats();
  assert.ok(seenUrl.indexOf("since=7d") !== -1, "unexpected stats url: " + seenUrl);
  assert.ok(seenUrl.indexOf("flag=") === -1, "flag param must never be sent: " + seenUrl);
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
  assert.ok(html.indexOf("Load failed.") !== -1, "missing failed wall: " + html);
  assert.ok(html.indexOf("down") !== -1, "missing error text: " + html);
});

// --- series stacked-area chart + per-version bars (stats.js renderSeries/bindSeriesTip/renderVerBars) ---

test("renderStats series svg is a stacked area chart with axes, tooltip and hover targets", function () {
  var ctx = freshStats();
  var data = fullData();
  data.series = [
    { day: "2026-09-01", fetches: 10, exposures: 5 },
    { day: "2026-09-02", fetches: 100, exposures: 20 },
    { day: "2026-09-03", fetches: 50, exposures: 50 },
  ];
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  // Stacked totals 15/120/100 -> max 120 (stats.js renderSeries).
  assert.ok(html.indexOf('class="stats-series-wrap"') !== -1, "missing series wrap: " + html);
  assert.ok(html.indexOf('tabindex="0"') !== -1, "wrap must be keyboard-focusable: " + html);
  assert.ok(html.indexOf('data-count="3"') !== -1, "missing data-count: " + html);
  assert.ok(html.indexOf('data-peak="120"') !== -1, "missing data-peak 120: " + html);
  assert.ok(html.indexOf('class="stats-series"') !== -1, "missing series svg: " + html);
  assert.ok(html.indexOf('role="img"') !== -1, "missing role=img: " + html);
  assert.ok(
    html.indexOf('aria-label="Daily fetches and exposures for 3 days, 09-01 to 09-03, peak 120"') !== -1,
    "missing series aria-label range+peak: " + html
  );
  assert.ok(html.indexOf('viewBox="0 0 560 220"') !== -1, "wrong viewBox 560x220: " + html);
  // Exactly two stacked fills + two smooth strokes, no legacy grouped bars.
  assert.equal(html.split("<path").length - 1, 4, "must be 2 areas + 2 smooth lines: " + html);
  assert.equal(html.split("stats-area-fetch").length - 1, 1, "fetch area wrong: " + html);
  assert.equal(html.split("stats-area-exposure").length - 1, 1, "exposure area wrong: " + html);
  assert.ok(html.indexOf("stats-series-bar-") === -1, "legacy bar rects must be gone: " + html);
  assert.equal(html.split("<title>").length - 1, 2, "one title per area path: " + html);
  // Smoothed areas use bezier curves (3 points -> Catmull-Rom).
  var fetchD = /stats-area-fetch" d="([^"]+)/.exec(html);
  assert.ok(fetchD && fetchD[1].indexOf("C") !== -1, "fetch area must be smoothed: " + html);
  // Grid + axes: max=120 -> ticks 0/40/80/120; 3d shows every day label.
  assert.equal(html.split('class="stats-grid"').length - 1, 4, "gridline count wrong: " + html);
  assert.ok(html.indexOf('class="stats-tick"') !== -1, "missing y ticks: " + html);
  assert.ok(html.indexOf(">0<") !== -1, "missing y tick 0: " + html);
  assert.ok(html.indexOf(">120<") !== -1, "missing y tick max: " + html);
  assert.equal(html.split('class="stats-x"').length - 1, 3, "x label count wrong: " + html);
  assert.ok(html.indexOf(">09-01<") !== -1, "missing x label 09-01: " + html);
  assert.ok(html.indexOf(">09-03<") !== -1, "missing x label 09-03: " + html);
  // Hover affordances: crosshair line + dots hidden, hit overlay, tooltip hidden+empty.
  assert.ok(html.indexOf('class="stats-hover"') !== -1, "missing hover line: " + html);
  assert.ok(html.indexOf('class="stats-dot-fetch"') !== -1, "missing fetch dot: " + html);
  assert.ok(html.indexOf('class="stats-dot-exposure"') !== -1, "missing exposure dot: " + html);
  assert.ok(html.indexOf('class="stats-hit"') !== -1, "missing hit overlay: " + html);
  assert.ok(html.indexOf('<div class="stats-tip" hidden></div>') !== -1, "tooltip must be hidden+empty: " + html);
  // Interactivity is wired via the exposed binder (stub DOM has no inner nodes).
  assert.equal(typeof ctx.CW.bindSeriesTip, "function", "binder must be exposed");
});

test("renderStats series scales y to max stacked total; fetches-only still paints", function () {
  var ctx = freshStats();
  var data = fullData();
  data.series = [
    { day: "d1", fetches: 10, exposures: 5 },
    { day: "d2", fetches: 100, exposures: 20 },
  ];
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  // Stacked totals 15/120 -> peak 120 drives the y scale, not per-kind max.
  assert.ok(html.indexOf('data-peak="120"') !== -1, "peak must be stacked total 120: " + html);
  assert.ok(html.indexOf("peak 120") !== -1, "aria peak must be 120: " + html);
  assert.ok(html.indexOf(">120<") !== -1, "y tick max must be 120: " + html);
  var ctx2 = freshStats();
  var data2 = fullData();
  data2.series = [
    { day: "2026-09-01", fetches: 5, exposures: 0 },
    { day: "2026-09-02", fetches: 7, exposures: 0 },
  ];
  ctx2.CW.renderStats(data2);
  var html2 = statsHTML(ctx2);
  assert.ok(html2.indexOf('class="stats-series"') !== -1, "fetches-only must render areas: " + html2);
  assert.equal(html2.split("<path").length - 1, 4, "fetches-only keeps 2 areas + 2 lines: " + html2);
  assert.ok(html2.indexOf('data-peak="7"') !== -1, "fetches-only peak must be 7: " + html2);
  assert.ok(html2.indexOf("stats-series-empty") === -1, "fetches-only is not empty: " + html2);
});

test("renderStats series sparse x labels on long windows; single day paints a band", function () {
  var ctx = freshStats();
  var data = fullData();
  data.series = [];
  for (var i = 0; i < 30; i++) {
    var dd = (i < 9 ? "0" : "") + (i + 1);
    data.series.push({ day: "2026-08-" + dd, fetches: i + 1, exposures: 0 });
  }
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  // 30d: every 7th + last -> 0,7,14,21,28,29 (6 labels, not 30).
  assert.equal(html.split('class="stats-x"').length - 1, 6, "30d must sparsify x labels: " + html);
  assert.ok(html.indexOf(">08-01<") !== -1, "missing first x label: " + html);
  assert.ok(html.indexOf(">08-30<") !== -1, "missing last x label: " + html);
  var ctx2 = freshStats();
  var data2 = fullData();
  data2.series = [{ day: "2026-09-05", fetches: 4, exposures: 3 }];
  ctx2.CW.renderStats(data2);
  var html2 = statsHTML(ctx2);
  assert.ok(html2.indexOf('data-count="1"') !== -1, "single day count wrong: " + html2);
  assert.ok(html2.indexOf('data-peak="7"') !== -1, "single day peak must stack to 7: " + html2);
  assert.ok(
    html2.indexOf("for 1 day, 09-05, peak 7") !== -1,
    "single day aria wrong: " + html2
  );
  assert.equal(html2.split("<path").length - 1, 4, "single day keeps 2 areas + 2 lines: " + html2);
});

test("bindSeriesTip is a safe no-op without parsed nodes and clamps bad values", function () {
  var ctx = freshStats();
  assert.doesNotThrow(function () { ctx.CW.bindSeriesTip(); });
  var data = fullData();
  data.series = [
    { day: "2026-09-01", fetches: -5, exposures: "9" },
    { day: "2026-09-02", fetches: "xx", exposures: -2 },
  ];
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  // -5 clamps to 0, "9" coerces to 9, "xx" to 0: totals 9/0 -> peak 9.
  assert.ok(html.indexOf('data-peak="9"') !== -1, "sanitized peak must be 9: " + html);
  assert.doesNotThrow(function () { ctx.CW.bindSeriesTip(); });
});

test("renderStats per-version bars scale to total and sort numerically", function () {
  var ctx = freshStats();
  var data = fullData();
  data.perVersion = { 1: 10, 2: 100, 10: 50 };
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  // total=160 -> v1 6.3% / v2 62.5% / v10 31.3% plus total row at 100.0%.
  assert.ok(html.indexOf("width: 6.3%") !== -1, "missing v1 width: " + html);
  assert.ok(html.indexOf("width: 62.5%") !== -1, "missing v2 width: " + html);
  assert.ok(html.indexOf("width: 31.3%") !== -1, "missing v10 width: " + html);
  assert.ok(html.indexOf('aria-label="v2 62.5%"') !== -1, "missing v2 aria-label: " + html);
  assert.ok(html.indexOf('class="stats-ver-row stats-bar-row"') !== -1, "missing dual row class: " + html);
  assert.ok(html.indexOf('class="stats-ver-fill stats-bar-fill"') !== -1, "missing dual fill class: " + html);
  assert.ok(html.indexOf(">total<") !== -1, "missing total row: " + html);
  assert.ok(html.indexOf("160 (100.0%)") !== -1, "missing total count: " + html);
  assert.ok(html.indexOf('aria-label="total 100.0%"') !== -1, "missing total aria-label: " + html);
  var i1 = html.indexOf(">v1<");
  var i2 = html.indexOf(">v2<");
  var i10 = html.indexOf(">v10<");
  assert.ok(i1 !== -1 && i2 !== -1 && i10 !== -1, "missing version labels: " + html);
  assert.ok(i1 < i2 && i2 < i10, "versions must sort numerically 1,2,10: " + html);
});

test("renderStats populated branch shows empty-series note when series missing/empty/all-zero", function () {
  var cases = [
    { name: "missing", series: undefined },
    { name: "empty", series: [] },
    {
      name: "all-zero",
      series: [
        { day: "d1", fetches: 0, exposures: 0 },
        { day: "d2", fetches: 0, exposures: 0 },
      ],
    },
  ];
  cases.forEach(function (c) {
    var ctx = freshStats();
    var data = fullData();
    if (c.series !== undefined) data.series = c.series;
    ctx.CW.renderStats(data);
    var html = statsHTML(ctx);
    assert.ok(html.indexOf("stats-count") !== -1, c.name + ": must stay populated: " + html);
    assert.ok(html.indexOf("stats-series-empty") !== -1, c.name + ": missing empty note: " + html);
    assert.ok(
      html.indexOf("No activity.") !== -1,
      c.name + ": missing empty text: " + html
    );
    assert.ok(html.indexOf("<svg") === -1, c.name + ": must have no svg: " + html);
  });
});

test("renderStats true-empty branch renders no svg even with series present", function () {
  var ctx = freshStats();
  ctx.CW.renderStats({
    version: 0, fetches: 0, exposures: 0,
    perVariant: {}, perVersion: {},
    series: [{ day: "d1", fetches: 5, exposures: 5 }],
  });
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("Publish, fetch, then send exposure.") !== -1, "missing onboarding guide: " + html);
  assert.ok(html.indexOf("<svg") === -1, "true-empty must have no svg: " + html);
  assert.ok(html.indexOf("stats-series") === -1, "true-empty must have no series markup: " + html);
});

// --- per-version series lines (stats.js renderSeries versions/legend/tip/aria) ---

test("renderStats series draws one smooth line per version with legend and aria", function () {
  var ctx = freshStats();
  var data = fullData();
  data.series = [
    { day: "2026-09-21", fetches: 9, exposures: 0, versions: { 0: 6, 24: 3 } },
    { day: "2026-09-22", fetches: 5, exposures: 1, versions: { 0: 1, 24: 4 } },
    { day: "2026-09-23", fetches: 7, exposures: 2, versions: { 0: 7, 24: 0 } },
  ];
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  // 2 areas + 2 base lines + 2 version lines = 6 paths.
  assert.equal(html.split("<path").length - 1, 6, "must be 2 areas + 2 lines + 2 version lines: " + html);
  assert.equal(html.split("stats-line-ver").length - 1, 4, "two version lines (class x2 each): " + html);
  assert.ok(html.indexOf('data-version="0"') !== -1, "missing v0 line: " + html);
  assert.ok(html.indexOf('data-version="24"') !== -1, "missing v24 line: " + html);
  // Distinct solid 1.5px strokes from the ver palette.
  var strokes = [];
  var re = /stats-line-ver[^>]*stroke="([^"]+)"/g;
  var m;
  while ((m = re.exec(html)) !== null) strokes.push(m[1]);
  assert.equal(strokes.length, 2, "two version strokes: " + html);
  assert.ok(strokes[0] !== strokes[1], "version strokes must differ: " + strokes);
  assert.ok(html.indexOf('stroke-width="1.5"') !== -1, "version lines must be 1.5px: " + html);
  // Smoothed (3 points -> bezier) with native titles.
  var v0d = /data-version="0" d="([^"]+)/.exec(html);
  assert.ok(v0d && v0d[1].indexOf("C") !== -1, "v0 line must be smoothed: " + html);
  assert.ok(html.indexOf("<title>v0 ") !== -1, "missing v0 title: " + html);
  assert.ok(html.indexOf("<title>v24 ") !== -1, "missing v24 title: " + html);
  // Legend swatches + aria range.
  assert.equal(html.split("stats-legend-ver").length - 1, 2, "two legend entries: " + html);
  assert.ok(html.indexOf("v0: <strong>14</strong>") !== -1, "missing v0 legend count: " + html);
  assert.ok(html.indexOf("v24: <strong>7</strong>") !== -1, "missing v24 legend count: " + html);
  assert.ok(html.indexOf("versions v0, v24") !== -1, "aria must list versions: " + html);
  // Y-scale stays on the stacked total (9/6/9 -> peak 9).
  assert.ok(html.indexOf('data-peak="9"') !== -1, "peak must stay stacked total 9: " + html);
  // Areas + bars below intact.
  assert.ok(html.indexOf("stats-area-fetch") !== -1, "fetch area lost: " + html);
  assert.ok(html.indexOf("stats-area-exposure") !== -1, "exposure area lost: " + html);
  assert.ok(html.indexOf("stats-ver-row") !== -1, "per-version bars lost: " + html);
});

test("seriesTipText shows per-version counts for the hovered day", function () {
  var ctx = freshStats();
  var withVer = ctx.CW.seriesTipText({ day: "2026-09-21", fetches: 9, exposures: 0, versions: { 0: 6, 24: 3 } });
  assert.ok(
    withVer === "2026-09-21 — 9 fetches (v0: 6, v24: 3) · 0 exposures · total 9",
    "tooltip wrong: " + withVer
  );
  var plain = ctx.CW.seriesTipText({ day: "d1", fetches: 10, exposures: 5, versions: null });
  assert.equal(plain, "d1 — 10 fetches · 5 exposures · total 15", "plain tooltip wrong: " + plain);
  var empty = ctx.CW.seriesTipText({ day: "d2", fetches: 4, exposures: 1, versions: {} });
  assert.equal(empty, "d2 — 4 fetches · 1 exposures · total 5", "empty-versions tooltip wrong: " + empty);
});

test("seriesTipHTML renders multi-line rows with version dots and escapes probes", function () {
  var ctx = freshStats();
  var tip = ctx.CW.seriesTipHTML({ day: "2026-09-28", fetches: 132, exposures: 0, versions: { 0: 115, 1: 17 } });
  assert.ok(tip.indexOf('class="stats-tip-day">2026-09-28<') !== -1, "missing day header: " + tip);
  assert.ok(tip.indexOf("Fetches") !== -1 && tip.indexOf(">132<") !== -1, "missing fetches row: " + tip);
  assert.ok(tip.indexOf("stats-tip-ver") !== -1, "missing version rows: " + tip);
  assert.ok(tip.indexOf(">v0<") !== -1 && tip.indexOf(">115<") !== -1, "missing v0 row: " + tip);
  assert.ok(tip.indexOf(">v1<") !== -1 && tip.indexOf(">17<") !== -1, "missing v1 row: " + tip);
  assert.ok(tip.indexOf("Exposures") !== -1, "missing exposures row: " + tip);
  assert.ok(tip.indexOf("stats-tip-total") !== -1, "missing total row: " + tip);
  assert.ok(tip.split("stats-tip-row").length - 1 >= 5, "must stack day+fetch+2ver+expo+total rows: " + tip);
  var plain = ctx.CW.seriesTipHTML({ day: "d1", fetches: 10, exposures: 5, versions: null });
  assert.ok(plain.indexOf("stats-tip-ver") === -1, "no version rows expected: " + plain);
  assert.ok(plain.indexOf(">15<") !== -1, "missing total 15: " + plain);
  var xss = ctx.CW.seriesTipHTML({ day: "<b>", fetches: 5, exposures: 0, versions: { "<img>": 5 } });
  assert.ok(xss.indexOf("&lt;b&gt;") !== -1, "day not escaped: " + xss);
  assert.ok(xss.indexOf("&lt;img&gt;") !== -1, "version key not escaped: " + xss);
  assert.ok(xss.indexOf("<img>") === -1, "raw version key leaked: " + xss);
  assert.ok(xss.indexOf("<b>") === -1, "raw day leaked: " + xss);
});

test("renderStats series tolerates missing/empty versions with areas intact", function () {
  var ctx = freshStats();
  var data = fullData();
  data.series = [
    { day: "2026-09-01", fetches: 5, exposures: 1 },
    { day: "2026-09-02", fetches: 7, exposures: 0, versions: {} },
  ];
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("stats-line-ver") === -1, "no version lines expected: " + html);
  assert.ok(html.indexOf("stats-legend-ver") === -1, "no version legend expected: " + html);
  assert.equal(html.split("<path").length - 1, 4, "areas + base lines intact: " + html);
  assert.ok(html.indexOf("versions v") === -1, "aria must not list versions: " + html);
});

test("renderStats escapes version keys and version-day labels", function () {
  var ctx = freshStats();
  var data = fullData();
  data.series = [
    { day: "<b>day", fetches: 5, exposures: 0, versions: { "<img>": 5 } },
    { day: "d2", fetches: 3, exposures: 0, versions: { "<img>": 3 } },
  ];
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("&lt;b&gt;da") !== -1, "day not escaped: " + html);
  assert.ok(html.indexOf("&lt;img&gt;") !== -1, "version key not escaped: " + html);
  assert.ok(html.indexOf("<img>") === -1, "raw version key leaked: " + html);
  assert.ok(html.indexOf("<b>day") === -1, "raw day leaked: " + html);
});

test("renderStats escapes series day and variant XSS probes", function () {
  var ctx = freshStats();
  var data = fullData();
  data.perVariant = { "<img>": 60, ok: 40 };
  data.series = [{ day: "<b>", fetches: 5, exposures: 5 }];
  ctx.CW.renderStats(data);
  var html = statsHTML(ctx);
  assert.ok(html.indexOf("&lt;img&gt;") !== -1, "variant not escaped: " + html);
  assert.ok(html.indexOf("&lt;b&gt;") !== -1, "series day not escaped: " + html);
  assert.ok(
    html.indexOf(">&lt;b&gt;<") !== -1,
    "day x label not escaped: " + html
  );
  assert.ok(html.indexOf("<img>") === -1, "raw variant tag leaked: " + html);
  assert.ok(html.indexOf("<b>") === -1, "raw day tag leaked: " + html);
});
