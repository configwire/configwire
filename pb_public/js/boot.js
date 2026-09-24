/* ConfigWire admin shell — boot.js (event wiring + init). Loads LAST. */
(function () {
  "use strict";

  var CW = window.CW;

  // ---- wiring ----

  document.addEventListener("DOMContentLoaded", function () {
    // Restore session (localStorage copy; memory-first once loaded).
    try { CW.state.token = localStorage.getItem(CW.LS_KEY); } catch (e) { CW.state.token = null; }
    CW.loadPersistedScope();
    if (CW.state.token) { CW.setLoggedIn(true); CW.refreshAll(); }

    CW.on("login-form", "submit", function (ev) {
      ev.preventDefault();
      CW.login(CW.$("login-email").value, CW.$("login-password").value).catch(function () { /* shown inline */ });
    });
    CW.on("logout-btn", "click", CW.logout);
    CW.on("refresh-scope", "click", CW.refreshAll);
    CW.on("back-to-projects", "click", function () {
      window.location.hash = "#/";
      CW.showHome();
    });
    window.addEventListener("hashchange", CW.route);
    CW.on("project-grid", "click", function (ev) {
      var t = ev && ev.target && ev.target.closest ? ev.target.closest("[data-project-id]") : null;
      if (!t) return;
      var id = t.getAttribute("data-project-id");
      if (id) window.location.hash = "#/p/" + encodeURIComponent(id);
    });
    CW.on("project-grid", "keydown", function (ev) {
      if (ev.key !== "Enter" && ev.key !== " ") return;
      var t = ev && ev.target && ev.target.closest ? ev.target.closest("[data-project-id]") : null;
      if (!t) return;
      ev.preventDefault();
      var id = t.getAttribute("data-project-id");
      if (id) window.location.hash = "#/p/" + encodeURIComponent(id);
    });
    CW.on("project-select", "change", function () {
      var id = CW.$("project-select").value || "";
      if (!id) return;
      if (window.location.hash !== "#/p/" + encodeURIComponent(id)) {
        window.location.hash = "#/p/" + encodeURIComponent(id);
      }
      CW.openProject(id, "");
    });
    CW.on("env-select", "change", function () {
      CW.state.envId = CW.$("env-select").value || null;
      var sel = CW.selectedEnv();
      if (sel && sel.slug) CW.state.envSlug = sel.slug;
      CW.persistScope();
      CW.renderScopeHint();
      CW.loadReleases().catch(function () {});
      CW.loadKeys().catch(function () {});
      CW.loadStats().catch(function () {});
    });
    CW.on("stats-flag", "change", function () { CW.loadStats().catch(function () {}); });
    CW.on("stats-since", "change", function () { CW.loadStats().catch(function () {}); });
    CW.on("refresh-flags", "click", function () { CW.loadFlags().catch(function (e) { CW.toast(e.message); }); });
    CW.on("refresh-releases", "click", function () { CW.loadReleases().catch(function (e) { CW.toast(e.message); }); });
    CW.on("refresh-experiments", "click", function () { CW.loadExperiments().catch(function (e) { CW.toast(e.message); }); });
    CW.on("refresh-keys", "click", function () { CW.loadKeys().catch(function (e) { CW.toast(e.message); }); });
    CW.on("refresh-stats", "click", function () {
      CW.loadStats().catch(function () { /* loadStats renders inline */ });
    });
    CW.on("stats-view", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var btn = null;
      if (t) {
        if (t.closest) btn = t.closest("#stats-copy");
        else if (t.id === "stats-copy") btn = t;
      }
      if (!btn) return;
      CW.copyStatsJson();
    });
    CW.on("group-filter", "change", CW.renderFlags);
    CW.on("flag-form", "submit", CW.saveFlag);
    CW.on("flag-default", "input", CW.updateFlagDefaultHint);
    CW.on("flag-rules-condition", "input", CW.updateFlagRuleHints);
    CW.on("flag-rules-value", "input", CW.updateFlagRuleHints);
    CW.on("exp-variants", "input", CW.updateExpVariantsHint);
    document.addEventListener("click", function (ev) {
      var t = ev && ev.target && ev.target.closest ? ev.target.closest(".json-expand") : null;
      if (t && t.getAttribute) {
        var target = t.getAttribute("data-target");
        if (target) CW.openJsonEditorFor(target);
      }
    });
    CW.on("json-editor-text", "input", CW.updateEditorStatus);
    CW.on("json-editor-format", "click", function () {
      var ta = CW.$("json-editor-text");
      if (!ta) return;
      var parsed = CW.jsonDetail(ta.value);
      if (!parsed.ok) { CW.updateEditorStatus(); return; }
      ta.value = JSON.stringify(parsed.value, null, 2);
      CW.updateEditorStatus();
      try { ta.focus(); } catch (e) { /* best-effort */ }
    });
    CW.on("json-editor-save", "click", function () { CW.closeJsonEditor(true); });
    CW.on("json-editor-cancel", "click", function () { CW.closeJsonEditor(false); });
    CW.updateAllJsonHints();
    if (CW.updateFlagRuleHints) CW.updateFlagRuleHints();
    CW.on("flag-reset", "click", function () {
      CW.$("flag-id").value = "";
      CW.$("flag-form").reset();
      CW.renderFlagGroupSelect();
      CW.updateFlagDefaultHint();
    });
    CW.on("project-create-form", "submit", CW.createProject);
    CW.on("env-create-form", "submit", CW.createEnv);
    CW.on("group-create-form", "submit", CW.createGroup);
    CW.on("experiment-form", "submit", CW.createExperiment);
    CW.on("key-form", "submit", CW.createKey);
    CW.on("key-copy", "click", function () {
      var v = CW.$("key-once-value").textContent;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(v).then(function () { CW.toast("copied", true); }, function () { CW.toast("copy failed"); });
      } else {
        var ta = document.createElement("textarea");
        ta.value = v;
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand("copy"); CW.toast("copied", true); }
        catch (e) { CW.toast("copy failed"); }
        document.body.removeChild(ta);
      }
    });

    CW.on("flag-tbody", "click", function (ev) {
      var t = ev.target;
      var fr = t && t.getAttribute && t.getAttribute("data-flag-rules");
      if (fr) {
        if (CW.openFlagRulesDialog) CW.openFlagRulesDialog(fr);
        return;
      }
      var del = t && t.getAttribute && t.getAttribute("data-delete-flag");
      if (del) {
        if (!window.confirm("Delete this flag and all its rules?")) return;
        CW.deleteFlag(del).then(function () {
          CW.loadRules().catch(function (e) { CW.toast(e.message); });
        }).catch(function (e) { CW.toast(e.message); });
        return;
      }
      var k = t && t.getAttribute && t.getAttribute("data-stats-flag");
      if (k) {
        var sflag = CW.$("stats-flag");
        if (sflag) {
          var hasOpt = false;
          for (var i = 0; i < sflag.options.length; i++) {
            if (sflag.options[i].value === k) { hasOpt = true; break; }
          }
          if (!hasOpt) {
            var opt = document.createElement("option");
            opt.value = k;
            opt.textContent = k;
            sflag.appendChild(opt);
          }
          sflag.value = k;
        }
        CW.loadStats().catch(function () {});
        var card = CW.$("stats") && CW.$("stats").closest ? CW.$("stats").closest("section") : null;
        if (card && card.scrollIntoView) card.scrollIntoView();
        return;
      }
      var fid = t && t.getAttribute && t.getAttribute("data-edit-flag");
      if (fid) {
        for (var i = 0; i < CW.state.flags.length; i++) {
          if (CW.state.flags[i].id === fid) {
            var f = CW.state.flags[i];
            CW.$("flag-id").value = f.id;
            CW.$("flag-key").value = f.key || "";
            CW.$("flag-type").value = f.type || "bool";
            CW.$("flag-group").value = f.group || "";
            CW.$("flag-default").value = JSON.stringify(f.defaultValue === undefined ? null : f.defaultValue);
            CW.updateFlagDefaultHint();
            CW.$("flag-result").textContent = "editing " + (f.key || fid);
            break;
          }
        }
      }
    });

    CW.on("flag-rules-list", "click", function (ev) {
      var t = ev.target;
      var del = t && t.getAttribute && t.getAttribute("data-delete-flag-rule");
      if (del) {
        if (!window.confirm("Delete this rule?")) return;
        CW.deleteRule(del).then(function () {
          if (CW.renderFlagRulesList) CW.renderFlagRulesList();
          if (CW.renderFlags) CW.renderFlags();
        }).catch(function (e) { CW.toast(e.message); });
        return;
      }
      var rid = t && t.getAttribute && t.getAttribute("data-edit-flag-rule");
      if (rid) {
        var found = null;
        for (var i = 0; i < CW.state.rules.length; i++) {
          if (CW.state.rules[i].id === rid) { found = CW.state.rules[i]; break; }
        }
        if (!found) { CW.toast("rule not found: " + rid); return; }
        CW.$("flag-rules-flag-id").value = found.flag || CW.state.activeFlagRulesId || "";
        CW.$("flag-rules-id").value = found.id;
        CW.$("flag-rules-priority").value = found.priority == null ? 0 : found.priority;
        try {
          CW.$("flag-rules-condition").value = typeof found.condition === "string"
            ? found.condition
            : JSON.stringify(found.condition);
        } catch (e) { CW.$("flag-rules-condition").value = "{}"; }
        try {
          CW.$("flag-rules-value").value = found.value === undefined
            ? "null"
            : JSON.stringify(found.value);
        } catch (e2) { CW.$("flag-rules-value").value = "null"; }
        if (CW.updateFlagRuleHints) CW.updateFlagRuleHints();
        CW.$("flag-rules-result").textContent = "editing " + found.id;
      }
    });
    CW.on("flag-rules-form", "submit", function (ev) {
      ev.preventDefault();
      if (CW.saveFlagRule) CW.saveFlagRule(ev).catch(function (e) { CW.toast(e.message); });
    });
    CW.on("flag-rules-reset", "click", function () {
      if (CW.resetFlagRuleForm) CW.resetFlagRuleForm();
    });
    CW.on("flag-rules-close", "click", function () {
      if (CW.resetFlagRuleForm) CW.resetFlagRuleForm();
      if (CW.closeFlagRulesDialog) CW.closeFlagRulesDialog();
      else { var dlg = CW.$("flag-rules-dialog"); if (dlg && dlg.open) dlg.close(); }
    });

    CW.on("release-list", "click", function (ev) {      var v = ev.target && ev.target.getAttribute && ev.target.getAttribute("data-rollback-version");
      if (!v) return;
      CW.rollback(v, "rollback via admin UI").then(function (out) {
        CW.$("publish-result").textContent = out.status === 200
          ? "rolled back: now v" + out.data.version
          : "rollback failed (" + out.status + "): " + CW.serverMessage(out.data);
        CW.loadReleases().catch(function () {});
      });
    });

    CW.on("key-list", "click", function (ev) {
      var id = ev.target && ev.target.getAttribute && ev.target.getAttribute("data-revoke-key");
      if (id) CW.revokeKey(id);
    });

    CW.on("publish-form", "submit", function (ev) {
      ev.preventDefault();
      var base = parseInt(CW.$("publish-base").value, 10);
      CW.publish(CW.$("publish-note").value, base).then(function (out) {
        if (out.status === 200) {
          CW.$("publish-result").textContent = "published v" + out.data.version + " etag " + out.data.etag;
        } else if (out.status === 409) {
          CW.$("publish-result").textContent = "stale baseVersion (409): currentVersion is " +
            out.data.currentVersion + " — refreshed latest, retry publish. " + CW.serverMessage(out.data);
        } else {
          CW.$("publish-result").textContent = "publish failed (" + out.status + "): " + CW.serverMessage(out.data);
        }
        CW.loadReleases().catch(function () {});
      });
    });

    // HTMX: inject the BARE superuser token on every HTMX-driven request so
    // partials (if any) share the same auth as fetch calls.
    document.body.addEventListener("htmx:configRequest", function (ev) {
      if (CW.state.token) ev.detail.headers["Authorization"] = CW.state.token; // bare, no TOKEN prefix
    });
  });

  // Exposed for QA/contract checks (what the UI sends).
  window.cwAdmin = {
    get state() { return CW.state; },
    get login() { return CW.login; }, get logout() { return CW.logout; },
    get loadFlags() { return CW.loadFlags; }, get loadReleases() { return CW.loadReleases; },
    get loadStats() { return CW.loadStats; },
    get renderStats() { return CW.renderStats; },
    get loadProjects() { return CW.loadProjects; }, get loadEnvs() { return CW.loadEnvs; },
    get loadRules() { return CW.loadRules; }, get loadExperiments() { return CW.loadExperiments; },
    get loadKeys() { return CW.loadKeys; },
    get saveFlag() { return CW.saveFlag; },
    get deleteRule() { return CW.deleteRule; },
    get createExperiment() { return CW.createExperiment; },
    get createKey() { return CW.createKey; }, get revokeKey() { return CW.revokeKey; },
    get createProject() { return CW.createProject; }, get createEnv() { return CW.createEnv; },
    get createGroup() { return CW.createGroup; },
    get deleteFlag() { return CW.deleteFlag; },
    get openFlagRulesDialog() { return CW.openFlagRulesDialog; },
    get renderFlagRulesList() { return CW.renderFlagRulesList; },
    get saveFlagRule() { return CW.saveFlagRule; },
    get resetFlagRuleForm() { return CW.resetFlagRuleForm; },
    get closeFlagRulesDialog() { return CW.closeFlagRulesDialog; },
    get conditionHTML() { return CW.conditionHTML; }, get valueHTML() { return CW.valueHTML; },
    get publish() { return CW.publish; }, get rollback() { return CW.rollback; },
    get openProject() { return CW.openProject; }, get showHome() { return CW.showHome; },
    get route() { return CW.route; }, get parseHash() { return CW.parseHash; },
    get renderProjectCards() { return CW.renderProjectCards; },
    get loadHomeStats() { return CW.loadHomeStats; },
    get openJsonEditor() { return CW.openJsonEditor; }, get closeJsonEditor() { return CW.closeJsonEditor; },
    get openJsonEditorFor() { return CW.openJsonEditorFor; },
    get updateFlagDefaultHint() { return CW.updateFlagDefaultHint; },
    get updateEditorStatus() { return CW.updateEditorStatus; },
    get updateJsonHint() { return CW.updateJsonHint; },
    get updateAllJsonHints() { return CW.updateAllJsonHints; },
    get updateRuleConditionHint() { return CW.updateRuleConditionHint; },
    get updateRuleValueHint() { return CW.updateRuleValueHint; },
    get updateExpVariantsHint() { return CW.updateExpVariantsHint; },
  };
})();
