/* ConfigWire admin shell — releases.js (releases + publish + rollback). */
(function () {
  "use strict";

  var CW = window.CW;

  function renderReleases() {
    var list = CW.$("release-list");
    if (!list) return;
    if (!CW.state.releases.length) { list.innerHTML = "<li>No releases for this env.</li>"; return; }
    if (typeof CW.state.releasesExpanded === "undefined") CW.state.releasesExpanded = false;
    var expanded = !!CW.state.releasesExpanded;
    var visible = expanded ? CW.state.releases : CW.state.releases.slice(0, 3);
    var html = visible.map(function (r, idx) {
      var base = "<li>v" + CW.esc(r.version) + " etag " + CW.esc(r.etag) +
        (r.note ? " — " + CW.esc(r.note) : "");
      if (idx === 0) {
        return base + ' <span class="badge ok">current</span>' +
          ' <button type="button" data-view-release="' + CW.esc(r.id) + '">view</button></li>';
      }
      return base +
        ' <button type="button" data-view-release="' + CW.esc(r.id) + '">view</button>' +
        ' <button type="button" data-rollback-version="' + CW.esc(r.version) + '">rollback to v' +
        CW.esc(r.version) + "</button></li>";
    }).join("");
    if (CW.state.releases.length > 3) {
      var hidden = CW.state.releases.length - 3;
      html += '<li><button type="button" id="releases-toggle">' +
        (expanded ? "show less" : "show " + hidden + " more") + "</button></li>";
    }
    list.innerHTML = html;
    var tog = CW.$("releases-toggle");
    if (tog) {
      tog.addEventListener("click", function () { toggleReleasesExpanded(); });
    }
  }

  function toggleReleasesExpanded() {
    if (typeof CW.state.releasesExpanded === "undefined") CW.state.releasesExpanded = false;
    CW.state.releasesExpanded = !CW.state.releasesExpanded;
    renderReleases();
  }

  function findReleaseById(id) {
    var items = CW.state.releases || [];
    for (var i = 0; i < items.length; i++) {
      if (String(items[i].id) === String(id)) return items[i];
    }
    return null;
  }

  function openReleaseDialog(releaseOrId) {
    var rec = null;
    if (releaseOrId && typeof releaseOrId === "object") {
      rec = releaseOrId;
    } else {
      rec = findReleaseById(releaseOrId);
    }
    if (!rec) return;
    var dlg = CW.$("release-dialog");
    if (!dlg) return;
    var title = CW.$("release-dialog-title");
    if (title) title.textContent = "Release v" + rec.version;
    var dl = CW.$("release-detail");
    if (dl) {
      dl.innerHTML = "";
      var keys = Object.keys(rec);
      for (var i = 0; i < keys.length; i++) {
        var k = keys[i];
        if (k === "expand" || k === "collectionId" || k === "collectionName") continue;
        var dt = document.createElement("dt");
        dt.textContent = k;
        dl.appendChild(dt);
        var dd = document.createElement("dd");
        var val = rec[k];
        if (val !== null && typeof val === "object") {
          var pre = document.createElement("pre");
          var code = document.createElement("code");
          try {
            code.textContent = JSON.stringify(val, null, 2);
          } catch (e) {
            code.textContent = String(val);
          }
          pre.appendChild(code);
          dd.appendChild(pre);
        } else {
          dd.textContent = (val === undefined || val === null) ? "" : String(val);
        }
        dl.appendChild(dd);
      }
    }
    if (dlg.open) return;
    try {
      if (typeof dlg.showModal === "function") dlg.showModal();
      else dlg.setAttribute("open", "");
    } catch (e) { /* already open */ }
  }

  function closeReleaseDialog() {
    var dlg = CW.$("release-dialog");
    if (dlg && dlg.open) dlg.close();
  }

  function latestVersion() {
    return CW.state.releases.reduce(function (m, r) {
      return r.version > m ? r.version : m;
    }, 0);
  }

  var UNPUBLISHED_KINDS = ["flag", "rule", "experiment", "group"];
  var UNPUBLISHED_LS_PREFIX = "cw_unpublished_";

  function ensureUnpublishedShape() {
    if (!CW.state.unpublished || typeof CW.state.unpublished !== "object") {
      CW.state.unpublished = { flag: {}, rule: {}, experiment: {}, group: {} };
    }
    for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
      var k = UNPUBLISHED_KINDS[i];
      if (!CW.state.unpublished[k] || typeof CW.state.unpublished[k] !== "object") {
        CW.state.unpublished[k] = {};
      }
    }
    return CW.state.unpublished;
  }

  function unpublishedScopeKey() {
    var scope = "";
    try {
      scope = CW.state.envId || (CW.envSlug ? CW.envSlug() : "") || "";
    } catch (e) { scope = CW.state.envId || ""; }
    return UNPUBLISHED_LS_PREFIX + scope;
  }

  function persistUnpublished() {
    try {
      ensureUnpublishedShape();
      localStorage.setItem(unpublishedScopeKey(), JSON.stringify(CW.state.unpublished));
    } catch (e) { /* private mode / quota: memory copy still works */ }
  }

  function restoreUnpublished() {
    ensureUnpublishedShape();
    var raw = null;
    try { raw = localStorage.getItem(unpublishedScopeKey()); } catch (e) { raw = null; }
    if (!raw) return CW.state.unpublished;
    try {
      var parsed = JSON.parse(raw);
      if (parsed && typeof parsed === "object") {
        for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
          var k = UNPUBLISHED_KINDS[i];
          if (parsed[k] && typeof parsed[k] === "object") {
            CW.state.unpublished[k] = parsed[k];
          } else {
            CW.state.unpublished[k] = {};
          }
        }
      }
    } catch (e) { /* corrupt entry: keep empty shape */ }
    return CW.state.unpublished;
  }

  // Draft signal (Unit A store): CW.state.drafts = { flag:{}, rule:{},
  // experiment:{}, group:{} }, entry { op, body, baseId, tempId, label, at }.
  // When Unit A's module (CW.drafts) is loaded, drafts ARE the unpublished
  // signal and legacy marker buckets are ignored. Before drafts.js lands
  // (or if it ever unloads), fall back to the legacy marker buckets so the
  // publish chrome never goes blank. All guards are call-time (never parse
  // time) so script order between drafts.js and releases.js does not matter.
  function useDraftSignal() {
    return !!(CW.state && CW.state.drafts && typeof CW.state.drafts === "object" && CW.drafts);
  }

  // Temp-id test is pure shape: the "draft-" prefix only. Never consult draft
  // membership here — every staged update is keyed by its live id, so a
  // membership test would misreport live ids as temp (and break live-id
  // resolution in draftLiveId plus the "(new)" label below).
  function isDraftTempId(kind, id) {
    return typeof id === "string" && id.indexOf("draft-") === 0;
  }

  function draftBucketEntries(kind) {
    if (!CW.state || !CW.state.drafts || typeof CW.state.drafts !== "object") return [];
    var bucket = CW.state.drafts[kind];
    if (!bucket || typeof bucket !== "object") return [];
    var out = [];
    var keys = Object.keys(bucket);
    for (var i = 0; i < keys.length; i++) {
      out.push({ key: keys[i], entry: bucket[keys[i]] || {} });
    }
    return out;
  }

  function draftSummary() {
    var out = { flag: 0, rule: 0, experiment: 0, group: 0, total: 0 };
    for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
      var k = UNPUBLISHED_KINDS[i];
      var n = draftBucketEntries(k).length;
      out[k] = n;
      out.total += n;
    }
    return out;
  }

  function unpublishedSummary() {
    if (useDraftSignal()) return draftSummary();
    ensureUnpublishedShape();
    var out = { flag: 0, rule: 0, experiment: 0, group: 0, total: 0 };
    for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
      var k = UNPUBLISHED_KINDS[i];
      var n = Object.keys(CW.state.unpublished[k] || {}).length;
      out[k] = n;
      out.total += n;
    }
    return out;
  }

  function plural(n, one, many) {
    return n + " " + (n === 1 ? one : many);
  }

  function summaryText() {
    var s = unpublishedSummary();
    var parts = [];
    if (s.flag) parts.push(plural(s.flag, "flag", "flags"));
    if (s.rule) parts.push(plural(s.rule, "rule", "rules"));
    if (s.experiment) parts.push(plural(s.experiment, "experiment", "experiments"));
    if (s.group) parts.push(plural(s.group, "group", "groups"));
    return parts.join(" \u2022 ");
  }

  function ensureUnpublishedListEl() {
    var list = CW.$("unpublished-list");
    if (list) return list;
    var hint = CW.$("publish-hint");
    list = document.createElement("ul");
    list.id = "unpublished-list";
    list.setAttribute("aria-label", "Unpublished changes");
    if (hint && hint.parentNode) {
      hint.parentNode.insertBefore(list, hint.nextSibling);
    } else {
      var form = CW.$("publish-form");
      if (form && form.parentNode) form.parentNode.insertBefore(list, form);
    }
    return list;
  }

  function renderUnpublishedList() {
    var list = ensureUnpublishedListEl();
    if (!list) return;
    var rows = [];
    if (useDraftSignal()) {
      for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
        var kind = UNPUBLISHED_KINDS[i];
        var items = draftBucketEntries(kind);
        for (var j = 0; j < items.length; j++) {
          var entry = items[j].entry;
          var label = entry.label || items[j].key;
          var isNew = isDraftTempId(kind, items[j].key) || isDraftTempId(kind, entry.tempId);
          var isDel = entry.op === "delete";
          rows.push('<li data-unpublished-kind="' + CW.esc(kind) + '" data-unpublished-id="' +
            CW.esc(items[j].key) + '" data-op="' + CW.esc(entry.op || "") + '">' + CW.esc(kind) + ": " + CW.esc(label) +
            (isNew ? " (new)" : (isDel ? " (deleted)" : "")) + "</li>");
        }
      }
    } else {
      ensureUnpublishedShape();
      for (var m = 0; m < UNPUBLISHED_KINDS.length; m++) {
        var lkind = UNPUBLISHED_KINDS[m];
        var bucket = CW.state.unpublished[lkind] || {};
        var ids = Object.keys(bucket);
        for (var n = 0; n < ids.length; n++) {
          var bentry = bucket[ids[n]] || {};
          var blabel = bentry.label || ids[n];
          rows.push('<li data-unpublished-kind="' + CW.esc(lkind) + '" data-unpublished-id="' +
            CW.esc(ids[n]) + '">' + CW.esc(lkind) + ": " + CW.esc(blabel) + "</li>");
        }
      }
    }
    if (!rows.length) {
      list.innerHTML = "";
      list.hidden = true;
      return;
    }
    list.hidden = false;
    list.innerHTML = rows.join("");
  }

  function updatePublishNavBadge() {
    var dirty = !!CW.state.unpublishedChanges;
    var link = null;
    try { link = document.querySelector('a[href="#/p/publish"]'); } catch (e) { link = null; }
    if (!link) return;
    var badge = link.querySelector ? link.querySelector(".badge.unpublished") : null;
    if (dirty) {
      if (!badge) {
        badge = document.createElement("span");
        badge.className = "badge unpublished";
        badge.textContent = "Unpublished";
        link.appendChild(badge);
      } else {
        badge.textContent = "Unpublished";
      }
    } else if (badge && badge.parentNode) {
      badge.parentNode.removeChild(badge);
    }
  }

  function updateDirtyHighlights() {
    var dirty = !!CW.state.unpublishedChanges;
    var s = unpublishedSummary();
    var rel = null;
    try { rel = document.querySelector('section[aria-labelledby="releases"]'); } catch (e) { rel = null; }
    if (rel && rel.classList) rel.classList.toggle("is-dirty", dirty);
    var flagsSec = null;
    try { flagsSec = document.querySelector('section[aria-labelledby="flags"]'); } catch (e) { flagsSec = null; }
    if (flagsSec && flagsSec.classList) {
      flagsSec.classList.toggle("has-unpublished", (s.flag + s.rule + s.group) > 0);
    }
    var expSec = null;
    try { expSec = document.querySelector('section[aria-labelledby="experiments"]'); } catch (e) { expSec = null; }
    if (expSec && expSec.classList) {
      expSec.classList.toggle("has-unpublished", s.experiment > 0);
    }
  }

  function setPublishState(dirty) {
    CW.state.unpublishedChanges = !!dirty;
    var form = CW.$("publish-form");
    var btn = form ? form.querySelector('button[type="submit"]') : null;
    if (btn) btn.disabled = !dirty;
    var discard = CW.$("discard-unpublished");
    if (discard) discard.disabled = !dirty;
    var hint = CW.$("publish-hint");
    if (hint) {
      if (dirty) {
        var detail = summaryText();
        hint.textContent = detail
          ? "Unpublished changes \u2014 publish to release (" + detail + ")"
          : "Unpublished changes \u2014 publish to release";
      } else {
        hint.textContent = "No unpublished changes";
      }
      if (hint.classList) hint.classList.toggle("is-dirty", !!dirty);
    }
    renderUnpublishedList();
    updatePublishNavBadge();
    updateDirtyHighlights();
  }

  function markUnpublished(kind, id, label) {
    // Zero-arg path: publish-failure re-dirty relies on CW.markUnpublished().
    if (kind === undefined || kind === null || kind === "") {
      setPublishState(true);
      return;
    }
    var valid = false;
    for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
      if (UNPUBLISHED_KINDS[i] === kind) { valid = true; break; }
    }
    if (!valid || id === undefined || id === null || id === "") {
      setPublishState(true);
      return;
    }
    ensureUnpublishedShape();
    var key = String(id);
    CW.state.unpublished[kind][key] = { label: label === undefined || label === null ? key : String(label), at: Date.now() };
    persistUnpublished();
    setPublishState(true);
  }

  function isUnpublished(kind, id) {
    ensureUnpublishedShape();
    if (!kind || !isKnownKind(kind)) return false;
    if (id === undefined || id === null || id === "") return false;
    return Object.prototype.hasOwnProperty.call(CW.state.unpublished[kind] || {}, String(id));
  }

  function isKnownKind(kind) {
    for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
      if (UNPUBLISHED_KINDS[i] === kind) return true;
    }
    return false;
  }

  function clearUnpublished() {
    CW.state.unpublished = { flag: {}, rule: {}, experiment: {}, group: {} };
    try { localStorage.removeItem(unpublishedScopeKey()); } catch (e) { /* private mode */ }
  }

  function clearDraftStore() {
    if (CW.drafts && typeof CW.drafts.draftClearAll === "function") {
      try { CW.drafts.draftClearAll(); return; } catch (e) { /* fall through to shape reset */ }
    }
    if (CW.state) {
      try { CW.state.drafts = { flag: {}, rule: {}, experiment: {}, group: {} }; } catch (e2) { /* memory-only */ }
    }
  }

  function markPublished() {
    var base = CW.$("publish-base");
    if (base) base.value = latestVersion();
    clearUnpublished();
    clearDraftStore();
    if (CW.drafts && typeof CW.drafts.refreshDraftChrome === "function") {
      try { CW.drafts.refreshDraftChrome(); }
      catch (e) { setPublishState(false); }
    } else {
      setPublishState(false);
    }
  }

  function setApplying(applying) {
    CW.state.applying = !!applying;
    var dis = !!applying;
    var form = CW.$("publish-form");
    var btn = form ? form.querySelector('button[type="submit"]') : null;
    if (btn) btn.disabled = dis ? true : !CW.state.unpublishedChanges;
    var discard = CW.$("discard-unpublished");
    if (discard) discard.disabled = dis ? true : !CW.state.unpublishedChanges;
    var rb = null;
    try { rb = document.querySelectorAll('button[data-rollback-version]'); } catch (e) { rb = null; }
    if (rb) {
      for (var i = 0; i < rb.length; i++) {
        try { rb[i].disabled = dis; } catch (e2) { /* best-effort */ }
      }
    }
  }

  function applyOk(out, codes) {
    for (var i = 0; i < codes.length; i++) {
      if (out.status === codes[i]) return true;
    }
    return false;
  }

  function applyReject(out, name) {
    return Promise.reject({ status: out.status, msg: CW.serverMessage(out.data), name: name || "APPLY_HTTP" });
  }

  function normDraftOp(kind, op, key, entry) {
    var o = String(op == null ? "" : op).toLowerCase();
    if (o === "create" || o === "add" || o === "post") return "create";
    if (o === "update" || o === "edit" || o === "patch" || o === "rename" ||
        o === "move" || o === "status") return "update";
    if (o === "delete" || o === "remove" || o === "del" || o === "destroy") return "delete";
    if (isDraftTempId(kind, key) || (entry && isDraftTempId(kind, entry.tempId))) {
      if (entry && entry.deleted) return "delete";
      return "create";
    }
    if (entry && entry.baseId && !isDraftTempId(kind, entry.baseId)) return "update";
    return "update";
  }

  function draftLiveId(kind, key, entry) {
    var base = entry && entry.baseId ? String(entry.baseId) : "";
    if (base && !isDraftTempId(kind, base)) return base;
    if (!isDraftTempId(kind, key)) return String(key);
    return "";
  }

  function draftTempId(key, entry) {
    if (entry && entry.tempId) return String(entry.tempId);
    return String(key);
  }

  function resolveFlagRef(ref, tempMap) {
    var s = ref == null ? "" : String(ref);
    if (!s) return "";
    if (Object.prototype.hasOwnProperty.call(tempMap, s)) return tempMap[s];
    if (typeof s === "string" && s.indexOf("draft-") === 0) return null;
    if (CW.drafts && typeof CW.drafts.resolveFlagId === "function") {
      try {
        var r = CW.drafts.resolveFlagId(s);
        if (r) return String(r);
      } catch (e) { /* fall through to live passthrough */ }
    }
    return s;
  }

  function remapGroupRef(g, tempMap) {
    var s = g == null ? "" : String(g);
    if (!s) return s;
    if (Object.prototype.hasOwnProperty.call(tempMap, s)) return tempMap[s];
    return s;
  }

  function liveGroupMembers(gid) {
    var out = [];
    var flags = (CW.state && Array.isArray(CW.state.flags)) ? CW.state.flags : [];
    for (var i = 0; i < flags.length; i++) {
      if (flags[i] && flags[i].group === gid && flags[i].id) out.push(flags[i].id);
    }
    return out;
  }

  function buildApplySteps() {
    var steps = [];
    function pushPhase(kind, order) {
      var items = draftBucketEntries(kind);
      var ranked = [];
      for (var i = 0; i < items.length; i++) {
        var op = normDraftOp(kind, items[i].entry.op, items[i].key, items[i].entry);
        var rank = order.indexOf(op);
        if (rank < 0) rank = order.length;
        ranked.push({ item: items[i], op: op, rank: rank, idx: i });
      }
      ranked.sort(function (a, b) { return (a.rank - b.rank) || (a.idx - b.idx); });
      for (var j = 0; j < ranked.length; j++) {
        var e = ranked[j].item.entry;
        steps.push({
          kind: kind,
          key: ranked[j].item.key,
          entry: e,
          op: ranked[j].op,
          label: e.label || ranked[j].item.key,
        });
      }
    }
    pushPhase("group", ["create", "update", "delete"]);
    pushPhase("flag", ["create", "update", "delete"]);
    pushPhase("rule", ["create", "update", "delete"]);
    pushPhase("experiment", ["create", "update", "delete"]);
    return steps;
  }

  function runApplyStep(s, tempMap) {
    var entry = s.entry || {};
    var body = entry.body && typeof entry.body === "object" ? entry.body : {};
    function clone(o) {
      var c = {};
      for (var k in o) {
        if (Object.prototype.hasOwnProperty.call(o, k)) c[k] = o[k];
      }
      return c;
    }
    if (s.kind === "group") {
      if (s.op === "create") {
        return CW.apiMut("POST", "/api/collections/groups/records", body).then(function (out) {
          if (!applyOk(out, [200, 201])) return applyReject(out, "GROUP_CREATE");
          if (out.data && out.data.id) tempMap[draftTempId(s.key, entry)] = out.data.id;
          return out;
        });
      }
      if (s.op === "update") {
        var gid = draftLiveId(s.kind, s.key, entry);
        if (!gid) return Promise.reject({ status: 0, msg: "group rename without live id", name: "GROUP_NO_ID" });
        var gbody = { name: body.name !== undefined ? body.name : entry.label };
        return CW.apiMut("PATCH", "/api/collections/groups/records/" + encodeURIComponent(gid), gbody).then(function (out) {
          if (!applyOk(out, [200, 201])) return applyReject(out, "GROUP_RENAME");
          return out;
        });
      }
      var dgid = draftLiveId(s.kind, s.key, entry);
      if (!dgid) return Promise.resolve({ status: 0, data: null });
      var members = liveGroupMembers(dgid);
      var clear = Promise.resolve();
      members.forEach(function (fid) {
        clear = clear.then(function () {
          return CW.apiMut("PATCH", "/api/collections/flags/records/" + encodeURIComponent(fid), { group: null }).then(
            function () {},
            function () {}
          );
        });
      });
      return clear.then(function () {
        return CW.apiMut("DELETE", "/api/collections/groups/records/" + encodeURIComponent(dgid));
      }).then(function (out) {
        if (!applyOk(out, [200, 201, 204])) return applyReject(out, "GROUP_DELETE");
        return out;
      });
    }
    if (s.kind === "flag") {
      if (s.op === "create") {
        var fbody = clone(body);
        if (fbody.group !== undefined && fbody.group !== null && fbody.group !== "") {
          fbody.group = remapGroupRef(fbody.group, tempMap);
        }
        return CW.apiMut("POST", "/api/collections/flags/records", fbody).then(function (out) {
          if (!applyOk(out, [200, 201])) return applyReject(out, "FLAG_CREATE");
          if (out.data && out.data.id) tempMap[draftTempId(s.key, entry)] = out.data.id;
          return out;
        });
      }
      if (s.op === "update") {
        var fid = draftLiveId(s.kind, s.key, entry);
        if (!fid) return Promise.reject({ status: 0, msg: "flag update without live id", name: "FLAG_NO_ID" });
        var ubody = clone(body);
        if (ubody.group !== undefined && ubody.group !== null && ubody.group !== "") {
          ubody.group = remapGroupRef(ubody.group, tempMap);
        }
        return CW.apiMut("PATCH", "/api/collections/flags/records/" + encodeURIComponent(fid), ubody).then(function (out) {
          if (!applyOk(out, [200, 201])) return applyReject(out, "FLAG_UPDATE");
          return out;
        });
      }
      var dfid = draftLiveId(s.kind, s.key, entry);
      if (!dfid) return Promise.resolve({ status: 0, data: null });
      return CW.apiMut("DELETE", "/api/collections/flags/records/" + encodeURIComponent(dfid)).then(function (out) {
        if (!applyOk(out, [200, 201, 204])) return applyReject(out, "FLAG_DELETE");
        return out;
      });
    }
    if (s.kind === "rule") {
      if (s.op === "delete") {
        var drid = draftLiveId(s.kind, s.key, entry);
        if (!drid) return Promise.resolve({ status: 0, data: null });
        return CW.apiMut("DELETE", "/api/collections/rules/records/" + encodeURIComponent(drid)).then(function (out) {
          if (!applyOk(out, [200, 201, 204])) return applyReject(out, "RULE_DELETE");
          return out;
        });
      }
      var rbody = clone(body);
      var rflag = resolveFlagRef(rbody.flag, tempMap);
      if (rflag === null) {
        return Promise.reject({ status: 0, msg: "rule staged against a discarded draft flag (" + rbody.flag + ")", name: "DANGLING_TEMP" });
      }
      rbody.flag = rflag;
      var rid = draftLiveId(s.kind, s.key, entry);
      var isNew = s.op === "create" || !rid;
      if (isNew) {
        return CW.apiMut("POST", "/api/collections/rules/records", rbody).then(function (out) {
          if (!applyOk(out, [200, 201])) return applyReject(out, "RULE_CREATE");
          return out;
        });
      }
      return CW.apiMut("PATCH", "/api/collections/rules/records/" + encodeURIComponent(rid), rbody).then(function (out) {
        if (!applyOk(out, [200, 201])) return applyReject(out, "RULE_UPDATE");
        return out;
      });
    }
    var xbody = clone(body);
    if (s.op === "delete") {
      var dxid = draftLiveId(s.kind, s.key, entry);
      if (!dxid) return Promise.resolve({ status: 0, data: null });
      return CW.apiMut("DELETE", "/api/collections/experiments/records/" + encodeURIComponent(dxid)).then(function (out) {
        if (!applyOk(out, [200, 201, 204])) return applyReject(out, "EXPERIMENT_DELETE");
        return out;
      });
    }
    if (xbody.flag !== undefined && xbody.flag !== null && String(xbody.flag) !== "") {
      var xflag = resolveFlagRef(xbody.flag, tempMap);
      if (xflag === null) {
        return Promise.reject({ status: 0, msg: "experiment staged against a discarded draft flag (" + xbody.flag + ")", name: "DANGLING_TEMP" });
      }
      xbody.flag = xflag;
    } else {
      delete xbody.flag;
    }
    var xid = draftLiveId(s.kind, s.key, entry);
    if (s.op === "create" || !xid) {
      return CW.apiMut("POST", "/api/collections/experiments/records", xbody).then(function (out) {
        if (!applyOk(out, [200, 201])) return applyReject(out, "EXPERIMENT_CREATE");
        return out;
      });
    }
    return CW.apiMut("PATCH", "/api/collections/experiments/records/" + encodeURIComponent(xid), xbody).then(function (out) {
      if (!applyOk(out, [200, 201])) return applyReject(out, "EXPERIMENT_UPDATE");
      return out;
    });
  }

  function countDrafts() {
    return draftSummary().total;
  }

  function consumeDraft(kind, key) {
    if (CW.drafts && typeof CW.drafts.draftDiscard === "function") {
      try { CW.drafts.draftDiscard(kind, key); return; } catch (e) { /* fall through */ }
    }
    try {
      if (CW.state && CW.state.drafts && CW.state.drafts[kind]) {
        delete CW.state.drafts[kind][key];
      }
    } catch (e2) { /* memory-only */ }
    try {
      if (CW.drafts && typeof CW.drafts.persistDrafts === "function") CW.drafts.persistDrafts();
    } catch (e3) { /* kept in memory */ }
  }

  function rerenderMerged() {
    if (CW.drafts && typeof CW.drafts.refreshDraftChrome === "function") {
      try { CW.drafts.refreshDraftChrome(); } catch (e) { /* best-effort */ }
    }
    try { if (CW.renderFlags) CW.renderFlags(); } catch (e2) { /* best-effort */ }
    try { if (CW.renderFlagRulesList && CW.state && CW.state.activeFlagRulesId) CW.renderFlagRulesList(); } catch (e3) { /* best-effort */ }
    try { if (CW.renderExperiments) CW.renderExperiments(); } catch (e4) { /* best-effort */ }
    try { renderUnpublishedList(); updatePublishNavBadge(); updateDirtyHighlights(); } catch (e5) { /* best-effort */ }
  }

  function applyDrafts() {
    if (!useDraftSignal()) return Promise.resolve({ applied: 0, total: 0, noop: true });
    var steps = buildApplySteps();
    var total = steps.length;
    if (!total) return Promise.resolve({ applied: 0, total: 0 });
    var tempMap = {};
    var idx = 0;
    function fail(s, stepNo, status, msg, name) {
      try {
        if (CW.drafts && typeof CW.drafts.persistDrafts === "function") CW.drafts.persistDrafts();
      } catch (e) { /* kept in memory */ }
      rerenderMerged();
      var kept = countDrafts();
      var err = new Error("draft apply failed at " + s.kind + " " + s.label +
        " (step " + stepNo + "/" + total + "): HTTP " + status + " \u2014 " + msg +
        ". " + kept + (kept === 1 ? " draft" : " drafts") + " kept; fix and retry Publish.");
      err.draftKind = s.kind;
      err.draftLabel = s.label;
      err.step = stepNo;
      err.total = total;
      err.status = status;
      err.kept = kept;
      if (name) err.code = name;
      return Promise.reject(err);
    }
    function next() {
      if (idx >= steps.length) return Promise.resolve({ applied: idx, total: total });
      var s = steps[idx];
      var stepNo = idx + 1;
      var p = null;
      try {
        p = runApplyStep(s, tempMap);
      } catch (e) {
        return fail(s, stepNo, 0, (e && e.message) || "apply error", "APPLY_THROW");
      }
      return p.then(function () {
        consumeDraft(s.kind, s.key);
        idx++;
        return next();
      }, function (e) {
        var st = (e && typeof e.status === "number") ? e.status : 0;
        var m = (e && (e.msg || e.message)) || "request failed";
        return fail(s, stepNo, st, m, e && e.name);
      });
    }
    return next();
  }

  // Per-form dirty gating (publish-global CW.state.unpublishedChanges is
  // separate: one form's save must not clear another form's dirty, and only
  // publish-success clears the publish-global flag). armDirtyForm disables
  // the form's primary submit until the first input/change bubbles from a
  // child field; markFormClean returns it to disabled after a save or Clear.
  // Programmatic pre-fills (dialog edit, form.reset()) fire no input/change
  // events, so opening a dialog never counts as dirty by itself.
  function setFormDirty(formId, dirty) {
    if (!CW.state.dirtyForms) CW.state.dirtyForms = {};
    CW.state.dirtyForms[formId] = !!dirty;
    var form = CW.$(formId);
    var btn = form ? form.querySelector('button[type="submit"]') : null;
    if (btn) btn.disabled = !dirty;
  }

  function markFormDirty(formId) {
    setFormDirty(formId, true);
  }

  function markFormClean(formId) {
    setFormDirty(formId, false);
  }

  function armDirtyForm(formId) {
    var form = CW.$(formId);
    if (!form) return;
    markFormClean(formId);
    if (form.getAttribute("data-dirty-armed")) return;
    form.setAttribute("data-dirty-armed", "1");
    form.addEventListener("input", function () { markFormDirty(formId); });
    form.addEventListener("change", function () { markFormDirty(formId); });
  }

  function loadReleases() {
    // Fetch all then filter client-side by selected env relation; sort -version.
    return CW.apiAll("/api/collections/releases/records?perPage=200&sort=-version").then(function (items) {
      items = items || [];
      if (CW.state.envId) items = items.filter(function (r) { return r.env === CW.state.envId; });
      CW.state.releases = items.slice().sort(function (a, b) { return b.version - a.version; });
      renderReleases();
      // Publish form auto-fills baseVersion from the latest version so the
      // first submit never goes stale (no 409 on first try by default).
      CW.$("publish-base").value = latestVersion();
      // Fresh load means clean, unless a persisted per-env map survives reload.
      restoreUnpublished();
      if (unpublishedSummary().total > 0) setPublishState(true);
      else { clearUnpublished(); setPublishState(false); }
    });
  }

  function publish(note, baseVersion) {
    var url = "/api/v1/admin/env/" + encodeURIComponent(CW.envSlug()) + "/publish";
    if (CW.state.projectId) url += "?project=" + encodeURIComponent(CW.state.projectId);
    return fetch(url, {
      method: "POST",
      headers: Object.assign({ "Content-Type": "application/json" }, CW.authHeaders()),
      body: JSON.stringify({ note: note || "", baseVersion: baseVersion }),
    }).then(function (res) {
      return res.json().then(function (data) {
        return { status: res.status, data: data };
      });
    });
  }

  function rollback(version, note) {
    var url = "/api/v1/admin/env/" + encodeURIComponent(CW.envSlug()) +
      "/releases/" + encodeURIComponent(version) + "/rollback";
    if (CW.state.projectId) url += "?project=" + encodeURIComponent(CW.state.projectId);
    return fetch(url, {
      method: "POST",
      headers: Object.assign({ "Content-Type": "application/json" }, CW.authHeaders()),
      body: JSON.stringify({ note: note || "" }),
    }).then(function (res) {
      return res.json().then(function (data) {
        return { status: res.status, data: data };
      });
    });
  }

  CW.renderReleases = renderReleases;
  CW.toggleReleasesExpanded = toggleReleasesExpanded;
  CW.openReleaseDialog = openReleaseDialog;
  CW.closeReleaseDialog = closeReleaseDialog;
  CW.loadReleases = loadReleases;
  CW.publish = publish;
  CW.rollback = rollback;
  CW.latestVersion = latestVersion;
  CW.markUnpublished = markUnpublished;
  CW.markPublished = markPublished;
  CW.setPublishState = setPublishState;
  CW.isUnpublished = isUnpublished;
  CW.clearUnpublished = clearUnpublished;
  CW.unpublishedSummary = unpublishedSummary;
  CW.renderUnpublishedList = renderUnpublishedList;
  CW.applyDrafts = applyDrafts;
  CW.setApplying = setApplying;
  CW.updatePublishNavBadge = updatePublishNavBadge;
  CW.updateDirtyHighlights = updateDirtyHighlights;
  CW.armDirtyForm = armDirtyForm;
  CW.markFormDirty = markFormDirty;
  CW.markFormClean = markFormClean;
})();
