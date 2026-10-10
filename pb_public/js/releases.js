/* ConfigWire admin shell — releases.js (releases + publish + rollback). */
(function () {
  "use strict";

  var CW = window.CW;

  // Hover-note source of truth for the single-environment guard (menu
  // title + dialog note + submit guard below all read this one string).
  var NO_DEST_NOTE = "Only one environment exists — create another environment to promote to";

  function renderReleases() {
    var list = CW.$("release-list");
    if (!list) return;
    if (!CW.state.releases.length) { list.innerHTML = "<li>No releases.</li>"; return; }
    if (typeof CW.state.releasesExpanded === "undefined") CW.state.releasesExpanded = false;
    var expanded = !!CW.state.releasesExpanded;
    var visible = expanded ? CW.state.releases : CW.state.releases.slice(0, 3);
    var html = visible.map(function (r, idx) {
      // Single-environment guard: destEnvs() is the same helper the dialog
      // uses (single source of truth); with zero destinations the menu item
      // renders disabled with a hover note (native title, no tooltip).
      var noDest = !destEnvs().length;
      var noDestAttr = noDest
        ? ' disabled title="' + NO_DEST_NOTE + '" data-promote-nodest="1"'
        : "";
      var label = "v" + CW.esc(r.version) + " etag " + CW.esc(r.etag) +
        (r.note ? " — " + CW.esc(r.note) : "");
      var svgOpen = '<svg class="menu-icon" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">';
      var viewIcon = svgOpen + '<path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z"/><circle cx="8" cy="8" r="2"/></svg>';
      var exportIcon = svgOpen + '<path d="M8 2v9"/><path d="M4.5 7.5 8 11l3.5-3.5"/><path d="M2.5 11v2.5h11V11"/></svg>';
      var rollbackIcon = svgOpen + '<path d="M2.5 6.5h7a3 3 0 0 1 0 6H5"/><path d="M5.5 3.5 2.5 6.5l3 3"/></svg>';
      var promoteIcon = svgOpen + '<path d="M8 13V3"/><path d="M4.5 6.5 8 3l3.5 3.5"/><path d="M2.5 13h11"/></svg>';
      var menu = '<span class="release-menu-wrap">' +
        '<button type="button" class="release-menu-btn" data-release-menu="' + CW.esc(r.id) + '" aria-haspopup="menu" aria-expanded="false" aria-label="Actions for release v' + CW.esc(r.version) + '">&#8943;</button>' +
        '<div class="release-menu" role="menu" hidden>' +
        '<button type="button" role="menuitem" data-view-release="' + CW.esc(r.id) + '">' + viewIcon + "<span>view</span></button>" +
        '<button type="button" role="menuitem" data-export-release="' + CW.esc(r.id) + '">' + exportIcon + "<span>export</span></button>" +
        '<button type="button" role="menuitem" data-promote-version="' + CW.esc(r.version) + '"' + noDestAttr + '>' + promoteIcon + "<span>Promote to…</span></button>" +
        (idx === 0 ? "" :
          '<button type="button" role="menuitem" data-rollback-version="' + CW.esc(r.version) + '">' + rollbackIcon + "<span>Rollback to v" +
          CW.esc(r.version) + "</span></button>") +
        "</div></span>";
      if (idx === 0) {
        return "<li>" + label + ' <span class="badge ok">current</span>' + menu + "</li>";
      }
      return "<li>" + label + menu + "</li>";
    }).join("");
    if (CW.state.releases.length > 3) {
      var hidden = CW.state.releases.length - 3;
      html += '<li><button type="button" id="releases-toggle">' +
        (expanded ? "Show less" : "Show " + hidden + " more") + "</button></li>";
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

  var RELEASE_FIELD_ORDER = ["id", "author", "env", "etag", "version", "note", "snapshot"];
  var RELEASE_FIELD_LABELS = {
    id: "ID",
    author: "Author",
    env: "Env",
    etag: "Etag",
    version: "Version",
    note: "Note",
    snapshot: "Snapshot"
  };

  function releaseFieldLabel(k) {
    if (Object.prototype.hasOwnProperty.call(RELEASE_FIELD_LABELS, k)) return RELEASE_FIELD_LABELS[k];
    return String(k);
  }

  function appendReleaseField(dl, k, val) {
    var dt = document.createElement("dt");
    dt.textContent = releaseFieldLabel(k);
    dl.appendChild(dt);
    var dd = document.createElement("dd");
    if (k === "snapshot") dd.className = "release-snapshot";
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
      var seen = {};
      var i, k;
      for (i = 0; i < RELEASE_FIELD_ORDER.length; i++) {
        k = RELEASE_FIELD_ORDER[i];
        seen[k] = true;
        if (!Object.prototype.hasOwnProperty.call(rec, k)) continue;
        appendReleaseField(dl, k, rec[k]);
      }
      var rest = Object.keys(rec);
      for (i = 0; i < rest.length; i++) {
        k = rest[i];
        if (seen[k]) continue;
        if (k === "expand" || k === "collectionId" || k === "collectionName") continue;
        appendReleaseField(dl, k, rec[k]);
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

  // Draft signal (Unit A store): CW.state.drafts = { flag:{}, rule:{},
  // experiment:{}, group:{} }, entry { op, body, baseId, tempId, label, at }.
  // Drafts-only: CW.drafts is required, no legacy marker-bucket fallback.
  // All guards are call-time (never parse time) so script order between
  // drafts.js and releases.js does not matter.
  function useDraftSignal() {
    return !!(CW.drafts);
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
    return draftSummary();
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
      flagsSec.classList.toggle("has-unpublished", (s.flag + s.rule + s.group + s.experiment) > 0);
    }
  }

  function setPublishState(dirty) {
    CW.state.unpublishedChanges = !!dirty;
    var form = CW.$("publish-form");
    var btn = form ? form.querySelector('button[type="submit"]') : null;
    if (btn) btn.disabled = !dirty;
    var discard = CW.$("discard-unpublished");
    if (discard) discard.disabled = !dirty;
    var draftView = CW.$("transfer-draft-view-btn");
    if (draftView) draftView.hidden = !dirty;
    var hint = CW.$("publish-hint");
    if (hint) {
      if (dirty) {
        var detail = summaryText();
        hint.textContent = detail
          ? "Unpublished changes \u2014 publish to release (" + detail + ")"
          : "Unpublished changes \u2014 publish to release";
      } else {
        hint.textContent = "Up to date.";
      }
      if (hint.classList) hint.classList.toggle("is-dirty", !!dirty);
    }
    renderUnpublishedList();
    updatePublishNavBadge();
    updateDirtyHighlights();
  }

  function markUnpublished(kind, id, label) {
    setPublishState(true);
  }

  function isUnpublished(kind, id) {
    if (!kind || !isKnownKind(kind)) return false;
    if (id === undefined || id === null || id === "") return false;
    try {
      if (CW.drafts && typeof CW.drafts.isDraft === "function") return !!CW.drafts.isDraft(kind, id);
    } catch (e) { /* ignore */ }
    return false;
  }

  function isKnownKind(kind) {
    for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
      if (UNPUBLISHED_KINDS[i] === kind) return true;
    }
    return false;
  }

  function clearUnpublished() {
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
    try { rb = document.querySelectorAll('button[data-rollback-version],button[data-promote-version]'); } catch (e) { rb = null; }
    if (rb) {
      for (var i = 0; i < rb.length; i++) {
        try {
          var el = rb[i];
          // Guard-disabled (no destination) buttons stay disabled: a plain
          // setApplying(false) pass must never re-enable them.
          if (el && el.getAttribute && el.getAttribute("data-promote-nodest")) { el.disabled = true; continue; }
          el.disabled = dis;
        } catch (e2) { /* best-effort */ }
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

  // applyRunning guards direct invocations (e.g. window.cwAdmin.
  // applyDrafts): the UI-level CW.state.applying flag is set by callers
  // BEFORE calling, so it cannot guard the function itself. A concurrent
  // call gets a busy noop instead of interleaving POSTs with the
  // in-flight run.
  var applyRunning = false;
  function applyDrafts() {
    if (applyRunning) return Promise.resolve({ applied: 0, total: 0, noop: true, busy: true });
    if (!useDraftSignal()) return Promise.resolve({ applied: 0, total: 0, noop: true });
    var steps = buildApplySteps();
    var total = steps.length;
    if (!total) return Promise.resolve({ applied: 0, total: 0 });
    applyRunning = true;
    var tempMap = {};
    var idx = 0;
    function fail(s, stepNo, status, msg, name) {
      try {
        if (CW.drafts && typeof CW.drafts.persistDrafts === "function") CW.drafts.persistDrafts();
      } catch (e) { /* kept in memory */ }
      rerenderMerged();
      var kept = countDrafts();
      var err = new Error("Publish stopped at " + s.label +
        " (step " + stepNo + "/" + total + "). " + kept + " drafts kept.");
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
    return next().then(function (out) { applyRunning = false; return out; }, function (e) { applyRunning = false; throw e; });
  }

  // Per-form dirty gating (publish-global CW.state.unpublishedChanges is
  // separate: one form's save must not clear another form's dirty, and only
  // publish-success clears the publish-global flag). Submit is disabled
  // unless dirty OR (create-mode AND form.checkValidity()): edit dialogs
  // (hidden id filled) start disabled until input/change, while create
  // dialogs with prefilled valid defaults (e.g. new rule) start enabled.
  // Programmatic pre-fills (dialog edit, form.reset()) fire no input/change
  // events, so opening a dialog never counts as dirty by itself.
  function isCreateForm(formId) {
    try {
      if (formId === "project-dialog-form" || formId === "account-create-form") return true;
      if (formId === "key-limits-form" || formId === "limits-form" || formId === "settings-admin-form") return false;
      var map = { "flag-form": "flag-id", "flag-rules-form": "flag-rules-id", "experiment-form": "exp-id" };
      var hid = map[formId];
      if (!hid) return false;
      var el = CW.$(hid);
      if (!el) return false;
      return String(el.value == null ? "" : el.value).trim() === "";
    } catch (e) { return false; }
  }

  function refreshFormSubmit(formId) {
    try {
      if (!CW.state.dirtyForms) CW.state.dirtyForms = {};
      var form = null;
      try { form = CW.$(formId); } catch (e) { form = null; }
      var btn = null;
      try { btn = form ? form.querySelector('button[type="submit"]') : null; } catch (e2) { btn = null; }
      if (!btn) return;
      if (!!CW.state.dirtyForms[formId]) { btn.disabled = false; return; }
      var create = false;
      try { create = isCreateForm(formId); } catch (e3) { create = false; }
      if (!create) { btn.disabled = true; return; }
      var valid = false;
      try {
        if (form && typeof form.checkValidity === "function") valid = !!form.checkValidity();
        else valid = false;
      } catch (e4) { valid = false; }
      btn.disabled = !valid;
    } catch (e) { /* best-effort */ }
  }

  function setFormDirty(formId, dirty) {
    if (!CW.state.dirtyForms) CW.state.dirtyForms = {};
    CW.state.dirtyForms[formId] = !!dirty;
    refreshFormSubmit(formId);
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
    var gen = CW.state.scopeGen;
    // Fetch all then filter client-side by selected env relation; sort -version.
    // allReleases-vs-second-fetch choice: keep the unfiltered full list in
    // CW.state.allReleases (one fetch, no extra round-trip); CW.state.releases
    // stays dest-filtered so the release list + publish-base keep their
    // current-env semantics, while the promote dialog sources the fixed
    // source release and each dest env's max version from allReleases.
    return CW.apiAll("/api/collections/releases/records?perPage=200&sort=-version").then(function (items) {
      if (CW.scopeStale(gen)) return;
      items = items || [];
      CW.state.allReleases = items.slice().sort(function (a, b) { return b.version - a.version; });
      if (CW.state.envId) items = items.filter(function (r) { return r.env === CW.state.envId; });
      CW.state.releases = items.slice().sort(function (a, b) { return b.version - a.version; });
      renderReleases();
      // Publish form auto-fills baseVersion from the latest version so the
      // first submit never goes stale (no 409 on first try by default).
      CW.$("publish-base").value = latestVersion();
      if (unpublishedSummary().total > 0) setPublishState(true);
      else setPublishState(false);
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

  function promote(srcEnv, srcVersion, destBaseVersion, note, srcProject, destSlug) {
    var dest = destSlug || CW.envSlug();
    var url = "/api/v1/admin/env/" + encodeURIComponent(dest) + "/promote";
    if (CW.state.projectId) url += "?project=" + encodeURIComponent(CW.state.projectId);
    var body = { srcEnv: srcEnv, srcVersion: srcVersion, note: note || "", destBaseVersion: destBaseVersion };
    if (srcProject) body.srcProject = srcProject;
    return fetch(url, {
      method: "POST",
      headers: Object.assign({ "Content-Type": "application/json" }, CW.authHeaders()),
      body: JSON.stringify(body),
    }).then(function (res) {
      return res.json().then(function (data) {
        return { status: res.status, data: data };
      });
    });
  }

  // Control-surface choice (A, promote-to direction): release-menu item
  // "Promote to…" opens a dialog with the SOURCE fixed to the current env
  // at the row's version (summary line only, no src dropdowns) and a
  // DESTINATION env dropdown (same-project envs minus the current env,
  // dest-base auto-filled from the dest env's max version in allReleases).
  // No index.html/CSS change was needed: outcome text reuses the existing
  // publish-result/publish-base IDs, and the dialog below is built lazily
  // in JS so no new static markup was required.
  function promoteDefaultNote(srcEnv, srcVersion) {
    return "promoted from environment " + srcEnv + "@v" + srcVersion;
  }

  function resolveEnvId(slug) {
    var envs = (CW.state && Array.isArray(CW.state.envs)) ? CW.state.envs : [];
    for (var i = 0; i < envs.length; i++) {
      if (envs[i] && String(envs[i].slug) === String(slug)) return envs[i].id;
    }
    return null;
  }

  function destEnvs() {
    var cur = "";
    try { cur = CW.envSlug(); } catch (e) { cur = ""; }
    var pid = (CW.state && CW.state.projectId) || null;
    var envs = (CW.state && Array.isArray(CW.state.envs)) ? CW.state.envs : [];
    var out = [];
    for (var i = 0; i < envs.length; i++) {
      if (!envs[i] || !envs[i].slug) continue;
      if (String(envs[i].slug) === String(cur)) continue;
      if (pid && String(envs[i].project) !== String(pid)) continue;
      out.push(String(envs[i].slug));
    }
    return out;
  }

  // Pure over CW.state.allReleases + CW.state.envs: the dest env's max
  // version (by env id via slug→id resolution, slug fallback when the id
  // is unknown), 0 when the dest has no releases. Unit-tested in
  // js_tests/test-promote.js via CW.destLatestVersion.
  function destLatestVersion(destSlug) {
    var id = resolveEnvId(destSlug);
    var all = (CW.state && Array.isArray(CW.state.allReleases)) ? CW.state.allReleases : [];
    var m = 0;
    for (var i = 0; i < all.length; i++) {
      var r = all[i];
      if (!r) continue;
      if (id ? String(r.env) === String(id) : String(r.env) === String(destSlug)) {
        if (r.version > m) m = r.version;
      }
    }
    return m;
  }

  function findSourceRelease(srcEnv, srcVersion) {
    var all = (CW.state && Array.isArray(CW.state.allReleases)) ? CW.state.allReleases : [];
    var id = resolveEnvId(srcEnv);
    for (var i = 0; i < all.length; i++) {
      var r = all[i];
      if (!r) continue;
      if (String(r.version) !== String(srcVersion)) continue;
      if (id ? String(r.env) === String(id) : String(r.env) === String(srcEnv)) return r;
    }
    return null;
  }

  // Source-fixed promote-to state: the row version captured by
  // openPromoteDialog (the source is always the current env).
  var pendingPromoteSrcVersion = null;

  function ensurePromoteDialog() {
    var dlg = null;
    try { dlg = document.getElementById("promote-dialog"); } catch (e) { dlg = null; }
    if (dlg) return dlg;
    dlg = document.createElement("dialog");
    dlg.id = "promote-dialog";
    dlg.setAttribute("aria-labelledby", "promote-dialog-title");
    dlg.innerHTML =
      '<h3 id="promote-dialog-title">Promote to…</h3>' +
      '<p id="promote-src-summary" role="status"></p>' +
      '<form id="promote-form" method="dialog">' +
      '<label>Destination environment <select id="promote-dest-env"></select></label>' +
      '<label>Note (optional) <input id="promote-note" type="text" maxlength="500"></label>' +
      '<label>Destination base version <input id="promote-dest-base" type="number" min="0" required></label>' +
      '<div class="dialog-actions">' +
      '<span class="dialog-spacer"></span>' +
      '<button type="button" id="promote-cancel" class="btn ghost">Cancel</button>' +
      '<button type="submit" id="promote-submit" class="btn primary">Promote</button>' +
      "</div></form>" +
      '<p id="promote-result" role="status"></p>';
    if (document.body && document.body.appendChild) document.body.appendChild(dlg);
    function close() {
      try {
        if (dlg.open) dlg.close();
        else if (dlg.removeAttribute) dlg.removeAttribute("open");
      } catch (e) { /* already closed */ }
    }
    var cancel = null;
    try { cancel = dlg.querySelector ? dlg.querySelector("#promote-cancel") : document.getElementById("promote-cancel"); } catch (e) { cancel = null; }
    if (cancel && cancel.addEventListener) cancel.addEventListener("click", close);
    var form = null;
    try { form = dlg.querySelector ? dlg.querySelector("#promote-form") : document.getElementById("promote-form"); } catch (e) { form = null; }
    function onChange() { refreshPromoteDialog(); }
    var destEnv = null;
    try {
      destEnv = dlg.querySelector ? dlg.querySelector("#promote-dest-env") : document.getElementById("promote-dest-env");
    } catch (e) { destEnv = null; }
    if (destEnv && destEnv.addEventListener) destEnv.addEventListener("change", onChange);
    if (form && form.addEventListener) form.addEventListener("submit", function (ev) {
      if (ev && ev.preventDefault) ev.preventDefault();
      submitPromote();
    });
    return dlg;
  }

  function setSelectOptions(sel, values, current) {
    if (!sel) return;
    sel.innerHTML = values.map(function (v) {
      return '<option value="' + CW.esc(v) + '"' +
        (String(v) === String(current) ? " selected" : "") + ">" + CW.esc(v) + "</option>";
    }).join("");
    try { sel.value = current; } catch (e) { /* stub DOM */ }
  }

  function currentPromoteSel() {
    var srcEnv = "";
    try { srcEnv = CW.envSlug(); } catch (e) { srcEnv = ""; }
    var srcVersion = pendingPromoteSrcVersion;
    var destSlug = "", note = "", destBase = null;
    try {
      var de = document.getElementById("promote-dest-env");
      if (de && de.value != null && String(de.value) !== "") destSlug = String(de.value);
      var nt = document.getElementById("promote-note");
      if (nt && nt.value != null) note = String(nt.value);
      var db = document.getElementById("promote-dest-base");
      if (db && db.value != null && String(db.value) !== "") destBase = parseInt(db.value, 10);
    } catch (e) { /* stub DOM */ }
    if (!destSlug) {
      var ds = destEnvs();
      if (ds.length) destSlug = ds[0];
    }
    return { srcEnv: srcEnv, srcVersion: srcVersion, destSlug: destSlug, note: note, destBase: destBase };
  }

  function refreshPromoteDialog() {
    var sel = currentPromoteSel();
    var dests = destEnvs();
    setSelectOptions(document.getElementById("promote-dest-env"), dests, sel.destSlug);
    var rec = (sel.srcVersion != null && sel.srcVersion !== "")
      ? findSourceRelease(sel.srcEnv, sel.srcVersion)
      : null;
    var sum = null;
    try { sum = document.getElementById("promote-src-summary"); } catch (e) { sum = null; }
    if (sum) {
      sum.textContent = rec
        ? "Source environment " + sel.srcEnv + " · v" + rec.version + " · etag " + rec.etag + (rec.note ? " \u2014 " + rec.note : "")
        : "No matching source release in environment " + sel.srcEnv + " at v" + sel.srcVersion + ".";
    }
    try {
      var nt = document.getElementById("promote-note");
      if (nt) nt.placeholder = promoteDefaultNote(sel.srcEnv, (sel.srcVersion != null && sel.srcVersion !== "") ? sel.srcVersion : "?");
      var db = document.getElementById("promote-dest-base");
      if (db) db.value = destLatestVersion(sel.destSlug);
    } catch (e) { /* stub DOM */ }
    // Empty-destination guard (defensive: state may have changed since the
    // menu rendered): explain in the result line and keep submit disabled
    // until a destination exists. Non-empty leaves any in-flight result
    // text alone (success/error messages are written by submitPromote).
    try {
      var res = document.getElementById("promote-result");
      var sub = document.getElementById("promote-submit");
      if (!dests.length) {
        if (res) res.textContent = NO_DEST_NOTE;
        if (sub) sub.disabled = true;
      } else if (sub) {
        sub.disabled = false;
      }
    } catch (e2) { /* stub DOM */ }
  }

  function openPromoteDialog(presetVersion) {
    var dlg = ensurePromoteDialog();
    pendingPromoteSrcVersion = (presetVersion != null && presetVersion !== "") ? String(presetVersion) : null;
    if (pendingPromoteSrcVersion == null) {
      var lv = latestVersion();
      pendingPromoteSrcVersion = lv ? String(lv) : null;
    }
    var dests = destEnvs();
    setSelectOptions(document.getElementById("promote-dest-env"), dests, dests.length ? dests[0] : "");
    try {
      var nt = document.getElementById("promote-note");
      if (nt) nt.value = "";
      var pr = document.getElementById("promote-result");
      if (pr) pr.textContent = "";
    } catch (e) { /* stub DOM */ }
    refreshPromoteDialog();
    try {
      if (dlg.open) return dlg;
      if (typeof dlg.showModal === "function") dlg.showModal();
      else if (dlg.setAttribute) dlg.setAttribute("open", "");
    } catch (e) { /* already open */ }
    return dlg;
  }

  function submitPromote() {
    if (CW.state.applying) return Promise.resolve(null);
    var sel = currentPromoteSel();
    var srcVersion = (sel.srcVersion == null || sel.srcVersion === "") ? 0 : parseInt(sel.srcVersion, 10);
    var note = sel.note || promoteDefaultNote(sel.srcEnv, sel.srcVersion);
    var destBase = sel.destBase;
    if (destBase == null || isNaN(destBase)) destBase = destLatestVersion(sel.destSlug);
    var srcProject = (CW.state && CW.state.projectId) ? String(CW.state.projectId) : "";
    var resultEl = null;
    try { resultEl = document.getElementById("promote-result") || CW.$("publish-result"); }
    catch (e) { resultEl = null; }
    function say(msg) {
      if (resultEl) resultEl.textContent = msg;
      var pub = null;
      try { pub = CW.$("publish-result"); } catch (e2) { pub = null; }
      if (pub && pub !== resultEl) pub.textContent = msg;
    }
    // Empty-destination guard: never submit a promote with no dest selection
    // (the menu disables the entry, and the dialog keeps submit disabled;
    // this covers a state change between dialog open and submit).
    if (!sel.destSlug) {
      say(NO_DEST_NOTE);
      return Promise.resolve(null);
    }
    setApplying(true);
    return promote(sel.srcEnv, srcVersion, destBase, note, srcProject, sel.destSlug).then(function (out) {
      setApplying(false);
      if (out.status === 200) {
        var nv = (out.data && out.data.version != null) ? out.data.version : "?";
        say("Promoted to environment " + sel.destSlug + " as v" + nv + ".");
        return loadReleases().then(function () {
          refreshPromoteDialog();
        }, function () {});
      }
      if (out.status === 409) {
        return loadReleases().then(function () {
          var cur = (out.data && out.data.currentVersion != null) ? out.data.currentVersion : destLatestVersion(sel.destSlug);
          try {
            var db = document.getElementById("promote-dest-base");
            if (db) db.value = destLatestVersion(sel.destSlug);
          } catch (e) { /* stub DOM */ }
          // Wording choice: keep our "Stale baseVersion:" prefix verbatim
          // (it quotes the server message/field name; rewording it would
          // bury the server detail) and spell out only our own tail as
          // "current destination version".
          say("Stale baseVersion: " + CW.serverMessage(out.data) + " (current destination version: " + cur + ")");
        }, function () {
          say("Stale baseVersion: " + CW.serverMessage(out.data));
        });
      }
      say("Promote failed (" + out.status + "): " + CW.serverMessage(out.data));
      return null;
    }, function (e) {
      setApplying(false);
      say("Promote failed: " + ((e && e.message) || e));
      return null;
    });
  }

  function armPromoteMenuClicks() {
    if (armPromoteMenuClicks.armed) return;
    armPromoteMenuClicks.armed = true;
    function handler(ev) {
      var t = ev && ev.target ? ev.target : null;
      var btn = null;
      if (t) {
        if (t.closest) btn = t.closest("[data-promote-version]");
        else if (t.getAttribute && t.getAttribute("data-promote-version")) btn = t;
      }
      if (!btn || !btn.getAttribute) return;
      if (CW.state.applying) return;
      if (btn.disabled) return;
      var v = btn.getAttribute("data-promote-version");
      var menu = t.closest ? t.closest(".release-menu") : null;
      if (menu) menu.hidden = true;
      openPromoteDialog(v);
    }
    try {
      if (document.addEventListener) document.addEventListener("click", handler);
    } catch (e) { /* non-DOM */ }
  }

  armPromoteMenuClicks();

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
  CW.promote = promote;
  CW.openPromoteDialog = openPromoteDialog;
  CW.submitPromote = submitPromote;
  CW.promoteDefaultNote = promoteDefaultNote;
  CW.destEnvs = destEnvs;
  CW.destLatestVersion = destLatestVersion;
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
  CW.isCreateForm = isCreateForm;
  CW.refreshFormSubmit = refreshFormSubmit;
  CW.syncFormSubmit = refreshFormSubmit;
})();
