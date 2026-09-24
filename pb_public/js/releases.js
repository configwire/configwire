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

  function loadReleases() {
    // Fetch all then filter client-side by selected env relation; sort -version.
    return CW.api("/api/collections/releases/records?perPage=200&sort=-version").then(function (data) {
      var items = data.items || [];
      if (CW.state.envId) items = items.filter(function (r) { return r.env === CW.state.envId; });
      CW.state.releases = items.slice().sort(function (a, b) { return b.version - a.version; });
      renderReleases();
      // Publish form auto-fills baseVersion from the latest version so the
      // first submit never goes stale (no 409 on first try by default).
      CW.$("publish-base").value = latestVersion();
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
})();
