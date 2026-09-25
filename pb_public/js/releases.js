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

  function setPublishState(dirty) {
    CW.state.unpublishedChanges = !!dirty;
    var form = CW.$("publish-form");
    var btn = form ? form.querySelector('button[type="submit"]') : null;
    if (btn) btn.disabled = !dirty;
    var hint = CW.$("publish-hint");
    if (hint) {
      hint.textContent = dirty
        ? "Unpublished changes \u2014 publish to release"
        : "No unpublished changes";
    }
  }

  function markUnpublished() {
    setPublishState(true);
  }

  function markPublished() {
    var base = CW.$("publish-base");
    if (base) base.value = latestVersion();
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
      // Fresh load means clean: no unpublished changes yet.
      markPublished();
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
  CW.armDirtyForm = armDirtyForm;
  CW.markFormDirty = markFormDirty;
  CW.markFormClean = markFormClean;
})();
