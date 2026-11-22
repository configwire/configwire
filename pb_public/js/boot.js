/* ConfigWire admin shell — boot.js (event wiring + init). Loads LAST. */
(function () {
  "use strict";

  var CW = window.CW;

  function cwConfirm(m, o) {
    return CW.confirmDialog(m, o);
  }

  function condRowOf(t) {
    if (!t) return null;
    if (t.closest) {
      try { return t.closest("[data-cond-row]"); } catch (e) { return null; }
    }
    return null;
  }

  function condRowKind(t) {
    if (!t || !t.getAttribute) return "other";
    if (t.getAttribute("data-cond-remove") != null) return "remove";
    if (t.getAttribute("data-c-field") != null || t.id === "flag-rules-field") return "field";
    if (t.getAttribute("data-c-op") != null || t.id === "flag-rules-op") return "op";
    if (t.getAttribute("data-c-value") != null || t.getAttribute("data-c-lo") != null ||
      t.getAttribute("data-c-hi") != null || t.getAttribute("data-c-custom") != null ||
      t.getAttribute("data-c-seed") != null || t.id === "flag-rules-cond-value" ||
      t.id === "flag-rules-cond-lo" || t.id === "flag-rules-cond-hi" ||
      t.id === "flag-rules-custom" || t.id === "flag-rules-seed") return "value";
    return "other";
  }

  function condRowEvent(ev) {
    var t = ev && ev.target ? ev.target : null;
    var kind = condRowKind(t);
    if (kind === "remove" || kind === "other") return;
    var row = condRowOf(t);
    if (CW.refreshCondRow) {
      if (kind === "field") CW.refreshCondRow(row, "field");
      else if (kind === "op") CW.refreshCondRow(row, "op");
    }
    if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
  }

  // ---- wiring ----

  document.addEventListener("DOMContentLoaded", function () {
    // Restore session (memory-only token; HttpOnly cookie carries reloads).
    function bootWithoutTicket() {
      // Standard-path invariant: the SSO notice never shows here (the
      // markup already hides it; this keeps it hidden if a prior SSO
      // attempt revealed it before falling back).
      try { var sn = CW.$("sso-section"); if (sn) sn.hidden = true; } catch (e) { /* best-effort */ }
      // Cookie-only restore: probe the HttpOnly session (raw fetch, so a
      // 401 here never fans out into the logout flow) before falling back
      // to the login/setup surface. Memory holds no token, so there is no
      // short-circuit: every reload re-probes the cookie.
      CW.loadPersistedScope();
      var probed = false;
      try {
        if (typeof fetch === "function") {
          probed = true;
          fetch("/api/v1/admin/account/list", { credentials: "same-origin" })
            .then(function (res) {
              if (res && res.ok) bootAuthed();
              else bootLoggedOut();
            }, function () { bootLoggedOut(); });
        }
      } catch (e3) { probed = false; }
      if (!probed) bootLoggedOut();
    }

    function bootAuthed() {
      CW.setLoggedIn(true);
      CW.refreshAll();
      try {
        if (CW.refreshSession) {
          if (!window.__cwRefreshTimer) {
            window.__cwRefreshTimer = setInterval(function () {
              try { CW.refreshSession(); } catch (e) { /* silent: next api 401 logs out */ }
            }, 12 * 60 * 60 * 1000);
          }
        }
      } catch (e) { /* silent: next api 401 logs out */ }
      // Reload-with-cookie in sso mode: the cached mode may be unknown on
      // a fresh page load (mode resolves via fetch), so revalidate in the
      // background and re-apply the sso gating if the server says sso.
      // Standard mode is unaffected: the shell already flipped above and
      // the gating no-ops when the mode is not sso.
      try {
        if (CW.getAuthMode) {
          CW.getAuthMode().then(function (m) {
            if (m === "sso" && CW.applySsoUiGating) CW.applySsoUiGating();
          }, function () {});
        }
      } catch (e2) { /* today's behavior already applied */ }
    }

    function bootLoggedOut() {
      // No session: resolve the auth mode, then gate. standard (or any mode
      // lookup failure) keeps today's login/setup behavior verbatim; sso
      // hides both forms behind the managed-by-cloud notice.
      var modeP = null;
      try { modeP = CW.getAuthMode ? CW.getAuthMode() : Promise.resolve("standard"); }
      catch (e) { modeP = Promise.resolve("standard"); }
      modeP.then(function (mode) {
        if (mode === "sso") {
          if (CW.showSsoNotice) CW.showSsoNotice("");
          return;
        }
        CW.setLoggedIn(false);
        if (CW.syncSidebar) CW.syncSidebar();
        if (CW.checkSetup) CW.checkSetup().catch(function (e) { console.warn('boot: setup check', e); });
      }, function () {
        CW.setLoggedIn(false);
        if (CW.syncSidebar) CW.syncSidebar();
        if (CW.checkSetup) CW.checkSetup().catch(function (e) { console.warn('boot: setup recheck', e); });
      });
    }
    function normalBoot() {
      // ?sso= present: redeem same-origin first (cloud SSO landing). On
      // failure fall back per mode — sso shows the notice with the error,
      // standard keeps the normal login path so a stray param never locks
      // out a standalone deploy.
      var ticket = "";
      try { ticket = CW.getSsoTicket ? CW.getSsoTicket() : ""; } catch (e) { ticket = ""; }
      if (ticket) {
        if (CW.redeemSsoTicket) {
          CW.redeemSsoTicket(ticket).then(null, function (err) {
            var msg = (err && err.message) || "sso redeem failed";
            var modeP = null;
            try { modeP = CW.getAuthMode ? CW.getAuthMode() : Promise.resolve("standard"); }
            catch (e) { modeP = Promise.resolve("standard"); }
            modeP.then(function (mode) {
              if (mode === "sso") { if (CW.showSsoNotice) CW.showSsoNotice(msg); return; }
              bootWithoutTicket();
              try { CW.showLoginError(msg); } catch (e2) { /* best-effort */ }
            }, function () { bootWithoutTicket(); });
          });
        } else {
          bootWithoutTicket();
        }
        return;
      }
      bootWithoutTicket();
    }
    normalBoot();

    CW.on("login-form", "submit", function (ev) {
      ev.preventDefault();
      CW.login(CW.$("login-email").value, CW.$("login-password").value).catch(function () { /* shown inline */ });
    });
    CW.on("logout-btn", "click", CW.logout);

    var navToggle = CW.$("nav-toggle");
    var navSidebar = document.querySelector(".sidebar");
    var navBackdrop = CW.$("nav-backdrop");
    var setNavOpen = function (open, restoreFocus) {
      if (!navSidebar || !navToggle) return;
      navSidebar.classList.toggle("is-open", open);
      navToggle.setAttribute("aria-expanded", open ? "true" : "false");
      navToggle.setAttribute("aria-label", open ? "Close navigation" : "Open navigation");
      if (navBackdrop) navBackdrop.hidden = !open;
      if (open) {
        var first = navSidebar.querySelector("a:not([hidden])");
        if (first) first.focus();
      } else if (restoreFocus) {
        navToggle.focus();
      }
    };
    CW.on("nav-toggle", "click", function () {
      setNavOpen(!(navSidebar && navSidebar.classList.contains("is-open")));
    });
    CW.on("nav-backdrop", "click", function () { setNavOpen(false, true); });
    window.addEventListener("hashchange", function () { setNavOpen(false); });
    document.addEventListener("keydown", function (ev) {
      if (ev.key === "Escape" && navSidebar && navSidebar.classList.contains("is-open")) setNavOpen(false, true);
    });

    CW.on("setup-form", "submit", function (ev) {
      if (CW.createSetup) CW.createSetup(ev).catch(function () { /* shown inline */ });
      else ev.preventDefault();
    });
    CW.on("account-create-form", "submit", function (ev) {
      if (CW.createAccount) CW.createAccount(ev).catch(CW.toastErr);
      else ev.preventDefault();
    });
    CW.on("account-list", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var pw = t && t.getAttribute ? t.getAttribute("data-account-password") : null;
      if (pw) {
        if (CW.changeAccountPassword) CW.changeAccountPassword(pw).catch(CW.toastErr);
        return;
      }
      var del = t && t.getAttribute ? t.getAttribute("data-account-delete") : null;
      if (del) {
        if (CW.deleteAccount) CW.deleteAccount(del).catch(CW.toastErr);
      }
    });
    CW.on("refresh-scope", "click", CW.refreshAll);
    CW.on("back-to-projects", "click", function () {
      window.location.hash = "#/";
      CW.showHome();
    });
    window.addEventListener("hashchange", CW.route);
    CW.on("project-grid", "click", function (ev) {
      var r = ev && ev.target && ev.target.closest ? ev.target.closest("[data-register-project]") : null;
      if (r) {
        if (CW.openProjectDialog) CW.openProjectDialog();
        return;
      }
      var t = ev && ev.target && ev.target.closest ? ev.target.closest("[data-project-id]") : null;
      if (!t) return;
      var id = t.getAttribute("data-project-id");
      if (id) window.location.hash = "#/p/" + encodeURIComponent(id);
    });
    CW.on("project-grid", "keydown", function (ev) {
      if (ev.key !== "Enter" && ev.key !== " ") return;
      var r = ev && ev.target && ev.target.closest ? ev.target.closest("[data-register-project]") : null;
      if (r) {
        ev.preventDefault();
        if (CW.openProjectDialog) CW.openProjectDialog();
        return;
      }
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
    CW.on("brand-home-btn", "click", function (ev) {
      if (CW.isAuthed && !CW.isAuthed()) { if (ev) ev.preventDefault(); return; }
      CW.state.projectId = null;
      CW.state.envId = null;
      CW.state.envs = [];
      CW.persistScope();
      CW.bumpScopeGen();
      if (CW.renderProjectEnv) CW.renderProjectEnv();
      if (window.location.hash !== "#/") window.location.hash = "#/";
      CW.showHome();
    });
    CW.on("env-select", "change", function () {
      CW.state.envId = CW.$("env-select").value || null;
      var sel = CW.selectedEnv();
      if (sel && sel.slug) CW.state.envSlug = sel.slug;
      CW.persistScope();
      CW.bumpScopeGen();
      var eg = CW.$("env-group-name");
      if (eg) eg.textContent = (sel && sel.slug) || CW.state.envSlug || "env";
      CW.loadReleases().catch(function (e) { console.warn('boot: env switch releases', e); });
      CW.loadKeys().catch(function (e) { console.warn('boot: env switch keys', e); });
      CW.loadStats().catch(function (e) { console.warn('boot: env switch stats', e); });
    });
    CW.on("stats-since", "change", function () { CW.loadStats().catch(function (e) { console.warn('boot: stats since change', e); }); });
    CW.on("home-since", "change", function () {
      CW.setHomeSince(CW.$("home-since").value);
      CW.showHome();
    });
    CW.on("stats-flag", "change", function () { CW.loadStats().catch(function (e) { console.warn('boot: stats flag change', e); }); });
    CW.on("stats-flag", "input", function () { CW.loadStats().catch(function (e) { console.warn('boot: stats flag input', e); }); });
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
        "key-limits-form", "limits-form", "settings-admin-form",
        "log-settings-form"
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
    CW.on("flag-rules-add-cond", "click", function () {
      if (CW.addConditionRow) CW.addConditionRow();
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-conditions", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var rm = null;
      if (t) {
        if (t.closest) {
          try { rm = t.closest("[data-cond-remove]"); } catch (e) { rm = null; }
        } else if (t.getAttribute && t.getAttribute("data-cond-remove") != null) rm = t;
      }
      if (!rm) return;
      var row = null;
      if (rm.closest) {
        try { row = rm.closest("[data-cond-row]"); } catch (e2) { row = null; }
      }
      if (!row && rm.parentNode) row = rm.parentNode;
      if (CW.removeConditionRow) CW.removeConditionRow(row);
      if (CW.applyRuleBuilderToCondition) CW.applyRuleBuilderToCondition();
    });
    CW.on("flag-rules-conditions", "change", condRowEvent);
    CW.on("flag-rules-conditions", "input", condRowEvent);
    CW.on("flag-rules-value", "input", CW.updateFlagRuleHints);
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
      if (CW.refreshFormSubmit) CW.refreshFormSubmit("flag-form");
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
        CW.copyText(v).then(function (ok) {
          if (ok) CW.toast("Copied", true);
          else CW.toast("Copy failed");
        });
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
    CW.on("project-dialog-form", "submit", function (ev) {
      if (CW.saveProjectDialog) CW.saveProjectDialog(ev).catch(CW.toastErr);
      else ev.preventDefault();
    });
    CW.on("project-dialog-close", "click", function () {
      if (CW.closeProjectDialog) CW.closeProjectDialog();
    });
    CW.on("project-dialog-cancel", "click", function () {
      if (CW.closeProjectDialog) CW.closeProjectDialog();
    });
    CW.on("project-rename-btn", "click", function () {
      if (CW.renameProject) CW.renameProject().catch(CW.toastErr);
    });
    CW.on("project-delete-btn", "click", function () {
      if (CW.deleteProject) CW.deleteProject().catch(CW.toastErr);
    });
    CW.on("env-bar", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var menuBtn = null;
      if (t && t.closest) menuBtn = t.closest("[data-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-menu") != null) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".menu") : null;
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
        if (CW.createEnv) CW.createEnv().catch(CW.toastErr);
        return;
      }
      var renBtn = t && t.closest ? t.closest("[data-env-rename]") : null;
      if (!renBtn && t && t.getAttribute && t.getAttribute("data-env-rename") != null) renBtn = t;
      if (renBtn) {
        closeEnvMenu();
        if (CW.renameEnv) CW.renameEnv().catch(CW.toastErr);
        return;
      }
      var delBtn = t && t.closest ? t.closest("[data-env-delete]") : null;
      if (!delBtn && t && t.getAttribute && t.getAttribute("data-env-delete") != null) delBtn = t;
      if (delBtn) {
        closeEnvMenu();
        if (CW.deleteEnv) CW.deleteEnv().catch(CW.toastErr);
      }
    });
    CW.on("flags-head-actions", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var menuBtn = null;
      if (t && t.closest) menuBtn = t.closest("[data-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-menu") != null) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeFlagsHeadMenu();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var item = t && t.closest ? t.closest('.menu button[role="menuitem"]') : null;
      if (item) closeFlagsHeadMenu();
    });
    CW.on("group-add-btn", "click", function () { CW.promptCreateGroup().catch(CW.toastErr); });
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
      if (CW.refreshFormSubmit) CW.refreshFormSubmit("experiment-form");
    });
    CW.on("experiment-dialog-close", "click", function () {
      if (CW.closeExperimentDialog) CW.closeExperimentDialog();
      else { var dlg = CW.$("experiment-dialog"); if (dlg && dlg.open) dlg.close(); }
    });
    CW.on("exp-seed-regen", "click", function () {
      if (CW.regenerateExperimentSeed) CW.regenerateExperimentSeed();
    });
    CW.on("exp-seed-info-btn", "click", function () {
      var wrap = null;
      var btn = CW.$("exp-seed-info-btn");
      if (btn && btn.parentNode && btn.parentNode.classList) wrap = btn.parentNode;
      else if (btn && btn.closest) wrap = btn.closest(".field-info");
      if (!wrap || !wrap.classList) return;
      var willOpen = !wrap.classList.contains("open");
      if (willOpen && wrap.classList.add) wrap.classList.add("open");
      else if (wrap.classList.remove) wrap.classList.remove("open");
      if (btn && btn.setAttribute) btn.setAttribute("aria-expanded", String(!!willOpen));
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
      if (t && t.closest) menuBtn = t.closest("[data-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-menu")) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeExpMenus();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var inMenu = t && t.closest ? t.closest(".menu") : null;
      var delBtn = t && t.closest ? t.closest("[data-delete-experiment]") : null;
      var editBtn = t && t.closest ? t.closest("[data-edit-experiment]") : null;
      if (!delBtn && t && t.getAttribute && t.getAttribute("data-delete-experiment")) delBtn = t;
      if (!editBtn && t && t.getAttribute && t.getAttribute("data-edit-experiment")) editBtn = t;
      var del = delBtn && delBtn.getAttribute ? delBtn.getAttribute("data-delete-experiment") : null;
      if (del) {
        if (inMenu) closeExpMenus();
        cwConfirm("Delete this experiment?", {title: "Delete experiment", okText: "Delete", danger: true}).then(function (ok) {
          if (!ok) return;
          CW.deleteExperiment(del).catch(CW.toastErr);
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
      CW.setExperimentStatus(sid, status).catch(CW.toastErr);
      closeExpMenus();
    });
    CW.on("key-form", "submit", CW.createKey);
    CW.on("key-limits-form", "submit", CW.saveKeyLimits);
    CW.on("key-limits-close", "click", function () { if (CW.closeKeyLimitsDialog) CW.closeKeyLimitsDialog(); });
    CW.on("key-limits-cancel", "click", function () { if (CW.closeKeyLimitsDialog) CW.closeKeyLimitsDialog(); });
    CW.on("limits-form", "submit", CW.saveLimits);
    CW.on("settings-admin-form", "submit", CW.saveAdminLimit);
    CW.on("log-settings-form", "submit", CW.saveLogSettings);
    CW.on("log-settings-open", "click", function () { if (CW.openLogSettingsDialog) CW.openLogSettingsDialog(); });
    CW.on("log-settings-cancel", "click", function () {
      if (CW.closeLogSettingsDialog) CW.closeLogSettingsDialog();
      else { var dlg = CW.$("log-settings-dialog"); if (dlg && dlg.open) dlg.close(); }
    });
    CW.on("log-enabled", "click", function () { if (CW.onLogEnabledSwitch) CW.onLogEnabledSwitch(); });
    CW.on("log-success", "click", function () { if (CW.onLogSuccessSwitch) CW.onLogSuccessSwitch(); });
    CW.on("log-admin-requests", "click", function () { if (CW.onLogAdminRequestsSwitch) CW.onLogAdminRequestsSwitch(); });
    CW.on("log-admin-success", "click", function () { if (CW.onLogAdminSuccessSwitch) CW.onLogAdminSuccessSwitch(); });
    CW.on("log-admin-health", "click", function () { if (CW.onLogAdminHealthSwitch) CW.onLogAdminHealthSwitch(); });
    CW.on("logs-filter-form", "submit", function (ev) {
      if (CW.filterLogs) return CW.filterLogs(ev);
      ev.preventDefault();
    });
    CW.on("logs-prev", "click", function () { if (CW.pageLogs) CW.pageLogs(-1); });
    CW.on("logs-next", "click", function () { if (CW.pageLogs) CW.pageLogs(1); });
    CW.on("audit-filter-form", "submit", function (ev) {
      if (CW.filterAudit) return CW.filterAudit(ev);
      ev.preventDefault();
    });
    CW.on("audit-prev", "click", function () { if (CW.pageAudit) CW.pageAudit(-1); });
    CW.on("audit-next", "click", function () { if (CW.pageAudit) CW.pageAudit(1); });
    CW.on("logs-refresh", "click", function () { if (CW.refreshLogs) CW.refreshLogs(); });
    CW.on("backup-create-form", "submit", CW.createBackup);
    CW.on("backup-upload-btn", "click", CW.triggerBackupUpload);
    CW.on("backup-upload-file", "change", CW.uploadBackup);
    CW.on("backup-list", "click", CW.onBackupListClick);
    CW.on("backup-s3-form", "submit", CW.onBackupS3Action);
    CW.on("backup-s3-action", "click", CW.onBackupS3Action);
    CW.on("backup-s3-switch", "click", CW.onBackupS3Switch);
    CW.on("backup-s3-form", "input", CW.revertBackupS3Check);
    CW.on("backup-s3-form", "change", CW.revertBackupS3Check);
    CW.on("backup-s3-secret-toggle", "click", CW.toggleBackupS3Secret);
    CW.on("backup-schedule-form", "submit", CW.saveBackupSchedule);
    CW.on("backup-schedule-cron", "input", CW.refreshBackupScheduleCronHint);
    CW.on("backup-schedule-helper-btn", "click", CW.openBackupScheduleHelper);
    CW.on("backup-schedule-helper-close", "click", CW.closeBackupScheduleHelper);
    CW.on("backup-schedule-helper-cancel", "click", CW.closeBackupScheduleHelper);
    CW.on("backup-schedule-helper-apply", "click", CW.applyBackupScheduleHelper);
    CW.on("backup-schedule-helper-freq", "change", CW.refreshBackupScheduleHelper);
    CW.on("backup-schedule-helper-hour", "change", CW.refreshBackupScheduleHelper);
    CW.on("backup-schedule-helper-minute", "change", CW.refreshBackupScheduleHelper);
    CW.on("backup-schedule-helper-dow", "change", CW.refreshBackupScheduleHelper);
    CW.on("backup-schedule-helper-dom", "change", CW.refreshBackupScheduleHelper);
    CW.on("backup-schedule-helper-dom", "input", CW.refreshBackupScheduleHelper);
    CW.on("backup-schedule-helper-keep", "input", CW.refreshBackupScheduleHelper);
    CW.on("backup-schedule-helper-custom", "input", CW.refreshBackupScheduleHelper);
    CW.on("admin-current-ip-copy", "click", function () {
      try {
        if (typeof CW.copyCurrentIP === "function") { CW.copyCurrentIP(); return; }
        var d = (CW.state && CW.state.limits) || {};
        var ip = d.clientIp != null ? String(d.clientIp) : "";
        if (!ip) { CW.toast("Nothing to copy."); return; }
        CW.copyText(ip).then(function (ok) {
          if (ok) CW.toast("Copied", true);
          else CW.toast("Copy failed");
        });
      } catch (e) { CW.toast((e && e.message) || "Copy failed"); }
    });
    CW.on("key-copy", "click", function () {
      var v = CW.$("key-once-value").textContent;
      CW.copyText(v).then(function (ok) {
        if (ok) CW.toast("Copied", true);
        else CW.toast("Copy failed");
      });
    });

    function closeMenus(rootId, except) {
      var box = document.getElementById(rootId);
      if (!box || !box.querySelectorAll) return;
      var wraps = box.querySelectorAll(".menu-wrap");
      for (var i = 0; i < wraps.length; i++) {
        var menu = wraps[i].querySelector ? wraps[i].querySelector(".menu") : null;
        var btn = wraps[i].querySelector ? wraps[i].querySelector("[data-menu]") : null;
        if (!menu || menu === except) continue;
        menu.hidden = true;
        if (btn) btn.setAttribute("aria-expanded", "false");
      }
    }

    function closeFlagMenus(except) { closeMenus("flag-folders", except); }
    function closeExpMenus(except) { closeMenus("flag-experiments-list", except); }
    function closeRuleMenus(except) { closeMenus("flag-rules-list", except); }
    function closeEnvMenu(except) { closeMenus("env-bar", except); }
    function closeReleaseMenus(except) { closeMenus("release-list", except); }
    function closeHeadMenus(except) { closeFlagsHeadMenu(except); }
    function closeFlagsHeadMenu(except) { closeMenus("flags-head-actions", except); }

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
            CW.loadRules().catch(CW.toastErr);
          }).catch(CW.toastErr);
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
      if (t && t.closest) menuBtn = t.closest("[data-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-menu")) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeFlagMenus();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var inMenu = t && t.closest ? t.closest(".menu") : null;
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
      if (eg) { CW.renameGroup(eg).catch(CW.toastErr); return; }
      var dg = t && t.getAttribute && t.getAttribute("data-delete-group");
      if (dg) { CW.deleteGroup(dg).catch(CW.toastErr); return; }
      flagRowClick(ev);
      closeFlagMenus();
    });
    CW.on("flag-folders", "change", function (ev) {
      var t = ev.target;
      var mid = t && t.getAttribute && t.getAttribute("data-move-flag");
      if (mid) {
        CW.moveFlag(mid, t.value || "").catch(CW.toastErr);
        closeFlagMenus();
      }
    });
    document.addEventListener("click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var inside = t && t.closest ? t.closest(".menu-wrap") : null;
      if (!inside) { closeFlagMenus(); closeFlagsHeadMenu(); closeHeadMenus(); closeExpMenus(); closeReleaseMenus(); closeEnvMenu(); closeRuleMenus(); }
    });
    document.addEventListener("keydown", function (ev) {
      if (ev && ev.key === "Escape") { closeFlagMenus(); closeFlagsHeadMenu(); closeHeadMenus(); closeExpMenus(); closeReleaseMenus(); closeEnvMenu(); closeRuleMenus(); }
    });

    CW.on("flag-rules-list", "click", function (ev) {
      var t = ev.target;
      var menuBtn = null;
      if (t && t.closest) menuBtn = t.closest("[data-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-menu")) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeRuleMenus();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var inMenu = t && t.closest ? t.closest(".menu") : null;
      var delBtn = t && t.closest ? t.closest("[data-delete-flag-rule]") : null;
      var editBtn = t && t.closest ? t.closest("[data-edit-flag-rule]") : null;
      if (!delBtn && t && t.getAttribute && t.getAttribute("data-delete-flag-rule")) delBtn = t;
      if (!editBtn && t && t.getAttribute && t.getAttribute("data-edit-flag-rule")) editBtn = t;
      var del = delBtn && delBtn.getAttribute ? delBtn.getAttribute("data-delete-flag-rule") : null;
      if (del) {
        if (inMenu) closeRuleMenus();
        cwConfirm("Delete this rule?", {title: "Delete rule", okText: "Delete", danger: true}).then(function (ok) {
          if (!ok) return;
          CW.deleteRule(del).then(function () {
            if (CW.renderFlagRulesList) CW.renderFlagRulesList();
            if (CW.renderFlags) CW.renderFlags();
          }).catch(CW.toastErr);
        });
        return;
      }
      var editEl = editBtn && editBtn.getAttribute ? editBtn.getAttribute("data-edit-flag-rule") : null;
      if (editEl) {
        if (inMenu) closeRuleMenus();
        var rid = editEl;
        var found = null;
        var ruleList = CW.drafts ? CW.drafts.mergedRules() : CW.state.rules;
        for (var i = 0; i < ruleList.length; i++) {
          if (ruleList[i].id === rid) { found = ruleList[i]; break; }
        }
        if (!found) { CW.toast("Rule not found: " + rid); return; }
        if (CW.openRuleDialog) CW.openRuleDialog(found);
        else {
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
      }
    });
    CW.on("flag-rules-add", "click", function () {
      if (CW.openRuleDialog) CW.openRuleDialog(null, CW.state.activeFlagRulesId || "");
    });
    CW.on("rule-dialog-close", "click", function () {
      if (CW.closeRuleDialog) CW.closeRuleDialog();
      else { var dlg = CW.$("rule-dialog"); if (dlg && dlg.open) dlg.close(); }
    });
    CW.on("flag-rules-form", "submit", function (ev) {
      ev.preventDefault();
      if (CW.saveFlagRule) CW.saveFlagRule(ev).catch(CW.toastErr);
    });
    CW.on("flag-rules-reset", "click", function () {
      if (CW.resetFlagRuleForm) CW.resetFlagRuleForm();
      if (CW.markFormClean) CW.markFormClean("flag-rules-form");
      if (CW.refreshFormSubmit) CW.refreshFormSubmit("flag-rules-form");
    });
    CW.on("flag-rules-close", "click", function () {
      if (CW.closeFlagRulesDialog) CW.closeFlagRulesDialog();
      else { var dlg = CW.$("flag-rules-dialog"); if (dlg && dlg.open) dlg.close(); }
    });

    CW.on("release-list", "click", function (ev) {
      var t = ev && ev.target ? ev.target : null;
      var menuBtn = null;
      if (t && t.closest) menuBtn = t.closest("[data-menu]");
      else if (t && t.getAttribute && t.getAttribute("data-menu")) menuBtn = t;
      if (menuBtn) {
        var wrap = menuBtn.parentNode;
        var menu = wrap && wrap.querySelector ? wrap.querySelector(".menu") : null;
        if (menu) {
          var willOpen = menu.hidden;
          closeReleaseMenus();
          menu.hidden = !willOpen;
          menuBtn.setAttribute("aria-expanded", String(!!willOpen));
        }
        return;
      }
      var inMenu = t && t.closest ? t.closest(".menu") : null;
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
          : CW.apiError(out, "Rollback failed");
        CW.loadReleases().catch(function (e) { console.warn('boot: rollback releases', e); });
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
        // IS-4 push-time race: a server 400 on flag create names the
        // conflicting key plus the kept-draft count and highlights the
        // offending unpublished row (fail() already re-rendered the list,
        // so the highlight lands after rerenderMerged and survives until
        // the next list re-render on edit/discard).
        if (err && err.code === "FLAG_CREATE" && CW.renderApplyFailure) {
          try {
            if (CW.renderApplyFailure(err)) return;
          } catch (e2) { /* fall through to generic message */ }
        }
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
            try { CW.loadRules().catch(function (e2) { console.warn('boot: publish reload rules', e2); }); } catch (e) { /* best-effort */ }
          }
          if (CW.loadExperiments) {
            try { CW.loadExperiments().catch(CW.toastErr); } catch (e3) { /* best-effort */ }
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
            CW.$("publish-result").textContent = CW.apiError(out, "Publish failed");
          }
          CW.loadReleases().then(function () {
            if (out.status !== 200 && CW.markUnpublished) CW.markUnpublished();
          }).catch(function (e) { console.warn('boot: publish reload releases', e); });
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
  });

  // Global instant tooltip: replaces native title="" hover delay with a
  // same-frame custom bubble. Document-level delegation (capture phase) so
  // dynamically injected HTML (e.g. select[data-move-flag] rendered by
  // flags.js folderRowHTML) is covered with no per-node init. Native title
  // is stashed to data-cw-title on show and restored on hide, so no
  // double tooltip. aria-label is never touched; the bubble carries
  // role="tooltip". A fixed-position singleton sidesteps select::after
  // (pseudo-elements never render on <select>) and .menu clipping.
  (function initCwTip() {
    var TIP_ID = "cw-tip";
    var tip = null;
    var activeEl = null;
    var describedSet = false;

    function ensureTip() {
      if (tip) return tip;
      tip = document.getElementById(TIP_ID);
      if (!tip) {
        tip = document.createElement("div");
        tip.id = TIP_ID;
        tip.setAttribute("role", "tooltip");
        tip.hidden = true;
        (document.body || document.documentElement).appendChild(tip);
      } else {
        tip.setAttribute("role", "tooltip");
      }
      return tip;
    }

    function resolveTarget(node) {
      if (!node || node === document || node === window) return null;
      if (node.getAttribute && (node.getAttribute("data-cw-tip") || node.getAttribute("title") || node.getAttribute("data-cw-title"))) return node;
      if (node.closest) {
        try { return node.closest("[data-cw-tip],[title],[data-cw-title]"); } catch (e) { return null; }
      }
      return null;
    }

    function tipText(el) {
      var t = el.getAttribute("data-cw-tip");
      if (t) return t;
      t = el.getAttribute("title");
      if (t) return t;
      return el.getAttribute("data-cw-title") || "";
    }

    function stashTitle(el) {
      var t = el.getAttribute("title");
      if (t !== null && t !== "") {
        el.setAttribute("data-cw-title", t);
        el.removeAttribute("title");
      }
    }

    function restoreTitle(el) {
      if (!el) return;
      var stashed = el.getAttribute("data-cw-title");
      if (stashed !== null) {
        if (!el.getAttribute("title")) el.setAttribute("title", stashed);
        el.removeAttribute("data-cw-title");
      }
    }

    function place(el) {
      var bubble = ensureTip();
      var r;
      try { r = el.getBoundingClientRect(); } catch (e) { return; }
      bubble.hidden = false;
      bubble.setAttribute("data-show", "");
      var w = bubble.offsetWidth || 0;
      var h = bubble.offsetHeight || 0;
      var gap = 8;
      var x = r.left + (r.width / 2) - (w / 2);
      x = Math.max(8, Math.min(x, window.innerWidth - w - 8));
      // Prefer below the anchor: above would overlap the preceding row
      // (e.g. delete-project tooltip covering the rename button).
      var y = r.bottom + gap;
      if (y + h > window.innerHeight - 8) y = r.top - h - gap;
      if (y < 8) y = 8;
      if (y + h > window.innerHeight - 8) y = Math.max(8, window.innerHeight - h - 8);
      bubble.style.left = Math.round(x) + "px";
      bubble.style.top = Math.round(y) + "px";
    }

    function show(el) {
      if (!el) return;
      var text = tipText(el);
      if (!text) return;
      if (activeEl && activeEl !== el) hide();
      stashTitle(el);
      activeEl = el;
      var bubble = ensureTip();
      if (bubble.textContent !== text) bubble.textContent = text;
      if (!el.hasAttribute("aria-describedby")) {
        el.setAttribute("aria-describedby", TIP_ID);
        describedSet = true;
      } else {
        describedSet = false;
      }
      place(el);
    }

    function hide() {
      if (!tip) { activeEl = null; return; }
      tip.hidden = true;
      tip.removeAttribute("data-show");
      if (activeEl) {
        restoreTitle(activeEl);
        if (describedSet) activeEl.removeAttribute("aria-describedby");
        activeEl = null;
      }
      describedSet = false;
    }

    function onOver(ev) {
      var t = ev && ev.target ? ev.target : null;
      if (tip && (t === tip || (tip.contains && tip.contains(t)))) return;
      var el = resolveTarget(t);
      if (!el) {
        if (activeEl && !(ev.relatedTarget && activeEl.contains && activeEl.contains(ev.relatedTarget))) hide();
        return;
      }
      if (el !== activeEl) show(el);
      else place(el);
    }

    function onOut(ev) {
      if (!activeEl) return;
      var to = ev && ev.relatedTarget ? ev.relatedTarget : null;
      if (to && (to === activeEl || (activeEl.contains && activeEl.contains(to)))) return;
      if (to && resolveTarget(to) === activeEl) return;
      hide();
    }

    function onFocusIn(ev) {
      var el = resolveTarget(ev && ev.target);
      if (el) show(el);
    }

    function onFocusOut() { hide(); }

    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", function init() { ensureTip(); });
    } else {
      ensureTip();
    }
    document.addEventListener("mouseover", onOver, true);
    document.addEventListener("mouseout", onOut, true);
    document.addEventListener("focusin", onFocusIn, true);
    document.addEventListener("focusout", onFocusOut, true);
    document.addEventListener("scroll", hide, true);
    document.addEventListener("keydown", function (ev) {
      if (ev && ev.key === "Escape") hide();
    });
  })();

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
    get showLogs() { return CW.showLogs; },
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
    get openRuleDialog() { return CW.openRuleDialog; },
    get closeRuleDialog() { return CW.closeRuleDialog; },
    get fillRuleForm() { return CW.fillRuleForm; },
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
    get addConditionRow() { return CW.addConditionRow; },
    get removeConditionRow() { return CW.removeConditionRow; },
    get condRows() { return CW.condRows; },
    get refreshCondRow() { return CW.refreshCondRow; },
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

/* ConfigWire topbar version + update badge — same-origin only, so the admin
 * browser never calls a third party (CSP connect-src stays 'self').
 * #cw-version fills from GET /api/v1/meta. #cw-update restores the
 * update-pill badge: with the Settings "Automatic checks" switch on it
 * shows update <tag> / latest exactly like the old badge (one same-origin
 * POST /api/v1/admin/update-check on load and every 6 hours); with the
 * switch off it reads "Check for updates" and a click runs one check.
 * Per-browser opt-in (localStorage, default off); hidden-tab ticks are
 * skipped; dev builds and servers with update checks disabled
 * (CONFIGWIRE_CHECK_UPDATES unset, surfaced as updates:false in
 * /api/v1/meta or a 403 from update-check) keep the badge hidden. The
 * badge stays hidden until /api/v1/meta confirms checks are enabled,
 * so a disabled server never flashes the trigger. */
(function () {
  "use strict";
  var AUTO_MS = 6 * 60 * 60 * 1000;
  var AUTO_KEY = "cw.autoUpdateCheck";
  var autoOn = false;
  var devBuild = false;
  var updatesKnown = false;
  var updatesDisabled = false;
  var checking = false;
  var checkDone = false;
  function isDevTag(tag) {
    var t = String(tag || "").trim().toLowerCase();
    return !t || t === "dev" || t === "vdev";
  }
  function readAuto() {
    try {
      if (typeof localStorage === "undefined" || !localStorage.getItem) return false;
      return localStorage.getItem(AUTO_KEY) === "1";
    } catch (e) { return false; }
  }
  function writeAuto(on) {
    try {
      if (typeof localStorage === "undefined" || !localStorage.setItem) return;
      if (on) localStorage.setItem(AUTO_KEY, "1");
      else localStorage.removeItem(AUTO_KEY);
    } catch (e) { /* private mode: toggle lasts this page load only */ }
  }
  function badge() {
    try { return document.getElementById("cw-update"); } catch (e) { return null; }
  }
  function hideBadge(el) {
    if (!el) return;
    try { el.setAttribute("hidden", ""); } catch (e) { /* stub-safe */ }
    el.hidden = true;
  }
  function showBadge(el) {
    if (!el) return;
    try { el.removeAttribute("hidden"); } catch (e) { /* stub-safe */ }
    el.hidden = false;
  }
  function paint(el, text, label, current) {
    el.textContent = text;
    el.title = label;
    try { el.setAttribute("aria-label", label); } catch (e) { /* stub-safe */ }
    try {
      if (!el.classList) return;
      if (current) el.classList.add("is-current");
      else el.classList.remove("is-current");
    } catch (e) { /* stub-safe */ }
    showBadge(el);
  }
  function showTrigger(el) {
    if (!el) return;
    paint(el, "Check for updates", "Check for updates now — runs one same-origin update check", false);
  }
  function setStatus(msg, tone) {
    try {
      var s = document.getElementById("auto-update-result");
      if (!s) return;
      s.textContent = msg;
      if (!s.classList) return;
      s.classList.remove("is-ok");
      s.classList.remove("is-warn");
      if (tone) s.classList.add(tone);
    } catch (e) { /* settings card absent in some shells */ }
  }
  function updateVersion() {
    var el = document.getElementById("cw-version");
    try {
      fetch("/api/v1/meta", { headers: { "Accept": "application/json" } })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (d) {
          if (el && d && typeof d.version === "string" && d.version) {
            el.textContent = d.version;
            if (isDevTag(d.version)) {
              devBuild = true;
              hideBadge(badge());
            }
          }
          updatesKnown = true;
          if (d && "updates" in d && !d.updates) {
            updatesDisabled = true;
            hideBadge(badge());
          } else if (!checking && !checkDone) {
            // Fail-open (old servers/meta failures); skip when a check ran
            // so a late meta never clobbers its paint.
            renderMode();
          }
          if (d && d.updates) {
            try {
              var card = document.getElementById("console-updates-card");
              if (card) {
                card.removeAttribute("hidden");
                card.hidden = false;
              }
            } catch (e) { /* stub-safe */ }
          }
        })
        .catch(function () {
          if (!updatesKnown) {
            updatesKnown = true;
            if (!checking && !checkDone) renderMode();
          }
        });
    } catch (e) { /* keep fallback silently */ }
  }
  function runCheck() {
    var el = badge();
    if (!el || checking || devBuild || updatesDisabled) return;
    checking = true;
    try {
      fetch("/api/v1/admin/update-check", { method: "POST", headers: { "Accept": "application/json" } })
        .then(function (r) {
          if (r.status === 401) { // logged out: ON stays hidden, OFF keeps its trigger
            if (autoOn) hideBadge(el);
            else showTrigger(el);
            return null;
          }
          if (r.status === 403) {
            updatesKnown = true;
            updatesDisabled = true;
            hideBadge(el);
            setStatus("Update checks are disabled on this server.", "is-warn");
            return { handled: true };
          }
          return r.ok ? r.json() : null;
        })
        .then(function (d) {
          if (!d || d.handled) {
            if (!d && !autoOn) showTrigger(el);
            else if (!d) hideBadge(el);
            return;
          }
          if (typeof d.latest !== "string" || !d.latest || isDevTag(d.current)) {
            if (!autoOn) showTrigger(el);
            else hideBadge(el);
            return;
          }
          if (d.update) {
            paint(el, "update " + d.latest,
              "New version available: " + d.latest + " (current " + d.current + ") — open releases", false);
            setStatus("Update available: " + d.latest + " (current " + d.current + ").", "is-warn");
          } else {
            paint(el, "latest",
              "ConfigWire is up to date (current " + d.current + ") — open releases", true);
            setStatus("Up to date (current " + d.current + ").", "is-ok");
          }
        })
        .catch(function () {
          if (!autoOn) showTrigger(el);
          /* ON stays hidden on failure: silent like the old badge */
        })
        .then(function () { checking = false; checkDone = true; }, function () { checking = false; checkDone = true; });
    } catch (e) { checking = false; /* keep fallback silently */ }
  }
  function stopAuto() {
    try {
      if (window.__cwAutoUpdateTimer) {
        clearInterval(window.__cwAutoUpdateTimer);
        window.__cwAutoUpdateTimer = null;
      }
    } catch (e) { /* never break the shell */ }
  }
  function startAuto() {
    stopAuto();
    try {
      window.__cwAutoUpdateTimer = setInterval(function () {
        if (document.hidden) return;
        runCheck();
      }, AUTO_MS);
    } catch (e) { /* timers unavailable: badge clicks still work */ }
  }
  function setSwitch(sw, on) {
    try { sw.setAttribute("aria-checked", on ? "true" : "false"); } catch (e) { /* stub-safe */ }
  }
  function renderMode() {
    var el = badge();
    if (!el) return;
    if (devBuild || updatesDisabled) { hideBadge(el); return; }
    if (!updatesKnown) { hideBadge(el); return; }
    if (!autoOn) showTrigger(el);
    else hideBadge(el);
  }
  function wireUpdates() {
    var el = badge();
    autoOn = readAuto();
    if (el) {
      el.addEventListener("click", function (ev) {
        if (autoOn || updatesDisabled) return; // original behavior: follow the Releases link
        if (ev && ev.preventDefault) ev.preventDefault();
        runCheck();
      });
      renderMode();
    }
    var sw = null;
    try { sw = document.getElementById("auto-update-switch"); } catch (e) { sw = null; }
    if (sw) {
      setSwitch(sw, autoOn);
      sw.addEventListener("click", function () {
        autoOn = !autoOn;
        writeAuto(autoOn);
        setSwitch(sw, autoOn);
        if (autoOn) {
          startAuto();
          renderMode();
          runCheck();
          setStatus("Automatic checks on — this browser checks every 6 hours.");
        } else {
          stopAuto();
          renderMode();
          setStatus("Automatic checks off.");
        }
      });
    }
    if (autoOn) {
      startAuto();
      runCheck();
    }
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", updateVersion);
    document.addEventListener("DOMContentLoaded", wireUpdates);
  } else {
    updateVersion();
    wireUpdates();
  }
})();
