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
    try {
      var envDel = document.querySelector ? document.querySelector("[data-env-delete]") : null;
      if (envDel) {
        var single = CW.state.envs.length <= 1;
        envDel.disabled = single;
        if (envDel.setAttribute) {
          if (single) envDel.setAttribute("aria-disabled", "true");
          else if (envDel.removeAttribute) envDel.removeAttribute("aria-disabled");
        }
        envDel.title = single ? "Cannot delete the last environment" : "Delete selected environment and its keys/releases";
      }
    } catch (e2) { /* best-effort menu state */ }
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

  function projectDialogEls() {
    function opt(id) {
      try { return CW.$(id) || null; } catch (e) { return null; }
    }
    return {
      dlg: opt("project-dialog"),
      name: opt("project-dialog-name"),
      env: opt("project-dialog-env"),
      result: opt("project-dialog-result"),
      legacyName: opt("project-name"),
      legacyResult: opt("project-result")
    };
  }

  function openProjectDialog() {
    var d = projectDialogEls();
    if (d.name) d.name.value = "";
    if (d.env && !String(d.env.value || "").trim()) d.env.value = "production";
    if (d.result) d.result.textContent = "";
    if (d.legacyResult) d.legacyResult.textContent = "";
    if (d.dlg && typeof d.dlg.showModal === "function" && !d.dlg.open) {
      try { d.dlg.showModal(); } catch (e) { /* harness stub */ }
    } else if (d.dlg && d.dlg.setAttribute) {
      try { d.dlg.setAttribute("open", ""); } catch (e2) { /* noop */ }
    }
    try { if (d.name && d.name.focus) d.name.focus(); } catch (e3) { /* best-effort */ }
  }

  function closeProjectDialog() {
    var d = projectDialogEls();
    if (d.dlg && d.dlg.open && typeof d.dlg.close === "function") {
      try { d.dlg.close(); } catch (e) { /* already closed */ }
    } else if (d.dlg && d.dlg.removeAttribute) {
      try { d.dlg.removeAttribute("open"); } catch (e2) { /* noop */ }
    }
  }

  function saveProjectDialog(ev) {
    if (ev) ev.preventDefault();
    var d = projectDialogEls();
    var name = d.name && d.name.value != null ? String(d.name.value).trim() : "";
    var envSlug = d.env && d.env.value != null ? String(d.env.value).trim() : "";
    if (!name) {
      var msg = "Project name required.";
      if (d.result) d.result.textContent = msg;
      if (d.legacyResult) d.legacyResult.textContent = msg;
      if (d.name && d.name.focus) { try { d.name.focus(); } catch (e) { /* noop */ } }
      return Promise.resolve();
    }
    if (!envSlug) {
      var msg2 = "Environment required.";
      if (d.result) d.result.textContent = msg2;
      if (d.legacyResult) d.legacyResult.textContent = msg2;
      if (d.env && d.env.focus) { try { d.env.focus(); } catch (e2) { /* noop */ } }
      return Promise.resolve();
    }
    return createProjectWithEnv(name, envSlug);
  }

  function createProjectWithEnv(name, envSlug) {
    var d = projectDialogEls();
    return CW.apiMut("POST", "/api/collections/projects/records", { name: name }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      var msg = ok
        ? "Project created: " + (out.data.name || out.data.id)
        : "Project create failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (d.result) d.result.textContent = msg;
      if (d.legacyResult) d.legacyResult.textContent = msg;
      if (!ok) return out;
      CW.toast("Project created: " + (out.data.name || out.data.id), true);
      var newId = out.data.id;
      return CW.apiMut("POST", "/api/collections/environments/records", { project: newId, slug: envSlug }).then(function (eout) {
        var eok = eout.status === 200 || eout.status === 201;
        if (!eok) {
          var emsg = "Env create failed (" + eout.status + "): " + CW.serverMessage(eout.data);
          if (d.result) d.result.textContent = emsg;
          if (d.legacyResult) d.legacyResult.textContent = emsg;
          CW.toast(emsg);
          return eout;
        }
        CW.toast("Env created: " + (eout.data.slug || eout.data.id), true);
        if (d.name) d.name.value = "";
        if (d.env) d.env.value = "production";
        var legacy = d.legacyName;
        if (legacy) legacy.value = "";
        if (CW.markFormClean) {
          try { CW.markFormClean("project-dialog-form"); } catch (e) { /* noop */ }
        }
        closeProjectDialog();
        loadProjects().then(function () {
          if (newId) { CW.state.projectId = newId; CW.persistScope(); }
          if (CW.loadHomeStats) CW.loadHomeStats().catch(function () {});
          if (newId) {
            window.location.hash = "#/p/" + encodeURIComponent(newId);
            // route() picks it up via hashchange; cover no-change case.
            if (CW.parseHash().id !== newId) CW.openProject(newId, "");
          }
          return loadEnvs();
        }).then(function () {
          if (CW.loadFlags) CW.loadFlags().catch(function () {});
        }).catch(function () {});
        return out;
      });
    });
  }

  function createProject(ev, envArg) {
    if (ev && ev.preventDefault) ev.preventDefault();
    var nameArg = null;
    if (typeof ev === "string") nameArg = ev;
    var slugArg = typeof envArg === "string" ? envArg : null;
    if (nameArg != null) return createProjectWithEnv(String(nameArg).trim(), slugArg != null ? slugArg : "production").then(function (out) {
      return out;
    });
    var d = projectDialogEls();
    var hasDialog = !!(d.name || d.env);
    if (hasDialog) return saveProjectDialog(null);
    var legacyName = d.legacyName ? String(d.legacyName.value || "").trim() : "";
    if (!legacyName) {
      if (d.legacyResult) d.legacyResult.textContent = "Project name required.";
      return Promise.resolve();
    }
    return createProjectWithEnv(legacyName, "production");
  }

  function createEnv(ev) {
    if (ev && ev.preventDefault) ev.preventDefault();
    if (!CW.state.projectId) { CW.$("env-result").textContent = "Pick a project first."; return Promise.resolve(); }
    var slugArg = typeof ev === "string" ? ev.trim() : "";
    function post(slug) {
      return CW.apiMut("POST", "/api/collections/environments/records", { project: CW.state.projectId, slug: slug }).then(function (out) {
        var ok = out.status === 200 || out.status === 201;
        CW.$("env-result").textContent = ok
          ? "Env created: " + (out.data.slug || out.data.id)
          : "Env create failed (" + out.status + "): " + CW.serverMessage(out.data);
        if (ok) {
          CW.toast("Env created: " + (out.data.slug || out.data.id), true);
          loadEnvs().catch(function () {});
        }
        return out;
      });
    }
    if (slugArg) return post(slugArg);
    return CW.promptDialog("New environment", "", { title: "Add environment", okText: "Add", required: true, placeholder: "staging" }).then(function (slug) {
      if (slug == null) return;
      slug = String(slug).trim();
      if (!slug) { CW.$("env-result").textContent = "Env slug required."; return; }
      return post(slug);
    });
  }

  function projectByIdLocal(id) {
    if (CW.projectById) return CW.projectById(id);
    for (var i = 0; i < CW.state.projects.length; i++) {
      if (CW.state.projects[i].id === id) return CW.state.projects[i];
    }
    return null;
  }

  function envByIdLocal(id) {
    for (var i = 0; i < CW.state.envs.length; i++) {
      if (CW.state.envs[i].id === id) return CW.state.envs[i];
    }
    return null;
  }

  function renameProject(id) {
    var pid = id || CW.state.projectId;
    if (!pid) { CW.toast("Pick a project first."); return Promise.resolve(); }
    var cur = projectByIdLocal(pid);
    var curName = (cur && cur.name) || "";
    return CW.promptDialog("Rename project", curName, { title: "Rename project", okText: "Rename", required: true }).then(function (name) {
      if (name == null) return; // cancelled
      name = name.trim();
      if (!name) { CW.toast("Project name required."); return; }
      if (cur && name === cur.name) return;
      return CW.apiMut("PATCH", "/api/collections/projects/records/" + encodeURIComponent(pid), { name: name })
        .then(function (out) {
          var ok = out.status === 200 || out.status === 204;
          if (ok) {
            for (var i = 0; i < CW.state.projects.length; i++) {
              if (CW.state.projects[i].id === pid) { CW.state.projects[i].name = (out.data && out.data.name) || name; break; }
            }
            CW.persistScope();
            if (CW.renderDetailHeader) CW.renderDetailHeader();
            if (CW.loadHomeStats) CW.loadHomeStats().catch(function () {});
            CW.toast("Project renamed: " + name, true);
          } else {
            CW.toast("Rename failed (" + out.status + "): " + CW.serverMessage(out.data));
          }
          return out;
        });
    });
  }

  function deleteProject(id) {
    var pid = id || CW.state.projectId;
    if (!pid) { CW.toast("Pick a project first."); return Promise.resolve(); }
    var cur = projectByIdLocal(pid);
    var label = (cur && (cur.name || cur.id)) || pid;
    return CW.confirmDialog(
      'Delete project "' + label + '" and all its environments, flags, keys and releases? This cannot be undone.',
      { title: "Delete project", okText: "Delete", danger: true }
    ).then(function (ok) {
      if (!ok) return;
      return CW.apiMut("DELETE", "/api/collections/projects/records/" + encodeURIComponent(pid))
        .then(function (out) {
          var okDel = out.status === 200 || out.status === 204;
          if (!okDel) {
            CW.toast("Delete failed (" + out.status + "): " + CW.serverMessage(out.data));
            return out;
          }
          CW.state.projects = CW.state.projects.filter(function (p) { return p.id !== pid; });
          if (CW.state.projectId === pid) {
            CW.state.projectId = null;
            CW.state.envId = null;
            CW.state.envs = [];
          }
          CW.persistScope();
          CW.toast("Project deleted: " + label, true);
          try {
            if (window.location.hash !== "#/") window.location.hash = "#/";
          } catch (e) { /* non-browser harness */ }
          if (CW.showHome) CW.showHome();
          else renderProjectEnv();
          loadProjects().then(function () {
            if (CW.loadHomeStats) CW.loadHomeStats().catch(function () {});
          }).catch(function () {});
          return out;
        });
    });
  }

  function renameEnv(id) {
    var eid = id || CW.state.envId;
    if (!eid) { CW.toast("Pick an environment first."); return Promise.resolve(); }
    var cur = envByIdLocal(eid);
    var curSlug = (cur && cur.slug) || CW.state.envSlug || "";
    return CW.promptDialog("Rename environment", curSlug, { title: "Rename environment", okText: "Rename", required: true }).then(function (slug) {
      if (slug == null) return; // cancelled
      slug = slug.trim();
      if (!slug) { CW.toast("Env slug required."); return; }
      if (cur && slug === cur.slug) return;
      return CW.apiMut("PATCH", "/api/collections/environments/records/" + encodeURIComponent(eid), { slug: slug })
        .then(function (out) {
          var ok = out.status === 200 || out.status === 204;
          if (ok) {
            var nextSlug = (out.data && out.data.slug) || slug;
            for (var i = 0; i < CW.state.envs.length; i++) {
              if (CW.state.envs[i].id === eid) { CW.state.envs[i].slug = nextSlug; break; }
            }
            if (CW.state.envId === eid) CW.state.envSlug = nextSlug;
            CW.persistScope();
            renderProjectEnv();
            if (CW.loadReleases) CW.loadReleases().catch(function () {});
            if (CW.loadKeys) CW.loadKeys().catch(function () {});
            if (CW.loadStats) CW.loadStats().catch(function () {});
            CW.toast("Environment renamed: " + nextSlug, true);
          } else {
            CW.toast("Rename failed (" + out.status + "): " + CW.serverMessage(out.data));
          }
          return out;
        });
    });
  }

  function deleteEnv(id) {
    var eid = id || CW.state.envId;
    if (!eid) { CW.toast("Pick an environment first."); return Promise.resolve(); }
    if (CW.state.envs.length <= 1) {
      try { CW.$("env-result").textContent = "Cannot delete the last environment."; } catch (e) { /* noop */ }
      CW.toast("Cannot delete the last environment.");
      return Promise.resolve();
    }
    var cur = envByIdLocal(eid);
    var label = (cur && (cur.slug || cur.id)) || eid;
    return CW.confirmDialog(
      'Delete environment "' + label + '" and its keys and releases? This cannot be undone.',
      { title: "Delete environment", okText: "Delete", danger: true }
    ).then(function (ok) {
      if (!ok) return;
      return CW.apiMut("DELETE", "/api/collections/environments/records/" + encodeURIComponent(eid))
        .then(function (out) {
          var okDel = out.status === 200 || out.status === 204;
          if (!okDel) {
            CW.toast("Delete failed (" + out.status + "): " + CW.serverMessage(out.data));
            return out;
          }
          CW.state.envs = CW.state.envs.filter(function (e) { return e.id !== eid; });
          if (CW.state.envId === eid) CW.state.envId = null;
          CW.persistScope();
          CW.toast("Environment deleted: " + label, true);
          loadEnvs().then(function () {
            if (CW.loadReleases) CW.loadReleases().catch(function () {});
            if (CW.loadKeys) CW.loadKeys().catch(function () {});
            if (CW.loadStats) CW.loadStats().catch(function () {});
          }).catch(function () {});
          return out;
        });
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
  CW.openProjectDialog = openProjectDialog;
  CW.closeProjectDialog = closeProjectDialog;
  CW.saveProjectDialog = saveProjectDialog;
  CW.createEnv = createEnv;
  CW.renameProject = renameProject;
  CW.deleteProject = deleteProject;
  CW.renameEnv = renameEnv;
  CW.deleteEnv = deleteEnv;
  CW.refreshAll = refreshAll;
})();
