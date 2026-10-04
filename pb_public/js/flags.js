/* ConfigWire admin shell — flags.js (flags + groups). */
(function () {
  "use strict";

  var CW = window.CW;
  if (CW.state.rulesLoaded === undefined) CW.state.rulesLoaded = false;
  if (CW.state.activeFlagRulesId === undefined) CW.state.activeFlagRulesId = null;
  if (CW.state.collapsedGroups === undefined) CW.state.collapsedGroups = {};

  function groupStatus(msg) {
    var el = CW.$("group-result");
    if (el) el.textContent = msg;
  }

  function ruleCountFor(flagId) {
    var rules = CW.drafts ? CW.drafts.mergedRules() : CW.state.rules;
    if (!Array.isArray(rules)) return null;
    // Fallback to plain "rules" label until rules have loaded at least once.
    if (!CW.state.rulesLoaded && CW.state.rules.length === 0) return null;
    var n = 0;
    for (var i = 0; i < rules.length; i++) {
      if (rules[i] && rules[i].flag === flagId && !rules[i]._draftDeleted) n++;
    }
    return n;
  }

  function renderFlags() {
    renderFlagFolders();
    renderFlagGroupSelect();
    renderFlagSelects();
  }

  function flagsInGroup(gid) {
    var list = CW.drafts ? CW.drafts.mergedFlags() : CW.state.flags;
    return list.filter(function (f) { return f && (f.group || "") === (gid || ""); });
  }

  function sortedGroupIds() {
    var groups = CW.drafts ? CW.drafts.mergedGroups() : CW.state.groups;
    return Object.keys(groups).sort(function (a, b) {
      var na = (groups[a] || "").toLowerCase();
      var nb = (groups[b] || "").toLowerCase();
      return na < nb ? -1 : na > nb ? 1 : 0;
    });
  }

  function groupNameById(id) {
    var groups = CW.drafts ? CW.drafts.mergedGroups() : CW.state.groups;
    return groups[id];
  }

  function isUnpub(kind, id) {
    try { if (CW.drafts && CW.drafts.isDraft(kind, id)) return true; } catch (e) { /* ignore */ }
    return false;
  }

  function draftOpOf(kind, id) {
    try {
      if (CW.drafts && typeof CW.drafts.draftOp === "function") return CW.drafts.draftOp(kind, id);
    } catch (e) { /* ignore */ }
    return null;
  }

  function isDeleted(kind, id, obj) {
    if (obj && obj._draftDeleted) return true;
    return draftOpOf(kind, id) === "delete";
  }

  function selectableGroupIds() {
    return sortedGroupIds().filter(function (id) { return draftOpOf("group", id) !== "delete"; });
  }

  function flagChildDrafts() {
    var out = {};
    try {
      if (!CW.drafts) return out;
      var kinds = ["rule", "experiment"];
      for (var i = 0; i < kinds.length; i++) {
        var fn = kinds[i] === "rule" ? "mergedRules" : "mergedExperiments";
        if (typeof CW.drafts[fn] !== "function") continue;
        var list = CW.drafts[fn]();
        if (!Array.isArray(list)) continue;
        for (var j = 0; j < list.length; j++) {
          var it = list[j];
          if (!it || it.flag === undefined || it.flag === null || it.flag === "") continue;
          if (!isUnpub(kinds[i], it.id)) continue;
          var fid = String(it.flag);
          if (!out[fid]) out[fid] = {};
          out[fid][kinds[i]] = true;
        }
      }
    } catch (e) { /* best-effort */ }
    return out;
  }

  function folderRowHTML(f, childSet) {
    var count = ruleCountFor(f.id);
    var rulesLabel = count == null ? "rules" : "rules (" + count + ")";
    var expCount = null;
    try { expCount = CW.expCountFor ? CW.expCountFor(f.id) : null; } catch (e) { expCount = null; }
    var expsLabel = expCount == null ? "experiments" : "experiments (" + expCount + ")";
    var deleted = isDeleted("flag", f.id, f);
    var childKinds = (childSet && childSet[f.id]) || {};
    var unpub = deleted || isUnpub("flag", f.id) || !!(childKinds.rule || childKinds.experiment);
    var rowClass = deleted ? ' class="is-deleted"' : (unpub ? ' class="is-unpublished"' : "");
    var badge = deleted
      ? ' <span class="badge deleted">Deleted</span>'
      : (unpub ? ' <span class="badge unpublished">Unpublished</span>' : "");
    var moveOpts = '<option value="">Default</option>' +
      selectableGroupIds().map(function (id) {
        return '<option value="' + CW.esc(id) + '"' + (f.group === id ? " selected" : "") + ">" +
          CW.esc(groupNameById(id)) + "</option>";
      }).join("");
    var moveDisabled = deleted ? " disabled" : "";
    var ruleDot = childKinds.rule ? ' <span class="dot dirty" aria-hidden="true"></span>' : "";
    var expDot = childKinds.experiment ? ' <span class="dot dirty" aria-hidden="true"></span>' : "";
    var svgOpen = '<svg class="menu-icon" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">';
    var rulesIcon = svgOpen + '<path d="M2.5 5h11"/><circle cx="6" cy="5" r="1.6"/><path d="M2.5 11h11"/><circle cx="10" cy="11" r="1.6"/></svg>';
    var expsIcon = svgOpen + '<path d="M6 2h4"/><path d="M7 2v4.5L3.5 12a1.5 1.5 0 0 0 1.3 2.5h6.4a1.5 1.5 0 0 0 1.3-2.5L9 6.5V2"/><path d="M5.5 10h5"/></svg>';
    var editIcon = svgOpen + '<path d="M11 2.5l2.5 2.5L5 13.5l-3.2 1 1-3.2z"/></svg>';
    var deleteIcon = svgOpen + '<path d="M2.5 4h11"/><path d="M6 4V2.5h4V4"/><path d="M4 4l.7 9.2a1 1 0 0 0 1 .8h4.6a1 1 0 0 0 1-.8L12 4"/><path d="M6.5 7v4"/><path d="M9.5 7v4"/></svg>';
    return '<tr' + rowClass + "><td>" + CW.esc(f.key) + badge + "</td><td>" + CW.esc(f.description || "") + "</td><td>" + CW.esc(f.type) + "</td>" +
      "<td><code>" + CW.esc(JSON.stringify(f.defaultValue)) + "</code></td>" +
      '<td class="flag-actions-cell"><div class="flag-menu-wrap">' +
      '<button type="button" class="flag-menu-btn" data-flag-menu="' + CW.esc(f.id) + '" aria-haspopup="menu" aria-expanded="false" aria-label="Actions for ' + CW.esc(f.key) + '">&#8943;</button>' +
      '<div class="flag-menu" role="menu" hidden>' +
      '<button type="button" role="menuitem" data-flag-rules="' + CW.esc(f.id) + '">' + rulesIcon + "<span>" + CW.esc(rulesLabel) + "</span>" + ruleDot + "</button>" +
      '<button type="button" role="menuitem" data-flag-experiments="' + CW.esc(f.id) + '">' + expsIcon + "<span>" + CW.esc(expsLabel) + "</span>" + expDot + "</button>" +
      '<button type="button" role="menuitem" data-edit-flag="' + CW.esc(f.id) + '">' + editIcon + "<span>edit</span></button>" +
      '<button type="button" role="menuitem" data-delete-flag="' + CW.esc(f.id) + '">' + deleteIcon + "<span>delete</span></button>" +
      '<span class="flag-menu-label">Move to group</span>' +
      '<select data-move-flag="' + CW.esc(f.id) + '" aria-label="Move ' + CW.esc(f.key) + ' to group"' + moveDisabled + ">" +
      moveOpts + "</select>" +
      "</div></div></td></tr>";
  }

  function folderHTML(gid, name, childSet) {
    var key = gid || "__none";
    var flags = flagsInGroup(gid);
    var groupDeleted = gid ? isDeleted("group", gid, null) : false;
    var groupDirty = groupDeleted || isUnpub("group", gid);
    if (!groupDirty) {
      for (var gi = 0; gi < flags.length; gi++) {
        if (flags[gi] && (isUnpub("flag", flags[gi].id) || (childSet && childSet[flags[gi].id]))) { groupDirty = true; break; }
      }
    }
    var collapsed = !!CW.state.collapsedGroups[key];
    var rows = flags.map(function (f) { return folderRowHTML(f, childSet); }).join("");
    var body = collapsed ? "" :
      '<div class="table-wrap"><table aria-label="Flags in ' + CW.esc(name) + '">' +
      "<thead><tr><th>Key</th><th>Description</th><th>Type</th><th>Default</th><th></th></tr></thead>" +
      "<tbody>" + (rows || '<tr><td colspan="5">No flags in this group.</td></tr>') + "</tbody></table></div>";
    var groupBtns = gid
      ? '<button type="button" data-add-flag-group="' + CW.esc(gid) + '">+ flag</button> ' +
        '<button type="button" data-edit-group="' + CW.esc(gid) + '">edit</button> ' +
        '<button type="button" data-delete-group="' + CW.esc(gid) + '">delete</button>'
      : '<button type="button" data-add-flag-group="">+ flag</button>';
    return '<section class="folder' + (groupDeleted ? " is-deleted" : (groupDirty ? " is-unpublished" : "")) + '">' +
      '<div class="folder-head"><button type="button" class="folder-toggle" data-toggle-group="' + CW.esc(key) +
      '" aria-expanded="' + String(!collapsed) + '" aria-label="Toggle ' + CW.esc(name) + '">' +
      (collapsed ? "&#9656;" : "&#9662;") + "</button>" +
      '<span class="folder-name">' + CW.esc(name) + "</span>" +
      (groupDeleted ? ' <span class="badge deleted">Deleted</span>' : "") +
      ' <span class="muted">(' + flags.length +
      (flags.length === 1 ? " flag" : " flags") + ")</span>" +
      '<span class="folder-actions">' + groupBtns + "</span></div>" + body + "</section>";
  }

  function renderFlagFolders() {
    var box = CW.$("flag-folders");
    if (!box) return;
    var childSet = flagChildDrafts();
    var ids = sortedGroupIds();
    var html = ids.map(function (id) { return folderHTML(id, groupNameById(id) || id, childSet); }).join("");
    var none = flagsInGroup("");
    if (none.length || !ids.length) html += folderHTML("", "Default", childSet);
    box.innerHTML = html || '<p class="muted">No flags.</p>';
  }

  function toggleGroupCollapse(key) {
    CW.state.collapsedGroups[key] = !CW.state.collapsedGroups[key];
    renderFlagFolders();
  }

  function addFlagToGroup(gid) {
    openFlagDialog(null);
    var sel = CW.$("flag-group");
    if (sel) sel.value = gid || "";
    var res = CW.$("flag-result");
    if (res) res.textContent = gid && CW.state.groups[gid] ? "New flag in " + CW.state.groups[gid] : "";
  }

  function moveFlag(id, gid) {
    // Local-only: stage a flag-update draft, zero server writes.
    var label = flagKeyById(id) || id;
    var staged = CW.drafts.draftStage("flag", {
      op: "update",
      body: { group: gid || null },
      baseId: id,
      label: label,
    });
    CW.toast(staged == null ? "No changes." : "Draft saved: " + label, true);
    CW.drafts.refreshDraftChrome();
    return Promise.resolve({ status: 200, data: {} });
  }

  function renderFlagSelects() {
    var opts = CW.drafts.mergedFlags().filter(function (f) {
      return f && !f._draftDeleted && draftOpOf("flag", f.id) !== "delete";
    }).map(function (f) {
      return '<option value="' + CW.esc(f.id) + '">' + CW.esc(f.key) + "</option>";
    }).join("");
    var xs = CW.$("exp-flag-select");
    if (!xs) return;
    var curX = xs.value;
    xs.innerHTML = opts;
    if (curX !== undefined) xs.value = curX;
  }

  function renderGroups() {
    renderFlagGroupSelect();
  }

  function renderFlagGroupSelect() {
    var sel = CW.$("flag-group");
    if (!sel) return;
    var groups = CW.drafts.mergedGroups();
    var cur = sel.value;
    sel.innerHTML = '<option value="">Default</option>' +
      Object.keys(groups).filter(function (id) { return draftOpOf("group", id) !== "delete"; }).map(function (id) {
        return '<option value="' + CW.esc(id) + '">' + CW.esc(groups[id]) + "</option>";
      }).join("");
    if (cur && groups[cur]) sel.value = cur;
    else sel.value = "";
  }

  function loadFlags() {
    var pid = CW.state.projectId;
    var fq = "/api/collections/flags/records?perPage=200";
    if (pid) fq += "&filter=" + encodeURIComponent('(project="' + pid + '")');
    return CW.apiAll(fq).then(function (fetched) {
      var items = fetched || [];
      // Client-side filter: strict match only — legacy unscoped rows must
      // not leak across projects (groups carry a required project relation).
      if (pid) items = items.filter(function (f) { return f.project === pid; });
      CW.state.flags = items.slice().sort(function (a, b) {
        return (a.key || "") < (b.key || "") ? -1 : 1;
      });
      var gq = "/api/collections/groups/records?perPage=200";
      if (pid) gq += "&filter=" + encodeURIComponent('(project="' + pid + '")');
      return CW.apiAll(gq).then(function (gitems) {
        CW.state.groups = {};
        (gitems || []).forEach(function (gr) {
          if (pid && gr.project !== pid) return;
          CW.state.groups[gr.id] = gr.name || gr.id;
        });
        renderGroups();
        renderFlags();
        // Drafts survive reloads (pure selectors); restore this scope's
        // persisted drafts before rendering so merged state shows at once.
        if (CW.drafts) {
          try { CW.drafts.restoreDrafts(); } catch (e) { /* best-effort */ }
          renderGroups();
          renderFlags();
          if (CW.setPublishState) {
            try { CW.setPublishState(CW.drafts.hasDrafts()); } catch (e2) { /* best-effort */ }
          }
        }
      }, function () { renderFlags(); }); // flags still render if groups missing
    });
  }

  function saveFlag(ev) {
    if (ev) ev.preventDefault();
    var id = CW.$("flag-id").value;
    var parsed = CW.parseJSONInput(CW.$("flag-default").value, "defaultValue");
    if (!parsed.ok) { CW.$("flag-result").textContent = parsed.error; return Promise.resolve(); }
    var body = {
      key: CW.$("flag-key").value.trim(),
      description: CW.$("flag-description") ? CW.$("flag-description").value.trim() : "",
      type: CW.$("flag-type").value,
      defaultValue: parsed.value,
      project: CW.state.projectId,
    };
    var group = CW.$("flag-group").value || "";
    if (group) body.group = group;
    else if (id) body.group = null;
    // Local-only: stage the full POST/PATCH body as a draft (group-omission
    // quirk preserved: create omits group unless set, update sets null).
    // Unchanged edits stage nothing (draftStage returns null) instead of
    // inflating the unpublished count.
    var stagedKey = null;
    if (id) {
      stagedKey = CW.drafts.draftStage("flag", { op: "update", body: body, baseId: id, label: body.key });
    } else {
      stagedKey = CW.drafts.draftStage("flag", { op: "create", body: body, label: body.key });
    }
    CW.toast(stagedKey == null ? "No changes." : "Draft saved: " + body.key, true);
    CW.$("flag-result").textContent = "";
    if (CW.markFormClean) CW.markFormClean("flag-form");
    closeFlagDialog();
    CW.drafts.refreshDraftChrome();
    return Promise.resolve({ status: 200, data: {} });
  }

  function deleteFlag(id) {
    var delKey = flagKeyById(id) || id;
    CW.drafts.draftStage("flag", { op: "delete", body: {}, baseId: id, label: delKey + " deleted" });
    CW.toast("Draft saved: " + delKey + " deleted", true);
    CW.drafts.refreshDraftChrome();
    return Promise.resolve({ status: 200, data: {} });
  }

  function promptCreateGroup() {
    if (!CW.state.projectId) { CW.toast("Select a project first."); return Promise.resolve(); }
    return CW.promptDialog("New group name", "", { title: "New group", okText: "Create", placeholder: "Group name", required: true }).then(function (name) {
      if (name == null) return;
      name = name.trim();
      if (!name) { CW.toast("Group name required."); return; }
      CW.drafts.draftStage("group", {
        op: "create",
        body: { name: name, project: CW.state.projectId },
        label: name,
      });
      groupStatus("Draft saved: " + name);
      CW.toast("Draft saved: " + name, true);
      CW.drafts.refreshDraftChrome();
      return { status: 200, data: {} };
    });
  }

  function renameGroup(id) {
    var groups = CW.drafts.mergedGroups();
    var cur = groups[id] || "";
    return CW.promptDialog("Rename group", cur, { title: "Rename group", okText: "Rename", required: true }).then(function (name) {
      if (name == null) return;
      name = name.trim();
      if (!name) { groupStatus("Group name required."); return; }
      if (name === cur) return;
      var stagedGroup = CW.drafts.draftStage("group", { op: "update", body: { name: name }, baseId: id, label: name });
      groupStatus(stagedGroup == null ? "No changes." : "Draft saved: " + name);
      CW.toast(stagedGroup == null ? "No changes." : "Draft saved: " + name, true);
      CW.drafts.refreshDraftChrome();
      return { status: 200, data: {} };
    });
  }

  function deleteGroup(id) {
    var groups = CW.drafts.mergedGroups();
    var name = groups[id] || id;
    var inGroup = CW.drafts.mergedFlags().filter(function (f) { return f && f.group === id && !f._draftDeleted; });
    var msg = inGroup.length
      ? 'Delete group "' + name + '" with ' + inGroup.length + (inGroup.length === 1 ? " flag" : " flags") + "? Its flags will be ungrouped."
      : 'Delete group "' + name + '"?';
    return CW.confirmDialog(msg, { title: "Delete group", okText: "Delete", danger: true }).then(function (ok) {
      if (!ok) return;
      // Local-only: snapshot member ids so apply-time can ungroup them first
      // (flags.group is an optional, non-cascade relation). No server writes.
      var memberIds = inGroup.map(function (f) { return f.id; }).filter(function (fid) {
        return fid !== undefined && fid !== null && fid !== "";
      });
      CW.drafts.draftStage("group", { op: "delete", body: {}, baseId: id, memberIds: memberIds, label: name });
      groupStatus("Draft saved: " + name + " deleted");
      CW.toast("Draft saved: " + name + " deleted", true);
      CW.drafts.refreshDraftChrome();
      return { status: 200, data: {} };
    });
  }

  function flagKeyById(id) {
    var list = CW.drafts ? CW.drafts.mergedFlags() : CW.state.flags;
    for (var i = 0; i < list.length; i++) {
      if (list[i].id === id) return list[i].key;
    }
    return "";
  }

  function flagRulesForActive() {
    var flagId = CW.state.activeFlagRulesId || "";
    var rules = CW.drafts ? CW.drafts.mergedRules() : CW.state.rules;
    if (!Array.isArray(rules)) return [];
    return rules.filter(function (r) { return r && r.flag === flagId; })
      .slice().sort(function (a, b) { return (a.priority || 0) - (b.priority || 0); });
  }

  function dialogConditionHTML(cond) {
    if (CW.conditionHTML) return CW.conditionHTML(cond);
    var raw;
    try { raw = typeof cond === "string" ? cond : JSON.stringify(cond); }
    catch (e) { raw = String(cond); }
    return "<code>" + CW.esc(raw == null || raw === "" ? "{}" : raw) + "</code>";
  }

  function dialogValueHTML(v) {
    if (CW.valueHTML) return CW.valueHTML(v);
    var raw;
    try { raw = JSON.stringify(v); if (raw === undefined) raw = String(v); }
    catch (e) { raw = String(v); }
    return "<code>" + CW.esc(raw) + "</code>";
  }

  function renderFlagRulesList() {
    var list = CW.$("flag-rules-list");
    if (!list) return;
    var flagId = CW.state.activeFlagRulesId || "";
    if (!flagId) { list.innerHTML = "<li>Pick a flag first.</li>"; return; }
    var items = flagRulesForActive();
    if (!items.length) { list.innerHTML = "<li>No rules.</li>"; return; }
    list.innerHTML = items.map(function (r) {
      var runpub = isUnpub("rule", r.id);
      var rdeleted = isDeleted("rule", r.id, r);
      var rclass = rdeleted ? " is-deleted" : (runpub ? " is-unpublished" : "");
      var rbadge = rdeleted
        ? ' <span class="badge deleted">Deleted</span>'
        : (runpub ? ' <span class="badge unpublished">Unpublished</span>' : "");
      var svgOpen = '<svg class="menu-icon" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">';
      var editIcon = svgOpen + '<path d="M11 2.5l2.5 2.5L5 13.5l-3.2 1 1-3.2z"/></svg>';
      var deleteIcon = svgOpen + '<path d="M2.5 4h11"/><path d="M6 4V2.5h4V4"/><path d="M4 4l.7 9.2a1 1 0 0 0 1 .8h4.6a1 1 0 0 0 1-.8L12 4"/><path d="M6.5 7v4"/><path d="M9.5 7v4"/></svg>';
      return '<li class="rule-item' + rclass + '">' +
        '<span class="rule-prio">P' + CW.esc(r.priority) + "</span>" + rbadge +
        dialogConditionHTML(r.condition) +
        '<span class="rule-arrow" aria-hidden="true">→</span>' +
        dialogValueHTML(r.value) +
        '<span class="rule-menu-wrap">' +
        '<button type="button" class="rule-menu-btn" data-rule-menu="' + CW.esc(r.id) + '" aria-haspopup="menu" aria-expanded="false" aria-label="Actions for rule P' + CW.esc(r.priority) + '">&#8942;</button>' +
        '<div class="rule-menu" role="menu" hidden>' +
        '<button type="button" role="menuitem" data-edit-flag-rule="' + CW.esc(r.id) + '">' + editIcon + "<span>edit</span></button>" +
        '<button type="button" role="menuitem" data-delete-flag-rule="' + CW.esc(r.id) + '">' + deleteIcon + "<span>delete</span></button>" +
        "</div></span></li>";
    }).join("");
  }

  function updateFlagRuleHints() {
    CW.updateJsonHint("flag-rules-condition");
    CW.updateJsonHint("flag-rules-value");
  }

  function resetFlagRuleForm() {
    var idEl = CW.$("flag-rules-id");
    if (idEl) idEl.value = "";
    var form = CW.$("flag-rules-form");
    if (form) form.reset();
    var prio = CW.$("flag-rules-priority");
    if (prio && !prio.value) prio.value = "0";
    if (CW.resetRuleBuilder) CW.resetRuleBuilder();
    if (CW.syncRuleBuilderFromCondition) CW.syncRuleBuilderFromCondition();
    updateFlagRuleHints();
    var res = CW.$("flag-rules-result");
    if (res) res.textContent = "";
    if (CW.markFormClean) CW.markFormClean("flag-rules-form");
    else if (CW.refreshFormSubmit) CW.refreshFormSubmit("flag-rules-form");
  }

  function openFlagRulesDialog(flagId) {
    CW.state.activeFlagRulesId = flagId || "";
    var title = CW.$("flag-rules-title");
    if (title) {
      var key = CW.flagKeyById ? CW.flagKeyById(flagId) : "";
      title.textContent = key ? "Rules for " + key : "Rules for flag";
    }
    renderFlagRulesList();
    var dlg = CW.$("flag-rules-dialog");
    if (!dlg) return;
    if (dlg.showModal) {
      try { if (!dlg.open) dlg.showModal(); } catch (e) { /* already open */ }
    }
  }

  function closeFlagRulesDialog() {
    var dlg = CW.$("flag-rules-dialog");
    if (dlg && dlg.open) dlg.close();
    CW.state.activeFlagRulesId = null;
  }

  function ruleDialogTitle(prefix, rule) {
    var key = "";
    try { key = CW.flagKeyById ? (CW.flagKeyById(CW.state.activeFlagRulesId) || "") : ""; }
    catch (e) { key = ""; }
    var head = prefix + " rule";
    if (rule && rule.priority !== undefined && rule.priority !== null) head += " P" + rule.priority;
    return key ? head + " for " + key : head;
  }

  function fillRuleForm(rule) {
    CW.$("flag-rules-flag-id").value = rule.flag || CW.state.activeFlagRulesId || "";
    CW.$("flag-rules-id").value = rule.id;
    CW.$("flag-rules-priority").value = rule.priority == null ? 0 : rule.priority;
    try {
      CW.$("flag-rules-condition").value = typeof rule.condition === "string"
        ? rule.condition
        : JSON.stringify(rule.condition);
    } catch (e) { CW.$("flag-rules-condition").value = "{}"; }
    try {
      CW.$("flag-rules-value").value = rule.value === undefined
        ? "null"
        : JSON.stringify(rule.value);
    } catch (e2) { CW.$("flag-rules-value").value = "null"; }
    if (CW.updateFlagRuleHints) CW.updateFlagRuleHints();
    if (CW.syncRuleBuilderFromCondition) CW.syncRuleBuilderFromCondition();
    CW.$("flag-rules-result").textContent = "Editing " + rule.id;
  }

  function openRuleDialog(rule, presetFlagId) {
    var title = CW.$("rule-dialog-title");
    if (!rule) {
      resetFlagRuleForm();
      var fid = presetFlagId || CW.state.activeFlagRulesId || "";
      var hidden = CW.$("flag-rules-flag-id");
      if (hidden) hidden.value = fid;
      if (title) title.textContent = ruleDialogTitle("Add");
    } else {
      if (rule.flag) {
        CW.state.activeFlagRulesId = rule.flag;
        var hid = CW.$("flag-rules-flag-id");
        if (hid) hid.value = rule.flag;
      }
      fillRuleForm(rule);
      if (title) title.textContent = ruleDialogTitle("Edit", rule);
    }
    if (CW.markFormClean) CW.markFormClean("flag-rules-form");
    else if (CW.refreshFormSubmit) CW.refreshFormSubmit("flag-rules-form");
    var dlg = CW.$("rule-dialog");
    if (!dlg) return;
    if (dlg.showModal) {
      try { if (!dlg.open) dlg.showModal(); } catch (e) { /* already open */ }
    }
  }

  function closeRuleDialog() {
    var dlg = CW.$("rule-dialog");
    if (dlg && dlg.open) dlg.close();
  }

  function saveFlagRule(ev) {
    if (ev) ev.preventDefault();
    var flagId = (CW.$("flag-rules-flag-id") && CW.$("flag-rules-flag-id").value) ||
      CW.state.activeFlagRulesId || "";
    if (!flagId) {
      var res0 = CW.$("flag-rules-result");
      if (res0) res0.textContent = "Pick a flag first.";
      return Promise.resolve();
    }
    var cond = CW.parseJSONInput(CW.$("flag-rules-condition").value, "condition");
    if (!cond.ok) { CW.$("flag-rules-result").textContent = cond.error; return Promise.resolve(); }
    var val = CW.parseJSONInput(CW.$("flag-rules-value").value, "value");
    if (!val.ok) { CW.$("flag-rules-result").textContent = val.error; return Promise.resolve(); }
    var body = {
      flag: flagId,
      priority: parseInt(CW.$("flag-rules-priority").value, 10) || 0,
      condition: cond.value,
      value: val.value,
    };
    var id = (CW.$("flag-rules-id") && String(CW.$("flag-rules-id").value || "").trim()) || "";
    // Local-only: stage the full POST/PATCH body as a draft.
    var ruleLabel = "rule for " + (flagKeyById(flagId) || flagId);
    if (id) {
      var stagedRule = CW.drafts.draftStage("rule", { op: "update", body: body, baseId: id, label: ruleLabel });
      CW.$("flag-rules-result").textContent = stagedRule == null ? "No changes." : "Rule staged.";
    } else {
      var stagedKey = CW.drafts.draftStage("rule", { op: "create", body: body, label: ruleLabel });
      CW.$("flag-rules-result").textContent = "Rule staged.";
    }
    var fid = CW.$("flag-rules-flag-id");
    if (fid) fid.value = flagId;
    CW.state.activeFlagRulesId = flagId;
    if (CW.markFormClean) CW.markFormClean("flag-rules-form");
    CW.toast((id && stagedRule == null ? "No changes." : "Draft saved: " + ruleLabel), true);
    var resEl = CW.$("flag-rules-result");
    if (resEl) resEl.textContent = "";
    closeRuleDialog();
    renderFlagRulesList();
    CW.drafts.refreshDraftChrome();
    return Promise.resolve({ status: 200, data: {} });
  }

  function updateFlagDefaultHint() {
    var input = CW.$("flag-default");
    var typeEl = CW.$("flag-type");
    var type = typeEl ? typeEl.value : "bool";
    if (!input) return CW.updateJsonHint("flag-default");
    var raw = input.value;
    // Empty input means null default — server accepts null for any type.
    if (raw.trim() === "") return CW.updateJsonHint("flag-default");
    var parsed;
    try { parsed = JSON.parse(raw); }
    catch (e) { return CW.updateJsonHint("flag-default"); }
    if (flagDefaultMatchesType(parsed, type)) return CW.updateJsonHint("flag-default");
    var hint = CW.$("flag-default-hint");
    if (hint) {
      hint.textContent = "Invalid default: " + flagTypeExpectation(type);
      hint.className = "json-hint err";
    }
    input.classList.remove("valid");
    input.classList.add("invalid");
    return false;
  }

  // Canonical JSON defaults per flag type (mirrors server coerceToType in
  // configwire/releases/snapshot.go). Used when the type changes and the
  // current defaultValue no longer matches, so the dialog never opens or
  // switches into a state that publish would reject.
  var FLAG_TYPE_DEFAULTS = {
    bool: "false",
    number: "0",
    string: '""',
    json: "{}",
  };

  var FLAG_TYPE_PLACEHOLDERS = {
    bool: "false",
    number: "0",
    string: '"hello"',
    json: '{"key": "value"}',
  };

  function flagTypeExpectation(type) {
    switch (type) {
      case "bool": return 'expected bool (true or false)';
      case "number": return "expected number (e.g. 0)";
      case "string": return 'expected string (JSON quoted, e.g. "hello")';
      case "json": return "expected JSON object or array (e.g. {} or [])";
      default: return 'expected value matching type "' + type + '"';
    }
  }

  // Mirrors Go coerceToType (nil/null always allowed — publish skips nil).
  function flagDefaultMatchesType(value, type) {
    if (value === null || value === undefined) return true;
    switch (type) {
      case "bool": return typeof value === "boolean";
      case "number": return typeof value === "number";
      case "string": return typeof value === "string";
      case "json":
        return typeof value === "object" && value !== null;
      default: return true;
    }
  }

  // Keep the defaultValue input in sync with the selected type: refresh the
  // placeholder every time, and replace the value with the type's canonical
  // default only when the current value would fail the type check (so
  // user-typed values that already match are never clobbered).
  // opts.reset=true forces the canonical default (used by Clear).
  function syncFlagDefaultForType(opts) {
    var typeEl = CW.$("flag-type");
    var input = CW.$("flag-default");
    if (!typeEl || !input) return;
    var type = typeEl.value || "bool";
    if (FLAG_TYPE_PLACEHOLDERS[type]) input.placeholder = FLAG_TYPE_PLACEHOLDERS[type];
    var force = !!(opts && opts.reset);
    if (!force) {
      var raw = input.value;
      if (raw.trim() === "") { updateFlagDefaultHint(); return; }
      try {
        if (flagDefaultMatchesType(JSON.parse(raw), type)) { updateFlagDefaultHint(); return; }
      } catch (e) { /* invalid JSON — fall through and reset to a valid default */ }
    }
    if (FLAG_TYPE_DEFAULTS[type] !== undefined) input.value = FLAG_TYPE_DEFAULTS[type];
    updateFlagDefaultHint();
  }

  function resetFlagForm() {
    var idEl = CW.$("flag-id");
    if (idEl) idEl.value = "";
    var form = CW.$("flag-form");
    if (form) form.reset();
    var group = CW.$("flag-group");
    if (group) group.value = "";
    renderFlagGroupSelect();
    syncFlagDefaultForType({ reset: true });
    var res = CW.$("flag-result");
    if (res) res.textContent = "";
    if (CW.markFormClean) CW.markFormClean("flag-form");
    else if (CW.refreshFormSubmit) CW.refreshFormSubmit("flag-form");
  }

  function openFlagDialog(flag) {
    var title = CW.$("flag-dialog-title");
    if (!flag) {
      resetFlagForm();
      if (title) title.textContent = "Add flag";
    } else {
      renderFlagGroupSelect();
      CW.$("flag-id").value = flag.id || "";
      CW.$("flag-key").value = flag.key || "";
      CW.$("flag-description").value = flag.description || "";
      CW.$("flag-type").value = flag.type || "bool";
      CW.$("flag-group").value = flag.group || "";
      try {
        CW.$("flag-default").value = JSON.stringify(flag.defaultValue === undefined ? null : flag.defaultValue);
      } catch (e) { CW.$("flag-default").value = "null"; }
      syncFlagDefaultForType();
      var res = CW.$("flag-result");
      if (res) res.textContent = "Editing " + (flag.key || flag.id || "");
      if (title) title.textContent = flag.key ? "Edit " + flag.key : "Edit flag";
    }
    if (CW.markFormClean) CW.markFormClean("flag-form");
    else if (CW.refreshFormSubmit) CW.refreshFormSubmit("flag-form");
    var dlg = CW.$("flag-dialog");
    if (!dlg) return;
    if (dlg.showModal) {
      try { if (!dlg.open) dlg.showModal(); } catch (e) { /* already open */ }
    }
  }

  function closeFlagDialog() {
    var dlg = CW.$("flag-dialog");
    if (dlg && dlg.open) dlg.close();
  }

  CW.renderFlags = renderFlags;
  CW.ruleCountFor = ruleCountFor;
  CW.openFlagRulesDialog = openFlagRulesDialog;
  CW.renderFlagRulesList = renderFlagRulesList;
  CW.saveFlagRule = saveFlagRule;
  CW.resetFlagRuleForm = resetFlagRuleForm;
  CW.closeFlagRulesDialog = closeFlagRulesDialog;
  CW.openRuleDialog = openRuleDialog;
  CW.closeRuleDialog = closeRuleDialog;
  CW.fillRuleForm = fillRuleForm;
  CW.updateFlagRuleHints = updateFlagRuleHints;
  CW.renderFlagSelects = renderFlagSelects;
  CW.renderGroups = renderGroups;
  CW.renderFlagGroupSelect = renderFlagGroupSelect;
  CW.loadFlags = loadFlags;
  CW.saveFlag = saveFlag;
  CW.deleteFlag = deleteFlag;
  CW.promptCreateGroup = promptCreateGroup;
  CW.renameGroup = renameGroup;
  CW.deleteGroup = deleteGroup;
  CW.toggleGroupCollapse = toggleGroupCollapse;
  CW.addFlagToGroup = addFlagToGroup;
  CW.moveFlag = moveFlag;
  CW.flagKeyById = flagKeyById;
  CW.updateFlagDefaultHint = updateFlagDefaultHint;
  CW.syncFlagDefaultForType = syncFlagDefaultForType;
  CW.resetFlagForm = resetFlagForm;
  CW.openFlagDialog = openFlagDialog;
  CW.closeFlagDialog = closeFlagDialog;
})();
