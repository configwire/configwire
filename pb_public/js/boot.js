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
    CW.on("flag-form", "submit", CW.saveFlag);
    // Drafts are now the unpublished signal: staging a draft marks publish
    // dirty via refreshDraftChrome, so no keystroke-level zero-arg dirty
    // markers. Per-form armed gating stays (submit enables on input = the
    // pre-stage signal; staging calls markFormClean on success).
    if (CW.armDirtyForm) {
      ["flag-form", "flag-rules-form", "experiment-form",
        "env-create-form", "project-create-form", "account-create-form"
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
    CW.on("project-create-form", "submit", CW.createProject);
    CW.on("env-create-form", "submit", CW.createEnv);
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
    CW.on("experiment-add-btn", "click", function () {
      if (CW.openExperimentDialog) CW.openExperimentDialog(null);
    });
    CW.on("experiment-dialog-close", "click", function () {
      if (CW.closeExperimentDialog) CW.closeExperimentDialog();
      else { var dlg = CW.$("experiment-dialog"); if (dlg && dlg.open) dlg.close(); }
    });
    CW.on("experiment-list", "click", function (ev) {
      var t = ev.target;
      var del = t && t.getAttribute && t.getAttribute("data-delete-experiment");
      if (del) {
        cwConfirm("Delete this experiment?", {title: "Delete experiment", okText: "Delete", danger: true}).then(function (ok) {
          if (!ok) return;
          CW.deleteExperiment(del).catch(function (e) { CW.toast(e.message); });
        });
        return;
      }
      var eid = t && t.getAttribute && t.getAttribute("data-edit-experiment");
      if (eid) {
        var found = null;
        var expList = CW.drafts ? CW.drafts.mergedExperiments() : CW.state.experiments;
        for (var i = 0; i < expList.length; i++) {
          if (expList[i].id === eid) { found = expList[i]; break; }
        }
        if (!found) { CW.toast("experiment not found: " + eid); return; }
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
          CW.$("experiment-result").textContent = "editing " + (found.name || eid);
        }
      }
    });
    CW.on("experiment-list", "change", function (ev) {
      var t = ev.target;
      var sid = t && t.getAttribute && t.getAttribute("data-exp-status");
      if (!sid) return;
      var status = t.value;
      CW.setExperimentStatus(sid, status).catch(function (e) { CW.toast(e.message); });
    });
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

    function flagRowClick(ev) {
      var t = ev.target;
      var fr = t && t.getAttribute && t.getAttribute("data-flag-rules");
      if (fr) {
        if (CW.openFlagRulesDialog) CW.openFlagRulesDialog(fr);
        return;
      }
      var del = t && t.getAttribute && t.getAttribute("data-delete-flag");
      if (del) {
        cwConfirm("Delete this flag and all its rules?", {title: "Delete flag", okText: "Delete", danger: true}).then(function (ok) {
          if (!ok) return;
          CW.deleteFlag(del).then(function () {
            CW.loadRules().catch(function (e) { CW.toast(e.message); });
          }).catch(function (e) { CW.toast(e.message); });
        });
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
              CW.$("flag-result").textContent = "editing " + (f.key || fid);
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
        var itemBtn = t.closest("[data-flag-rules],[data-stats-flag],[data-edit-flag],[data-delete-flag]");
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
      var inside = t && t.closest ? t.closest(".flag-menu-wrap") : null;
      if (!inside) closeFlagMenus();
    });
    document.addEventListener("keydown", function (ev) {
      if (ev && ev.key === "Escape") closeFlagMenus();
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
        if (CW.syncRuleBuilderFromCondition) CW.syncRuleBuilderFromCondition();
        CW.$("flag-rules-result").textContent = "editing " + found.id;
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

    CW.on("release-list", "click", function (ev) {      var v = ev.target && ev.target.getAttribute && ev.target.getAttribute("data-rollback-version");
      if (!v) return;
      if (CW.state.applying) return;
      var hasDrafts = false;
      try {
        if (CW.drafts && typeof CW.drafts.hasDrafts === "function") hasDrafts = !!CW.drafts.hasDrafts();
        else if (CW.unpublishedSummary) hasDrafts = CW.unpublishedSummary().total > 0;
        else hasDrafts = !!CW.state.unpublishedChanges;
      } catch (e) { hasDrafts = !!CW.state.unpublishedChanges; }
      if (hasDrafts) { CW.toast("discard or publish drafts first"); return; }
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
        CW.$("publish-result").textContent = (err && err.message) || "draft apply failed";
      }
      var applied = null;
      try {
        applied = CW.applyDrafts ? CW.applyDrafts() : Promise.resolve(null);
      } catch (e) { applyFailed(e); return; }
      applied.then(function () {
        var base = null;
        try { base = CW.latestVersion(); }
        catch (e) { base = parseInt(CW.$("publish-base").value, 10); }
        CW.$("publish-base").value = base;
        CW.publish(note, base).then(function (out) {
          finishApply();
          if (out.status === 200) {
            CW.$("publish-result").textContent = "published v" + out.data.version + " etag " + out.data.etag;
            if (CW.markPublished) CW.markPublished();
          } else if (out.status === 409) {
            CW.$("publish-result").textContent = "stale baseVersion (409): currentVersion is " +
              out.data.currentVersion + " — refreshed latest, retry publish. " + CW.serverMessage(out.data);
          } else {
            CW.$("publish-result").textContent = "publish failed (" + out.status + "): " + CW.serverMessage(out.data);
          }
          CW.loadReleases().then(function () {
            if (out.status !== 200 && CW.markUnpublished) CW.markUnpublished();
          }).catch(function () {});
        }, function (e) {
          finishApply();
          CW.$("publish-result").textContent = "publish failed: " + ((e && e.message) || e);
        });
      }, applyFailed);
    });

    CW.on("discard-unpublished", "click", function () {
      if (CW.state.applying) return;
      cwConfirm("Discard all local drafts? Nothing was published; server values unchanged.", {title: "Discard drafts", okText: "Discard", danger: true}).then(function (ok) {
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
        CW.$("publish-result").textContent = "local drafts discarded";
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
    get createProject() { return CW.createProject; }, get createEnv() { return CW.createEnv; },
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
  };
})();

/* ConfigWire topbar version — fetch same-origin /api/v1/meta, fallback keeps hardcoded text. */
(function () {
  "use strict";
  function updateVersion() {
    var el = document.getElementById("cw-version");
    if (!el) return;
    try {
      fetch("/api/v1/meta", { headers: { "Accept": "application/json" } })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (d) {
          if (d && typeof d.version === "string" && d.version) el.textContent = d.version;
        })
        .catch(function () { /* keep fallback silently */ });
    } catch (e) { /* keep fallback silently */ }
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", updateVersion);
  } else {
    updateVersion();
  }
})();
