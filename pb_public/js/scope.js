/* ConfigWire admin shell — scope.js (project/env scope + create + refreshAll). */
(function () {
  "use strict";

  var CW = window.CW;

  function renderProjectEnv() {
    var ps = CW.$("project-select");
    var cur = CW.state.projectId;
    var allOpt = '<option value="__all">All</option>';
    ps.innerHTML = CW.state.projects.length
      ? allOpt + CW.state.projects.map(function (p) {
          return '<option value="' + CW.esc(p.id) + '">' + CW.esc(p.name || p.id) + "</option>";
        }).join("")
      : allOpt + '<option value="" disabled>(no projects)</option>';
    if (cur && CW.state.projects.some(function (p) { return p.id === cur; })) ps.value = cur;
    else if (CW.state.projects.length) { ps.value = CW.state.projects[0].id; CW.state.projectId = CW.state.projects[0].id; }
    else { ps.value = "__all"; CW.state.projectId = null; }
    // Home lists all projects: keep the last project in state for instant
    // preload, but the dropdown must read All. Without this, the async
    // loadEnvs()->renderProjectEnv in refreshAll overwrites syncSidebar's
    // All with the persisted project after every refresh.
    if (CW.state.view === "home") ps.value = "__all";

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
    try {
      var envName = CW.$("env-group-name");
      if (envName) envName.textContent = (sel && sel.slug) || CW.state.envSlug || "env";
    } catch (e) { /* best-effort label */ }
  }

  function defaultEnvId() {
    for (var i = 0; i < CW.state.envs.length; i++) {
      if (CW.state.envs[i].slug === "dev") return CW.state.envs[i].id;
    }
    return CW.state.envs.length ? CW.state.envs[0].id : "";
  }

  function loadProjects() {
    return CW.apiAll("/api/collections/projects/records?perPage=200").then(function (items) {
      CW.state.projects = (items || []).slice().sort(function (a, b) {
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
    return CW.apiAll("/api/collections/environments/records" + filter).then(function (items) {
      CW.state.envs = (items || []).slice().sort(function (a, b) {
        return (a.slug || "") < (b.slug || "") ? -1 : 1;
      });
    }, function () {
      // Fallback: fetch all then filter client-side when the API filter fails.
      return CW.apiAll("/api/collections/environments/records?perPage=200").then(function (items) {
        CW.state.envs = (items || []).filter(function (e) { return e.project === CW.state.projectId; });
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
    if (!name) { CW.$("project-result").textContent = "Project name required."; return Promise.resolve(); }
    return CW.apiMut("POST", "/api/collections/projects/records", { name: name }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("project-result").textContent = ok
        ? "Project created: " + (out.data.name || out.data.id)
        : "Project create failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (ok) {
        CW.toast("Project created: " + (out.data.name || out.data.id), true);
        CW.$("project-name").value = "";
        if (CW.markFormClean) CW.markFormClean("project-create-form");
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
          CW.loadFlags().catch(function () {});
        }).catch(function () {});
      }
      return out;
    });
  }

  function createEnv(ev) {
    if (ev) ev.preventDefault();
    if (!CW.state.projectId) { CW.$("env-result").textContent = "Pick a project first."; return Promise.resolve(); }
    var slug = CW.$("env-slug").value.trim();
    if (!slug) { CW.$("env-result").textContent = "Env slug required."; return Promise.resolve(); }
    return CW.apiMut("POST", "/api/collections/environments/records", { project: CW.state.projectId, slug: slug }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("env-result").textContent = ok
        ? "Env created: " + (out.data.slug || out.data.id)
        : "Env create failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (ok) {
        CW.toast("Env created: " + (out.data.slug || out.data.id), true);
        CW.$("env-slug").value = "";
        if (CW.markFormClean) CW.markFormClean("env-create-form");
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
      // Flags first: loadExperiments filters by CW.state.flags and
      // mergedRules resolves against merged flags, so rules/experiments
      // must wait for flags or first-open counts render as (0).
      loadEnvs().then(function () {
        CW.loadReleases().catch(function (e) { CW.$("release-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
        CW.loadKeys().catch(function (e) { CW.$("key-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
        CW.loadStats().catch(function () { /* inline in stats card */ });
        CW.loadFlags().then(loadScopedAfterFlags, function (e) {
          CW.$("flag-folders").innerHTML = "<p class=\"muted\">" + CW.esc(e.message) + "</p>";
          loadScopedAfterFlags();
        });
        if (CW.state.view === "home" && CW.syncSidebar) CW.syncSidebar();
        function loadScopedAfterFlags() {
          CW.loadRules().catch(function (e) { CW.toast(e.message); });
          CW.loadExperiments().catch(function (e) { CW.toast(e.message); });
        }
      }).catch(function (e) { CW.toast(e.message); });
    }).catch(function (e) { CW.toast(e.message); });
  }

  CW.renderProjectEnv = renderProjectEnv;
  CW.defaultEnvId = defaultEnvId;
  CW.loadProjects = loadProjects;
  CW.loadEnvs = loadEnvs;
  CW.createProject = createProject;
  CW.createEnv = createEnv;
  CW.refreshAll = refreshAll;
})();
