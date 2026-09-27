"use strict";
/* test-drafts.js — drafts.js unit tests (Wave 2 task 4).
 * Loads core.js + drafts.js via harness.js; node:test + node:assert/strict only.
 * Normative contract: pb_public/js/drafts.js:1-24.
 */
const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { load } = require("./harness");

// Legible seeds (todo 10 dom-ops reuses this pattern): two flags intentionally
// out of key order (sort check), rules with mixed priorities + one dangling,
// one targeted + one untargeted experiment, one group.
function seedCtx(h) {
  h.CW.state.projectId = "p1";
  h.CW.state.envId = "e1";
  h.CW.state.flags = [
    { id: "f1", key: "b-flag", project: "p1", defaultValue: false },
    { id: "f2", key: "a-flag", project: "p1", defaultValue: true },
  ];
  h.CW.state.groups = { g1: "Alpha" };
  h.CW.state.rules = [
    { id: "r1", flag: "f1", priority: 20 },
    { id: "r2", flag: "f1", priority: 5 },
    { id: "rx", flag: "nope", priority: 1 },
  ];
  h.CW.state.experiments = [
    { id: "x1", flag: "f1" },
    { id: "x2" },
  ];
  return h;
}

function fresh(localStorageSeed) {
  const h = load(
    ["core.js", "drafts.js"],
    localStorageSeed ? { localStorageSeed } : undefined
  );
  return seedCtx(h);
}

function snapshot(h) {
  return JSON.stringify({
    flags: h.CW.state.flags,
    rules: h.CW.state.rules,
    experiments: h.CW.state.experiments,
    groups: h.CW.state.groups,
  });
}

// Values built inside the vm context carry the vm realm's Object/Array
// prototypes, so node:assert/strict deepEqual rejects them. Normalize through
// JSON first; primitives compare directly and need no help.
function plain(v) {
  return JSON.parse(JSON.stringify(v));
}

describe("drafts: scope key + persist/restore", () => {
  it("persists under cw_drafts_<project>_<env> and restores round-trip", () => {
    const h1 = fresh();
    const t = h1.CW.drafts.draftStage("flag", { op: "create", body: { key: "n" } });
    assert.match(t, /^draft-flag-\d+$/); // drafts.js:135 temp id draft-<kind>-<seq>
    assert.ok(h1.store["cw_drafts_p1_e1"]);
    const parsed = JSON.parse(h1.store["cw_drafts_p1_e1"]);
    assert.equal(typeof parsed.seq, "number");
    assert.ok(parsed.drafts.flag[t]);
    // Round-trip into a fresh context via localStorageSeed (harness.js:128-133).
    const h2 = load(["core.js", "drafts.js"], { localStorageSeed: h1.store });
    h2.CW.state.projectId = "p1";
    h2.CW.state.envId = "e1";
    assert.equal(h2.CW.drafts.restoreDrafts(), true);
    assert.equal(h2.CW.drafts.isDraft("flag", t), true);
    assert.equal(h2.CW.drafts.draftOp("flag", t), "create");
  });

  it("uses empty ids in scope key when project/env unset", () => {
    const h = load(["core.js", "drafts.js"]); // projectId/envId null (core.js:17-18)
    h.CW.state.flags = [];
    assert.equal(h.CW.drafts.persistDrafts(), true);
    assert.ok(Object.prototype.hasOwnProperty.call(h.store, "cw_drafts__")); // drafts.js:46
  });

  it("lazily swaps buckets on project change (lastRestoreScope, drafts.js:64-84)", () => {
    const h = fresh();
    const t = h.CW.drafts.draftStage("flag", { op: "create", body: { key: "n" } });
    h.CW.state.projectId = "p2"; // no explicit restore: next call lazy-swaps
    assert.equal(h.CW.drafts.hasDrafts(), false);
    const t2 = h.CW.drafts.draftStage("rule", { op: "create", body: { flag: "f1" } });
    assert.ok(h.store["cw_drafts_p2_e1"]);
    h.CW.state.projectId = "p1"; // swap back restores p1 buckets from storage
    assert.equal(h.CW.drafts.isDraft("flag", t), true);
    assert.equal(h.CW.drafts.isDraft("rule", t2), false);
  });

  it("tolerates corrupt JSON: empty buckets, no throw", () => {
    const h = load(["core.js", "drafts.js"], {
      localStorageSeed: { cw_drafts_p1_e1: "'{corrupt" },
    });
    h.CW.state.projectId = "p1";
    h.CW.state.envId = "e1";
    assert.doesNotThrow(() => h.CW.drafts.restoreDrafts()); // drafts.js:55-59
    assert.equal(h.CW.drafts.hasDrafts(), false);
  });
});

describe("drafts: draftStage coalescing matrix", () => {
  it("mints temp ids draft-<kind>-<seq> with incrementing seq", () => {
    const h = fresh();
    const a = h.CW.drafts.draftStage("flag", { op: "create", body: {} });
    const b = h.CW.drafts.draftStage("rule", { op: "create", body: {} });
    assert.match(a, /^draft-flag-1$/);
    assert.match(b, /^draft-rule-2$/);
    assert.equal(h.CW.state.draftSeq, 2);
  });

  it("delete over never-published create REMOVES the bucket entry", () => {
    const h = fresh();
    const t = h.CW.drafts.draftStage("flag", { op: "create", body: { key: "tmp" } });
    assert.equal(h.CW.drafts.isDraft("flag", t), true);
    h.CW.drafts.draftStage("flag", { op: "delete", baseId: t });
    assert.equal(h.CW.drafts.isDraft("flag", t), false); // drafts.js:144-145
    assert.equal(h.CW.drafts.hasDrafts(), false);
  });

  it("update over create/update merges bodies key-wise, keeps original op", () => {
    const h = fresh();
    const t = h.CW.drafts.draftStage("flag", {
      op: "create", body: { key: "k", a: 1, b: 1 },
    });
    h.CW.drafts.draftStage("flag", { op: "update", baseId: t, body: { b: 2 } });
    const stored = h.CW.state.drafts.flag[t];
    assert.equal(stored.op, "create"); // drafts.js:146-153 stays a create
    assert.deepEqual(plain(stored.body), { key: "k", a: 1, b: 2 });
    // Update over update merges too.
    h.CW.drafts.draftStage("flag", { op: "update", baseId: "f1", body: { x: 1 } });
    h.CW.drafts.draftStage("flag", { op: "update", baseId: "f1", body: { y: 2 } });
    assert.equal(h.CW.state.drafts.flag.f1.op, "update");
    assert.deepEqual(plain(h.CW.state.drafts.flag.f1.body), { x: 1, y: 2 });
  });

  it("create over anything replaces; anything over delete replaces", () => {
    const h = fresh();
    h.CW.drafts.draftStage("flag", { op: "update", baseId: "f1", body: { x: 1 } });
    h.CW.drafts.draftStage("flag", { op: "create", tempId: "f1", body: { fresh: true } });
    assert.equal(h.CW.state.drafts.flag.f1.op, "create"); // drafts.js:154 replace
    assert.deepEqual(plain(h.CW.state.drafts.flag.f1.body), { fresh: true });
    h.CW.drafts.draftStage("flag", { op: "delete", baseId: "f2", body: {} });
    h.CW.drafts.draftStage("flag", { op: "update", baseId: "f2", body: { back: 1 } });
    assert.equal(h.CW.state.drafts.flag.f2.op, "update");
    assert.deepEqual(plain(h.CW.state.drafts.flag.f2.body), { back: 1 });
  });

  it("unknown kind throws /unknown draft kind/; bad op throws", () => {
    const h = fresh();
    assert.throws(
      () => h.CW.drafts.draftStage("nope", { op: "create", body: {} }),
      /unknown draft kind/ // drafts.js:123
    );
    assert.throws(
      () => h.CW.drafts.draftStage("flag", { op: "bogus", body: {} }),
      /draft needs op/
    );
    assert.throws(
      () => h.CW.drafts.draftStage("flag", { op: "update", body: {} }),
      /needs baseId/
    );
    assert.throws(
      () => h.CW.drafts.draftStage("flag", { op: "delete", baseId: "" }),
      /needs baseId/
    );
  });

  it("copies bodies (copyBody drafts.js:104-113): later mutation is isolated", () => {
    const h = fresh();
    const body = { key: "k" };
    const members = ["f1"];
    const t = h.CW.drafts.draftStage("flag", { op: "create", body });
    body.key = "MUT";
    assert.equal(h.CW.state.drafts.flag[t].body.key, "k");
    h.CW.drafts.draftStage("group", { op: "delete", baseId: "g1", memberIds: members });
    members.push("MUT"); // drafts.js:163-165 slices memberIds
    assert.deepEqual(plain(h.CW.state.drafts.group.g1.memberIds), ["f1"]);
  });
});

describe("drafts: merged selectors", () => {
  it("mergedFlags sorts by key, includes temp creates, tombstones deletes", () => {
    const h = fresh();
    const before = snapshot(h);
    const t = h.CW.drafts.draftStage("flag", { op: "create", body: { key: "0-new" } });
    h.CW.drafts.draftStage("flag", { op: "update", baseId: "f1", body: { defaultValue: true } });
    h.CW.drafts.draftStage("flag", { op: "delete", baseId: "f2", body: {} });
    const out = h.CW.drafts.mergedFlags();
    assert.deepEqual(plain(out.map((f) => f.key)), ["0-new", "a-flag", "b-flag"]); // drafts.js:292 sort
    assert.equal(out[0].id, t);
    assert.equal(out[2].defaultValue, true); // update overlaid (applyBody drafts.js:235)
    const tomb = out.find((f) => f.id === "f2");
    assert.equal(tomb._draftDeleted, true); // drafts.js:271
    assert.equal(tomb._draftOp, "delete"); // drafts.js:272
    assert.equal(snapshot(h), before); // pure: server copy untouched (drafts.js:22-23)
  });

  it("create-then-update against a temp id resolves via pass 1 (drafts.js:259-262)", () => {
    const h = fresh();
    const t = h.CW.drafts.draftStage("flag", { op: "create", body: { key: "k" } });
    h.CW.drafts.draftStage("flag", { op: "update", baseId: t, body: { extra: 7 } });
    const found = h.CW.drafts.mergedFlags().find((f) => f.id === t);
    assert.equal(found.key, "k");
    assert.equal(found.extra, 7);
  });

  it("mergedGroups overlays creates", () => {
    const h = fresh();
    const t = h.CW.drafts.draftStage("group", { op: "create", body: { name: "Beta" } });
    const names = h.CW.drafts.mergedGroups();
    assert.equal(names.g1, "Alpha");
    assert.equal(names[t], "Beta"); // drafts.js:307
  });

  it("mergedRules sorts by priority and drops dangling flags incl. temp ids", () => {
    const h = fresh();
    const before = snapshot(h);
    const t = h.CW.drafts.draftStage("flag", { op: "create", body: { key: "k" } });
    h.CW.drafts.draftStage("rule", { op: "create", body: { flag: t, priority: 1 } });
    const out = h.CW.drafts.mergedRules();
    assert.deepEqual(
      plain(out.map((r) => r.id)).sort(),
      ["r1", "r2", out.find((r) => r.flag === t).id].sort()
    );
    assert.ok(!out.find((r) => r.id === "rx")); // dangling flag filtered (drafts.js:339)
    const prios = plain(out.filter((r) => r.flag === "f1").map((r) => r.priority));
    assert.deepEqual(prios, [5, 20]); // drafts.js:341 priority sort
    // Deleting f1 evicts its rules from the merged set (drafts.js:332 validFlagIds).
    h.CW.drafts.draftStage("flag", { op: "delete", baseId: "f1", body: {} });
    assert.ok(!h.CW.drafts.mergedRules().find((r) => r.flag === "f1"));
    assert.equal(snapshot(h), before);
  });

  it("mergedExperiments keeps server order first, then draft creates", () => {
    const h = fresh();
    const before = snapshot(h);
    h.CW.drafts.draftStage("experiment", { op: "create", body: { flag: "f1" } });
    h.CW.drafts.draftStage("experiment", { op: "create", body: {} }); // untargeted
    const out = h.CW.drafts.mergedExperiments();
    assert.equal(out[0].id, "x1"); // drafts.js:361-364 server order first
    assert.equal(out[1].id, "x2");
    assert.equal(out.length, 4); // drafts.js:368-372 then creates
    assert.equal(snapshot(h), before);
  });

  it("resolveFlagId passes live and temp ids through unchanged", () => {
    const h = fresh();
    assert.equal(h.CW.drafts.resolveFlagId("f1"), "f1"); // drafts.js:231-233
    const t = h.CW.drafts.draftStage("flag", { op: "create", body: { key: "k" } });
    assert.equal(h.CW.drafts.resolveFlagId(t), t);
  });
});

describe("drafts: discard / clear / queries", () => {
  it("draftDiscard removes one entry; false when missing or bad kind", () => {
    const h = fresh();
    const t = h.CW.drafts.draftStage("flag", { op: "create", body: {} });
    assert.equal(h.CW.drafts.draftDiscard("flag", t), true);
    assert.equal(h.CW.drafts.isDraft("flag", t), false);
    assert.equal(h.CW.drafts.draftDiscard("flag", t), false);
    assert.equal(h.CW.drafts.draftDiscard("nope", t), false); // drafts.js:176
  });

  it("draftClearAll empties every kind", () => {
    const h = fresh();
    h.CW.drafts.draftStage("flag", { op: "create", body: {} });
    h.CW.drafts.draftStage("rule", { op: "create", body: {} });
    h.CW.drafts.draftStage("group", { op: "create", body: {} });
    h.CW.drafts.draftStage("experiment", { op: "create", body: {} });
    assert.equal(h.CW.drafts.hasDrafts(), true);
    assert.equal(h.CW.drafts.draftClearAll(), true); // drafts.js:184-192
    assert.equal(h.CW.drafts.hasDrafts(), false);
  });

  it("isDraft/draftOp/hasDrafts edge cases", () => {
    const h = fresh();
    assert.equal(h.CW.drafts.hasDrafts(), false);
    assert.equal(h.CW.drafts.isDraft("flag", ""), false); // drafts.js:197
    assert.equal(h.CW.drafts.isDraft("nope", "f1"), false);
    assert.equal(h.CW.drafts.draftOp("flag", "f1"), null); // drafts.js:204-210
    assert.equal(h.CW.drafts.draftOp("nope", "f1"), null);
    assert.equal(h.CW.drafts.draftOp("flag", ""), null);
    h.CW.drafts.draftStage("flag", { op: "delete", baseId: "f1", body: {} });
    assert.equal(h.CW.drafts.isDraft("flag", "f1"), true);
    assert.equal(h.CW.drafts.draftOp("flag", "f1"), "delete"); // drafts.js:201-202
    assert.equal(h.CW.drafts.hasDrafts(), true);
  });
});

describe("drafts: refreshDraftChrome smoke", () => {
  it("no-ops with all render fns missing, even with active ids set", () => {
    const h = fresh();
    delete h.CW.setPublishState; // never defined by core+drafts alone
    delete h.CW.renderFlags;
    delete h.CW.renderExperiments;
    delete h.CW.renderFlagRulesList;
    delete h.CW.renderFlagExperimentsList;
    h.CW.state.activeFlagRulesId = "f1"; // drafts.js:390 branch taken, fn absent
    h.CW.state.activeFlagExperimentsId = "f1"; // drafts.js:393 branch taken, fn absent
    assert.doesNotThrow(() => h.CW.drafts.refreshDraftChrome());
  });

  it("fans out to chrome renderers with the dirty bit", () => {
    const h = fresh();
    const calls = [];
    h.CW.setPublishState = (dirty) => calls.push(["setPublishState", dirty]);
    h.CW.renderFlags = () => calls.push(["renderFlags"]);
    h.CW.renderExperiments = () => calls.push(["renderExperiments"]);
    h.CW.renderFlagRulesList = () => calls.push(["renderFlagRulesList"]);
    h.CW.renderFlagExperimentsList = () => calls.push(["renderFlagExperimentsList"]);
    h.CW.drafts.refreshDraftChrome();
    assert.deepEqual(calls, [
      ["setPublishState", false],
      ["renderFlags"],
      ["renderExperiments"],
    ]);
    h.CW.state.activeFlagRulesId = "f1";
    h.CW.state.activeFlagExperimentsId = "f1";
    h.CW.drafts.draftStage("flag", { op: "create", body: {} });
    calls.length = 0;
    h.CW.drafts.refreshDraftChrome();
    assert.deepEqual(calls, [
      ["setPublishState", true], // drafts.js:380-383 gating
      ["renderFlags"],
      ["renderExperiments"],
      ["renderFlagRulesList"],
      ["renderFlagExperimentsList"],
    ]);
  });
});
