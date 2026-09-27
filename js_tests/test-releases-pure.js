/* ConfigWire js_tests — releases pure helpers (no fetch performed).
 * Loads core+drafts+releases via harness. Most release helpers are
 * IIFE-private (releases.js), so assertions go through their exported
 * observable surface:
 * - releaseFieldLabel (releases.js:62-65) via openReleaseDialog dt cells
 *   (RELEASE_FIELD_ORDER/LABELS at releases.js:51-60).
 * - latestVersion (releases.js:140-144, exported).
 * - isDraftTempId (releases.js:162-163, prefix-only) via the "(new)" suffix
 *   in renderUnpublishedList (releases.js:232).
 * - draftBucketEntries/draftSummary (releases.js:165-186) via
 *   unpublishedSummary (exported) + renderUnpublishedList rows.
 * - plural/summaryText (releases.js:192-204) via setPublishState hint text.
 * - isKnownKind (releases.js:318-323) via isUnpublished (exported).
 * - normDraftOp (releases.js:378-390), draftLiveId/draftTempId
 *   (releases.js:392-402), resolveFlagRef/remapGroupRef (releases.js:404-423),
 *   buildApplySteps phase order + create/update/delete rank + stable idx
 *   (releases.js:434-462) via applyDrafts (exported) with a recording
 *   CW.apiMut double. The double never touches fetch — the harness
 *   deny-by-default stub asserts zero recorded calls at the end.
 * Drafts are seeded via real CW.drafts.draftStage (drafts.js:121-172).
 */
"use strict";

var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

var h = harness.load(["core.js", "drafts.js", "releases.js"]);

function reset() {
  h.CW.state.releases = [];
  h.CW.state.flags = [];
  h.CW.state.groups = {};
  h.CW.state.unpublishedChanges = false;
  h.CW.drafts.draftClearAll();
}

// Objects built inside the vm context carry the vm realm's Object.prototype,
// so strict deep-equality against literals fails on prototypes. Round-trip
// through JSON to compare by structure.
function plain(o) { return JSON.parse(JSON.stringify(o)); }

// Recording CW.apiMut double: resolves canned payloads, never fetches.
// POST /groups -> live-g1, POST /flags -> live-f1, live-f2 in call order.
function stubApiMut(calls) {
  var flagSeq = 0;
  h.CW.apiMut = function (method, path, body) {
    calls.push({ method: method, path: path, body: body });
    if (method === "POST" && path === "/api/collections/groups/records") {
      return Promise.resolve({ status: 201, data: { id: "live-g1" } });
    }
    if (method === "POST" && path === "/api/collections/flags/records") {
      flagSeq++;
      return Promise.resolve({ status: 201, data: { id: "live-f" + flagSeq } });
    }
    return Promise.resolve({ status: 200, data: {} });
  };
}

test("releaseFieldLabel: ordered dt labels with unknown-key fallback, snapshot pre/code", function () {
  reset();
  h.CW.state.releases = [{
    id: "rel1", author: "a@x", env: "e1", etag: "abc", version: 3,
    note: "hello", snapshot: { flags: { a: 1 } },
    custom: "z", empty: null,
    expand: {}, collectionId: "c", collectionName: "n",
  }];
  h.CW.openReleaseDialog("rel1");
  assert.equal(h.elementsById["release-dialog-title"].textContent, "Release v3");
  var kids = h.elementsById["release-detail"].children;
  var labels = [];
  var values = [];
  for (var i = 0; i < kids.length; i += 2) {
    labels.push(kids[i].textContent);
    values.push(kids[i + 1].textContent);
  }
  // RELEASE_FIELD_ORDER first, then rest keys; expand/collection* skipped
  // (releases.js:113-126); unknown key falls back to String(k) (releases.js:64).
  assert.deepEqual(labels, ["ID", "Author", "Env", "Etag", "Version", "Note", "Snapshot", "custom", "empty"]);
  assert.deepEqual(values.slice(0, 6), ["rel1", "a@x", "e1", "abc", "3", "hello"]);
  assert.equal(values[7], "z");
  assert.equal(values[8], ""); // null renders empty (releases.js:84)
  var snapDd = kids[13];
  assert.equal(snapDd.className, "release-snapshot");
  var code = snapDd.children[0].children[0];
  assert.equal(code.textContent, JSON.stringify({ flags: { a: 1 } }, null, 2));
});

test("latestVersion: empty-safe, returns max", function () {
  reset();
  assert.equal(h.CW.latestVersion(), 0); // reduce over [] seeds 0 (releases.js:140-144)
  h.CW.state.releases = [{ version: 2 }, { version: 5 }, { version: 3 }];
  assert.equal(h.CW.latestVersion(), 5);
});

test("isDraftTempId: pure draft- prefix, no membership consult", function () {
  reset();
  var gt = h.CW.drafts.draftStage("group", { op: "create", body: { name: "G" }, label: "G" });
  h.CW.drafts.draftStage("flag", { op: "update", body: { description: "d" }, baseId: "live1", label: "live1" });
  // Prefix-only even for updates keyed by a draft- id (releases.js:157-163).
  h.CW.drafts.draftStage("flag", { op: "update", body: { description: "d" }, baseId: "draft-legacy", label: "leg" });
  h.CW.drafts.draftStage("flag", { op: "update", body: { description: "d" }, baseId: "nodraft-1", label: "nd" });
  h.CW.renderUnpublishedList();
  var html = h.elementsById["unpublished-list"].innerHTML;
  assert.ok(html.indexOf("group: G (new)") >= 0, "temp-keyed create is (new)");
  assert.ok(html.indexOf("flag: live1</li>") >= 0, "live update has no suffix");
  assert.ok(html.indexOf("flag: leg (new)") >= 0, "draft- prefix alone marks (new)");
  assert.ok(html.indexOf("flag: nd</li>") >= 0, "nodraft-1 is not a temp id");
  assert.ok(gt.indexOf("draft-group-") === 0);
});

test("draftBucketEntries/draftSummary via unpublishedSummary counts", function () {
  reset();
  h.CW.drafts.draftStage("flag", { op: "create", body: { key: "f" }, label: "f" });
  h.CW.drafts.draftStage("rule", { op: "create", body: { flag: "x" }, label: "r1" });
  h.CW.drafts.draftStage("rule", { op: "create", body: { flag: "x" }, label: "r2" });
  h.CW.drafts.draftStage("group", { op: "update", body: { name: "G" }, baseId: "g1", label: "G" });
  assert.deepEqual(plain(h.CW.unpublishedSummary()), { flag: 1, rule: 2, experiment: 0, group: 1, total: 4 });
});

test("plural/summaryText via setPublishState hint text", function () {
  reset();
  h.CW.drafts.draftStage("flag", { op: "create", body: { key: "f" }, label: "f" });
  h.CW.setPublishState(true);
  assert.equal(
    h.elementsById["publish-hint"].textContent,
    "Unpublished changes — publish to release (1 flag)"
  );
  h.CW.drafts.draftStage("flag", { op: "create", body: { key: "f2" }, label: "f2" });
  h.CW.drafts.draftStage("rule", { op: "create", body: { flag: "x" }, label: "r" });
  h.CW.drafts.draftStage("group", { op: "update", body: { name: "G" }, baseId: "g1", label: "G" });
  h.CW.setPublishState(true);
  assert.equal(
    h.elementsById["publish-hint"].textContent,
    "Unpublished changes — publish to release (2 flags • 1 rule • 1 group)"
  );
  h.CW.setPublishState(false);
  assert.equal(h.elementsById["publish-hint"].textContent, "No unpublished changes");
});

test("isKnownKind via isUnpublished: unknown kind and empty id are false", function () {
  reset();
  h.CW.drafts.draftStage("flag", { op: "update", body: { description: "d" }, baseId: "kf", label: "kf" });
  assert.equal(h.CW.isUnpublished("flag", "kf"), true);
  assert.equal(h.CW.isUnpublished("bogus", "kf"), false); // isKnownKind gate (releases.js:311)
  assert.equal(h.CW.isUnpublished("flag", ""), false);
  assert.equal(h.CW.isUnpublished("flag", "missing"), false);
});

test("buildApplySteps: group→flag→rule→experiment, create/update/delete rank, stable idx, tempMap remap", async function () {
  reset();
  // Stage two flag creates first to capture temp keys, then scramble the
  // rest — step order must still follow phase, rank, insertion index
  // (releases.js:434-462).
  var ft = h.CW.drafts.draftStage("flag", {
    op: "create", body: { key: "f", type: "bool", defaultValue: false, project: "p1", group: null }, label: "f",
  });
  var ft2 = h.CW.drafts.draftStage("flag", {
    op: "create", body: { key: "f2", type: "string", defaultValue: "x", project: "p1" }, label: "f2",
  });
  var gt = h.CW.drafts.draftStage("group", { op: "create", body: { name: "G", project: "p1" }, label: "G" });
  h.CW.drafts.draftStage("group", { op: "update", body: { name: "G2" }, baseId: "g-live", label: "G2" });
  h.CW.drafts.draftStage("flag", { op: "update", body: { description: "d" }, baseId: "f-live", label: "f-live" });
  h.CW.drafts.draftStage("flag", { op: "delete", body: {}, baseId: "f-dead", label: "f-dead deleted" });
  // Point the flag create at the group temp id AFTER staging it, so the
  // group-create step must run first and remap the ref to the live id
  // (remapGroupRef, releases.js:418-423; runApplyStep flag path :510-521).
  h.CW.state.drafts.flag[ft].body.group = gt;
  h.CW.drafts.draftStage("rule", {
    op: "create", body: { flag: ft, priority: 0, condition: {}, value: true }, label: "rule for f",
  });
  h.CW.drafts.draftStage("experiment", { op: "create", body: { flag: ft, name: "E" }, label: "E" });

  var calls = [];
  stubApiMut(calls);
  var out = await h.CW.applyDrafts();
  assert.deepEqual(plain(out), { applied: 8, total: 8 });  assert.deepEqual(calls.map(function (c) { return c.method + " " + c.path; }), [
    "POST /api/collections/groups/records",
    "PATCH /api/collections/groups/records/g-live",
    "POST /api/collections/flags/records",
    "POST /api/collections/flags/records",
    "PATCH /api/collections/flags/records/f-live",
    "DELETE /api/collections/flags/records/f-dead",
    "POST /api/collections/rules/records",
    "POST /api/collections/experiments/records",
  ]);
  assert.equal(calls[0].body.name, "G");
  assert.deepEqual(plain(calls[1].body), { name: "G2" }); // group update rank after create
  assert.equal(calls[2].body.key, "f");
  assert.equal(calls[2].body.group, "live-g1"); // group temp id remapped to live id
  assert.equal(calls[3].body.key, "f2"); // stable idx: creates keep stage order
  assert.equal(calls[6].body.flag, "live-f1"); // resolveFlagRef tempMap remap
  assert.equal(calls[7].body.flag, "live-f1");
  assert.equal(h.CW.drafts.hasDrafts(), false); // every step consumed
  assert.ok(ft2.indexOf("draft-flag-") === 0, "creates keyed by temp id");
});

test("resolveFlagRef: dangling draft flag rejects with DANGLING_TEMP, draft kept", async function () {
  reset();
  h.CW.drafts.draftStage("rule", {
    op: "create", body: { flag: "draft-flag-9", priority: 0, condition: {}, value: true },
    label: "rule for ghost",
  });
  var calls = [];
  stubApiMut(calls);
  var err = null;
  try {
    await h.CW.applyDrafts();
  } catch (e) { err = e; }
  // resolveFlagRef returns null for unmapped draft- refs (releases.js:408),
  // which runApplyStep rejects as DANGLING_TEMP (releases.js:551-554).
  assert.ok(err instanceof Error);
  assert.ok(err.message.indexOf("rule staged against a discarded draft flag (draft-flag-9)") >= 0);
  assert.ok(err.message.indexOf("step 1/1") >= 0);
  assert.ok(err.message.indexOf("1 draft kept") >= 0);
  assert.equal(err.code, "DANGLING_TEMP");
  assert.equal(err.kept, 1);
  assert.equal(calls.length, 0);
  assert.equal(h.CW.drafts.hasDrafts(), true);
  h.CW.drafts.draftClearAll();
});

test("remapGroupRef: dangling draft-group-9 passes through (only tempMap hits remap)", async function () {
  reset();
  // remapGroupRef remaps tempMap hits and otherwise returns the ref as-is
  // (releases.js:418-423) — unlike resolveFlagRef it never nulls dangling
  // refs, so the staged group value reaches the POST body verbatim.
  h.CW.drafts.draftStage("flag", {
    op: "create",
    body: { key: "lonely", type: "bool", defaultValue: false, project: "p1", group: "draft-group-9" },
    label: "lonely",
  });
  var calls = [];
  stubApiMut(calls);
  var out = await h.CW.applyDrafts();
  assert.deepEqual(plain(out), { applied: 1, total: 1 });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].body.group, "draft-group-9");
  h.CW.drafts.draftClearAll();
});

test("no fetch performed by any pure-releases path", function () {
  assert.equal(h.fetchCalls.length, 0);
});
