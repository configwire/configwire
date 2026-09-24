/* ConfigWire admin shell — scope.js (project/env scope + create + refreshAll). */
(function () {
  "use strict";

  var CW = window.CW;

  function renderScopeHint() {
    var p = CW.$("project-select");
    var e = CW.$("env-select");
    var pn = p && p.selectedOptions && p.selectedOptions[0] ? p.selectedOptions[0].textContent : "";
    var en = e && e.selectedOptions && e.selectedOptions[0] ? e.selectedOptions[0].textContent : "";
    CW.$("scope-hint").textContent = CW.state.projectId
      ? ("project: " + pn + "  ·  env: " + (en || "(none)"))
      : "Pick a project to scope flags, releases, publish and stats.";
  }

  function renderProjectEnv() {
    var ps = CW.$("project-select");
    var cur = CW.state.projectId;
    ps.innerHTML = CW.state.projects.length
      ? CW.state.projects.map(function (p) {
          return '<option value="' + CW.esc(p.id) + '">' + CW.esc(p.name || p.id) + "</option>";
        }).join("")
      : '<option value="">(no projects)</option>';
    if (cur && CW.state.projects.some(function (p) { return p.id === cur; })) ps.value = cur;
    else if (CW.state.projects.length) { ps.value = CW.state.projects[0].id; CW.state.projectId = CW.state.projects[0].id; }
    else CW.state.projectId = null;

    var es = CW.$("env-select");
    var ecur = CW.state.envId;
    es.innerHTML = CW.state.envs.length
      ? CW.state.envs.map(function (e) {
          return '<option value="' + CW.esc(e.id) + '">' + CW.esc(e.slug || e.id) + "</option>";
        }).join("")
      : '<option value="">(no envs)</option>';
    if (ecur && CW.state.envs.some(function (e) { return e.id === ecur; })) es.value = ecur;
    else if (CW.state.envs.length) {
      es.value = defaultEnvId();
      CW.state.envId = es.value;
    } else CW.state.envId = null;
    var sel = CW.selectedEnv();
    if (sel && sel.slug) CW.state.envSlug = sel.slug;
    renderScopeHint();
  }

  function defaultEnvId() {
    for (var i = 0; i < CW.state.envs.length; i++) {
      if (CW.state.envs[i].slug === "dev") return CW.state.envs[i].id;
    }
    return CW.state.envs.length ? CW.state.envs[0].id : "";
  }

  function loadProjects() {
    return CW.api("/api/collections/projects/records?perPage=200").then(function (data) {
      CW.state.projects = (data.items || []).slice().sort(function (a, b) {
        return (a.name || "") < (b.name || "") ? -1 : 1;
      });
      if (CW.state.projectId && !CW.state.projects.some(function (p) { return p.id === CW.state.projectId; })) {
        CW.state.projectId = null;
        CW.state.envId = null;
      }
      if (!CW.state.projectId && CW.state.projects.length) CW.state.projectId = CW.state.projects[0].id;
      CW.persistScope();
    });
  }

  function loadEnvs() {
    if (!CW.state.projectId) { CW.state.envs = []; CW.state.envId = null; renderProjectEnv(); return Promise.resolve(); }
    var filter = "?perPage=200&filter=" + encodeURIComponent('(project="' + CW.state.projectId + '")');
    return CW.api("/api/collections/environments/records" + filter).then(function (data) {
      CW.state.envs = (data.items || []).slice().sort(function (a, b) {
        return (a.slug || "") < (b.slug || "") ? -1 : 1;
      });
    }, function () {
      // Fallback: fetch all then filter client-side when the API filter fails.
      return CW.api("/api/collections/environments/records?perPage=200").then(function (data) {
        CW.state.envs = (data.items || []).filter(function (e) { return e.project === CW.state.projectId; });
      });
    }).then(function () {
      if (CW.state.envId && !CW.state.envs.some(function (e) { return e.id === CW.state.envId; })) CW.state.envId = null;
      if (!CW.state.envId && CW.state.envs.length) CW.state.envId = defaultEnvId();
      var sel = CW.selectedEnv();
      if (sel && sel.slug) CW.state.envSlug = sel.slug;
      CW.persistScope();
      renderProjectEnv();
    });
  }

  function createProject(ev) {
    if (ev) ev.preventDefault();
    var name = CW.$("project-name").value.trim();
    if (!name) { CW.$("project-result").textContent = "project name is required"; return Promise.resolve(); }
    return CW.apiMut("POST", "/api/collections/projects/records", { name: name }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("project-result").textContent = ok
        ? "project created: " + (out.data.name || out.data.id)
        : "project create failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (ok) {
        CW.toast("project created: " + (out.data.name || out.data.id), true);
        CW.$("project-name").value = "";
        var newId = out.data.id;
        loadProjects().then(function () {
          if (newId) { CW.state.projectId = newId; CW.persistScope(); }
          CW.loadHomeStats().catch(function () {});
          if (newId) {
            window.location.hash = "#/p/" + encodeURIComponent(newId);
            // route() picks it up via hashchange; cover no-change case.
            if (CW.parseHash().id !== newId) CW.openProject(newId, "");
          }
          return loadEnvs();
        }).then(function () {
          renderScopeHint();
          CW.loadFlags().catch(function () {});
        }).catch(function () {});
      }
      return out;
    });
  }

  function createEnv(ev) {
    if (ev) ev.preventDefault();
    if (!CW.state.projectId) { CW.$("env-result").textContent = "pick a project first"; return Promise.resolve(); }
    var slug = CW.$("env-slug").value.trim();
    if (!slug) { CW.$("env-result").textContent = "env slug is required"; return Promise.resolve(); }
    return CW.apiMut("POST", "/api/collections/environments/records", { project: CW.state.projectId, slug: slug }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("env-result").textContent = ok
        ? "env created: " + (out.data.slug || out.data.id)
        : "env create failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (ok) {
        CW.toast("env created: " + (out.data.slug || out.data.id), true);
        CW.$("env-slug").value = "";
        loadEnvs().catch(function () {});
      }
      return out;
    });
  }

  function refreshAll() {
    CW.loadPersistedScope();
    loadProjects().then(function () {
      renderProjectEnv();
      CW.route();
      if (CW.state.view === "detail") return; // route()->openProject loads detail scope
      CW.loadHomeStats().catch(function () {});
      // Preload current scope in the background so a card click is instant.
      loadEnvs().then(function () {
        CW.loadFlags().catch(function (e) { CW.$("flag-tbody").innerHTML = "<tr><td colspan=5>" + CW.esc(e.message) + "</td></tr>"; });
        CW.loadReleases().catch(function (e) { CW.$("release-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
        CW.loadRules().catch(function (e) { CW.$("rule-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
        CW.loadExperiments().catch(function (e) { CW.$("experiment-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
        CW.loadKeys().catch(function (e) { CW.$("key-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
        CW.loadStats().catch(function () { /* inline in stats card */ });
      }).catch(function (e) { CW.toast(e.message); });
    }).catch(function (e) { CW.toast(e.message); });
  }

  CW.renderScopeHint = renderScopeHint;
  CW.renderProjectEnv = renderProjectEnv;
  CW.defaultEnvId = defaultEnvId;
  CW.loadProjects = loadProjects;
  CW.loadEnvs = loadEnvs;
  CW.createProject = createProject;
  CW.createEnv = createEnv;
  CW.refreshAll = refreshAll;
})();
