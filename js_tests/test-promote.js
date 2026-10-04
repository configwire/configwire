/* ConfigWire js_tests — promote client helper (todo 8).
 * Loads core.js + releases.js via harness (never boot.js/update.js).
 * Asserts CW.promote (releases.js:749-763) verbatim: POST URL shape,
 * exact JSON body (+srcProject only when truthy), {status,data} return
 * on 200/409/404/400 without throw, throw only on network failure
 * (mirroring publish/rollback), and setApplying (releases.js:356-371)
 * disabling button[data-rollback-version],button[data-promote-version].
 * ALL network flows through the harness deny-by-default fetchImpl with
 * canned payloads — no real sockets/ports/timers.
 * node:test + node:assert/strict only.
 * Run from configwire/: node --test js_tests/test-promote.js
 */
"use strict";

var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

function jsonRes(status, data) {
  return {
    status: status,
    ok: status >= 200 && status < 300,
    json: function () { return Promise.resolve(data); },
  };
}

// Fresh vm context per case: canned status/data, records every fetch call.
function fresh(status, data, fail) {
  var seen = [];
  var ctx = harness.load(["core.js", "releases.js"], {
    fetchImpl: function (url, opts) {
      seen.push({ url: url, opts: opts });
      if (fail) throw new Error("boom (forced failure): " + url);
      return Promise.resolve(jsonRes(status, data));
    },
  });
  ctx.seen = seen;
  ctx.CW.state.token = "tok";
  return ctx;
}

// Objects built inside the vm context carry the vm realm's Object.prototype,
// so strict deep-equality against literals fails on prototypes. Round-trip
// through JSON to compare by structure.
function plain(o) { return JSON.parse(JSON.stringify(o)); }

test("promote: URL + exact body with ?project= when projectId set", async function () {
  var ctx = fresh(200, { version: 2 });
  ctx.CW.state.envSlug = "prod"; ctx.CW.state.projectId = "p1";
  var out = await ctx.CW.promote("dev", 3, 1, "note"); // releases.js:749-763
  assert.equal(ctx.seen[0].url, "/api/v1/admin/env/prod/promote?project=p1");
  assert.equal(ctx.seen[0].opts.method, "POST");
  assert.deepEqual(plain(JSON.parse(ctx.seen[0].opts.body)),
    { srcEnv: "dev", srcVersion: 3, note: "note", destBaseVersion: 1 });
  assert.deepEqual(plain(out), { status: 200, data: { version: 2 } });
});

test("promote: bare path when projectId unset", async function () {
  var ctx = fresh(200, { version: 2 });
  ctx.CW.state.envSlug = "prod"; ctx.CW.state.projectId = null;
  await ctx.CW.promote("dev", 3, 1, "note"); // releases.js:751 guard
  assert.equal(ctx.seen[0].url, "/api/v1/admin/env/prod/promote");
});

test("promote: srcProject included only when truthy", async function () {
  var ctx = fresh(200, { version: 2 });
  ctx.CW.state.envSlug = "prod"; ctx.CW.state.projectId = "p1";
  await ctx.CW.promote("dev", 3, 1, "note", "sp1"); // releases.js:753
  assert.equal(JSON.parse(ctx.seen[0].opts.body).srcProject, "sp1");
  var ctx2 = fresh(200, { version: 2 });
  ctx2.CW.state.envSlug = "prod"; ctx2.CW.state.projectId = "p1";
  await ctx2.CW.promote("dev", 3, 1, "note", "");
  assert.ok(!("srcProject" in JSON.parse(ctx2.seen[0].opts.body)), "empty srcProject must leave the key absent, not null");
  var ctx3 = fresh(200, { version: 2 });
  ctx3.CW.state.envSlug = "prod"; ctx3.CW.state.projectId = "p1";
  await ctx3.CW.promote("dev", 3, 1, "note");
  assert.ok(!("srcProject" in JSON.parse(ctx3.seen[0].opts.body)), "omitted srcProject must leave the key absent");
});

test("promote: 200/409/404/400 return {status,data} without throwing", async function () {
  var cases = [
    [200, { version: 2 }],
    [409, { message: "stale", currentVersion: 4 }],
    [404, { message: "missing" }],
    [400, { message: "bad" }],
  ];
  for (var i = 0; i < cases.length; i++) {
    var ctx = fresh(cases[i][0], cases[i][1]);
    ctx.CW.state.envSlug = "prod"; ctx.CW.state.projectId = "p1";
    var out = await ctx.CW.promote("dev", 3, 1, "note"); // releases.js:758-762
    assert.equal(out.status, cases[i][0]);
    assert.deepEqual(plain(out.data), cases[i][1]);
  }
});

test("promote: network failure rejects (throws on await)", async function () {
  var ctx = fresh(200, {}, true);
  ctx.CW.state.envSlug = "prod"; ctx.CW.state.projectId = "p1";
  var err = null;
  try {
    await ctx.CW.promote("dev", 3, 1, "note");
  } catch (e) { err = e; }
  assert.ok(err instanceof Error, "fetch throw must surface, mirroring publish/rollback");
});

test("promote: destSlug override targets the dest env URL, keeps ?project=", async function () {
  var ctx = fresh(200, { version: 4 });
  ctx.CW.state.envSlug = "dev"; ctx.CW.state.projectId = "p1";
  var out = await ctx.CW.promote("dev", 3, 1, "note", "", "staging"); // releases.js:749 destSlug
  assert.equal(ctx.seen[0].url, "/api/v1/admin/env/staging/promote?project=p1");
  assert.deepEqual(plain(JSON.parse(ctx.seen[0].opts.body)),
    { srcEnv: "dev", srcVersion: 3, note: "note", destBaseVersion: 1 });
  assert.ok(!("srcProject" in JSON.parse(ctx.seen[0].opts.body)), "empty srcProject must leave the key absent, not null");
  assert.deepEqual(plain(out), { status: 200, data: { version: 4 } });
});

test("destLatestVersion: dest max from allReleases by env id, 0 when empty", function () {
  var ctx = fresh(200, {});
  ctx.CW.state.envSlug = "dev"; ctx.CW.state.projectId = "p1";
  ctx.CW.state.envs = [
    { id: "e1", slug: "dev", project: "p1" },
    { id: "e2", slug: "staging", project: "p1" },
    { id: "e3", slug: "other", project: "pX" },
    { id: "e4", slug: "empty", project: "p1" },
  ];
  ctx.CW.state.allReleases = [
    { env: "e1", version: 3, etag: "aaa" },
    { env: "e2", version: 1, etag: "bbb" },
    { env: "e2", version: 2, etag: "ccc" },
    { env: "e3", version: 9, etag: "ddd" },
  ];
  assert.equal(ctx.CW.destLatestVersion("staging"), 2);
  assert.equal(ctx.CW.destLatestVersion("empty"), 0);
  assert.deepEqual(plain(ctx.CW.destEnvs()), ["staging", "empty"]);
});

test("promote: single environment disables menu button with hover note; multi-env enables", async function () {
  var NOTE = "Only one environment exists — create another environment to promote to";
  // Single-env state: only the current environment exists → no destinations.
  var single = fresh(200, { version: 2 });
  single.CW.state.envId = "e1"; single.CW.state.envSlug = "dev"; single.CW.state.projectId = "p1";
  single.CW.state.envs = [{ id: "e1", slug: "dev", project: "p1" }];
  single.CW.state.releases = [{ id: "r1", version: 2, etag: "aaa", note: "" }];
  single.CW.state.allReleases = [{ env: "e1", version: 2, etag: "aaa", note: "" }];
  assert.deepEqual(plain(single.CW.destEnvs()), []);
  single.CW.renderReleases();
  var singleHtml = single.elementsById["release-list"].innerHTML;
  assert.ok(singleHtml.indexOf("Promote to…") !== -1, "menu label keeps the ellipsis form");
  assert.ok(singleHtml.indexOf("disabled") !== -1, "menu button renders disabled with no promotable destination");
  assert.ok(singleHtml.indexOf('title="' + NOTE + '"') !== -1, "hover title explains why");
  assert.ok(singleHtml.indexOf("data-promote-nodest") !== -1, "guard marker survives setApplying passes");
  // Guard-disabled buttons stay disabled across setApplying(false).
  var guardBtn = { disabled: true, getAttribute: function (k) { return k === "data-promote-nodest" ? "1" : null; } };
  single.document.querySelectorAll = function () { return [guardBtn]; };
  single.CW.setApplying(false);
  assert.equal(guardBtn.disabled, true, "setApplying(false) must not re-enable a no-dest button");
  // Defensive dialog: opened with an empty destination list shows the note
  // and keeps submit disabled.
  single.CW.openPromoteDialog(2);
  assert.equal(single.elementsById["promote-result"].textContent, NOTE);
  assert.equal(single.elementsById["promote-submit"].disabled, true);
  assert.equal(single.elementsById["promote-src-summary"].textContent, "Source environment dev · v2 · etag aaa");
  // Submit guard: no fetch leaves the harness with an empty destination.
  var noSend = fresh(200, { version: 2 });
  noSend.CW.state.envId = "e1"; noSend.CW.state.envSlug = "dev"; noSend.CW.state.projectId = "p1";
  noSend.CW.state.envs = [{ id: "e1", slug: "dev", project: "p1" }];
  var out = await noSend.CW.submitPromote();
  assert.equal(out, null);
  assert.equal(noSend.seen.length, 0, "empty dest selection must never submit");
  assert.equal(noSend.elementsById["promote-result"].textContent, NOTE);
  // Multi-env state: a destination exists → enabled button, no title.
  var multi = fresh(200, { version: 2 });
  multi.CW.state.envId = "e1"; multi.CW.state.envSlug = "dev"; multi.CW.state.projectId = "p1";
  multi.CW.state.envs = [
    { id: "e1", slug: "dev", project: "p1" },
    { id: "e2", slug: "staging", project: "p1" },
  ];
  multi.CW.state.releases = [{ id: "r1", version: 2, etag: "aaa", note: "" }];
  multi.CW.renderReleases();
  var multiHtml = multi.elementsById["release-list"].innerHTML;
  assert.ok(multiHtml.indexOf("disabled") === -1, "menu button enabled when a destination exists");
  assert.ok(multiHtml.indexOf("data-promote-nodest") === -1, "no guard marker when destinations exist");
  assert.ok(multiHtml.indexOf(NOTE) === -1, "no hover note when destinations exist");
});

test("setApplying: disables both rollback+promote buttons, then re-enables", function () {
  var ctx = fresh(200, {});
  var rb = { disabled: false };
  var pb = { disabled: false };
  var btns = [rb, pb];
  btns.length = 2;
  ctx.document.querySelectorAll = function (sel) { // releases.js:365
    assert.equal(sel, "button[data-rollback-version],button[data-promote-version]");
    return btns;
  };
  ctx.CW.setApplying(true);
  assert.equal(ctx.CW.state.applying, true);
  assert.equal(rb.disabled, true);
  assert.equal(pb.disabled, true);
  ctx.CW.setApplying(false);
  assert.equal(ctx.CW.state.applying, false);
  assert.equal(rb.disabled, false);
  assert.equal(pb.disabled, false);
});
