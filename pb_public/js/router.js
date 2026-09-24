/* ConfigWire admin shell — router.js (hash router + home/detail views). */
(function () {
  "use strict";

  var CW = window.CW;

  // ---- hash router: #/ => home, #/p/<id>[#anchor] => detail ----

  function parseHash() {
    var h = window.location.hash || "";
    if (h.indexOf("#/p/") === 0) {
      var rest = h.slice("#/p/".length);
      var anchor = "";
      var hi = rest.indexOf("#");
      if (hi !== -1) { anchor = rest.slice(hi + 1); rest = rest.slice(0, hi); }
      // Allow a trailing "/<anchor>" form too.
      var si = rest.indexOf("/");
      if (si !== -1 && !anchor) { anchor = rest.slice(si + 1); rest = rest.slice(0, si); }
      return { view: "detail", id: decodeURIComponent(rest), anchor: anchor };
    }
    return { view: "home", id: "", anchor: "" };
  }

  function projectById(id) {
    for (var i = 0; i < CW.state.projects.length; i++) {
      if (CW.state.projects[i].id === id) return CW.state.projects[i];
    }
    return null;
  }

  function syncSidebar() {
    var nav = CW.$("sidebar-nav");
    if (!nav) return;
    var links = nav.querySelectorAll("a");
    if (CW.state.view !== "detail" || !CW.state.projectId) {
      nav.setAttribute("aria-hidden", "true");
      for (var i = 0; i < links.length; i++) {
        links[i].setAttribute("tabindex", "-1");
        links[i].setAttribute("aria-disabled", "true");
      }
      nav.style.display = "none";
      return;
    }
    nav.removeAttribute("aria-hidden");
    nav.style.display = "";
    var mods = ["flags", "rules", "experiments", "keys", "releases", "publish", "stats"];
    for (var j = 0; j < links.length; j++) {
      var m = mods[j] || "";
      links[j].removeAttribute("tabindex");
      links[j].removeAttribute("aria-disabled");
      links[j].setAttribute("href", "#/p/" + encodeURIComponent(CW.state.projectId) + "#" + m);
    }
  }

  function showView(name) {
    CW.state.view = name;
    var home = CW.$("view-home");
    var detail = CW.$("view-detail");
    if (home) home.hidden = name !== "home";
    if (detail) detail.hidden = name !== "detail";
    syncSidebar();
  }

  function renderProjectCards() {
    var grid = CW.$("project-grid");
    var empty = CW.$("project-empty");
    if (!grid) return;
    if (!CW.state.projects.length) {
      grid.innerHTML = "";
      if (empty) empty.hidden = false;
      return;
    }
    if (empty) empty.hidden = true;
    grid.innerHTML = CW.state.projects.map(function (p) {
      var s = CW.state.homeStats[p.id] || { flags: 0, envs: 0, keys: 0 };
      return '<article class="card project-card" role="listitem" tabindex="0" data-project-id="' +
        CW.esc(p.id) + '" aria-label="' + CW.esc(p.name || p.id) + '">' +
        "<h3>" + CW.esc(p.name || p.id) + "</h3>" +
        '<p class="project-card-stats"><span>' + CW.esc(s.flags) + " flags</span>" +
        "<span> · </span><span>" + CW.esc(s.envs) + " envs</span>" +
        "<span> · </span><span>" + CW.esc(s.keys) + " active keys</span></p>" +
        "</article>";
    }).join("");
  }

  // Home aggregates via the existing perPage=200 reads (no new backend).
  // Keys are owned per-project through env->project, same as loadKeys.
  function loadHomeStats() {
    return CW.api("/api/collections/flags/records?perPage=200").then(function (f) {
      return CW.api("/api/collections/environments/records?perPage=200").then(function (e) {
        return CW.api("/api/collections/sdk_keys/records?perPage=200").then(function (k) {
          var envProject = {};
          (e.items || []).forEach(function (env) { envProject[env.id] = env.project; });
          var stats = {};
          CW.state.projects.forEach(function (p) { stats[p.id] = { flags: 0, envs: 0, keys: 0 }; });
          (f.items || []).forEach(function (fl) {
            if (stats[fl.project]) stats[fl.project].flags++;
          });
          (e.items || []).forEach(function (env) {
            if (stats[env.project]) stats[env.project].envs++;
          });
          (k.items || []).forEach(function (key) {
            var pid = envProject[key.env];
            if (pid && stats[pid] && !key.revoked) stats[pid].keys++;
          });
          CW.state.homeStats = stats;
          renderProjectCards();
        });
      });
    }, function () { renderProjectCards(); });
  }

  function loadDetailScope() {
    CW.loadEnvs().then(function () {
      renderDetailHeader();
      CW.loadFlags().catch(function (e) { CW.$("flag-tbody").innerHTML = "<tr><td colspan=5>" + CW.esc(e.message) + "</td></tr>"; });
      CW.loadReleases().catch(function (e) { CW.$("release-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
      CW.loadRules().catch(function (e) { CW.$("rule-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
      CW.loadExperiments().catch(function (e) { CW.$("experiment-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
      CW.loadKeys().catch(function (e) { CW.$("key-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
      CW.loadStats().catch(function () { /* inline in stats card */ });
    }).catch(function (e) { CW.toast(e.message); });
  }

  function renderDetailHeader() {
    var p = projectById(CW.state.projectId);
    var el = CW.$("detail-project-name");
    if (el) el.textContent = p ? (p.name || p.id) : "Project";
    CW.renderScopeHint();
    syncSidebar();
  }

  function showHome() {
    showView("home");
    renderProjectCards();
    if (CW.state.token) loadHomeStats().catch(function () {});
  }

  function openProject(id, anchor) {
    if (!projectById(id)) { showHome(); return; }
    CW.state.projectId = id;
    CW.persistScope();
    var want = "#/p/" + encodeURIComponent(id);
    if (window.location.hash !== want && !anchor) window.location.hash = want;
    showView("detail");
    renderDetailHeader();
    loadDetailScope();
    if (anchor) {
      var t = CW.$(anchor);
      var card = t && t.closest ? t.closest("section") : null;
      if (card && card.scrollIntoView) card.scrollIntoView();
    }
  }

  function route() {
    if (!CW.state.token) return;
    var r = parseHash();
    if (r.view === "detail" && r.id && projectById(r.id)) {
      // Avoid re-loading on pure in-page anchor hops to the same project.
      if (CW.state.view === "detail" && CW.state.projectId === r.id) {
        showView("detail");
        renderDetailHeader();
        if (r.anchor) {
          var t = CW.$(r.anchor);
          var card = t && t.closest ? t.closest("section") : null;
          if (card && card.scrollIntoView) card.scrollIntoView();
        }
        return;
      }
      openProject(r.id, r.anchor);
    } else if (r.view === "detail" && r.id) {
      // Unknown id (stale reload): fall back home rather than blank detail.
      showHome();
    } else {
      showHome();
    }
  }

  CW.parseHash = parseHash;
  CW.projectById = projectById;
  CW.syncSidebar = syncSidebar;
  CW.showView = showView;
  CW.renderProjectCards = renderProjectCards;
  CW.loadHomeStats = loadHomeStats;
  CW.loadDetailScope = loadDetailScope;
  CW.renderDetailHeader = renderDetailHeader;
  CW.showHome = showHome;
  CW.openProject = openProject;
  CW.route = route;
})();
