/* ConfigWire admin shell — releases.js (releases + publish + rollback). */
(function () {
  "use strict";

  var CW = window.CW;

  function renderReleases() {
    var list = CW.$("release-list");
    if (!CW.state.releases.length) { list.innerHTML = "<li>No releases for this env.</li>"; return; }
    list.innerHTML = CW.state.releases.map(function (r) {
      return "<li>v" + CW.esc(r.version) + " etag " + CW.esc(r.etag) +
        (r.note ? " — " + CW.esc(r.note) : "") +
        ' <button type="button" data-rollback-version="' + CW.esc(r.version) + '">rollback to v' +
        CW.esc(r.version) + "</button></li>";
    }).join("");
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

  function unpublishedSummary() {
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
    ensureUnpublishedShape();
    var rows = [];
    for (var i = 0; i < UNPUBLISHED_KINDS.length; i++) {
      var kind = UNPUBLISHED_KINDS[i];
      var bucket = CW.state.unpublished[kind] || {};
      var ids = Object.keys(bucket);
      for (var j = 0; j < ids.length; j++) {
        var id = ids[j];
        var entry = bucket[id] || {};
        var label = entry.label || id;
        rows.push('<li data-unpublished-kind="' + CW.esc(kind) + '" data-unpublished-id="' +
          CW.esc(id) + '">' + CW.esc(kind) + ": " + CW.esc(label) + "</li>");
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
    // Zero-arg path: boot.js form-typing relies on CW.markUnpublished().
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

  function markPublished() {
    var base = CW.$("publish-base");
    if (base) base.value = latestVersion();
    clearUnpublished();
    setPublishState(false);
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
  CW.updatePublishNavBadge = updatePublishNavBadge;
  CW.updateDirtyHighlights = updateDirtyHighlights;
  CW.armDirtyForm = armDirtyForm;
  CW.markFormDirty = markFormDirty;
  CW.markFormClean = markFormClean;
})();
