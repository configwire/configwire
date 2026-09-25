/* ConfigWire admin shell — drafts.js (local-draft store + merged selectors).
 * Loaded AFTER core.js (CW.state shape) and BEFORE flags.js.
 *
 * UNIT A (local-drafts): every flag/group/rule/experiment mutation stages a
 * local draft with ZERO server writes. Unit B (publish-apply + discard in
 * releases.js) flushes these drafts via apiMut and clears them.
 *
 * Contract (exact — Unit B implements against this):
 * - CW.state.drafts = { flag:{}, rule:{}, experiment:{}, group:{} },
 *   CW.state.draftSeq = 0.
 * - Entry { op:"create"|"update"|"delete", body:{...full POST/PATCH body...},
 *   baseId, tempId, label, at }. Key = live id for update/delete, temp id
 *   `draft-<kind>-<++seq>` for creates. Group deletes also carry
 *   `memberIds` (live flag ids snapshotted at stage time) for apply-time
 *   ungroup.
 * - CW.drafts exposes EXACTLY: draftStage, draftDiscard, draftClearAll,
 *   persistDrafts, restoreDrafts, mergedFlags, mergedGroups, mergedRules,
 *   mergedExperiments, resolveFlagId, isDraft, hasDrafts, refreshDraftChrome.
 * - Persist localStorage["cw_drafts_"+projectId+"_"+envId]; restore on
 *   boot/scope-load (loadFlags/loadRules/loadExperiments tails call
 *   restoreDrafts; every public function also lazy-restores on scope change).
 * - Merged selectors are PURE: they never write back into
 *   CW.state.flags/rules/experiments/groups (next loadX would corrupt).
 */
(function () {
  "use strict";

  var CW = window.CW;

  var DRAFT_KINDS = ["flag", "rule", "experiment", "group"];

  function ensureDraftShape() {
    if (!CW.state.drafts) {
      CW.state.drafts = { flag: {}, rule: {}, experiment: {}, group: {} };
    }
    var i;
    for (i = 0; i < DRAFT_KINDS.length; i++) {
      if (!CW.state.drafts[DRAFT_KINDS[i]]) CW.state.drafts[DRAFT_KINDS[i]] = {};
    }
    if (typeof CW.state.draftSeq !== "number" || !isFinite(CW.state.draftSeq)) {
      CW.state.draftSeq = 0;
    }
  }

  function scopeKey() {
    return "cw_drafts_" + (CW.state.projectId || "") + "_" + (CW.state.envId || "");
  }

  var lastRestoreScope = null;

  function readScopeBucket(key) {
    var raw = null;
    try { raw = localStorage.getItem(key); } catch (e) { raw = null; }
    if (!raw) return null;
    try {
      var parsed = JSON.parse(raw);
      if (!parsed || typeof parsed !== "object" || !parsed.drafts) return null;
      return parsed;
    } catch (e) { return null; }
  }

  // Lazy scope restore: when project/env changes, swap the in-memory buckets
  // to the newly-scoped persisted drafts (or empty when none were stored).
  function ensureScope() {
    ensureDraftShape();
    var key = scopeKey();
    if (key === lastRestoreScope) return;
    var parsed = readScopeBucket(key);
    var i;
    if (parsed) {
      for (i = 0; i < DRAFT_KINDS.length; i++) {
        CW.state.drafts[DRAFT_KINDS[i]] =
          (parsed.drafts && parsed.drafts[DRAFT_KINDS[i]]) || {};
      }
      if (typeof parsed.seq === "number" && isFinite(parsed.seq) && parsed.seq > CW.state.draftSeq) {
        CW.state.draftSeq = Math.floor(parsed.seq);
      }
    } else {
      for (i = 0; i < DRAFT_KINDS.length; i++) {
        CW.state.drafts[DRAFT_KINDS[i]] = {};
      }
    }
    lastRestoreScope = key;
  }

  function persistDrafts() {
    ensureDraftShape();
    lastRestoreScope = scopeKey();
    try {
      localStorage.setItem(lastRestoreScope, JSON.stringify({
        seq: CW.state.draftSeq,
        drafts: CW.state.drafts,
      }));
    } catch (e) { /* private mode */ }
    return true;
  }

  function restoreDrafts() {
    lastRestoreScope = null;
    ensureScope();
    return true;
  }

  function copyBody(body) {
    var out = {};
    var k;
    if (body && typeof body === "object") {
      for (k in body) {
        if (Object.prototype.hasOwnProperty.call(body, k)) out[k] = body[k];
      }
    }
    return out;
  }

  // Stage one draft. Coalescing rules (same key):
  // - delete over a never-published create: drop the entry entirely.
  // - update over create/update: merge bodies, keep the original op identity
  //   (a create stays a create so apply POSTs the full body once).
  // - anything over delete, or create over anything: replace.
  // Returns the storage key (live id, or temp id for creates).
  function draftStage(kind, entry) {
    ensureScope();
    if (DRAFT_KINDS.indexOf(kind) < 0) throw new Error("unknown draft kind: " + kind);
    entry = entry || {};
    if (entry.op !== "create" && entry.op !== "update" && entry.op !== "delete") {
      throw new Error("draft needs op create|update|delete");
    }
    var bucket = CW.state.drafts[kind];
    var key = null;
    if (entry.op === "create") {
      if (entry.tempId) {
        key = String(entry.tempId);
      } else {
        CW.state.draftSeq++;
        key = "draft-" + kind + "-" + CW.state.draftSeq;
      }
    } else {
      if (entry.baseId === undefined || entry.baseId === null || entry.baseId === "") {
        throw new Error("draft update/delete needs baseId");
      }
      key = String(entry.baseId);
    }
    var prev = Object.prototype.hasOwnProperty.call(bucket, key) ? bucket[key] : null;
    if (entry.op === "delete" && prev && prev.op === "create") {
      delete bucket[key];
    } else if (entry.op === "update" && prev && (prev.op === "create" || prev.op === "update")) {
      var merged = copyBody(prev.body);
      var patch = copyBody(entry.body);
      var k;
      for (k in patch) merged[k] = patch[k];
      prev.body = merged;
      if (entry.label) prev.label = String(entry.label);
      prev.at = Date.now();
    } else {
      var stored = {
        op: entry.op,
        body: copyBody(entry.body),
        baseId: entry.baseId === undefined || entry.baseId === null ? null : String(entry.baseId),
        tempId: entry.tempId === undefined || entry.tempId === null ? null : String(entry.tempId),
        label: entry.label === undefined || entry.label === null ? key : String(entry.label),
        at: Date.now(),
      };
      if (kind === "group" && entry.op === "delete" && Array.isArray(entry.memberIds)) {
        stored.memberIds = entry.memberIds.slice();
      }
      bucket[key] = stored;
    }
    // Creates are keyed by temp id: remember it on the entry for callers.
    if (entry.op === "create" && !bucket[key].tempId) bucket[key].tempId = key;
    persistDrafts();
    return key;
  }

  function draftDiscard(kind, id) {
    ensureScope();
    if (DRAFT_KINDS.indexOf(kind) < 0) return false;
    var key = String(id);
    if (!Object.prototype.hasOwnProperty.call(CW.state.drafts[kind], key)) return false;
    delete CW.state.drafts[kind][key];
    persistDrafts();
    return true;
  }

  function draftClearAll() {
    ensureDraftShape();
    var i;
    for (i = 0; i < DRAFT_KINDS.length; i++) {
      CW.state.drafts[DRAFT_KINDS[i]] = {};
    }
    persistDrafts();
    return true;
  }

  function isDraft(kind, id) {
    ensureScope();
    if (!kind || DRAFT_KINDS.indexOf(kind) < 0) return false;
    if (id === undefined || id === null || id === "") return false;
    return Object.prototype.hasOwnProperty.call(CW.state.drafts[kind] || {}, String(id));
  }

  function hasDrafts() {
    ensureScope();
    var i, bucket, k;
    for (i = 0; i < DRAFT_KINDS.length; i++) {
      bucket = CW.state.drafts[DRAFT_KINDS[i]] || {};
      for (k in bucket) {
        if (Object.prototype.hasOwnProperty.call(bucket, k)) return true;
      }
    }
    return false;
  }

  // Temp-id resolution. Pre-publish there is no live id yet, so temp ids are
  // valid values everywhere (selects, rule bodies, experiment links) and pass
  // through unchanged; live ids pass through unchanged too. Unit B remaps
  // temp ids to live ids during apply.
  function resolveFlagId(id) {
    ensureScope();
    return id;
  }

  function applyBody(base, body) {
    var out = {};
    var k;
    for (k in base) {
      if (Object.prototype.hasOwnProperty.call(base, k)) out[k] = base[k];
    }
    body = body || {};
    for (k in body) {
      if (Object.prototype.hasOwnProperty.call(body, k)) out[k] = body[k];
    }
    return out;
  }

  // Two-pass overlay: pass 1 lays down creates (so updates/deletes against
  // temp ids resolve), pass 2 applies updates/deletes.
  function overlayById(serverItems, bucket, makeCreate) {
    var byId = {};
    var i;
    for (i = 0; i < serverItems.length; i++) {
      var it = serverItems[i];
      if (it && it.id !== undefined && it.id !== null && it.id !== "") byId[it.id] = it;
    }
    var keys = Object.keys(bucket || {});
    var ki, e;
    for (ki = 0; ki < keys.length; ki++) {
      e = bucket[keys[ki]];
      if (e && e.op === "create") byId[keys[ki]] = makeCreate(keys[ki], e);
    }
    for (ki = 0; ki < keys.length; ki++) {
      e = bucket[keys[ki]];
      if (!e || e.op === "create") continue;
      if (e.op === "update") {
        if (byId[keys[ki]]) byId[keys[ki]] = applyBody(byId[keys[ki]], e.body);
      } else if (e.op === "delete") {
        delete byId[keys[ki]];
      }
    }
    return byId;
  }

  function mergedFlags() {
    ensureScope();
    var server = Array.isArray(CW.state.flags) ? CW.state.flags : [];
    var byId = overlayById(server, CW.state.drafts.flag, function (key, e) {
      var obj = { id: key, project: CW.state.projectId };
      return applyBody(obj, e.body);
    });
    var out = [];
    var k;
    for (k in byId) {
      if (Object.prototype.hasOwnProperty.call(byId, k)) out.push(byId[k]);
    }
    out.sort(function (a, b) { return (a.key || "") < (b.key || "") ? -1 : 1; });
    return out;
  }

  function mergedGroups() {
    ensureScope();
    var out = {};
    var server = CW.state.groups || {};
    var k;
    for (k in server) {
      if (Object.prototype.hasOwnProperty.call(server, k)) out[k] = server[k];
    }
    var byId = overlayById(
      Object.keys(out).map(function (id) { return { id: id, name: out[id] }; }),
      CW.state.drafts.group,
      function (key, e) { return { id: key, name: (e.body && e.body.name) || key }; }
    );
    var names = {};
    for (k in byId) {
      if (Object.prototype.hasOwnProperty.call(byId, k) && byId[k]) {
        names[k] = byId[k].name || k;
      }
    }
    return names;
  }

  function mergedRules() {
    ensureScope();
    var server = Array.isArray(CW.state.rules) ? CW.state.rules : [];
    var byId = overlayById(server, CW.state.drafts.rule, function (key, e) {
      var obj = { id: key };
      return applyBody(obj, e.body);
    });
    // loadRules is unscoped: never leak rules across projects. Keep only
    // rules whose flag link resolves in the current-project merged flag set
    // (draft-created flags carry temp ids, which are valid merged members).
    var validFlagIds = {};
    var mf = mergedFlags();
    var i;
    for (i = 0; i < mf.length; i++) validFlagIds[mf[i].id] = true;
    var out = [];
    var k;
    for (k in byId) {
      if (!Object.prototype.hasOwnProperty.call(byId, k)) continue;
      var r = byId[k];
      if (r && validFlagIds[r.flag]) out.push(r);
    }
    out.sort(function (a, b) { return (a.priority || 0) - (b.priority || 0); });
    return out;
  }

  function mergedExperiments() {
    ensureScope();
    var server = Array.isArray(CW.state.experiments) ? CW.state.experiments : [];
    var byId = overlayById(server, CW.state.drafts.experiment, function (key, e) {
      var obj = { id: key };
      return applyBody(obj, e.body);
    });
    var validFlagIds = {};
    var mf = mergedFlags();
    var i;
    for (i = 0; i < mf.length; i++) validFlagIds[mf[i].id] = true;
    var out = [];
    var seen = {};
    // Server order first (loadExperiments already project-filtered at load).
    for (i = 0; i < server.length; i++) {
      var s = server[i];
      if (s && byId[s.id] && !seen[s.id]) { out.push(byId[s.id]); seen[s.id] = true; }
    }
    // Then draft creates (untargeted or linked to a merged flag id).
    var keys = Object.keys(byId);
    var k;
    for (k = 0; k < keys.length; k++) {
      if (seen[keys[k]]) continue;
      var x = byId[keys[k]];
      if (x && (!x.flag || validFlagIds[x.flag])) { out.push(x); seen[keys[k]] = true; }
    }
    return out;
  }

  // Chrome refresh from draft presence: publish gating + existing
  // list/badge/highlight renderers. Calls them, never rewrites them.
  function refreshDraftChrome() {
    ensureScope();
    var dirty = hasDrafts();
    if (CW.setPublishState) {
      try { CW.setPublishState(dirty); } catch (e) { /* chrome best-effort */ }
    }
    if (CW.renderFlags) {
      try { CW.renderFlags(); } catch (e) { /* render best-effort */ }
    }
    if (CW.renderExperiments) {
      try { CW.renderExperiments(); } catch (e) { /* render best-effort */ }
    }
    if (CW.state.activeFlagRulesId && CW.renderFlagRulesList) {
      try { CW.renderFlagRulesList(); } catch (e) { /* render best-effort */ }
    }
  }

  CW.drafts = {
    draftStage: draftStage,
    draftDiscard: draftDiscard,
    draftClearAll: draftClearAll,
    persistDrafts: persistDrafts,
    restoreDrafts: restoreDrafts,
    mergedFlags: mergedFlags,
    mergedGroups: mergedGroups,
    mergedRules: mergedRules,
    mergedExperiments: mergedExperiments,
    resolveFlagId: resolveFlagId,
    isDraft: isDraft,
    hasDrafts: hasDrafts,
    refreshDraftChrome: refreshDraftChrome,
  };
})();
