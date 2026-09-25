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
    if (!Array.isArray(CW.state.rules)) return null;
    // Fallback to plain "rules" label until rules have loaded at least once.
    if (!CW.state.rulesLoaded && CW.state.rules.length === 0) return null;
    var n = 0;
    for (var i = 0; i < CW.state.rules.length; i++) {
      if (CW.state.rules[i] && CW.state.rules[i].flag === flagId) n++;
    }
    return n;
  }

  function renderFlags() {
    renderFlagFolders();
    renderFlagGroupSelect();
    renderFlagSelects();
    CW.renderStatsFlagOptions();
  }

  function flagsInGroup(gid) {
    return CW.state.flags.filter(function (f) { return f && (f.group || "") === (gid || ""); });
  }

  function sortedGroupIds() {
    return Object.keys(CW.state.groups).sort(function (a, b) {
      var na = (CW.state.groups[a] || "").toLowerCase();
      var nb = (CW.state.groups[b] || "").toLowerCase();
      return na < nb ? -1 : na > nb ? 1 : 0;
    });
  }

  function folderRowHTML(f) {
    var count = ruleCountFor(f.id);
    var rulesLabel = count == null ? "rules" : "rules (" + count + ")";
    var moveOpts = '<option value="">(no group)</option>' +
      sortedGroupIds().map(function (id) {
        return '<option value="' + CW.esc(id) + '"' + (f.group === id ? " selected" : "") + ">" +
          CW.esc(CW.state.groups[id]) + "</option>";
      }).join("");
    return "<tr><td>" + CW.esc(f.key) + "</td><td>" + CW.esc(f.description || "") + "</td><td>" + CW.esc(f.type) + "</td>" +
      "<td><code>" + CW.esc(JSON.stringify(f.defaultValue)) + "</code></td>" +
      '<td><select data-move-flag="' + CW.esc(f.id) + '" aria-label="Move ' + CW.esc(f.key) + ' to group">' +
      moveOpts + "</select></td>" +
      '<td><button type="button" data-flag-rules="' + CW.esc(f.id) + '">' + CW.esc(rulesLabel) + "</button> " +
      '<button type="button" data-stats-flag="' + CW.esc(f.key) + '">stats</button> ' +
      '<button type="button" data-edit-flag="' + CW.esc(f.id) + '">edit</button> ' +
      '<button type="button" data-delete-flag="' + CW.esc(f.id) + '">delete</button></td></tr>';
  }

  function folderHTML(gid, name) {
    var key = gid || "__none";
    var flags = flagsInGroup(gid);
    var collapsed = !!CW.state.collapsedGroups[key];
    var rows = flags.map(folderRowHTML).join("");
    var body = collapsed ? "" :
      '<div class="table-wrap"><table aria-label="Flags in ' + CW.esc(name) + '">' +
      "<thead><tr><th>Key</th><th>Description</th><th>Type</th><th>Default</th><th>Move to</th><th></th></tr></thead>" +
      "<tbody>" + (rows || '<tr><td colspan="6">No flags in this group.</td></tr>') + "</tbody></table></div>";
    var groupBtns = gid
      ? '<button type="button" data-add-flag-group="' + CW.esc(gid) + '">+ flag</button> ' +
        '<button type="button" data-edit-group="' + CW.esc(gid) + '">edit</button> ' +
        '<button type="button" data-delete-group="' + CW.esc(gid) + '">delete</button>'
      : '<button type="button" data-add-flag-group="">+ flag</button>';
    return '<section class="folder">' +
      '<div class="folder-head"><button type="button" class="folder-toggle" data-toggle-group="' + CW.esc(key) +
      '" aria-expanded="' + String(!collapsed) + '" aria-label="Toggle ' + CW.esc(name) + '">' +
      (collapsed ? "&#9656;" : "&#9662;") + "</button>" +
      '<span class="folder-name">' + CW.esc(name) + '</span> <span class="muted">(' + flags.length +
      (flags.length === 1 ? " flag" : " flags") + ")</span>" +
      '<span class="folder-actions">' + groupBtns + "</span></div>" + body + "</section>";
  }

  function renderFlagFolders() {
    var box = CW.$("flag-folders");
    if (!box) return;
    var ids = sortedGroupIds();
    var html = ids.map(function (id) { return folderHTML(id, CW.state.groups[id] || id); }).join("");
    var none = flagsInGroup("");
    if (none.length || !ids.length) html += folderHTML("", "(no group)");
    box.innerHTML = html || '<p class="muted">No flags for this project.</p>';
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
    if (res) res.textContent = gid && CW.state.groups[gid] ? "new flag in " + CW.state.groups[gid] : "";
  }

  function moveFlag(id, gid) {
    return CW.apiMut("PATCH", "/api/collections/flags/records/" + encodeURIComponent(id), { group: gid || null }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.toast(ok ? "flag moved" : "flag move failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
      if (ok && CW.markUnpublished) CW.markUnpublished();
      loadFlags().catch(function () {});
      return out;
    });
  }

  function renderFlagSelects() {
    var opts = CW.state.flags.map(function (f) {
      return '<option value="' + CW.esc(f.id) + '">' + CW.esc(f.key) + "</option>";
    }).join("");
    var xs = CW.$("exp-flag-select");
    var curX = xs.value;
    xs.innerHTML = '<option value="">(none)</option>' + opts;
    if (curX !== undefined) xs.value = curX;
  }

  function renderGroups() {
    renderFlagGroupSelect();
  }

  function renderFlagGroupSelect() {
    var sel = CW.$("flag-group");
    if (!sel) return;
    var cur = sel.value;
    sel.innerHTML = '<option value="">(no group)</option>' +
      Object.keys(CW.state.groups).map(function (id) {
        return '<option value="' + CW.esc(id) + '">' + CW.esc(CW.state.groups[id]) + "</option>";
      }).join("");
    if (cur && CW.state.groups[cur]) sel.value = cur;
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
    var req = id
      ? CW.apiMut("PATCH", "/api/collections/flags/records/" + encodeURIComponent(id), body)
      : CW.apiMut("POST", "/api/collections/flags/records", body);
    return req.then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      if (ok) {
        CW.toast("flag saved: " + (out.data.key || out.data.id), true);
        CW.$("flag-result").textContent = "";
        if (CW.markUnpublished) CW.markUnpublished();
        if (CW.markFormClean) CW.markFormClean("flag-form");
        closeFlagDialog();
      } else {
        CW.$("flag-result").textContent = "flag save failed (" + out.status + "): " + CW.serverMessage(out.data);
        if (out.status === 409) CW.$("flag-result").textContent += " — refresh and retry";
      }
      loadFlags().catch(function () {});
      return out;
    });
  }

  function deleteFlag(id) {
    return CW.apiMut("DELETE", "/api/collections/flags/records/" + encodeURIComponent(id)).then(function (out) {
      var ok = out.status === 200 || out.status === 201 || out.status === 204;
      CW.toast(ok ? "flag deleted" : "flag delete failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
      if (ok && CW.markUnpublished) CW.markUnpublished();
      loadFlags().catch(function () {});
      return out;
    });
  }

  function promptCreateGroup() {
    if (!CW.state.projectId) { CW.toast("select a project first"); return Promise.resolve(); }
    var name = window.prompt("New group name");
    if (name == null) return Promise.resolve();
    name = name.trim();
    if (!name) { CW.toast("group name is required"); return Promise.resolve(); }
    return CW.apiMut("POST", "/api/collections/groups/records", { name: name, project: CW.state.projectId }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      groupStatus(ok
        ? "group created: " + (out.data.name || out.data.id)
        : "group create failed (" + out.status + "): " + CW.serverMessage(out.data));
      if (ok) {
        CW.toast("group created: " + (out.data.name || out.data.id), true);
        if (CW.markUnpublished) CW.markUnpublished();
        loadFlags().catch(function () {});
      }
      return out;
    });
  }

  function renameGroup(id) {
    var cur = CW.state.groups[id] || "";
    var name = window.prompt("Rename group", cur);
    if (name == null) return Promise.resolve();
    name = name.trim();
    if (!name) { groupStatus("group name is required"); return Promise.resolve(); }
    if (name === cur) return Promise.resolve();
    return CW.apiMut("PATCH", "/api/collections/groups/records/" + encodeURIComponent(id), { name: name }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      groupStatus(ok
        ? "group renamed: " + (out.data.name || out.data.id)
        : "group rename failed (" + out.status + "): " + CW.serverMessage(out.data));
      if (ok) {
        CW.toast("group renamed: " + (out.data.name || out.data.id), true);
        if (CW.markUnpublished) CW.markUnpublished();
        loadFlags().catch(function () {});
      }
      return out;
    });
  }

  function deleteGroup(id) {
    var name = CW.state.groups[id] || id;
    var inGroup = CW.state.flags.filter(function (f) { return f && f.group === id; });
    var msg = inGroup.length
      ? 'Delete group "' + name + '" with ' + inGroup.length + (inGroup.length === 1 ? " flag" : " flags") + "? Those flags will become ungrouped."
      : 'Delete group "' + name + '"?';
    if (!window.confirm(msg)) return Promise.resolve();
    // Ungroup flags first so the delete never leaves dangling group refs
    // (flags.group is an optional, non-cascade relation).
    var clear = inGroup.reduce(function (p, f) {
      return p.then(function () {
        return CW.apiMut("PATCH", "/api/collections/flags/records/" + encodeURIComponent(f.id), { group: null }).then(
          function () {},
          function () {}
        );
      });
    }, Promise.resolve());
    return clear.then(function () {
      return CW.apiMut("DELETE", "/api/collections/groups/records/" + encodeURIComponent(id));
    }).then(function (out) {
      var ok = out.status === 200 || out.status === 201 || out.status === 204;
      groupStatus(ok
        ? "group deleted: " + name
        : "group delete failed (" + out.status + "): " + CW.serverMessage(out.data));
      CW.toast(ok ? "group deleted" : "group delete failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
      if (ok && CW.markUnpublished) CW.markUnpublished();
      loadFlags().catch(function () {});
      return out;
    });
  }

  function flagKeyById(id) {
    for (var i = 0; i < CW.state.flags.length; i++) {
      if (CW.state.flags[i].id === id) return CW.state.flags[i].key;
    }
    return "";
  }

  function flagRulesForActive() {
    var flagId = CW.state.activeFlagRulesId || "";
    if (!Array.isArray(CW.state.rules)) return [];
    return CW.state.rules.filter(function (r) { return r && r.flag === flagId; })
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
    if (!items.length) { list.innerHTML = "<li>No rules for this flag.</li>"; return; }
    list.innerHTML = items.map(function (r) {
      return '<li class="rule-item">' +
        '<span class="rule-prio">P' + CW.esc(r.priority) + "</span>" +
        dialogConditionHTML(r.condition) +
        '<span class="rule-arrow" aria-hidden="true">→</span>' +
        dialogValueHTML(r.value) +
        '<span class="rule-actions">' +
        '<button type="button" data-edit-flag-rule="' + CW.esc(r.id) + '">edit</button>' +
        '<button type="button" data-delete-flag-rule="' + CW.esc(r.id) + '">delete</button>' +
        "</span></li>";
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
  }

  function openFlagRulesDialog(flagId) {
    CW.state.activeFlagRulesId = flagId || "";
    var hidden = CW.$("flag-rules-flag-id");
    if (hidden) hidden.value = CW.state.activeFlagRulesId;
    var title = CW.$("flag-rules-title");
    if (title) {
      var key = CW.flagKeyById ? CW.flagKeyById(flagId) : "";
      title.textContent = key ? "Rules for " + key : "Rules for flag";
    }
    resetFlagRuleForm();
    if (hidden) hidden.value = CW.state.activeFlagRulesId;
    renderFlagRulesList();
    updateFlagRuleHints();
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

  function saveFlagRule(ev) {
    if (ev) ev.preventDefault();
    var flagId = (CW.$("flag-rules-flag-id") && CW.$("flag-rules-flag-id").value) ||
      CW.state.activeFlagRulesId || "";
    if (!flagId) {
      var res0 = CW.$("flag-rules-result");
      if (res0) res0.textContent = "pick a flag first";
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
    var req = id
      ? CW.apiMut("PATCH", "/api/collections/rules/records/" + encodeURIComponent(id), body)
      : CW.apiMut("POST", "/api/collections/rules/records", body);
    return req.then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("flag-rules-result").textContent = ok
        ? (id ? "rule saved: " + id : "rule created")
        : "rule save failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (ok) {
        var fid = CW.$("flag-rules-flag-id");
        if (fid) fid.value = flagId;
        CW.state.activeFlagRulesId = flagId;
        if (CW.markUnpublished) CW.markUnpublished();
        if (CW.markFormClean) CW.markFormClean("flag-rules-form");
      }
      return CW.loadRules().then(function () {
        renderFlags();
        renderFlagRulesList();
      }, function () { renderFlagRulesList(); }).then(function () { return out; });
    });
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
      hint.textContent = "Invalid defaultValue: " + flagTypeExpectation(type);
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
      if (res) res.textContent = "editing " + (flag.key || flag.id || "");
      if (title) title.textContent = flag.key ? "Edit " + flag.key : "Edit flag";
    }
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
