"use strict";
// test-transfer-pure.js — transfer snapshot helpers (no fetch performed).
// Loads core+drafts+flags+rules+experiments+transfer via harness (transfer.js
// sits between releases.js and stats.js in FULL_ORDER, index.html:577).
// Assertions go through the transfer.js exported surface:
// - buildTransferSnapshot(opts?) maps the MERGED flag/experiment sets
//   (drafts.js:280-374 mergedFlags/mergedRules/mergedExperiments) into
//   {flags, experiments}: flags sorted by key with group ids resolved to
//   names ({key, type, default, group, rules:[{priority, condition, value}]}),
//   experiments linked by flag key ({name, flag, seed, variants, status}).
// - parseTransferSnapshot(text) -> {ok, snap, value, error}: rejects malformed
//   JSON and non-{flags:[...]} shapes, accepts a minimal {flags:[]} envelope
//   (experiments defaults to []).
// - validateTransferSnapshot(snap) -> {ok, error}: rejects bad flag key,
//   unknown flag type, rule condition op outside the transfer.js:17-24
//   CONDITION_OPS allowlist (mirrors rules.js:125-132), and experiment
//   variant weights not totalling 10000 (experiments.js:324
//   validateVariants); accepts a good snapshot.
// - importTransferSnapshot(snap) -> counts {groupsCreated, flagsCreated,
//   flagsUpdated, rulesCreated, rulesDeleted, experimentsCreated,
//   experimentsUpdated, skipped}: stages local-only drafts via real
//   CW.drafts.draftStage (drafts.js:121-172) — unknown group names created,
//   flags matched by key (new create + existing update), rules
//   replaced per flag (old deletes + snapshot creates), experiments matched
//   by flag key + seed (create or update).
// - diffTransferSnapshots(oldS, neuS) -> {flags, experiments, counts}: row
//   arrays of {key|id, status, old, new} with status added/removed/modified/
//   unchanged, plus per-kind and total counts.
// Drafts are seeded via real CW.drafts.draftStage; live collections are set
// directly on CW.state (no loaders run). CW.toast/CW.esc come from core.js
// as-is (no stubs needed — pure paths never toast). node:test +
// node:assert/strict only. Run from configwire/:
// node --test js_tests/test-transfer-pure.js
//
// NOTE: pb_public/js/transfer.js is owned by a sibling agent and does not
// exist on disk yet — harness.load throws ENOENT until it lands, so this
// suite fails at load by design. The assertions below pin the shared
// contract so the sibling can land against them.

var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

var h = harness.load(["core.js", "drafts.js", "flags.js", "rules.js", "experiments.js", "releases.js", "transfer.js"]);

function reset() {
  h.CW.state.projectId = "p1";
  h.CW.state.flags = [];
  h.CW.state.groups = {};
  h.CW.state.rules = [];
  h.CW.state.rulesLoaded = false;
  h.CW.state.experiments = [];
  h.CW.state.experimentsLoaded = false;
  h.CW.state.releases = [];
  h.CW.state.releasesExpanded = false;
  h.CW.state.unpublishedChanges = false;
  h.CW.drafts.draftClearAll();
  h.fetchCalls.length = 0;
  h.CW.$("release-list").innerHTML = "";
  h.CW.$("transfer-export-text").value = "";
}

// Objects built inside the vm context carry the vm realm's Object.prototype,
// so strict deep-equality against literals fails on prototypes. Round-trip
// through JSON to compare by structure.
function plain(o) { return JSON.parse(JSON.stringify(o)); }

// Seed the live (server-side) collections. Keys/types mirror the shapes
// flags.js / rules.js / experiments.js load into CW.state.
function seedLive() {
  h.CW.state.projectId = "p1";
  h.CW.state.flags = [
    { id: "f1", project: "p1", key: "keep", type: "bool", defaultValue: false, group: "g1", description: "" },
    { id: "f2", project: "p1", key: "zeta", type: "string", defaultValue: "z", group: null, description: "" },
  ];
  h.CW.state.groups = { g1: "G1" };
  h.CW.state.rules = [
    { id: "r1", flag: "f1", priority: 0, condition: { field: "platform", op: "==", value: "ios" }, value: true },
  ];
  h.CW.state.rulesLoaded = true;
  h.CW.state.experiments = [
    {
      id: "x1", flag: "f1", name: "Exp", seed: "s", status: "draft",
      variants: [{ name: "control", weightBps: 5000 }, { name: "treatment", weightBps: 5000 }],
    },
  ];
  h.CW.state.experimentsLoaded = true;
}

function flagByKey(snap, key) {
  for (var i = 0; i < snap.flags.length; i++) {
    if (snap.flags[i].key === key) return snap.flags[i];
  }
  return null;
}

// Diff rows carry {key|id, status, old, new} — select by status first.
function rowsWith(rows, status) {
  return (rows || []).filter(function (r) { return r && r.status === status; });
}

function draftOps(kind) {
  var bucket = h.CW.state.drafts[kind] || {};
  var out = {};
  Object.keys(bucket).forEach(function (k) { out[k] = bucket[k].op; });
  return out;
}

// --- buildTransferSnapshot: merged sets, group names, sorted keys ---

test("buildTransferSnapshot maps merged flags/rules/experiments with group names + sorted keys", function () {
  reset();
  seedLive();
  // Draft overlay proves the merged (not live-only) view: an unpublished
  // create joins the snapshot alongside the live flags.
  h.CW.drafts.draftStage("flag", {
    op: "create",
    body: { key: "aaa", type: "bool", defaultValue: true, project: "p1", group: null },
    label: "aaa",
  });
  var snap = plain(h.CW.buildTransferSnapshot());
  assert.ok(Array.isArray(snap.flags), "snapshot carries flags array");
  assert.deepEqual(
    snap.flags.map(function (f) { return f.key; }),
    ["aaa", "keep", "zeta"],
    "flags sorted by key, draft create merged in"
  );
  assert.equal(flagByKey(snap, "keep").group, "G1", "group id resolved to name");
  assert.equal(flagByKey(snap, "keep").default, false, "flag body preserved");
  assert.equal(flagByKey(snap, "keep").type, "bool", "flag type preserved");
  var keepRules = flagByKey(snap, "keep").rules;
  assert.ok(Array.isArray(keepRules), "rules nest per flag");
  assert.equal(keepRules.length, 1);
  assert.deepEqual(
    keepRules[0].condition,
    { field: "platform", op: "==", value: "ios" },
    "rule condition preserved"
  );
  assert.deepEqual(flagByKey(snap, "zeta").rules, [], "flag without rules carries []");
  assert.deepEqual(flagByKey(snap, "aaa").rules, [], "draft flag carries []");
  assert.ok(Array.isArray(snap.experiments), "snapshot carries experiments array");
  assert.equal(snap.experiments.length, 1);
  assert.equal(snap.experiments[0].name, "Exp");
});

// --- parseTransferSnapshot: malformed rejected, minimal accepted ---

test("parseTransferSnapshot rejects malformed input and accepts a minimal envelope", function () {
  reset();
  var bad = plain(h.CW.parseTransferSnapshot("{oops"));
  assert.equal(bad.ok, false, "truncated JSON rejected");
  assert.equal(typeof bad.error, "string");
  assert.ok(bad.error.length > 0, "rejection carries an error message");
  var empty = plain(h.CW.parseTransferSnapshot(""));
  assert.equal(empty.ok, false, "empty input rejected");
  var good = plain(h.CW.parseTransferSnapshot('{"flags":[]}'));
  assert.equal(good.ok, true, "minimal envelope accepted");
  assert.deepEqual(good.snap.flags, []);
  assert.deepEqual(good.snap.experiments, [], "experiments defaults to []");
  var full = plain(h.CW.parseTransferSnapshot('{"flags":[],"rules":[],"experiments":[]}'));
  assert.equal(full.ok, true, "extra keys tolerated");
});

// --- validateTransferSnapshot: bad key/type/condition-op/variant-sum ---

test("validateTransferSnapshot accepts a built snapshot", function () {
  reset();
  seedLive();
  var snap = plain(h.CW.buildTransferSnapshot());
  assert.deepEqual(plain(h.CW.validateTransferSnapshot(snap)), { ok: true });
});

test("validateTransferSnapshot rejects a flag with a bad key", function () {
  reset();
  seedLive();
  var snap = plain(h.CW.buildTransferSnapshot());
  snap.flags[0].key = "";
  var out = plain(h.CW.validateTransferSnapshot(snap));
  assert.equal(out.ok, false, "empty flag key rejected");
  assert.ok(typeof out.error === "string" && out.error.length > 0);
});

test("validateTransferSnapshot rejects a flag with an unknown type", function () {
  reset();
  seedLive();
  var snap = plain(h.CW.buildTransferSnapshot());
  snap.flags[0].type = "bogus";
  var out = plain(h.CW.validateTransferSnapshot(snap));
  assert.equal(out.ok, false, "unknown flag type rejected");
  assert.ok(typeof out.error === "string" && out.error.length > 0);
});

test("validateTransferSnapshot rejects a rule with an invalid condition op", function () {
  reset();
  seedLive();
  var snap = plain(h.CW.buildTransferSnapshot());
  var keep = snap.flags.filter(function (f) { return f.key === "keep"; })[0];
  keep.rules[0].condition.op = "=~="; // outside transfer.js:17-24 CONDITION_OPS
  var out = plain(h.CW.validateTransferSnapshot(snap));
  assert.equal(out.ok, false, "unknown condition op rejected");
  assert.ok(typeof out.error === "string" && out.error.length > 0);
});

test("validateTransferSnapshot rejects an experiment whose variants do not total 10000", function () {
  reset();
  seedLive();
  var snap = plain(h.CW.buildTransferSnapshot());
  snap.experiments[0].variants[0].weightBps = 1000; // sum 6000, want 10000
  var out = plain(h.CW.validateTransferSnapshot(snap));
  assert.equal(out.ok, false, "variant sum != 10000 rejected");
  assert.ok(typeof out.error === "string" && out.error.length > 0);
});

// --- importTransferSnapshot: drafts staged, counts returned ---

function importSnapshot() {
  return {
    flags: [
      {
        key: "keep", type: "bool", default: true, group: "G1", description: "",
        rules: [{ priority: 5, condition: { field: "platform", op: "!=", value: "android" }, value: false }],
      },
      { key: "fresh", type: "bool", default: false, group: "G-new", description: "", rules: [] },
    ],
    experiments: [
      {
        name: "NExp", flag: "fresh", seed: "s2", status: "draft",
        variants: [{ name: "control", weightBps: 5000 }, { name: "treatment", weightBps: 5000 }],
      },
    ],
  };
}

test("importTransferSnapshot stages drafts: flag create+update, rule replace, experiment+group create", function () {
  reset();
  seedLive();
  var counts = plain(h.CW.importTransferSnapshot(importSnapshot()));
  // transfer.js:333-344 emptyCounts keys: G-new created, keep updated +
  // fresh created, r1 deleted + 1 rule created, NExp created.
  assert.deepEqual(counts, {
    groupsCreated: 1, flagsCreated: 1, flagsUpdated: 1,
    rulesCreated: 1, rulesDeleted: 1,
    experimentsCreated: 1, experimentsUpdated: 0, skipped: 0,
  });
  var flagOps = draftOps("flag");
  assert.equal(flagOps.f1, "update", "existing flag matched by key stages an update on the live id");
  assert.equal(h.CW.state.drafts.flag.f1.body.defaultValue, true, "update carries the snapshot value");
  var creates = Object.keys(h.CW.state.drafts.flag).filter(function (k) {
    return h.CW.state.drafts.flag[k].op === "create";
  });
  assert.equal(creates.length, 1, "one flag create staged");
  assert.equal(h.CW.state.drafts.flag[creates[0]].body.key, "fresh");
  var ruleOps = draftOps("rule");
  assert.equal(ruleOps.r1, "delete", "old rule for a replaced flag stages a delete");
  var ruleCreates = Object.keys(h.CW.state.drafts.rule).filter(function (k) {
    return h.CW.state.drafts.rule[k].op === "create";
  });
  assert.equal(ruleCreates.length, 1, "replacement rule stages a create");
  assert.equal(h.CW.state.drafts.rule[ruleCreates[0]].body.priority, 5);
  assert.equal(h.CW.state.drafts.rule[ruleCreates[0]].body.flag, "f1", "rule links the live flag id");
  var expKeys = Object.keys(h.CW.state.drafts.experiment);
  assert.equal(expKeys.length, 1, "one experiment create staged");
  assert.equal(h.CW.state.drafts.experiment[expKeys[0]].body.name, "NExp");
  var groupKeys = Object.keys(h.CW.state.drafts.group);
  assert.equal(groupKeys.length, 1, "one group create staged");
  assert.equal(h.CW.state.drafts.group[groupKeys[0]].body.name, "G-new");
  assert.equal(h.CW.drafts.hasDrafts(), true, "staged drafts are pending");
});

// --- diffTransferSnapshots: flags added/removed/modified + experiments ---

test("diffTransferSnapshots reports flag added/removed/modified and experiment adds", function () {
  reset();
  seedLive();
  var oldS = plain(h.CW.buildTransferSnapshot());
  // Next state: drop "zeta", add "brand", flip "keep", add an experiment.
  h.CW.state.flags = [
    { id: "f1", project: "p1", key: "keep", type: "bool", defaultValue: true, group: "g1", description: "" },
    { id: "f3", project: "p1", key: "brand", type: "bool", defaultValue: false, group: null, description: "" },
  ];
  h.CW.state.rules = [];
  h.CW.state.experiments = [
    {
      id: "x1", flag: "f1", name: "Exp", seed: "s", status: "draft",
      variants: [{ name: "control", weightBps: 5000 }, { name: "treatment", weightBps: 5000 }],
    },
    {
      id: "x2", flag: "f3", name: "NExp2", seed: "s2", status: "draft",
      variants: [{ name: "control", weightBps: 5000 }, { name: "treatment", weightBps: 5000 }],
    },
  ];
  var neuS = plain(h.CW.buildTransferSnapshot());
  var d = plain(h.CW.diffTransferSnapshots(oldS, neuS));
  // transfer.js:629-646: flags/experiments are row arrays
  // ({key|id, status, old, new}) plus per-kind and total counts.
  assert.deepEqual(
    rowsWith(d.flags, "added").map(function (r) { return r.key; }),
    ["brand"],
    "added flags list brand"
  );
  assert.deepEqual(
    rowsWith(d.flags, "removed").map(function (r) { return r.key; }),
    ["zeta"],
    "removed flags list zeta"
  );
  assert.deepEqual(
    rowsWith(d.flags, "modified").map(function (r) { return r.key; }),
    ["keep"],
    "modified flags list keep"
  );
  assert.deepEqual(
    rowsWith(d.experiments, "added").map(function (r) { return r.new.name; }),
    ["NExp2"],
    "added experiments list NExp2"
  );
  assert.deepEqual(
    rowsWith(d.experiments, "unchanged").map(function (r) { return r.new.name; }),
    ["Exp"],
    "unchanged experiments list Exp"
  );
  assert.deepEqual(d.counts, {
    flagsAdded: 1, flagsRemoved: 1, flagsModified: 1, flagsUnchanged: 0,
    experimentsAdded: 1, experimentsRemoved: 0, experimentsModified: 0, experimentsUnchanged: 1,
    added: 2, removed: 1, modified: 1, unchanged: 1,
  });
});

test("release menu renders an export action per release row", function () {
  reset();
  h.CW.state.releases = [
    { id: "r1", version: 2, etag: "e2", note: "new" },
    { id: "r2", version: 1, etag: "e1", note: "" },
  ];
  h.CW.renderReleases();
  var html = h.CW.$("release-list").innerHTML;
  assert.ok(html.indexOf('data-export-release="r1"') >= 0, "newest release has export");
  assert.ok(html.indexOf('data-export-release="r2"') >= 0, "older release has export");
  assert.ok(html.indexOf(">export<") >= 0 || html.indexOf("<span>export</span>") >= 0, "export label present");
});

test("releaseSnapshotToTransfer converts frozen snapshots with group names", function () {
  reset();
  h.CW.state.groups = { g1: "G1" };
  var rel = {
    id: "r1", version: 3,
    snapshot: {
      flags: [
        { key: "keep", type: "bool", default: false, group: "g1", rules: [{ priority: 0, condition: { field: "platform", op: "==", value: "ios" }, value: true }] },
        { key: "plain", type: "string", default: "x", group: "", rules: [] },
      ],
      experiments: [{ id: "x1", flag: "keep", seed: "s", variants: [{ name: "control", weightBps: 5000 }, { name: "treatment", weightBps: 5000 }], status: "draft" }],
    },
  };
  var out = plain(h.CW.releaseSnapshotToTransfer(rel));
  assert.equal(out.flags.length, 2);
  assert.equal(out.flags[0].group, "G1");
  assert.equal(out.flags[1].group, "");
  assert.deepEqual(out.experiments.length, 1);
  // String snapshots (raw DB bytes) convert identically.
  var relStr = { id: "r1", version: 3, snapshot: JSON.stringify(rel.snapshot) };
  assert.deepEqual(plain(h.CW.releaseSnapshotToTransfer(relStr)), out);
  // Converted snapshots stay re-importable.
  assert.equal(h.CW.validateTransferSnapshot(out).ok, true);
  // Missing/corrupt snapshots yield null.
  assert.equal(h.CW.releaseSnapshotToTransfer({ id: "r9", version: 9 }), null);
  assert.equal(h.CW.releaseSnapshotToTransfer({ id: "r9", version: 9, snapshot: "{nope" }), null);
  assert.equal(h.CW.releaseSnapshotToTransfer(null), null);
});

test("exportReleaseSnapshot fills the export dialog from a release row", function () {
  reset();
  h.CW.state.groups = { g1: "G1" };
  h.CW.state.releases = [{
    id: "r1", version: 3, etag: "e3",
    snapshot: {
      flags: [{ key: "keep", type: "bool", default: false, group: "g1", rules: [] }],
      experiments: [],
    },
  }];
  h.CW.exportReleaseSnapshot("r1");
  var text = h.CW.$("transfer-export-text").value;
  assert.ok(text.indexOf('"keep"') >= 0, "dialog shows release flag");
  assert.ok(text.indexOf('"G1"') >= 0, "group id resolved to name");
  var parsed = JSON.parse(text);
  assert.equal(h.CW.validateTransferSnapshot(parsed).ok, true);
  // Unknown id toasts and leaves the dialog text untouched.
  h.CW.exportReleaseSnapshot("ghost");
  assert.equal(h.CW.$("transfer-export-text").value, text);
});

test("openDraftSnapshotDialog renders git-style hunks with +/- lines", function () {
  reset();
  seedLive();
  h.CW.drafts.draftStage("flag", { op: "update", body: { defaultValue: true }, baseId: "f1", label: "keep" });
  h.CW.openDraftSnapshotDialog();
  var html = h.CW.$("transfer-diff-detail").innerHTML;
  assert.ok(html.indexOf("diff-file diff-modified") >= 0, "modified flag is a diff file block");
  assert.ok(html.indexOf("flags/keep") >= 0, "file path shown");
  assert.ok(html.indexOf("diff-line del") >= 0, "removed line present");
  assert.ok(html.indexOf("diff-line add") >= 0, "added line present");
  assert.ok(html.indexOf("diff-stat") >= 0, "stat line present");
  assert.ok(html.indexOf("zeta") < 0, "unchanged flags hidden");
  assert.ok(html.indexOf("experiments/") < 0, "no experiment blocks without experiment changes");
  assert.ok(html.indexOf("diff-field") >= 0 && html.indexOf(">default<") >= 0, "changed field badged");
});

test("openDraftSnapshotDialog titles experiments by flag/name, not the composite id", function () {
  reset();
  seedLive();
  h.CW.drafts.draftStage("experiment", { op: "update", body: { status: "running" }, baseId: "x1", label: "Exp" });
  h.CW.openDraftSnapshotDialog();
  var html = h.CW.$("transfer-diff-detail").innerHTML;
  assert.ok(html.indexOf("experiments/keep/Exp") >= 0, "header shows flag and experiment name");
  assert.ok(html.indexOf("keep\0s") < 0, "raw composite id never rendered");
});

test("openDraftSnapshotDialog badges rule-only changes and nests experiments under flags", function () {
  reset();
  seedLive();
  h.CW.drafts.draftStage("rule", {
    op: "update", baseId: "r1",
    body: { flag: "f1", priority: 0, condition: { field: "platform", op: "==", value: "ios" }, value: false },
    label: "rule for keep",
  });
  h.CW.drafts.draftStage("experiment", { op: "update", body: { status: "running" }, baseId: "x1", label: "Exp" });
  h.CW.openDraftSnapshotDialog();
  var html = h.CW.$("transfer-diff-detail").innerHTML;
  assert.ok(html.indexOf("diff-field\">rules<") >= 0, "rule change badged on its flag");
  assert.ok(html.indexOf("diff-field\">default<") < 0, "unchanged fields not badged");
  assert.ok(html.indexOf("diff-field\">experiments<") >= 0, "flag with experiment changes badged");
  assert.ok(html.indexOf("diff-nest") >= 0, "experiment hunks nest inside the flag block");
  assert.ok(html.indexOf("experiments/keep/Exp") >= 0, "nested experiment keeps its flag/name header");
});

test("openDraftSnapshotDialog renders a slim block for flags with only experiment changes", function () {
  reset();
  seedLive();
  h.CW.drafts.draftStage("experiment", { op: "update", body: { status: "running" }, baseId: "x1", label: "Exp" });
  h.CW.openDraftSnapshotDialog();
  var html = h.CW.$("transfer-diff-detail").innerHTML;
  assert.ok(html.indexOf("diff-flag-exps") >= 0, "experiments-only flag gets a slim container");
  assert.ok(html.indexOf("1 changed (0 added, 1 modified, 0 removed, 2 unchanged)") >= 0, "stat counts the experiment change");
});

test("openDraftSnapshotDialog lists every staged draft alongside the snapshot diff", function () {
  reset();
  seedLive();
  h.CW.drafts.draftStage("flag", { op: "update", body: { description: "same" }, baseId: "f2", label: "zeta" });
  h.CW.drafts.draftStage("rule", { op: "create", body: { flag: "f2", priority: 0, condition: { field: "platform", op: "==", value: "ios" }, value: "z" }, label: "rule for zeta" });
  h.CW.drafts.draftStage("experiment", { op: "update", body: { status: "running" }, baseId: "x1", label: "Exp" });
  h.CW.openDraftSnapshotDialog();
  var html = h.CW.$("transfer-diff-detail").innerHTML;
  assert.ok(html.indexOf("Staged drafts (3)") >= 0, "staged count matches the unpublished list");
  assert.ok(html.indexOf("flag/zeta") >= 0, "flag draft listed");
  assert.ok(html.indexOf("rule/rule for zeta") >= 0, "rule draft listed even though it folds into its flag in the snapshot");
  assert.ok(html.indexOf("experiment/Exp") >= 0, "experiment draft listed");
});

test("importTransferSnapshot of an identical snapshot stages nothing", function () {
  reset();
  seedLive();
  var snap = plain(h.CW.buildTransferSnapshot());
  var counts = plain(h.CW.importTransferSnapshot(snap, {}));
  assert.deepEqual(counts, {
    groupsCreated: 0, flagsCreated: 0, flagsUpdated: 0,
    rulesCreated: 0, rulesDeleted: 0,
    experimentsCreated: 0, experimentsUpdated: 0, skipped: 0,
  });
  assert.equal(h.CW.drafts.hasDrafts(), false);
});

test("no fetch performed by any pure-transfer path", function () {
  assert.equal(h.fetchCalls.length, 0);
});
