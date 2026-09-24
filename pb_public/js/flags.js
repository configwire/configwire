/* ConfigWire admin shell — flags.js (flags + groups). */
(function () {
  "use strict";

  var CW = window.CW;
  if (CW.state.rulesLoaded === undefined) CW.state.rulesLoaded = false;
  if (CW.state.activeFlagRulesId === undefined) CW.state.activeFlagRulesId = null;

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
    var groupFilter = CW.$("group-filter").value;
    var rows = CW.state.flags
      .filter(function (f) { return !groupFilter || (f.group || "") === groupFilter; })
      .map(function (f) {
        var gname = CW.state.groups[f.group] || f.group || "";
        var count = ruleCountFor(f.id);
        var rulesLabel = count == null ? "rules" : "rules (" + count + ")";
        return "<tr><td>" + CW.esc(f.key) + "</td><td>" + CW.esc(f.type) + "</td>" +
          "<td>" + CW.esc(gname) + "</td><td><code>" + CW.esc(JSON.stringify(f.defaultValue)) +
          "</code></td>" +
          '<td><button type="button" data-flag-rules="' + CW.esc(f.id) + '">' + CW.esc(rulesLabel) + "</button> " +
          '<button type="button" data-stats-flag="' + CW.esc(f.key) + '">stats</button> ' +
          '<button type="button" data-edit-flag="' + CW.esc(f.id) + '">edit</button> ' +
          '<button type="button" data-delete-flag="' + CW.esc(f.id) + '">delete</button></td></tr>';
      });
    CW.$("flag-tbody").innerHTML = rows.length
      ? rows.join("")
      : '<tr><td colspan="5">No flags for this project.</td></tr>';
    renderFlagGroupSelect();
    renderFlagSelects();
    CW.renderStatsFlagOptions();
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
    var sel = CW.$("group-filter");
    var cur = sel.value;
    sel.innerHTML = '<option value="">(all groups)</option>' +
      Object.keys(CW.state.groups).map(function (id) {
        return '<option value="' + CW.esc(id) + '">' + CW.esc(CW.state.groups[id]) + "</option>";
      }).join("");
    sel.value = cur;
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
    var tryFiltered = pid
      ? CW.api("/api/collections/flags/records?perPage=200&filter=" + encodeURIComponent('(project="' + pid + '")'))
      : CW.api("/api/collections/flags/records?perPage=200");
    return tryFiltered.then(function (data) {
      var items = data.items || [];
      // Client-side filter: strict match only — legacy unscoped rows must
      // not leak across projects (groups carry a required project relation).
      if (pid) items = items.filter(function (f) { return f.project === pid; });
      CW.state.flags = items.slice().sort(function (a, b) {
        return (a.key || "") < (b.key || "") ? -1 : 1;
      });
      var gq = "/api/collections/groups/records?perPage=200";
      if (pid) gq += "&filter=" + encodeURIComponent('(project="' + pid + '")');
      return CW.api(gq).then(function (g) {
        CW.state.groups = {};
        (g.items || []).forEach(function (gr) {
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
      type: CW.$("flag-type").value,
      defaultValue: parsed.value,
      project: CW.state.projectId,
    };
    var group = CW.$("flag-group").value || "";
    if (group) body.group = group;
    var req = id
      ? CW.apiMut("PATCH", "/api/collections/flags/records/" + encodeURIComponent(id), body)
      : CW.apiMut("POST", "/api/collections/flags/records", body);
    return req.then(function (out) {
      CW.$("flag-result").textContent = out.status === 200 || out.status === 201
        ? "flag saved: " + (out.data.key || out.data.id)
        : "flag save failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (out.status === 409) CW.$("flag-result").textContent += " — refresh and retry";
      loadFlags().catch(function () {});
      return out;
    });
  }

  function deleteFlag(id) {
    return CW.apiMut("DELETE", "/api/collections/flags/records/" + encodeURIComponent(id)).then(function (out) {
      var ok = out.status === 200 || out.status === 201 || out.status === 204;
      CW.toast(ok ? "flag deleted" : "flag delete failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
      loadFlags().catch(function () {});
      return out;
    });
  }

  function createGroup(ev) {
    if (ev) ev.preventDefault();
    if (!CW.state.projectId) { CW.$("group-result").textContent = "select a project first"; return Promise.resolve(); }
    var name = CW.$("group-name").value.trim();
    if (!name) { CW.$("group-result").textContent = "group name is required"; return Promise.resolve(); }
    return CW.apiMut("POST", "/api/collections/groups/records", { name: name, project: CW.state.projectId }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("group-result").textContent = ok
        ? "group created: " + (out.data.name || out.data.id)
        : "group create failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (ok) {
        CW.toast("group created: " + (out.data.name || out.data.id), true);
        CW.$("group-name").value = "";
        loadFlags().catch(function () {});
      }
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
      }
      return CW.loadRules().then(function () {
        renderFlags();
        renderFlagRulesList();
      }, function () { renderFlagRulesList(); }).then(function () { return out; });
    });
  }

  function updateFlagDefaultHint() {
    return CW.updateJsonHint("flag-default");
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
  CW.createGroup = createGroup;
  CW.flagKeyById = flagKeyById;
  CW.updateFlagDefaultHint = updateFlagDefaultHint;
})();
