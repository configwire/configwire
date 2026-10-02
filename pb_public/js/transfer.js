/* ConfigWire admin shell — transfer.js (flag import/export + draft-snapshot diff).
 * Loaded AFTER releases.js. Defines ONLY the CW.* transfer surface; all
 * helpers stay IIFE-private. Pure logic (build/parse/validate/import/diff)
 * runs with core+drafts only; every DOM touch is null-guarded so the module
 * works when the transfer dialogs are absent (node:vm harness). Never calls
 * fetch/CW.apiMut and never touches localStorage directly — staging goes
 * through CW.drafts.draftStage only.
 */
(function () {
  "use strict";

  var CW = window.CW;

  // Condition op allowlists mirror rules.js CONDITION_OPS (frozen contract
  // with releases/snapshot.go + eval/eval.go). Copied here so this module
  // works standalone with core+drafts only (no rules.js dependency).
  var CONDITION_OPS = {
    platform: ["==", "!=", "contains", "regex"],
    appVersion: ["<", "<=", "==", "!=", ">=", ">", "contains", "regex"],
    locale: ["==", "!=", "contains", "regex"],
    country: ["==", "!=", "contains", "regex"],
    percentile: ["<=", "between"],
    "custom.": ["==", "!=", "<", "<=", ">", ">=", "contains", "regex"],
  };

  var FLAG_KEY_RE = /^[A-Za-z_][A-Za-z0-9_.-]*$/;
  var MAX_FLAG_KEY_LEN = 128;
  var MAX_FLAGS = 1000;
  var VALID_FLAG_TYPES = { number: true, string: true, bool: true, json: true };
  var VALID_EXP_STATUS = { draft: true, running: true, stopped: true };
  var SHAPE_ERROR = "Expected {flags:[...], experiments:[...]}";

  function hasOwn(o, k) {
    return Object.prototype.hasOwnProperty.call(o, k);
  }

  function isObject(o) {
    return o !== null && typeof o === "object" && !Array.isArray(o);
  }

  function draftOpOf(kind, id) {
    try {
      if (CW.drafts && typeof CW.drafts.draftOp === "function") return CW.drafts.draftOp(kind, id);
    } catch (e) { /* ignore */ }
    return null;
  }

  function isDeletedRecord(kind, rec) {
    if (rec && rec._draftDeleted) return true;
    if (rec && rec.id !== undefined && rec.id !== null && rec.id !== "") {
      return draftOpOf(kind, rec.id) === "delete";
    }
    return false;
  }

  function mergedList(fn, fallback) {
    try {
      if (CW.drafts && typeof CW.drafts[fn] === "function") {
        var out = CW.drafts[fn]();
        if (Array.isArray(out)) return out;
      }
    } catch (e) { /* fall through to live state */ }
    if (Array.isArray(fallback)) return fallback;
    return [];
  }

  function mergedMap(fn, fallback) {
    var out = null;
    try {
      if (CW.drafts && typeof CW.drafts[fn] === "function") out = CW.drafts[fn]();
    } catch (e) { out = null; }
    if (out && typeof out === "object" && !Array.isArray(out)) return out;
    if (fallback && typeof fallback === "object" && !Array.isArray(fallback)) return fallback;
    return {};
  }

  function liveStateArray(key) {
    try {
      if (CW.state && Array.isArray(CW.state[key])) return CW.state[key];
    } catch (e) { /* ignore */ }
    return [];
  }

  // --- buildTransferSnapshot ---

  function buildTransferSnapshot(opts) {
    var from = (opts && opts.from) || "merged";
    var useMerged = from !== "live";
    var flags = useMerged ? mergedList("mergedFlags", liveStateArray("flags")) : liveStateArray("flags");
    var rules = useMerged ? mergedList("mergedRules", liveStateArray("rules")) : liveStateArray("rules");
    var exps = useMerged ? mergedList("mergedExperiments", liveStateArray("experiments")) : liveStateArray("experiments");
    var groups = useMerged ? mergedMap("mergedGroups", CW.state ? CW.state.groups : {}) : (CW.state ? CW.state.groups : {});
    if (!groups || typeof groups !== "object" || Array.isArray(groups)) groups = {};

    var kept = [];
    var idToKey = {};
    var i, f;
    for (i = 0; i < flags.length; i++) {
      f = flags[i];
      if (!f || typeof f.key !== "string" || !f.key) continue;
      if (isDeletedRecord("flag", f)) continue;
      kept.push(f);
      if (f.id !== undefined && f.id !== null && f.id !== "") idToKey[String(f.id)] = f.key;
    }

    var rulesByFlag = {};
    var r;
    for (i = 0; i < rules.length; i++) {
      r = rules[i];
      if (!r || r._draftDeleted) continue;
      var fid = r.flag === undefined || r.flag === null ? "" : String(r.flag);
      if (!fid) continue;
      if (!hasOwn(rulesByFlag, fid)) rulesByFlag[fid] = [];
      rulesByFlag[fid].push({
        priority: typeof r.priority === "number" && isFinite(r.priority) ? r.priority : 0,
        condition: r.condition === undefined ? null : r.condition,
        value: r.value === undefined ? null : r.value,
      });
    }
    var fk;
    for (fk in rulesByFlag) {
      if (hasOwn(rulesByFlag, fk)) {
        rulesByFlag[fk].sort(function (a, b) { return a.priority - b.priority; });
      }
    }

    var outFlags = kept.map(function (fl) {
      var gid = fl.group === undefined || fl.group === null ? "" : String(fl.group);
      var gname;
      if (!gid) gname = "";
      else if (hasOwn(groups, gid)) gname = groups[gid] || "";
      else gname = gid;
      var entry = {
        key: fl.key,
        type: fl.type || "bool",
        default: fl.defaultValue === undefined ? null : fl.defaultValue,
        group: gname,
        rules: fl.id !== undefined && fl.id !== null && hasOwn(rulesByFlag, String(fl.id))
          ? rulesByFlag[String(fl.id)]
          : [],
      };
      if (typeof fl.description === "string" && fl.description) entry.description = fl.description;
      return entry;
    });
    outFlags.sort(function (a, b) { return a.key < b.key ? -1 : a.key > b.key ? 1 : 0; });

    var keptIds = {};
    for (i = 0; i < kept.length; i++) {
      if (kept[i].id !== undefined && kept[i].id !== null && kept[i].id !== "") {
        keptIds[String(kept[i].id)] = true;
      }
    }
    var outExps = [];
    var x;
    for (i = 0; i < exps.length; i++) {
      x = exps[i];
      if (!x || isDeletedRecord("experiment", x)) continue;
      var xfid = x.flag === undefined || x.flag === null ? "" : String(x.flag);
      if (!xfid || !hasOwn(keptIds, xfid)) continue;
      var xkey = hasOwn(idToKey, xfid) ? idToKey[xfid] : "";
      if (!xkey) continue;
      var xe = {
        flag: xkey,
        seed: x.seed === undefined || x.seed === null ? "" : x.seed,
        variants: x.variants === undefined ? [] : x.variants,
        status: x.status || "draft",
      };
      if (x.name !== undefined && x.name !== null) xe.name = x.name;
      outExps.push(xe);
    }
    // Canonical order: name first when present (matches {"name","flag",...}
    // shape), so reorder keys for deterministic pretty JSON.
    outExps = outExps.map(function (e) {
      var ordered = {};
      if (hasOwn(e, "name")) ordered.name = e.name;
      ordered.flag = e.flag;
      ordered.seed = e.seed;
      ordered.variants = e.variants;
      ordered.status = e.status;
      return ordered;
    });
    outExps.sort(function (a, b) {
      if (a.flag !== b.flag) return a.flag < b.flag ? -1 : 1;
      var sa = String(a.seed), sb = String(b.seed);
      if (sa !== sb) return sa < sb ? -1 : 1;
      var na = String(a.name || ""), nb = String(b.name || "");
      return na < nb ? -1 : na > nb ? 1 : 0;
    });

    return { flags: outFlags, experiments: outExps };
  }

  // --- parseTransferSnapshot ---

  function parseTransferSnapshot(text) {
    var v;
    try {
      v = JSON.parse(text);
    } catch (e) {
      return { ok: false, error: "Invalid JSON: " + ((e && e.message) || "parse error") };
    }
    if (!isObject(v) || !Array.isArray(v.flags)) {
      return { ok: false, error: SHAPE_ERROR };
    }
    var exps = v.experiments;
    if (exps === undefined) exps = [];
    if (!Array.isArray(exps)) {
      return { ok: false, error: SHAPE_ERROR };
    }
    var snap = { flags: v.flags, experiments: exps };
    return { ok: true, snap: snap, value: snap };
  }

  // --- validateTransferSnapshot ---

  function defaultMatchesType(v, t) {
    if (v === null || v === undefined) return true;
    switch (t) {
      case "number": return typeof v === "number";
      case "string": return typeof v === "string";
      case "bool": return typeof v === "boolean";
      case "json": return typeof v === "object" && v !== null;
      default: return false;
    }
  }

  function conditionError(cond) {
    if (!isObject(cond)) return "Must be a JSON object with field/op/value.";
    if (typeof cond.field !== "string" || !cond.field) return "Field must be a non-empty string.";
    if (typeof cond.op !== "string" || !cond.op) return "Op must be a non-empty string.";
    if (!hasOwn(cond, "value")) return "Missing value.";
    var field = cond.field;
    var base = null;
    if (field === "platform" || field === "locale" || field === "country") base = field;
    else if (field === "appVersion" || field === "percentile") base = field;
    else if (field === "custom." || field.indexOf("custom.") === 0) base = "custom.";
    else return "Unknown field " + JSON.stringify(field);
    if (base === "custom." && field === "custom.") return "Custom attribute name must not be empty.";
    var allowed = CONDITION_OPS[base] || [];
    if (allowed.indexOf(cond.op) < 0) {
      return "Op " + JSON.stringify(cond.op) + " not allowed for field " + JSON.stringify(field) + ".";
    }
    return null;
  }

  function validateTransferSnapshot(snap) {
    if (!isObject(snap)) return { ok: false, error: SHAPE_ERROR };
    var flags = snap.flags;
    var exps = snap.experiments === undefined ? [] : snap.experiments;
    if (!Array.isArray(flags) || !Array.isArray(exps)) return { ok: false, error: SHAPE_ERROR };
    if (!flags.length) return { ok: false, error: "Nothing to import: no flags." };
    if (flags.length > MAX_FLAGS) {
      return { ok: false, error: "Flag limit reached: max " + MAX_FLAGS + " flags per project." };
    }
    var i, j, f;
    for (i = 0; i < flags.length; i++) {
      f = flags[i];
      if (!isObject(f)) return { ok: false, error: "Flag " + i + " must be an object." };
      if (typeof f.key !== "string" || !f.key.length || f.key.length > MAX_FLAG_KEY_LEN || !FLAG_KEY_RE.test(f.key)) {
        return { ok: false, error: "Invalid flag key " + JSON.stringify(f.key) + ": must match ^[A-Za-z_][A-Za-z0-9_.-]*$ and be 1-128 chars." };
      }
      if (!hasOwn(VALID_FLAG_TYPES, f.type)) {
        return { ok: false, error: "Invalid flag type " + JSON.stringify(f.type) + " for flag " + JSON.stringify(f.key) + ": must be number|string|bool|json." };
      }
      if (!defaultMatchesType(f.default, f.type)) {
        return { ok: false, error: "Flag " + JSON.stringify(f.key) + ": default does not match type " + JSON.stringify(f.type) + "." };
      }
      var rules = f.rules === undefined ? [] : f.rules;
      if (!Array.isArray(rules)) {
        return { ok: false, error: "Flag " + JSON.stringify(f.key) + ": rules must be an array." };
      }
      for (j = 0; j < rules.length; j++) {
        var rl = rules[j];
        if (!isObject(rl)) {
          return { ok: false, error: "Flag " + JSON.stringify(f.key) + ": rule " + j + " must be an object." };
        }
        if (!defaultMatchesType(rl.value, f.type)) {
          return { ok: false, error: "Flag " + JSON.stringify(f.key) + ": rule value does not match type " + JSON.stringify(f.type) + "." };
        }
        var cerr = conditionError(rl.condition);
        if (cerr) {
          return { ok: false, error: "Flag " + JSON.stringify(f.key) + ": rule " + j + " has invalid condition: " + cerr };
        }
      }
    }
    for (i = 0; i < exps.length; i++) {
      var e = exps[i];
      if (!isObject(e)) return { ok: false, error: "Experiment " + i + " must be an object." };
      if (typeof e.flag !== "string" || !e.flag) {
        return { ok: false, error: "Experiment " + (e.name || i) + ": Flag required." };
      }
      var label = e.name || e.flag + "/" + (e.seed || "");
      if (!Array.isArray(e.variants) || !e.variants.length) {
        return { ok: false, error: "Experiment " + JSON.stringify(String(label)) + " has no variants." };
      }
      var seen = {};
      var sum = 0;
      for (j = 0; j < e.variants.length; j++) {
        var vv = e.variants[j];
        if (!isObject(vv)) {
          return { ok: false, error: "Experiment " + JSON.stringify(String(label)) + ": variant " + (j + 1) + " must be an object." };
        }
        if (typeof vv.name !== "string" || !vv.name.trim()) {
          return { ok: false, error: "Experiment " + JSON.stringify(String(label)) + ": variant " + (j + 1) + " has an empty name." };
        }
        var vn = vv.name.trim();
        if (hasOwn(seen, vn)) {
          return { ok: false, error: "Experiment " + JSON.stringify(String(label)) + ": Duplicate variant name: " + vn };
        }
        seen[vn] = true;
        var w = vv.weightBps;
        if (typeof w !== "number" || !isFinite(w) || Math.floor(w) !== w) {
          return { ok: false, error: "Experiment " + JSON.stringify(String(label)) + ': Variant "' + vn + '" weightBps must be an integer.' };
        }
        if (w < 0) {
          return { ok: false, error: "Experiment " + JSON.stringify(String(label)) + ': Variant "' + vn + '" has negative weightBps.' };
        }
        sum += w;
      }
      if (sum !== 10000) {
        return { ok: false, error: "Experiment " + JSON.stringify(String(label)) + ": Weights sum " + sum + "/10000 — must total 10000." };
      }
      var st = e.status === undefined || e.status === null || e.status === "" ? "draft" : e.status;
      if (!hasOwn(VALID_EXP_STATUS, st)) {
        return { ok: false, error: "Experiment " + JSON.stringify(String(label)) + ": Invalid status " + JSON.stringify(e.status) + ": must be draft|running|stopped." };
      }
    }
    return { ok: true };
  }

  // --- importTransferSnapshot ---

  function emptyCounts() {
    return {
      groupsCreated: 0,
      flagsCreated: 0,
      flagsUpdated: 0,
      rulesCreated: 0,
      rulesDeleted: 0,
      experimentsCreated: 0,
      experimentsUpdated: 0,
      skipped: 0,
    };
  }

  function importTransferSnapshot(snap, opts) {
    opts = opts || {};
    var counts = emptyCounts();
    if (!snap || !Array.isArray(snap.flags)) return counts;
    if (!CW.drafts || typeof CW.drafts.draftStage !== "function") return counts;
    var projectId = null;
    try { projectId = CW.state ? CW.state.projectId : null; } catch (e) { projectId = null; }

    function stage(kind, entry) {
      return CW.drafts.draftStage(kind, entry);
    }

    // (a) groups: name -> id from merged groups; create unknown names.
    var groups = mergedMap("mergedGroups", CW.state ? CW.state.groups : {});
    var groupMap = {};
    var gid;
    for (gid in groups) {
      if (hasOwn(groups, gid) && typeof groups[gid] === "string") groupMap[groups[gid]] = gid;
    }
    var stagedGroups = {};
    var i, f;
    for (i = 0; i < snap.flags.length; i++) {
      f = snap.flags[i] || {};
      var gname = typeof f.group === "string" ? f.group : "";
      if (!gname || hasOwn(groupMap, gname) || hasOwn(stagedGroups, gname)) continue;
      var tmp = stage("group", { op: "create", body: { name: gname, project: projectId }, label: gname });
      stagedGroups[gname] = true;
      groupMap[gname] = tmp;
      counts.groupsCreated++;
    }

    // (b) flags: key -> live record from merged (non-deleted) flags.
    var liveFlags = mergedList("mergedFlags", liveStateArray("flags"));
    var byKey = {};
    var k;
    for (k = 0; k < liveFlags.length; k++) {
      var lf = liveFlags[k];
      if (!lf || typeof lf.key !== "string" || !lf.key) continue;
      if (isDeletedRecord("flag", lf)) continue;
      if (!hasOwn(byKey, lf.key)) byKey[lf.key] = lf;
    }
    var keyToId = {};
    for (k in byKey) {
      if (hasOwn(byKey, k) && byKey[k].id !== undefined && byKey[k].id !== null && byKey[k].id !== "") {
        keyToId[k] = String(byKey[k].id);
      }
    }
    for (i = 0; i < snap.flags.length; i++) {
      f = snap.flags[i] || {};
      if (typeof f.key !== "string" || !f.key) { counts.skipped++; continue; }
      var gnm = typeof f.group === "string" ? f.group : "";
      var groupId = (gnm && hasOwn(groupMap, gnm)) ? groupMap[gnm] : "";
      var body = {
        key: f.key,
        type: f.type,
        defaultValue: f.default === undefined ? null : f.default,
        project: projectId,
      };
      if (groupId) body.group = groupId;
      else body.group = null;
      if (typeof f.description === "string" && f.description) body.description = f.description;
      else if (hasOwn(byKey, f.key) && byKey[f.key] && typeof byKey[f.key].description === "string" && byKey[f.key].description) {
        body.description = byKey[f.key].description;
      }
      if (hasOwn(byKey, f.key) && hasOwn(keyToId, f.key)) {
        var stagedFlag = stage("flag", { op: "update", body: body, baseId: keyToId[f.key], label: f.key });
        if (stagedFlag == null) continue;
        counts.flagsUpdated++;
      } else {
        var created = stage("flag", { op: "create", body: body, label: f.key });
        keyToId[f.key] = created;
        counts.flagsCreated++;
      }
    }

    // (c) rules replace-per-flag.
    var liveRules = mergedList("mergedRules", liveStateArray("rules"));
    var rulesByFid = {};
    for (k = 0; k < liveRules.length; k++) {
      var lr = liveRules[k];
      if (!lr || lr._draftDeleted) continue;
      var rfid = lr.flag === undefined || lr.flag === null ? "" : String(lr.flag);
      if (!rfid) continue;
      if (!hasOwn(rulesByFid, rfid)) rulesByFid[rfid] = [];
      rulesByFid[rfid].push(lr);
    }
    function normImportRule(r, fid) {
      r = r || {};
      return {
        flag: fid,
        priority: typeof r.priority === "number" && isFinite(r.priority) ? r.priority : 0,
        condition: r.condition === undefined ? null : r.condition,
        value: r.value === undefined ? null : r.value,
      };
    }

    function sameRuleSet(existing, incoming, fid) {
      function keyOf(r) {
        try { return JSON.stringify(normImportRule(r, fid)); } catch (e) { return ""; }
      }
      return JSON.stringify(existing.map(keyOf).sort()) === JSON.stringify(incoming.map(keyOf).sort());
    }

    for (i = 0; i < snap.flags.length; i++) {
      f = snap.flags[i] || {};
      if (typeof f.key !== "string" || !f.key || !hasOwn(keyToId, f.key)) continue;
      var fid = keyToId[f.key];
      var existing = hasOwn(rulesByFid, fid) ? rulesByFid[fid] : [];
      if (sameRuleSet(existing, Array.isArray(f.rules) ? f.rules : [], fid)) continue;
      var d;
      for (d = 0; d < existing.length; d++) {
        var rid = existing[d].id;
        if (rid === undefined || rid === null || rid === "") continue;
        stage("rule", { op: "delete", body: {}, baseId: String(rid) });
        counts.rulesDeleted++;
      }
      var frules = Array.isArray(f.rules) ? f.rules : [];
      for (d = 0; d < frules.length; d++) {
        var fr = frules[d] || {};
        stage("rule", {
          op: "create",
          body: {
            flag: fid,
            priority: typeof fr.priority === "number" && isFinite(fr.priority) ? fr.priority : 0,
            condition: fr.condition === undefined ? null : fr.condition,
            value: fr.value === undefined ? null : fr.value,
          },
          label: "rule for " + f.key,
        });
        counts.rulesCreated++;
      }
    }

    // (d) experiments matched by flagKey + seed.
    var liveExps = mergedList("mergedExperiments", liveStateArray("experiments"));
    var idToKey = {};
    for (k in byKey) {
      if (hasOwn(byKey, k)) {
        // keyToId[k] is the live-or-temp id; invert for experiment linkage.
        if (hasOwn(keyToId, k)) idToKey[keyToId[k]] = k;
      }
    }
    function seedNorm(s) {
      return (s === undefined || s === null) ? "" : String(s);
    }
    var expIndex = {};
    for (k = 0; k < liveExps.length; k++) {
      var le = liveExps[k];
      if (!le || isDeletedRecord("experiment", le)) continue;
      var lefid = le.flag === undefined || le.flag === null ? "" : String(le.flag);
      if (!lefid || !hasOwn(idToKey, lefid)) continue;
      var xkey = idToKey[lefid] + "\0" + seedNorm(le.seed);
      if (!hasOwn(expIndex, xkey)) expIndex[xkey] = le;
    }
    var importExps = Array.isArray(snap.experiments) ? snap.experiments : [];
    for (i = 0; i < importExps.length; i++) {
      var xe = importExps[i] || {};
      if (typeof xe.flag !== "string" || !xe.flag || !hasOwn(keyToId, xe.flag)) { counts.skipped++; continue; }
      var xfid = keyToId[xe.flag];
      var xseed = seedNorm(xe.seed);
      var xbody = {
        flag: xfid,
        seed: xseed,
        variants: xe.variants === undefined ? [] : xe.variants,
        status: xe.status || "draft",
      };
      if (xe.name !== undefined) xbody.name = xe.name;
      var xlabel = (typeof xe.name === "string" && xe.name) ? xe.name : xe.flag;
      var matchKey = xe.flag + "\0" + xseed;
      if (hasOwn(expIndex, matchKey) && expIndex[matchKey].id !== undefined &&
          expIndex[matchKey].id !== null && expIndex[matchKey].id !== "") {
        var stagedExp = stage("experiment", { op: "update", body: xbody, baseId: String(expIndex[matchKey].id), label: xlabel });
        if (stagedExp != null) counts.experimentsUpdated++;
      } else {
        stage("experiment", { op: "create", body: xbody, label: xlabel });
        counts.experimentsCreated++;
      }
    }

    try {
      if (CW.drafts && typeof CW.drafts.refreshDraftChrome === "function") CW.drafts.refreshDraftChrome();
    } catch (e) { /* best-effort */ }
    try {
      if (typeof CW.toast === "function") CW.toast(summarizeCounts(counts), true);
    } catch (e2) { /* best-effort */ }
    return counts;
  }

  function summarizeCounts(c) {
    var parts = [];
    if (c.flagsCreated) parts.push(c.flagsCreated + (c.flagsCreated === 1 ? " flag created" : " flags created"));
    if (c.flagsUpdated) parts.push(c.flagsUpdated + (c.flagsUpdated === 1 ? " flag updated" : " flags updated"));
    if (c.rulesCreated) parts.push(c.rulesCreated + (c.rulesCreated === 1 ? " rule created" : " rules created"));
    if (c.rulesDeleted) parts.push(c.rulesDeleted + (c.rulesDeleted === 1 ? " rule deleted" : " rules deleted"));
    if (c.experimentsCreated) parts.push(c.experimentsCreated + (c.experimentsCreated === 1 ? " experiment created" : " experiments created"));
    if (c.experimentsUpdated) parts.push(c.experimentsUpdated + (c.experimentsUpdated === 1 ? " experiment updated" : " experiments updated"));
    if (c.groupsCreated) parts.push(c.groupsCreated + (c.groupsCreated === 1 ? " group created" : " groups created"));
    if (c.skipped) parts.push(c.skipped + " skipped");
    if (!parts.length) return "Import staged: no changes.";
    return "Import staged: " + parts.join(", ");
  }

  // --- diffTransferSnapshots ---

  function expDiffId(e) {
    var flag = (e && typeof e.flag === "string") ? e.flag : String((e && e.flag) || "");
    var seed = e && e.seed !== undefined && e.seed !== null && e.seed !== "" ? String(e.seed) : String((e && e.name) || "");
    return flag + "\0" + seed;
  }

  function statusRank(s) {
    if (s === "added") return 0;
    if (s === "modified") return 1;
    if (s === "removed") return 2;
    return 3;
  }

  function diffTransferSnapshots(oldSnap, newSnap) {
    var o = (oldSnap && typeof oldSnap === "object") ? oldSnap : {};
    var n = (newSnap && typeof newSnap === "object") ? newSnap : {};
    var of = Array.isArray(o.flags) ? o.flags : [];
    var nf = Array.isArray(n.flags) ? n.flags : [];
    var oe = Array.isArray(o.experiments) ? o.experiments : [];
    var ne = Array.isArray(n.experiments) ? n.experiments : [];

    var oByKey = {};
    var nByKey = {};
    var i;
    for (i = 0; i < of.length; i++) {
      if (of[i] && typeof of[i].key === "string" && !hasOwn(oByKey, of[i].key)) oByKey[of[i].key] = of[i];
    }
    for (i = 0; i < nf.length; i++) {
      if (nf[i] && typeof nf[i].key === "string" && !hasOwn(nByKey, nf[i].key)) nByKey[nf[i].key] = nf[i];
    }
    var flagRows = [];
    var seen = {};
    var key;
    function flagRow(k2, a, b, status) {
      return { key: k2, status: status, old: a === undefined ? null : a, new: b === undefined ? null : b };
    }
    for (key in oByKey) {
      if (!hasOwn(oByKey, key)) continue;
      seen[key] = true;
      if (!hasOwn(nByKey, key)) flagRows.push(flagRow(key, oByKey[key], null, "removed"));
      else if (JSON.stringify(oByKey[key]) !== JSON.stringify(nByKey[key])) flagRows.push(flagRow(key, oByKey[key], nByKey[key], "modified"));
      else flagRows.push(flagRow(key, oByKey[key], nByKey[key], "unchanged"));
    }
    for (key in nByKey) {
      if (!hasOwn(nByKey, key) || hasOwn(seen, key)) continue;
      flagRows.push(flagRow(key, null, nByKey[key], "added"));
    }
    flagRows.sort(function (a, b) {
      var ra = statusRank(a.status), rb = statusRank(b.status);
      if (ra !== rb) return ra - rb;
      return a.key < b.key ? -1 : a.key > b.key ? 1 : 0;
    });

    var oById = {};
    var nById = {};
    for (i = 0; i < oe.length; i++) {
      if (!oe[i]) continue;
      var oid = expDiffId(oe[i]);
      if (!hasOwn(oById, oid)) oById[oid] = oe[i];
    }
    for (i = 0; i < ne.length; i++) {
      if (!ne[i]) continue;
      var nid = expDiffId(ne[i]);
      if (!hasOwn(nById, nid)) nById[nid] = ne[i];
    }
    var expRows = [];
    var eseen = {};
    var id;
    function expRow(id2, a, b, status) {
      return { id: id2, status: status, old: a === undefined ? null : a, new: b === undefined ? null : b };
    }
    for (id in oById) {
      if (!hasOwn(oById, id)) continue;
      eseen[id] = true;
      if (!hasOwn(nById, id)) expRows.push(expRow(id, oById[id], null, "removed"));
      else if (JSON.stringify(oById[id]) !== JSON.stringify(nById[id])) expRows.push(expRow(id, oById[id], nById[id], "modified"));
      else expRows.push(expRow(id, oById[id], nById[id], "unchanged"));
    }
    for (id in nById) {
      if (!hasOwn(nById, id) || hasOwn(eseen, id)) continue;
      expRows.push(expRow(id, null, nById[id], "added"));
    }
    expRows.sort(function (a, b) {
      var ra = statusRank(a.status), rb = statusRank(b.status);
      if (ra !== rb) return ra - rb;
      return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
    });

    function countRows(rows) {
      var c = { added: 0, removed: 0, modified: 0, unchanged: 0 };
      var j;
      for (j = 0; j < rows.length; j++) {
        if (hasOwn(c, rows[j].status)) c[rows[j].status]++;
      }
      return c;
    }
    var fc = countRows(flagRows);
    var ec = countRows(expRows);
    return {
      flags: flagRows,
      experiments: expRows,
      counts: {
        flagsAdded: fc.added,
        flagsRemoved: fc.removed,
        flagsModified: fc.modified,
        flagsUnchanged: fc.unchanged,
        experimentsAdded: ec.added,
        experimentsRemoved: ec.removed,
        experimentsModified: ec.modified,
        experimentsUnchanged: ec.unchanged,
        added: fc.added + ec.added,
        removed: fc.removed + ec.removed,
        modified: fc.modified + ec.modified,
        unchanged: fc.unchanged + ec.unchanged,
      },
    };
  }

  // --- release export ---

  // Group id -> name for release snapshots (ids are opaque record ids;
  // names keep the export portable and re-importable). Unknown ids fall
  // back to the raw id, mirroring buildTransferSnapshot.
  function transferGroupName(groups, gid) {
    if (!gid) return "";
    if (groups && typeof groups === "object" && !Array.isArray(groups) &&
        hasOwn(groups, gid) && typeof groups[gid] === "string") return groups[gid] || "";
    return gid;
  }

  // Convert a stored release row's frozen snapshot into transfer shape
  // ({flags, experiments}). Returns null when the row carries no parseable
  // snapshot. Group ids resolve to names; everything else passes through
  // verbatim so the payload stays re-importable via importTransferSnapshot.
  function releaseSnapshotToTransfer(release) {
    if (!release || typeof release !== "object") return null;
    var raw = release.snapshot;
    var snap = null;
    if (raw !== null && typeof raw === "object" && !Array.isArray(raw)) snap = raw;
    else if (typeof raw === "string" && raw) {
      try { snap = JSON.parse(raw); } catch (e) { return null; }
    } else return null;
    if (!snap || typeof snap !== "object" || Array.isArray(snap)) return null;
    var groups = (CW.state && CW.state.groups) || {};
    var flags = Array.isArray(snap.flags) ? snap.flags : [];
    var exps = Array.isArray(snap.experiments) ? snap.experiments : [];
    return {
      flags: flags.map(function (fl) {
        if (!fl || typeof fl !== "object" || Array.isArray(fl)) return null;
        var entry = {
          key: fl.key,
          type: fl.type || "bool",
          default: fl.default === undefined ? null : fl.default,
          group: transferGroupName(groups,
            fl.group === undefined || fl.group === null ? "" : String(fl.group)),
          rules: Array.isArray(fl.rules) ? fl.rules : [],
        };
        if (typeof fl.description === "string" && fl.description) entry.description = fl.description;
        return entry;
      }).filter(function (e) { return e && typeof e.key === "string" && e.key; }),
      experiments: exps.filter(function (x) { return x && typeof x === "object" && !Array.isArray(x); }),
    };
  }

  // --- dialogs ---

  function openDialogEl(id) {
    var dlg = null;
    try { dlg = CW.$(id); } catch (e) { dlg = null; }
    if (!dlg) return null;
    if (dlg.open) return dlg;
    try {
      if (typeof dlg.showModal === "function") dlg.showModal();
      else if (typeof dlg.setAttribute === "function") dlg.setAttribute("open", "");
    } catch (e) { /* already open */ }
    return dlg;
  }

  function closeDialogEl(id) {
    var dlg = null;
    try { dlg = CW.$(id); } catch (e) { dlg = null; }
    if (dlg && dlg.open) {
      try { dlg.close(); } catch (e2) { /* best-effort */ }
    }
  }

  function el(id) {
    try { return CW.$(id); } catch (e) { return null; }
  }

  function toastSafe(msg, ok) {
    try {
      if (typeof CW.toast === "function") CW.toast(msg, ok);
    } catch (e) { /* best-effort */ }
  }

  var wired = {};

  function wireOnce(key, id, ev, fn) {
    if (wired[key]) return;
    var b = el(id);
    if (!b || typeof b.addEventListener !== "function") return;
    try {
      b.addEventListener(ev, fn);
      wired[key] = true;
    } catch (e) { /* best-effort */ }
  }

  function exportText() {
    var ta = el("transfer-export-text");
    return ta ? (ta.value || "") : "";
  }

  function copyExportText() {
    var v = exportText();
    var ta = el("transfer-export-text");
    if (typeof navigator !== "undefined" && navigator && navigator.clipboard &&
        typeof navigator.clipboard.writeText === "function") {
      try {
        var p = navigator.clipboard.writeText(v);
        if (p && typeof p.then === "function") {
          p.then(function () { toastSafe("Copied", true); }, function () { fallbackCopy(ta, v); });
          return;
        }
        toastSafe("Copied", true);
        return;
      } catch (e) { /* fall through to textarea fallback */ }
    }
    fallbackCopy(ta, v);
  }

  function fallbackCopy(ta, v) {
    try {
      if (ta && typeof ta.select === "function" && typeof document !== "undefined" && document &&
          typeof document.execCommand === "function") {
        ta.select();
        if (document.execCommand("copy")) { toastSafe("Copied", true); return; }
      }
    } catch (e) { /* manual fallback below */ }
    toastSafe(v ? "Copy failed." : "Nothing to copy.");
  }

  function downloadExport() {
    var v = exportText();
    var proj = "";
    try { proj = (CW.state && CW.state.projectId) || ""; } catch (e) { proj = ""; }
    var name = exportName || ("ConfigWire-flags-" + (proj || "export") + ".json");
    try {
      if (typeof Blob === "undefined") return;
      if (typeof URL === "undefined" || !URL || typeof URL.createObjectURL !== "function") return;
      if (typeof document === "undefined" || !document || typeof document.createElement !== "function") return;
      var blob = new Blob([v], { type: "application/json" });
      var url = URL.createObjectURL(blob);
      if (!url) return;
      var a = document.createElement("a");
      if (!a) return;
      a.href = url;
      a.download = name;
      var body = document.body || null;
      try {
        if (body && typeof body.appendChild === "function") body.appendChild(a);
        if (typeof a.click === "function") a.click();
        else if (typeof document.execCommand === "function") document.execCommand("saveAs");
      } finally {
        try { if (body && typeof body.removeChild === "function" && a.parentNode) body.removeChild(a); } catch (e2) { /* best-effort */ }
        try { if (typeof URL.revokeObjectURL === "function") URL.revokeObjectURL(url); } catch (e3) { /* best-effort */ }
      }
      toastSafe("Downloaded " + name, true);
    } catch (e) { /* best-effort */ }
  }

  var exportName = "";

  function openExportDialog(snap) {
    var s = null;
    if (snap && typeof snap === "object" && !Array.isArray(snap)) {
      s = snap;
    } else {
      try {
        s = buildTransferSnapshot({ from: "merged" });
      } catch (e) {
        s = { flags: [], experiments: [] };
      }
      exportName = "";
    }
    var ta = el("transfer-export-text");
    if (ta) {
      try { ta.value = JSON.stringify(s, null, 2); } catch (e) { /* keep old text */ }
      try { ta.readOnly = true; } catch (e2) { /* best-effort */ }
    }
    wireOnce("copy", "transfer-export-copy", "click", copyExportText);
    wireOnce("download", "transfer-export-download", "click", downloadExport);
    openDialogEl("transfer-export-dialog");
  }

  function closeExportDialog() {
    closeDialogEl("transfer-export-dialog");
  }

  function exportReleaseSnapshot(releaseOrId) {
    var rec = null;
    if (releaseOrId && typeof releaseOrId === "object") {
      rec = releaseOrId;
    } else {
      var items = (CW.state && Array.isArray(CW.state.releases)) ? CW.state.releases : [];
      for (var i = 0; i < items.length; i++) {
        if (items[i] && String(items[i].id) === String(releaseOrId)) { rec = items[i]; break; }
      }
    }
    if (!rec) { toastSafe("Release not found."); return; }
    var snap = releaseSnapshotToTransfer(rec);
    if (!snap) { toastSafe("Release has no snapshot."); return; }
    try { exportName = "ConfigWire-release-v" + rec.version + ".json"; } catch (e) { exportName = ""; }
    openExportDialog(snap);
  }

  function stageImportFromDialog() {
    var ta = el("transfer-import-text");
    var res = el("transfer-import-result");
    var text = ta ? (ta.value || "") : "";
    var parsed = null;
    try {
      parsed = parseTransferSnapshot(text);
    } catch (e) {
      parsed = { ok: false, error: "Invalid JSON." };
    }
    if (!parsed.ok) {
      if (res) res.textContent = parsed.error;
      toastSafe(parsed.error);
      return;
    }
    var v = null;
    try {
      v = validateTransferSnapshot(parsed.snap);
    } catch (e) {
      v = { ok: false, error: "Invalid snapshot." };
    }
    if (!v.ok) {
      if (res) res.textContent = v.error;
      toastSafe(v.error);
      return;
    }
    var counts = null;
    try {
      counts = importTransferSnapshot(parsed.snap, {});
    } catch (e) {
      var msg = (e && e.message) || "Import failed.";
      if (res) res.textContent = msg;
      toastSafe(msg);
      return;
    }
    var summary = summarizeCounts(counts);
    if (res) res.textContent = summary;
    toastSafe(summary, true);
    closeImportDialog();
  }

  function openImportDialog() {
    var ta = el("transfer-import-text");
    if (ta) {
      try { ta.value = ""; } catch (e) { /* best-effort */ }
    }
    var res = el("transfer-import-result");
    if (res) {
      try { res.textContent = ""; } catch (e) { /* best-effort */ }
    }
    wireOnce("stage", "transfer-import-stage", "click", stageImportFromDialog);
    openDialogEl("transfer-import-dialog");
  }

  function closeImportDialog() {
    closeDialogEl("transfer-import-dialog");
  }

  function prettyJsonLines(v) {
    var s;
    try {
      s = JSON.stringify(v, null, 2);
      if (s === undefined) s = String(v);
    } catch (e) {
      s = String(v);
    }
    if (s === null || s === undefined) s = "";
    return String(s).split("\n");
  }

  function diffJsonLines(aLines, bLines) {
    var n = aLines.length, m = bLines.length, i, j;
    if (n * m > 20000) {
      return aLines.map(function (l) { return { op: "-", text: l }; }).concat(
        bLines.map(function (l) { return { op: "+", text: l }; }));
    }
    var dp = [];
    for (i = 0; i <= n; i++) {
      dp.push([]);
      for (j = 0; j <= m; j++) dp[i].push(0);
    }
    for (i = n - 1; i >= 0; i--) {
      for (j = m - 1; j >= 0; j--) {
        dp[i][j] = aLines[i] === bLines[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
    var out = [];
    i = 0;
    j = 0;
    while (i < n && j < m) {
      if (aLines[i] === bLines[j]) { out.push({ op: " ", text: aLines[i] }); i++; j++; }
      else if (dp[i + 1][j] >= dp[i][j + 1]) { out.push({ op: "-", text: aLines[i] }); i++; }
      else { out.push({ op: "+", text: bLines[j] }); j++; }
    }
    while (i < n) { out.push({ op: "-", text: aLines[i] }); i++; }
    while (j < m) { out.push({ op: "+", text: bLines[j] }); j++; }
    return out;
  }

  function diffLineHTML(op, text) {
    var cls = op === "+" ? "add" : op === "-" ? "del" : "ctx";
    var body = text === "" ? "&nbsp;" : CW.esc(text);
    return '<span class="diff-line ' + cls + '"><span class="diff-gutter">' + op +
      "</span>" + body + "</span>";
  }

  function opsForRow(row) {
    if (row.status === "added") {
      return prettyJsonLines(row.new).map(function (l) { return { op: "+", text: l }; });
    }
    if (row.status === "removed") {
      return prettyJsonLines(row.old).map(function (l) { return { op: "-", text: l }; });
    }
    return diffJsonLines(prettyJsonLines(row.old), prettyJsonLines(row.new));
  }

  function hunkHTML(ops) {
    return '<pre class="diff-hunk">' +
      ops.map(function (o) { return diffLineHTML(o.op, o.text); }).join("") +
      "</pre>";
  }

  function fileHeadHTML(status, path, extra) {
    return '<div class="diff-file-head"><span class="diff-badge">' + CW.esc(status) + "</span>" +
      '<span class="diff-path">' + CW.esc(path) + "</span>" + (extra || "") + "</div>";
  }

  function diffFileHTML(kind, label, row) {
    return '<div class="diff-file diff-' + row.status + '">' +
      fileHeadHTML(row.status, kind + "/" + label) +
      hunkHTML(opsForRow(row)) + "</div>";
  }

  function flagChangedFields(row) {
    var fields = ["type", "default", "group", "description", "rules"];
    var out = [];
    if (!row || row.status !== "modified") return out;
    var a = row.old || {}, b = row.new || {}, i, av, bv;
    for (i = 0; i < fields.length; i++) {
      try { av = JSON.stringify(a[fields[i]]); } catch (e) { av = ""; }
      try { bv = JSON.stringify(b[fields[i]]); } catch (e2) { bv = ""; }
      if (av !== bv) out.push(fields[i]);
    }
    return out;
  }

  function fieldBadgesHTML(fields) {
    return fields.map(function (f) {
      return '<span class="diff-field">' + CW.esc(f) + "</span>";
    }).join("");
  }

  function expFlagKey(row) {
    var src = (row && row.new) || (row && row.old) || {};
    return typeof src.flag === "string" ? src.flag : "";
  }

  function diffFlagBlock(flagKey, flagRow, expRows) {
    var path = flagKey ? "flags/" + flagKey : "experiments";
    var nest = expRows.length
      ? '<div class="diff-nest">' + expRows.map(function (row) {
        return diffFileHTML("experiments", expDisplayLabel(row), row);
      }).join("") + "</div>"
      : "";
    if (!flagRow) {
      var n = expRows.length;
      return '<div class="diff-file diff-flag-exps">' +
        '<div class="diff-file-head"><span class="diff-badge">' +
        CW.esc(n + (n === 1 ? " experiment" : " experiments")) + "</span>" +
        '<span class="diff-path">' + CW.esc(path) + "</span></div>" + nest + "</div>";
    }
    var extra = fieldBadgesHTML(flagChangedFields(flagRow)) +
      (expRows.length ? '<span class="diff-field">experiments</span>' : "");
    return '<div class="diff-file diff-' + flagRow.status + '">' +
      fileHeadHTML(flagRow.status, path, extra) +
      hunkHTML(opsForRow(flagRow)) + nest + "</div>";
  }

  function changedRows(rows) {
    return (rows || []).filter(function (r) { return r && r.status !== "unchanged"; });
  }

  function expDisplayLabel(row) {
    var src = (row && row.new) || (row && row.old) || {};
    var nm = typeof src.name === "string" && src.name ? src.name : "";
    var fl = typeof src.flag === "string" && src.flag ? src.flag : "";
    if (nm && fl) return fl + "/" + nm;
    if (nm) return nm;
    return (row && row.id) || "";
  }

  function stagedBucketList(kind) {
    var out = [];
    try {
      if (!CW.state || !CW.state.drafts || typeof CW.state.drafts !== "object") return out;
      var bucket = CW.state.drafts[kind];
      if (!bucket || typeof bucket !== "object") return out;
      Object.keys(bucket).forEach(function (k) {
        var e = bucket[k] || {};
        out.push({ key: k, op: e.op || "", label: e.label || k });
      });
    } catch (e) { /* best-effort */ }
    return out;
  }

  function stagedDraftsHTML() {
    var kinds = ["flag", "rule", "experiment", "group"];
    var total = 0;
    var rows = kinds.map(function (kind) {
      return stagedBucketList(kind).map(function (it) {
        total++;
        return '<li class="staged-' + CW.esc(it.op) + '"><span class="diff-badge">' +
          CW.esc(it.op || "staged") + "</span>" +
          '<span class="diff-path">' + CW.esc(kind + "/" + it.label) + "</span></li>";
      }).join("");
    }).join("");
    return "<h4>Staged drafts (" + total + ")</h4>" +
      (total ? '<ul class="staged-list">' + rows + "</ul>" : '<p class="muted">No staged drafts.</p>');
  }

  function diffStatText(d) {
    var c = (d && d.counts) || {};
    var add = c.added || 0, del = c.removed || 0, mod = c.modified || 0, un = c.unchanged || 0;
    return (add + del + mod) + " changed (" + add + " added, " + mod +
      " modified, " + del + " removed, " + un + " unchanged)";
  }

  function openDraftSnapshotDialog() {
    var live = null, merged = null;
    try {
      live = buildTransferSnapshot({ from: "live" });
    } catch (e) {
      live = { flags: [], experiments: [] };
    }
    try {
      merged = buildTransferSnapshot({ from: "merged" });
    } catch (e) {
      merged = { flags: [], experiments: [] };
    }
    var d = null;
    try {
      d = diffTransferSnapshots(live, merged);
    } catch (e) {
      d = null;
    }
    var box = el("transfer-diff-detail");
    if (box) {
      var html = "";
      if (!d) {
        html = "<p>Diff unavailable.</p>";
      } else {
        var frows = changedRows(d.flags);
        var xrows = changedRows(d.experiments);
        var expsByFlag = {};
        xrows.forEach(function (row) {
          var fk = expFlagKey(row);
          if (!hasOwn(expsByFlag, fk)) expsByFlag[fk] = [];
          expsByFlag[fk].push(row);
        });
        var flagsByKey = {};
        frows.forEach(function (row) { flagsByKey[row.key] = row; });
        var order = [];
        var seenK = {};
        frows.forEach(function (row) { order.push(row.key); seenK[row.key] = true; });
        Object.keys(expsByFlag).sort().forEach(function (fk) {
          if (!hasOwn(seenK, fk)) { order.push(fk); seenK[fk] = true; }
        });
        var blocks = order.map(function (fk) {
          return diffFlagBlock(fk, hasOwn(flagsByKey, fk) ? flagsByKey[fk] : null, expsByFlag[fk] || []);
        }).join("");
        html = '<p class="diff-stat">' + CW.esc(diffStatText(d)) + "</p>" +
          "<h4>Flags</h4>" +
          (blocks ? blocks : '<p class="muted">No changes.</p>') +
          stagedDraftsHTML();
      }
      try { box.innerHTML = html; } catch (e) { /* best-effort */ }
    }
    openDialogEl("transfer-diff-dialog");
  }

  function closeDraftSnapshotDialog() {
    closeDialogEl("transfer-diff-dialog");
  }

  CW.buildTransferSnapshot = buildTransferSnapshot;
  CW.parseTransferSnapshot = parseTransferSnapshot;
  CW.validateTransferSnapshot = validateTransferSnapshot;
  CW.importTransferSnapshot = importTransferSnapshot;
  CW.diffTransferSnapshots = diffTransferSnapshots;
  CW.releaseSnapshotToTransfer = releaseSnapshotToTransfer;
  CW.exportReleaseSnapshot = exportReleaseSnapshot;
  CW.openExportDialog = openExportDialog;
  CW.openImportDialog = openImportDialog;
  CW.openDraftSnapshotDialog = openDraftSnapshotDialog;
  CW.closeExportDialog = closeExportDialog;
  CW.closeImportDialog = closeImportDialog;
  CW.closeDraftSnapshotDialog = closeDraftSnapshotDialog;
})();
