/* ConfigWire admin shell — boot.js (event wiring + init). Loads LAST. */
(function () {
  "use strict";

  var CW = window.CW;

  function cwConfirm(m, o) {
    return CW.confirmDialog(m, o);
  }

  // ---- wiring ----

  document.addEventListener("DOMContentLoaded", function () {
    // Restore session (localStorage copy; memory-first once loaded).
    try { CW.state.token = localStorage.getItem(CW.LS_KEY); } catch (e) { CW.state.token = null; }
    CW.loadPersistedScope();
    if (CW.state.token) { CW.setLoggedIn(true); CW.refreshAll(); }
    else if (CW.checkSetup) { CW.checkSetup().catch(function () {}); }

    CW.on("login-form", "submit", function (ev) {
      ev.preventDefault();
      CW.login(CW.$("login-email").value, CW.$("login-password").value).catch(function () { /* shown inline */ });
    });
    CW.on("logout-btn", "click", CW.logout);
    CW.on("setup-form", "submit", function (ev) {
      if (CW.createSetup) CW.createSetup(ev).catch(function () { /* shown inline */ });
      else ev.preventDefault();
    });
    CW.on("account-create-form", "submit", function (ev) {
      if (CW.createAccount) CW.createAccount(ev).catch(function (e) { CW.toast(e.message); });
      else ev.preventDefault();
    });
    CW.on("refresh-accounts", "click", function () {
      if (CW.loadAccounts) CW.loadAccounts().catch(function (e) { CW.toast(e.message); });
    });
    CW.on("account-list", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var pw = t && t.getAttribute ? t.getAttribute("data-account-password") : null;
      if (pw) {
        if (CW.changeAccountPassword) CW.changeAccountPassword(pw).catch(function (e) { CW.toast(e.message); });
        return;
      }
      var del = t && t.getAttribute ? t.getAttribute("data-account-delete") : null;
      if (del) {
        if (CW.deleteAccount) CW.deleteAccount(del).catch(function (e) { CW.toast(e.message); });
      }
    });
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
    CW.on("all-projects-btn", "click", function () {
      CW.state.projectId = null;
      CW.state.envId = null;
      CW.state.envs = [];
      CW.persistScope();
      if (CW.renderProjectEnv) CW.renderProjectEnv();
      if (window.location.hash !== "#/") window.location.hash = "#/";
      CW.showHome();
    });
    CW.on("env-select", "change", function () {
      CW.state.envId = CW.$("env-select").value || null;
      var sel = CW.selectedEnv();
      if (sel && sel.slug) CW.state.envSlug = sel.slug;
      CW.persistScope();
      var eg = CW.$("env-group-name");
      if (eg) eg.textContent = (sel && sel.slug) || CW.state.envSlug || "env";
      CW.loadReleases().catch(function () {});
      CW.loadKeys().catch(function () {});
      CW.loadStats().catch(function () {});
    });
    CW.on("stats-since", "change", function () { CW.loadStats().catch(function () {}); });
    CW.on("refresh-flags", "click", function () { CW.loadFlags().catch(function (e) { CW.toast(e.message); }); });
    CW.on("refresh-releases", "click", function () { CW.loadReleases().catch(function (e) { CW.toast(e.message); }); });
    CW.on("refresh-keys", "click", function () { CW.loadKeys().catch(function (e) { CW.toast(e.message); }); });
    CW.on("refresh-limits", "click", function () { CW.loadLimits().catch(function (e) { CW.toast(e.message); }); });
    CW.on("refresh-settings-limits", "click", function () { CW.loadLimits().catch(function (e) { CW.toast(e.message); }); });
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
    CW.on("flag-form", "submit", CW.saveFlag);
    // Drafts are now the unpublished signal: staging a draft marks publish
    // dirty via refreshDraftChrome, so no keystroke-level zero-arg dirty
    // markers. Per-form armed gating stays (submit enables on input = the
    // pre-stage signal; staging calls markFormClean on success).
    if (CW.armDirtyForm) {
      ["flag-form", "flag-rules-form", "experiment-form",
        "project-dialog-form", "account-create-form",
        "key-limits-form", "limits-form", "settings-admin-form"
      ].forEach(function (id) { CW.armDirtyForm(id); });
    }
    CW.on("flag-type", "change", function () {
      if (CW.syncFlagDefaultForType) CW.syncFlagDefaultForType();
      else if (CW.updateFlagDefaultHint) CW.updateFlagDefaultHint();
    });
    CW.on("flag-default", "input", CW.updateFlagDefaultHint);
    CW.on("flag-rules-condition", "input", function () {
      if (CW.updateFlagRuleHints) CW.updateFlagRuleHints();
      if (CW.syncRuleBuilderFromCondition) CW.syncRuleBuilderFromCondition();
    });
    CW.on("flag-rules-field", "change", function () {
      var fieldSel = CW.$("flag-rules-field");
      var base = fieldSel ? fieldSel.value : "platform";
      var opSel = CW.$("flag-rules-op");
      var op = opSel ? opSel.value : "";
      if (CW.populateRuleOpOptions) CW.populateRuleOpOptions(base);
      var freshOp = CW.$("flag-rules-op");
      if (freshOp) op = freshOp.value;
      if (CW.updateRuleBuilderVisibility) CW.updateRuleBuilderVisibility(base, op);
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-op", "change", function () {
      var fieldSel = CW.$("flag-rules-field");
      var opSel = CW.$("flag-rules-op");
      if (CW.updateRuleBuilderVisibility) {
        CW.updateRuleBuilderVisibility(
          fieldSel ? fieldSel.value : "platform",
          opSel ? opSel.value : ""
        );
      }
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-custom", "input", function () {
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-cond-value", "input", function () {
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-cond-lo", "input", function () {
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-cond-hi", "input", function () {
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-seed", "input", function () {
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-value", "input", CW.updateFlagRuleHints);
    CW.on("exp-flag-select", "change", function () {
      if (CW.updateVariantPlaceholders) CW.updateVariantPlaceholders();
    });
    CW.on("exp-variant-add", "click", function () {
      if (CW.addVariantRow) CW.addVariantRow("", 0, "");
      if (CW.recalcLastVariantWeight) CW.recalcLastVariantWeight();
      if (CW.updateExpVariantsHint) CW.updateExpVariantsHint();
    });
    CW.on("exp-variants-balance", "click", function () {
      if (CW.balanceVariantsBuilder) CW.balanceVariantsBuilder();
      if (CW.applyVariantsBuilderToVariants) CW.applyVariantsBuilderToVariants();
    });
    CW.on("exp-variants-builder-rows", "click", function (ev) {
      var t = ev && ev.target && ev.target.closest ? ev.target.closest(".exp-variant-remove") : null;
      if (!t) return;
      var row = t.closest ? t.closest(".exp-variant-row") : null;
      if (!row) row = t.parentNode;
      if (row && row.parentNode) row.parentNode.removeChild(row);
      if (CW.recalcLastVariantWeight) CW.recalcLastVariantWeight();
      if (CW.updateExpVariantsHint) CW.updateExpVariantsHint();
    });
    CW.on("exp-variants-builder-rows", "input", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var w = null;
      if (t) {
        if (t.closest) w = t.closest(".exp-variant-weight");
        else if (t.className && String(t.className).indexOf("exp-variant-weight") !== -1) w = t;
      }
      if (w && CW.recalcLastVariantWeight) CW.recalcLastVariantWeight();
      if (CW.updateExpVariantsHint) CW.updateExpVariantsHint();
    });
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
    if (CW.updateVariantPlaceholders) CW.updateVariantPlaceholders();
    if (CW.updateFlagRuleHints) CW.updateFlagRuleHints();
    if (CW.resetRuleBuilder) CW.resetRuleBuilder();
    if (CW.syncRuleBuilderFromCondition) CW.syncRuleBuilderFromCondition();
    CW.on("flag-reset", "click", function () {
      if (CW.resetFlagForm) CW.resetFlagForm();
      else {
        CW.$("flag-id").value = "";
        CW.$("flag-form").reset();
        CW.renderFlagGroupSelect();
        CW.updateFlagDefaultHint();
      }
      if (CW.markFormClean) CW.markFormClean("flag-form");
    });
    CW.on("flag-add-btn", "click", function () {
      if (CW.openFlagDialog) CW.openFlagDialog(null);
    });
    CW.on("flag-dialog-close", "click", function () {
      if (CW.closeFlagDialog) CW.closeFlagDialog();
      else { var dlg = CW.$("flag-dialog"); if (dlg && dlg.open) dlg.close(); }
    });
    CW.on("release-dialog-close", "click", function () {
      if (CW.closeReleaseDialog) CW.closeReleaseDialog();
      else { var dlg = CW.$("release-dialog"); if (dlg && dlg.open) dlg.close(); }
    });
    CW.on("release-dialog-ok", "click", function () {
      if (CW.closeReleaseDialog) CW.closeReleaseDialog();
      else { var dlg = CW.$("release-dialog"); if (dlg && dlg.open) dlg.close(); }
    });
    CW.on("transfer-export-btn", "click", function () {
      try {
        if (typeof CW.openExportDialog !== "function") { CW.toast("Export unavailable."); return; }
        CW.openExportDialog();
      } catch (e) { CW.toast((e && e.message) || "Export failed."); }
    });
    CW.on("transfer-import-btn", "click", function () {
      try {
        if (typeof CW.openImportDialog !== "function") { CW.toast("Import unavailable."); return; }
        CW.openImportDialog();
      } catch (e) { CW.toast((e && e.message) || "Import failed."); }
    });
    CW.on("transfer-draft-view-btn", "click", function () {
      try {
        if (typeof CW.openDraftSnapshotDialog !== "function") { CW.toast("Draft snapshot unavailable."); return; }
        CW.openDraftSnapshotDialog();
      } catch (e) { CW.toast((e && e.message) || "Draft snapshot failed."); }
    });
    CW.on("transfer-export-close", "click", function () {
      try {
        if (typeof CW.closeExportDialog !== "function") return;
        CW.closeExportDialog();
      } catch (e) { CW.toast((e && e.message) || "Close failed."); }
    });
    CW.on("transfer-export-ok", "click", function () {
      try {
        if (typeof CW.closeExportDialog !== "function") return;
        CW.closeExportDialog();
      } catch (e) { CW.toast((e && e.message) || "Close failed."); }
    });
    CW.on("transfer-export-copy", "click", function () {
      try {
        var ta = CW.$("transfer-export-text");
        var v = ta && ta.value != null ? String(ta.value) : "";
        if (!v) { CW.toast("Nothing to copy."); return; }
        if (typeof navigator !== "undefined" && navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(v).then(function () { CW.toast("Copied", true); }, function () { CW.toast("Copy failed"); });
        } else {
          var tmp = document.createElement("textarea");
          tmp.value = v;
          document.body.appendChild(tmp);
          tmp.select();
          try { document.execCommand("copy"); CW.toast("Copied", true); }
          catch (e2) { CW.toast("Copy failed"); }
          document.body.removeChild(tmp);
        }
      } catch (e) { CW.toast((e && e.message) || "Copy failed"); }
    });
    CW.on("transfer-export-download", "click", function () {
      try {
        var ta = CW.$("transfer-export-text");
        var v = ta && ta.value != null ? String(ta.value) : "";
        if (!v) { CW.toast("Nothing to download."); return; }
        var blob = new Blob([v], { type: "application/json" });
        var URLobj = window.URL || window.webkitURL;
        var url = URLobj.createObjectURL(blob);
        var a = document.createElement("a");
        a.href = url;
        a.download = "configwire-flags.json";
        document.body.appendChild(a);
        if (a.click) a.click();
        document.body.removeChild(a);
        try { URLobj.revokeObjectURL(url); } catch (e2) { /* best-effort */ }
      } catch (e) { CW.toast((e && e.message) || "Download failed."); }
    });
    CW.on("transfer-import-close", "click", function () {
      try {
        if (typeof CW.closeImportDialog !== "function") return;
        CW.closeImportDialog();
      } catch (e) { CW.toast((e && e.message) || "Close failed."); }
    });
    CW.on("transfer-import-cancel", "click", function () {
      try {
        if (typeof CW.closeImportDialog !== "function") return;
        CW.closeImportDialog();
      } catch (e) { CW.toast((e && e.message) || "Close failed."); }
    });
    CW.on("transfer-import-stage", "click", function () {
      try {
        if (typeof CW.parseTransferSnapshot !== "function" ||
            typeof CW.validateTransferSnapshot !== "function" ||
            typeof CW.importTransferSnapshot !== "function") { CW.toast("Import unavailable."); return; }
        var ta = CW.$("transfer-import-text");
        var raw = ta && ta.value != null ? String(ta.value) : "";
        var parsed = CW.parseTransferSnapshot(raw);
        if (!parsed || !parsed.ok) {
          var perr = (parsed && parsed.error) || "Invalid snapshot.";
          var resEl = CW.$("transfer-import-result");
          if (resEl) resEl.textContent = perr;
          else CW.toast(perr);
          return;
        }
        var validated = CW.validateTransferSnapshot(parsed.snapshot);
        if (!validated || !validated.ok) {
          var verr = (validated && validated.error) || "Invalid snapshot.";
          var resEl2 = CW.$("transfer-import-result");
          if (resEl2) resEl2.textContent = verr;
          else CW.toast(verr);
          return;
        }
        var counts = CW.importTransferSnapshot(parsed.snapshot) || {};
        var resEl3 = CW.$("transfer-import-result");
        if (resEl3) resEl3.textContent = "Drafts staged.";
        if (typeof CW.closeImportDialog === "function") CW.closeImportDialog();
        CW.toast("Import staged.", true);
      } catch (e) { CW.toast((e && e.message) || "Import failed."); }
    });
    CW.on("transfer-import-file", "change", function (ev) {
      try {
        var input = (ev && ev.target) || CW.$("transfer-import-file");
        var f = input && input.files ? input.files[0] : null;
        if (!f) return;
        if (typeof FileReader === "undefined") { CW.toast("File import unavailable."); return; }
        var rd = new FileReader();
        rd.onload = function () {
          try {
            var ta = CW.$("transfer-import-text");
            if (ta) ta.value = rd.result != null ? String(rd.result) : "";
          } catch (e) { CW.toast((e && e.message) || "File read failed."); }
        };
        rd.onerror = function () { CW.toast("File read failed."); };
        rd.readAsText(f);
      } catch (e) { CW.toast((e && e.message) || "File read failed."); }
    });
    CW.on("transfer-diff-close", "click", function () {
      try {
        if (typeof CW.closeDraftSnapshotDialog !== "function") return;
        CW.closeDraftSnapshotDialog();
      } catch (e) { CW.toast((e && e.message) || "Close failed."); }
    });
    CW.on("transfer-diff-ok", "click", function () {
      try {
        if (typeof CW.closeDraftSnapshotDialog !== "function") return;
        CW.closeDraftSnapshotDialog();
      } catch (e) { CW.toast((e && e.message) || "Close failed."); }
    });
    CW.on("project-add-btn", "click", function () {
      if (CW.openProjectDialog) CW.openProjectDialog();
    });
    CW.on("project-dialog-form", "submit", function (ev) {
      if (CW.saveProjectDialog) CW.saveProjectDialog(ev).catch(function (e) { CW.toast(e.message); });
      else ev.preventDefault();
    });
    CW.on("project-dialog-close", "click", function () {
      if (CW.closeProjectDialog) CW.closeProjectDialog();
    });
    CW.on("project-dialog-cancel", "click", function () {
      if (CW.closeProjectDialog) CW.closeProjectDialog();
    });
    CW.on("project-rename-btn", "click", function () {
      if (CW.renameProject) CW.renameProject().catch(function (e) { CW.toast(e.message); });
    });
    CW.on("project-delete-btn", "click", function () {
      if (CW.deleteProject) CW.deleteProject().catch(function (e) { CW.toast(e.message); });
    });
    CW.on("env-bar", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var menuBtn = null;
      if (t && t.closest) menuBtn = t.closest("[data-env-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-env-menu") != null) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".env-menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeEnvMenu();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var addBtn = t && t.closest ? t.closest("[data-env-add]") : null;
      if (!addBtn && t && t.getAttribute && t.getAttribute("data-env-add") != null) addBtn = t;
      if (addBtn) {
        closeEnvMenu();
        if (CW.createEnv) CW.createEnv().catch(function (e) { CW.toast(e.message); });
        return;
      }
      var renBtn = t && t.closest ? t.closest("[data-env-rename]") : null;
      if (!renBtn && t && t.getAttribute && t.getAttribute("data-env-rename") != null) renBtn = t;
      if (renBtn) {
        closeEnvMenu();
        if (CW.renameEnv) CW.renameEnv().catch(function (e) { CW.toast(e.message); });
        return;
      }
      var delBtn = t && t.closest ? t.closest("[data-env-delete]") : null;
      if (!delBtn && t && t.getAttribute && t.getAttribute("data-env-delete") != null) delBtn = t;
      if (delBtn) {
        closeEnvMenu();
        if (CW.deleteEnv) CW.deleteEnv().catch(function (e) { CW.toast(e.message); });
      }
    });
    CW.on("group-add-btn", "click", function () { CW.promptCreateGroup().catch(function (e) { CW.toast(e.message); }); });
    CW.on("experiment-form", "submit", CW.saveExperiment);
    CW.on("experiment-reset", "click", function () {
      if (CW.resetExperimentForm) CW.resetExperimentForm();
      else {
        CW.$("exp-id").value = "";
        CW.$("experiment-form").reset();
        if (CW.resetVariantsBuilder) CW.resetVariantsBuilder();
        if (CW.updateExpVariantsHint) CW.updateExpVariantsHint();
        CW.$("experiment-result").textContent = "";
      }
      if (CW.markFormClean) CW.markFormClean("experiment-form");
    });
    CW.on("experiment-dialog-close", "click", function () {
      if (CW.closeExperimentDialog) CW.closeExperimentDialog();
      else { var dlg = CW.$("experiment-dialog"); if (dlg && dlg.open) dlg.close(); }
    });
    CW.on("flag-experiments-close", "click", function () {
      if (CW.closeFlagExperimentsDialog) CW.closeFlagExperimentsDialog();
      else { var fdlg = CW.$("flag-experiments-dialog"); if (fdlg && fdlg.open) fdlg.close(); }
    });
    CW.on("flag-experiments-add", "click", function () {
      if (CW.openExperimentDialog) CW.openExperimentDialog(null, CW.state.activeFlagExperimentsId || "");
    });
    CW.on("flag-experiments-list", "click", function (ev) {
      var t = ev.target;
      var menuBtn = null;
      if (t && t.closest) menuBtn = t.closest("[data-exp-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-exp-menu")) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".exp-menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeExpMenus();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var inMenu = t && t.closest ? t.closest(".exp-menu") : null;
      var delBtn = t && t.closest ? t.closest("[data-delete-experiment]") : null;
      var editBtn = t && t.closest ? t.closest("[data-edit-experiment]") : null;
      if (!delBtn && t && t.getAttribute && t.getAttribute("data-delete-experiment")) delBtn = t;
      if (!editBtn && t && t.getAttribute && t.getAttribute("data-edit-experiment")) editBtn = t;
      var del = delBtn && delBtn.getAttribute ? delBtn.getAttribute("data-delete-experiment") : null;
      if (del) {
        if (inMenu) closeExpMenus();
        cwConfirm("Delete this experiment?", {title: "Delete experiment", okText: "Delete", danger: true}).then(function (ok) {
          if (!ok) return;
          CW.deleteExperiment(del).catch(function (e) { CW.toast(e.message); });
        });
        return;
      }
      var editEl = editBtn && editBtn.getAttribute ? editBtn.getAttribute("data-edit-experiment") : null;
      if (editEl) {
        if (inMenu) closeExpMenus();
        var found = null;
        var expList = CW.drafts ? CW.drafts.mergedExperiments() : CW.state.experiments;
        for (var i = 0; i < expList.length; i++) {
          if (expList[i].id === editEl) { found = expList[i]; break; }
        }
        if (!found) { CW.toast("Experiment not found: " + editEl); return; }
        if (CW.openExperimentDialog) CW.openExperimentDialog(found);
        else {
          CW.$("exp-id").value = found.id;
          CW.$("exp-name").value = found.name || "";
          CW.$("exp-seed").value = found.seed || "";
          CW.$("exp-flag-select").value = found.flag || "";
          CW.$("exp-status").value = found.status || "draft";
          try {
            CW.$("exp-variants").value = JSON.stringify(found.variants || []);
          } catch (e) { CW.$("exp-variants").value = "[]"; }
          if (CW.syncVariantsBuilderFromInput) CW.syncVariantsBuilderFromInput();
          if (CW.updateExpVariantsHint) CW.updateExpVariantsHint();
          CW.$("experiment-result").textContent = "Editing " + (found.name || editEl);
        }
      }
    });
    CW.on("flag-experiments-list", "change", function (ev) {
      var t = ev.target;
      var sid = t && t.getAttribute && t.getAttribute("data-exp-status");
      if (!sid) return;
      var status = t.value;
      CW.setExperimentStatus(sid, status).catch(function (e) { CW.toast(e.message); });
      closeExpMenus();
    });
    CW.on("key-form", "submit", CW.createKey);
    CW.on("key-limits-form", "submit", CW.saveKeyLimits);
    CW.on("key-limits-close", "click", function () { if (CW.closeKeyLimitsDialog) CW.closeKeyLimitsDialog(); });
    CW.on("key-limits-cancel", "click", function () { if (CW.closeKeyLimitsDialog) CW.closeKeyLimitsDialog(); });
    CW.on("limits-form", "submit", CW.saveLimits);
    CW.on("settings-admin-form", "submit", CW.saveAdminLimit);
    CW.on("admin-current-ip-copy", "click", function () {
      try {
        if (typeof CW.copyCurrentIP === "function") { CW.copyCurrentIP(); return; }
        var d = (CW.state && CW.state.limits) || {};
        var ip = d.clientIp != null ? String(d.clientIp) : "";
        if (!ip) { CW.toast("Nothing to copy."); return; }
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(ip).then(function () { CW.toast("Copied", true); }, function () { CW.toast("Copy failed"); });
        } else { CW.toast("Copy failed"); }
      } catch (e) { CW.toast((e && e.message) || "Copy failed"); }
    });
    CW.on("key-copy", "click", function () {
      var v = CW.$("key-once-value").textContent;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(v).then(function () { CW.toast("Copied", true); }, function () { CW.toast("Copy failed"); });
      } else {
        var ta = document.createElement("textarea");
        ta.value = v;
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand("copy"); CW.toast("Copied", true); }
        catch (e) { CW.toast("Copy failed"); }
        document.body.removeChild(ta);
      }
    });

    function closeFlagMenus(except) {
      var box = document.getElementById("flag-folders");
      if (!box || !box.querySelectorAll) return;
      var wraps = box.querySelectorAll(".flag-menu-wrap");
      for (var i = 0; i < wraps.length; i++) {
        var menu = wraps[i].querySelector ? wraps[i].querySelector(".flag-menu") : null;
        var btn = wraps[i].querySelector ? wraps[i].querySelector("[data-flag-menu]") : null;
        if (!menu || menu === except) continue;
        menu.hidden = true;
        if (btn) btn.setAttribute("aria-expanded", "false");
      }
    }

    function closeExpMenus(except) {
      var box = document.getElementById("flag-experiments-list");
      if (!box || !box.querySelectorAll) return;
      var wraps = box.querySelectorAll(".exp-menu-wrap");
      for (var i = 0; i < wraps.length; i++) {
        var menu = wraps[i].querySelector ? wraps[i].querySelector(".exp-menu") : null;
        var btn = wraps[i].querySelector ? wraps[i].querySelector("[data-exp-menu]") : null;
        if (!menu || menu === except) continue;
        menu.hidden = true;
        if (btn) btn.setAttribute("aria-expanded", "false");
      }
    }

    function closeEnvMenu(except) {
      var bar = document.getElementById("env-bar");
      if (!bar || !bar.querySelectorAll) return;
      var wraps = bar.querySelectorAll(".env-menu-wrap");
      for (var i = 0; i < wraps.length; i++) {
        var menu = wraps[i].querySelector ? wraps[i].querySelector(".env-menu") : null;
        var btn = wraps[i].querySelector ? wraps[i].querySelector("[data-env-menu]") : null;
        if (!menu || menu === except) continue;
        menu.hidden = true;
        if (btn) btn.setAttribute("aria-expanded", "false");
      }
    }

    function closeReleaseMenus(except) {
      var box = document.getElementById("release-list");
      if (!box || !box.querySelectorAll) return;
      var wraps = box.querySelectorAll(".release-menu-wrap");
      for (var i = 0; i < wraps.length; i++) {
        var menu = wraps[i].querySelector ? wraps[i].querySelector(".release-menu") : null;
        var btn = wraps[i].querySelector ? wraps[i].querySelector("[data-release-menu]") : null;
        if (!menu || menu === except) continue;
        menu.hidden = true;
        if (btn) btn.setAttribute("aria-expanded", "false");
      }
    }

    function flagRowClick(ev) {
      var t = ev.target;
      function act(name) {
        var el = null;
        if (t && t.closest) el = t.closest("[" + name + "]");
        if (!el && t && t.getAttribute && t.getAttribute(name) != null) el = t;
        return el && el.getAttribute ? el.getAttribute(name) : null;
      }
      var fr = act("data-flag-rules");
      if (fr) {
        if (CW.openFlagRulesDialog) CW.openFlagRulesDialog(fr);
        return;
      }
      var fe = act("data-flag-experiments");
      if (fe) {
        if (CW.openFlagExperimentsDialog) CW.openFlagExperimentsDialog(fe);
        return;
      }
      var del = act("data-delete-flag");
      if (del) {
        cwConfirm("Delete this flag and all its rules?", {title: "Delete flag", okText: "Delete", danger: true}).then(function (ok) {
          if (!ok) return;
          CW.deleteFlag(del).then(function () {
            CW.loadRules().catch(function (e) { CW.toast(e.message); });
          }).catch(function (e) { CW.toast(e.message); });
        });
        return;
      }
      var fid = act("data-edit-flag");
      if (fid) {
        var flagList = CW.drafts ? CW.drafts.mergedFlags() : CW.state.flags;
        for (var i = 0; i < flagList.length; i++) {
          if (flagList[i].id === fid) {
            if (CW.openFlagDialog) CW.openFlagDialog(flagList[i]);
            else {
              var f = flagList[i];
              CW.$("flag-id").value = f.id;
              CW.$("flag-key").value = f.key || "";
              CW.$("flag-description").value = f.description || "";
              CW.$("flag-type").value = f.type || "bool";
              CW.$("flag-group").value = f.group || "";
              CW.$("flag-default").value = JSON.stringify(f.defaultValue === undefined ? null : f.defaultValue);
              if (CW.syncFlagDefaultForType) CW.syncFlagDefaultForType();
              else if (CW.updateFlagDefaultHint) CW.updateFlagDefaultHint();
              CW.$("flag-result").textContent = "Editing " + (f.key || fid);
            }
            break;
          }
        }
      }
    }

    CW.on("flag-folders", "click", function (ev) {
      var t = ev.target;
      var menuBtn = null;
      if (t && t.closest) menuBtn = t.closest("[data-flag-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-flag-menu")) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".flag-menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeFlagMenus();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var inMenu = t && t.closest ? t.closest(".flag-menu") : null;
      if (inMenu) {
        var itemBtn = t.closest("[data-flag-rules],[data-flag-experiments],[data-edit-flag],[data-delete-flag]");
        flagRowClick(ev);
        if (itemBtn) closeFlagMenus();
        return;
      }
      var tg = t && t.getAttribute && t.getAttribute("data-toggle-group");
      if (tg) { CW.toggleGroupCollapse(tg); return; }
      var ag = t && t.getAttribute ? t.getAttribute("data-add-flag-group") : null;
      if (ag !== null && ag !== undefined) { CW.addFlagToGroup(ag || ""); return; }
      var eg = t && t.getAttribute && t.getAttribute("data-edit-group");
      if (eg) { CW.renameGroup(eg).catch(function (e) { CW.toast(e.message); }); return; }
      var dg = t && t.getAttribute && t.getAttribute("data-delete-group");
      if (dg) { CW.deleteGroup(dg).catch(function (e) { CW.toast(e.message); }); return; }
      flagRowClick(ev);
      closeFlagMenus();
    });
    CW.on("flag-folders", "change", function (ev) {
      var t = ev.target;
      var mid = t && t.getAttribute && t.getAttribute("data-move-flag");
      if (mid) {
        CW.moveFlag(mid, t.value || "").catch(function (e) { CW.toast(e.message); });
        closeFlagMenus();
      }
    });
    document.addEventListener("click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var inside = t && t.closest ? (t.closest(".flag-menu-wrap") || t.closest(".exp-menu-wrap") || t.closest(".release-menu-wrap") || t.closest(".env-menu-wrap")) : null;
      if (!inside) { closeFlagMenus(); closeExpMenus(); closeReleaseMenus(); closeEnvMenu(); }
    });
    document.addEventListener("keydown", function (ev) {
      if (ev && ev.key === "Escape") { closeFlagMenus(); closeExpMenus(); closeReleaseMenus(); closeEnvMenu(); }
    });

    CW.on("flag-rules-list", "click", function (ev) {
      var t = ev.target;
      var del = t && t.getAttribute && t.getAttribute("data-delete-flag-rule");
      if (del) {
        cwConfirm("Delete this rule?", {title: "Delete rule", okText: "Delete", danger: true}).then(function (ok) {
          if (!ok) return;
          CW.deleteRule(del).then(function () {
            if (CW.renderFlagRulesList) CW.renderFlagRulesList();
            if (CW.renderFlags) CW.renderFlags();
          }).catch(function (e) { CW.toast(e.message); });
        });
        return;
      }
      var rid = t && t.getAttribute && t.getAttribute("data-edit-flag-rule");
      if (rid) {
        var found = null;
        var ruleList = CW.drafts ? CW.drafts.mergedRules() : CW.state.rules;
        for (var i = 0; i < ruleList.length; i++) {
          if (ruleList[i].id === rid) { found = ruleList[i]; break; }
        }
        if (!found) { CW.toast("Rule not found: " + rid); return; }
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
        if (CW.syncRuleBuilderFromCondition) CW.syncRuleBuilderFromCondition();
        CW.$("flag-rules-result").textContent = "Editing " + found.id;
      }
    });
    CW.on("flag-rules-form", "submit", function (ev) {
      ev.preventDefault();
      if (CW.saveFlagRule) CW.saveFlagRule(ev).catch(function (e) { CW.toast(e.message); });
    });
    CW.on("flag-rules-reset", "click", function () {
      if (CW.resetFlagRuleForm) CW.resetFlagRuleForm();
      if (CW.markFormClean) CW.markFormClean("flag-rules-form");
    });
    CW.on("flag-rules-close", "click", function () {
      if (CW.resetFlagRuleForm) CW.resetFlagRuleForm();
      if (CW.closeFlagRulesDialog) CW.closeFlagRulesDialog();
      else { var dlg = CW.$("flag-rules-dialog"); if (dlg && dlg.open) dlg.close(); }
    });

    CW.on("release-list", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var menuBtn = null;
      if (t && t.closest) menuBtn = t.closest("[data-release-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-release-menu")) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".release-menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeReleaseMenus();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var inMenu = t && t.closest ? t.closest(".release-menu") : null;
      var viewId = null;
      if (t) {
        if (t.closest) {
          var vb = t.closest("[data-view-release]");
          if (vb && vb.getAttribute) viewId = vb.getAttribute("data-view-release");
        } else if (t.getAttribute) {
          viewId = t.getAttribute("data-view-release");
        }
      }
      if (viewId) {
        if (inMenu) closeReleaseMenus();
        if (CW.openReleaseDialog) CW.openReleaseDialog(viewId);
        return;
      }
      var expId = null;
      if (t) {
        if (t.closest) {
          var eb = t.closest("[data-export-release]");
          if (eb && eb.getAttribute) expId = eb.getAttribute("data-export-release");
        } else if (t.getAttribute) {
          expId = t.getAttribute("data-export-release");
        }
      }
      if (expId) {
        if (inMenu) closeReleaseMenus();
        if (CW.exportReleaseSnapshot) CW.exportReleaseSnapshot(expId);
        else CW.toast("Export unavailable.");
        return;
      }
      var rb = t && t.closest ? t.closest("[data-rollback-version]") : null;
      if (!rb && t && t.getAttribute && t.getAttribute("data-rollback-version")) rb = t;
      var v = rb && rb.getAttribute ? rb.getAttribute("data-rollback-version") : null;
      if (!v) return;
      if (inMenu) closeReleaseMenus();
      if (CW.state.applying) return;
      var hasDrafts = false;
      try {
        if (CW.drafts && typeof CW.drafts.hasDrafts === "function") hasDrafts = !!CW.drafts.hasDrafts();
        else if (CW.unpublishedSummary) hasDrafts = CW.unpublishedSummary().total > 0;
        else hasDrafts = !!CW.state.unpublishedChanges;
      } catch (e) { hasDrafts = !!CW.state.unpublishedChanges; }
      if (hasDrafts) { CW.toast("Discard or publish drafts first."); return; }
      CW.rollback(v, "rollback via admin UI").then(function (out) {
        CW.$("publish-result").textContent = out.status === 200
          ? "Rolled back: now v" + out.data.version
          : "Rollback failed (" + out.status + "): " + CW.serverMessage(out.data);
        CW.loadReleases().catch(function () {});
      });
    });

    CW.on("key-list", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var editEl = null;
      if (t) {
        if (t.closest) editEl = t.closest("[data-edit-key]");
        else if (t.getAttribute && t.getAttribute("data-edit-key")) editEl = t;
      }
      var editId = editEl && editEl.getAttribute ? editEl.getAttribute("data-edit-key") : null;
      if (editId) {
        if (CW.editKeyLimits) CW.editKeyLimits(editId);
        return;
      }
      var revokeEl = null;
      if (t) {
        if (t.closest) revokeEl = t.closest("[data-revoke-key]");
        else if (t.getAttribute && t.getAttribute("data-revoke-key")) revokeEl = t;
      }
      var id = revokeEl && revokeEl.getAttribute ? revokeEl.getAttribute("data-revoke-key") : null;
      if (id) CW.revokeKey(id);
    });

    CW.on("publish-form", "submit", function (ev) {
      ev.preventDefault();
      if (CW.state.applying) return;
      var note = CW.$("publish-note").value;
      if (CW.setApplying) CW.setApplying(true);
      else CW.state.applying = true;
      function finishApply() {
        if (CW.setApplying) CW.setApplying(false);
        else CW.state.applying = false;
      }
      function applyFailed(err) {
        finishApply();
        try {
          if (CW.drafts && CW.drafts.persistDrafts) CW.drafts.persistDrafts();
        } catch (e) { /* kept in memory */ }
        CW.$("publish-result").textContent = (err && err.message) || "Publish failed.";
      }
      var applied = null;
      try {
        applied = CW.applyDrafts ? CW.applyDrafts() : Promise.resolve(null);
      } catch (e) { applyFailed(e); return; }
      // Apply wrote live records and consumed the drafts, so server
      // snapshots are stale until reloaded. Flags first: loadExperiments
      // filters by CW.state.flags.
      function reloadPublishedCollections() {
        function loadRest() {
          if (CW.loadRules) {
            try { CW.loadRules().catch(function () {}); } catch (e) { /* best-effort */ }
          }
          if (CW.loadExperiments) {
            try { CW.loadExperiments().catch(function (e2) { CW.toast(e2.message); }); } catch (e3) { /* best-effort */ }
          }
        }
        if (CW.loadFlags) {
          try { CW.loadFlags().then(loadRest, loadRest); } catch (e) { loadRest(); }
        } else { loadRest(); }
      }
      applied.then(function () {
        var base = null;
        try { base = CW.latestVersion(); }
        catch (e) { base = parseInt(CW.$("publish-base").value, 10); }
        CW.$("publish-base").value = base;
        CW.publish(note, base).then(function (out) {
          finishApply();
          if (out.status === 200) {
            CW.$("publish-result").textContent = "Published v" + out.data.version + ".";
            if (CW.markPublished) CW.markPublished();
          } else if (out.status === 409) {
            CW.$("publish-result").textContent = "Stale baseVersion (409): currentVersion is " +
              out.data.currentVersion + " — refreshed latest, retry publish. " + CW.serverMessage(out.data);
          } else {
            CW.$("publish-result").textContent = "Publish failed (" + out.status + "): " + CW.serverMessage(out.data);
          }
          CW.loadReleases().then(function () {
            if (out.status !== 200 && CW.markUnpublished) CW.markUnpublished();
          }).catch(function () {});
          reloadPublishedCollections();
        }, function (e) {
          finishApply();
          CW.$("publish-result").textContent = "Publish failed: " + ((e && e.message) || e);
          reloadPublishedCollections();
        });
      }, applyFailed);
    });

    CW.on("discard-unpublished", "click", function () {
      if (CW.state.applying) return;
      cwConfirm("Discard all drafts?", {title: "Discard drafts", okText: "Discard", danger: true}).then(function (ok) {
        if (!ok) return;
        try {
          if (CW.drafts && typeof CW.drafts.draftClearAll === "function") CW.drafts.draftClearAll();
          if (CW.clearUnpublished) CW.clearUnpublished();
          if (CW.drafts && typeof CW.drafts.refreshDraftChrome === "function") {
            try { CW.drafts.refreshDraftChrome(); }
            catch (e2) { if (CW.setPublishState) CW.setPublishState(false); }
          } else if (CW.setPublishState) CW.setPublishState(false);
        } catch (e) {
          if (CW.clearUnpublished) CW.clearUnpublished();
          if (CW.setPublishState) CW.setPublishState(false);
        }
        CW.$("publish-result").textContent = "Drafts discarded.";
      });
      return;
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
    get saveExperiment() { return CW.saveExperiment; },
    get openExperimentDialog() { return CW.openExperimentDialog; },
    get closeExperimentDialog() { return CW.closeExperimentDialog; },
    get resetExperimentForm() { return CW.resetExperimentForm; },
    get deleteExperiment() { return CW.deleteExperiment; },
    get setExperimentStatus() { return CW.setExperimentStatus; },
    get createKey() { return CW.createKey; }, get revokeKey() { return CW.revokeKey; },
    get checkSetup() { return CW.checkSetup; }, get createSetup() { return CW.createSetup; },
    get loadAccounts() { return CW.loadAccounts; }, get renderAccounts() { return CW.renderAccounts; },
    get createAccount() { return CW.createAccount; },
    get changeAccountPassword() { return CW.changeAccountPassword; },
    get deleteAccount() { return CW.deleteAccount; },
    get showAccount() { return CW.showAccount; },
    get showSettings() { return CW.showSettings; },
    get createProject() { return CW.createProject; }, get createEnv() { return CW.createEnv; },
    get openProjectDialog() { return CW.openProjectDialog; }, get closeProjectDialog() { return CW.closeProjectDialog; },
    get saveProjectDialog() { return CW.saveProjectDialog; },
    get renameProject() { return CW.renameProject; }, get deleteProject() { return CW.deleteProject; },
    get renameEnv() { return CW.renameEnv; }, get deleteEnv() { return CW.deleteEnv; },
    get promptCreateGroup() { return CW.promptCreateGroup; },
    get renameGroup() { return CW.renameGroup; },
    get deleteGroup() { return CW.deleteGroup; },
    get toggleGroupCollapse() { return CW.toggleGroupCollapse; },
    get addFlagToGroup() { return CW.addFlagToGroup; },
    get moveFlag() { return CW.moveFlag; },
    get deleteFlag() { return CW.deleteFlag; },
    get openFlagDialog() { return CW.openFlagDialog; },
    get closeFlagDialog() { return CW.closeFlagDialog; },
    get resetFlagForm() { return CW.resetFlagForm; },
    get openFlagRulesDialog() { return CW.openFlagRulesDialog; },
    get renderFlagRulesList() { return CW.renderFlagRulesList; },
    get saveFlagRule() { return CW.saveFlagRule; },
    get resetFlagRuleForm() { return CW.resetFlagRuleForm; },
    get closeFlagRulesDialog() { return CW.closeFlagRulesDialog; },
    get conditionHTML() { return CW.conditionHTML; }, get valueHTML() { return CW.valueHTML; },
    get publish() { return CW.publish; }, get rollback() { return CW.rollback; },
    get markUnpublished() { return CW.markUnpublished; }, get markPublished() { return CW.markPublished; },
    get armDirtyForm() { return CW.armDirtyForm; }, get markFormClean() { return CW.markFormClean; },
    get openProject() { return CW.openProject; }, get showHome() { return CW.showHome; },
    get route() { return CW.route; }, get parseHash() { return CW.parseHash; },
    get renderProjectCards() { return CW.renderProjectCards; },
    get loadHomeStats() { return CW.loadHomeStats; },
    get openJsonEditor() { return CW.openJsonEditor; }, get closeJsonEditor() { return CW.closeJsonEditor; },
    get openJsonEditorFor() { return CW.openJsonEditorFor; },
    get updateFlagDefaultHint() { return CW.updateFlagDefaultHint; },
    get syncFlagDefaultForType() { return CW.syncFlagDefaultForType; },
    get updateEditorStatus() { return CW.updateEditorStatus; },
    get updateJsonHint() { return CW.updateJsonHint; },
    get updateAllJsonHints() { return CW.updateAllJsonHints; },
    get updateRuleConditionHint() { return CW.updateRuleConditionHint; },
    get updateRuleValueHint() { return CW.updateRuleValueHint; },
    get updateExpVariantsHint() { return CW.updateExpVariantsHint; },
    get CONDITION_OPS() { return CW.CONDITION_OPS; },
    get populateRuleOpOptions() { return CW.populateRuleOpOptions; },
    get syncRuleBuilderFromCondition() { return CW.syncRuleBuilderFromCondition; },
    get applyRuleBuilderToCondition() { return CW.applyRuleBuilderToCondition; },
    get updateRuleBuilderVisibility() { return CW.updateRuleBuilderVisibility; },
    get resetRuleBuilder() { return CW.resetRuleBuilder; },
    get applyVariantsBuilderToVariants() { return CW.applyVariantsBuilderToVariants; },
    get syncVariantsBuilderFromInput() { return CW.syncVariantsBuilderFromInput; },
    get updateVariantPlaceholders() { return CW.updateVariantPlaceholders; },
    get validateVariants() { return CW.validateVariants; },
    get addVariantRow() { return CW.addVariantRow; },
    get balanceVariantsBuilder() { return CW.balanceVariantsBuilder; },
    get resetVariantsBuilder() { return CW.resetVariantsBuilder; },
    get recalcLastVariantWeight() { return CW.recalcLastVariantWeight; },
    get buildTransferSnapshot() { return CW.buildTransferSnapshot; },
    get parseTransferSnapshot() { return CW.parseTransferSnapshot; },
    get validateTransferSnapshot() { return CW.validateTransferSnapshot; },
    get importTransferSnapshot() { return CW.importTransferSnapshot; },
    get diffTransferSnapshots() { return CW.diffTransferSnapshots; },
    get releaseSnapshotToTransfer() { return CW.releaseSnapshotToTransfer; },
    get exportReleaseSnapshot() { return CW.exportReleaseSnapshot; },
    get openExportDialog() { return CW.openExportDialog; },
    get openImportDialog() { return CW.openImportDialog; },
    get openDraftSnapshotDialog() { return CW.openDraftSnapshotDialog; },
  };
})();

/* ConfigWire topbar version — fetch same-origin /api/v1/meta, fallback keeps hardcoded text.
 * Update check — fetch GitHub Releases in parallel, show #cw-update with latest state. Silent fail. */
(function () {
  "use strict";
  function isDevTag(tag) {
    var t = String(tag || "").trim().toLowerCase();
    return !t || t === "dev" || t === "vdev";
  }
  function parseVer(tag) {
    var s = String(tag || "").trim();
    if (s.charAt(0) === "v" || s.charAt(0) === "V") s = s.slice(1);
    s = s.trim();
    var dash = s.indexOf("-");
    var pre = "";
    if (dash >= 0) { pre = s.slice(dash + 1); s = s.slice(0, dash); }
    var core = s.split(".").map(function (p) {
      var n = parseInt(p, 10);
      return isNaN(n) ? 0 : n;
    });
    return { core: core, pre: pre };
  }
  function isNewer(latestTag, currentTag) {
    var l = parseVer(latestTag);
    var c = parseVer(currentTag);
    var n = Math.max(l.core.length, c.core.length);
    for (var i = 0; i < n; i++) {
      var lv = i < l.core.length ? l.core[i] : null;
      var cv = i < c.core.length ? c.core[i] : null;
      if (lv === null && cv === null) break;
      if (lv === null) return false;
      if (cv === null) return true;
      if (lv !== cv) return lv > cv;
    }
    if (c.pre && !l.pre) return true;
    return false;
  }
  function updateVersion() {
    var el = document.getElementById("cw-version");
    if (!el) return;
    var upd = document.getElementById("cw-update");
    var current = (el.textContent || "").trim() || "v0.0.5";
    var latest = null;
    function tryShow() {
      if (!upd || !latest || isDevTag(current)) return;
      try {
        if (isNewer(latest, current)) {
          upd.textContent = "update " + latest;
          var label = "New version available: " + latest + " (current " + current + ") \u2014 open releases";
          upd.setAttribute("title", label);
          upd.setAttribute("aria-label", label);
          if (upd.classList) upd.classList.remove("is-current");
          upd.removeAttribute("hidden");
        } else {
          upd.textContent = "latest";
          var label2 = "Up to date: current " + current + ", latest " + latest;
          upd.setAttribute("title", label2);
          upd.setAttribute("aria-label", label2);
          if (upd.classList) upd.classList.add("is-current");
          upd.removeAttribute("hidden");
        }
      } catch (e) { /* keep badge hidden silently */ }
    }
    try {
      fetch("/api/v1/meta", { headers: { "Accept": "application/json" } })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (d) {
          if (d && typeof d.version === "string" && d.version) {
            el.textContent = d.version;
            current = d.version;
            tryShow();
          }
        })
        .catch(function () { /* keep fallback silently */ });
    } catch (e) { /* keep fallback silently */ }
    try {
      fetch("https://api.github.com/repos/configwire/configwire/releases?per_page=10", {
        headers: { "Accept": "application/vnd.github+json" }
      })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (d) {
          if (!d || !d.length) return;
          var best = null;
          for (var i = 0; i < d.length; i++) {
            var name = d[i] && typeof d[i].tag_name === "string" ? d[i].tag_name.trim() : "";
            if (!name || isDevTag(name)) continue;
            if (!best || isNewer(name, best)) best = name;
          }
          if (best) {
            latest = best;
            tryShow();
          }
        })
        .catch(function () { /* keep badge hidden silently */ });
    } catch (e) { /* keep badge hidden silently */ }
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", updateVersion);
  } else {
    updateVersion();
  }
})();
