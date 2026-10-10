/* ConfigWire admin shell — rules.js. */
(function () {
  "use strict";

  var CW = window.CW;

  var TRUNC_COND = 48;
  var TRUNC_VALUE = 120;

  function trunc(s, n) {
    s = String(s);
    if (s.length > n) return s.slice(0, n - 1) + "…";
    return s;
  }

  function fmtOperand(v) {
    if (typeof v === "string") return trunc(v, TRUNC_COND);
    if (typeof v === "number" || typeof v === "boolean") return String(v);
    if (v == null) return "null";
    if (Array.isArray(v)) {
      return v.map(function (x) {
        return typeof x === "string" ? trunc(x, TRUNC_COND) : String(x);
      }).join(", ");
    }
    try { return trunc(JSON.stringify(v), TRUNC_COND); }
    catch (e) { return trunc(String(v), TRUNC_COND); }
  }

  function sentenceFor(cond) {
    if (!cond || typeof cond !== "object" || Array.isArray(cond)) return null;
    var field = cond.field, op = cond.op;
    if (typeof field !== "string" || !field) return null;
    if (typeof op !== "string" || !op) return null;
    var s;
    if (op === "between" && Array.isArray(cond.value) && cond.value.length >= 2) {
      s = field + " between " + fmtOperand(cond.value[0]) + " and " + fmtOperand(cond.value[1]);
    } else {
      s = field + " " + op + " " + fmtOperand(cond.value);
    }
    if (typeof cond.seed === "string" && cond.seed) s += " · seed " + trunc(cond.seed, TRUNC_COND);
    return s;
  }

  function normalizeCondition(cond) {
    if (typeof cond === "string") {
      try { return JSON.parse(cond); }
      catch (e) { return null; }
    }
    return cond;
  }

  function fallbackConditionHTML(cond) {
    var raw;
    try { raw = typeof cond === "string" ? cond : JSON.stringify(cond); }
    catch (e) { raw = String(cond); }
    if (raw == null || raw === "") raw = "{}";
    return "<code>" + CW.esc(raw) + "</code>";
  }

  function conditionHTML(cond) {
    var c = normalizeCondition(cond);
    var parts, i, s;
    if (Array.isArray(c)) {
      if (!c.length) return fallbackConditionHTML(cond);
      parts = [];
      for (i = 0; i < c.length; i++) {
        s = sentenceFor(c[i]);
        if (s == null) return fallbackConditionHTML(cond);
        parts.push(s);
      }
    } else {
      s = sentenceFor(c);
      if (s == null) return fallbackConditionHTML(cond);
      parts = [s];
    }
    return '<span class="rule-cond">' + CW.esc(parts.join(" and ")) + "</span>";
  }

  function valueHTML(v) {
    if (v === true) return '<span class="rule-value is-true">true</span>';
    if (v === false) return '<span class="rule-value is-false">false</span>';
    var raw;
    try {
      raw = JSON.stringify(v);
      if (raw === undefined) raw = String(v);
    } catch (e) { raw = String(v); }
    return '<span class="rule-value">' + CW.esc(trunc(raw, TRUNC_VALUE)) + "</span>";
  }

  function loadRules() {
    var gen = CW.state.scopeGen;
    return CW.apiAll("/api/collections/rules/records?sort=priority").then(function (items) {
      if (CW.scopeStale(gen)) return;
      items = items || [];
      CW.state.rules = items.slice().sort(function (a, b) { return (a.priority || 0) - (b.priority || 0); });
      CW.state.rulesLoaded = true;
      if (CW.drafts) {
        try { CW.drafts.restoreDrafts(); } catch (e) { /* best-effort */ }
      }
      if (CW.renderFlags) CW.renderFlags();
      if (CW.renderFlagRulesList && CW.state.activeFlagRulesId) CW.renderFlagRulesList();
      if (CW.drafts && CW.setPublishState) {
        try { CW.setPublishState(CW.drafts.hasDrafts()); } catch (e2) { /* best-effort */ }
      }
    });
  }

  function deleteRule(id) {
    CW.drafts.draftStage("rule", { op: "delete", body: {}, baseId: id, label: "rule deleted" });
    CW.toast("Draft saved: rule deleted", true);
    CW.drafts.refreshDraftChrome();
    return Promise.resolve({ status: 200, data: {} });
  }

  function updateRuleConditionHint() {
    return CW.updateJsonHint("flag-rules-condition");
  }

  function updateRuleValueHint() {
    return CW.updateJsonHint("flag-rules-value");
  }

  // Condition builder (dropdowns only): mirrors the frozen allowlists in
  // releases/snapshot.go + eval/eval.go so Field -> Op stays valid.
  // The selects write into the Condition JSON input; value/seed are
  // preserved from the current JSON and stay editable as raw JSON.
  var CONDITION_OPS = {
    platform: ["==", "!=", "contains", "regex"],
    appVersion: ["<", "<=", "==", "!=", ">=", ">", "contains", "regex"],
    locale: ["==", "!=", "contains", "regex"],
    country: ["==", "!=", "contains", "regex"],
    percentile: ["<=", "between"],
    "custom.": ["==", "!=", "<", "<=", ">", ">=", "contains", "regex"],
  };

  function ruleBaseField(field) {
    if (typeof field !== "string" || !field) return null;
    if (CONDITION_OPS[field]) return field;
    if (field === "custom." || field.indexOf("custom.") === 0) return "custom.";
    return null;
  }

  function parseRuleConditionInput() {
    var input = CW.$("flag-rules-condition");
    if (!input) return { ok: false, cond: null, conds: [] };
    var raw = input.value;
    if (!raw || !raw.trim()) return { ok: false, cond: null, conds: [] };
    try {
      var v = JSON.parse(raw);
      var i;
      if (Array.isArray(v)) {
        if (!v.length) return { ok: false, cond: null, conds: [] };
        for (i = 0; i < v.length; i++) {
          if (!v[i] || typeof v[i] !== "object" || Array.isArray(v[i])) {
            return { ok: false, cond: null, conds: [] };
          }
        }
        return { ok: true, cond: v[0], conds: v.slice() };
      }
      if (!v || typeof v !== "object") return { ok: false, cond: null, conds: [] };
      return { ok: true, cond: v, conds: [v] };
    } catch (e) { return { ok: false, cond: null, conds: [] }; }
  }

  // Multi-row condition builder: one .rule-cond-row per AND-ed condition.
  // Row 1 keeps the legacy element IDs (flag-rules-field/op/cond-value/lo/hi/
  // custom/seed + wraps) so single-row callers keep working; extra rows use
  // class/data-attribute selectors only (no duplicate IDs). Row-scoped helpers
  // take an optional row root; a null/legacy row falls back to the globals.
  var MAX_CONDS = 10;

  function condRowContainer() { return CW.$("flag-rules-conditions"); }

  function isCondRow(n) {
    if (!n) return false;
    if (n._cwCondRow) return true;
    try {
      if (n.getAttribute && n.getAttribute("data-cond-row") != null) return true;
    } catch (e) { /* attribute read is best-effort */ }
    try {
      if (n.className && String(n.className).indexOf("rule-cond-row") !== -1) return true;
    } catch (e2) { /* className read is best-effort */ }
    return false;
  }

  function condRows() {
    var c = condRowContainer();
    var out = [];
    if (!c) return out;
    var i, nl;
    if (c.children && c.children.length) {
      for (i = 0; i < c.children.length; i++) {
        if (isCondRow(c.children[i])) out.push(c.children[i]);
      }
      if (out.length) return out;
    }
    try {
      if (c.querySelectorAll) {
        nl = c.querySelectorAll("[data-cond-row]");
        for (i = 0; nl && i < nl.length; i++) out.push(nl[i]);
      }
    } catch (e) { /* selector lookup is best-effort */ }
    return out;
  }

  function condExtraChildren() {
    var c = condRowContainer();
    var out = [];
    if (!c || !c.children) return out;
    for (var i = 0; i < c.children.length; i++) {
      if (c.children[i] && c.children[i]._cwExtra) out.push(c.children[i]);
    }
    return out;
  }

  // Ordered row list where null stands for the legacy first-row globals.
  // Real DOM: the static row (no _cwExtra) is row 1. Stub DOMs have no static
  // row element, so globals are prepended explicitly instead of being dropped.
  function effectiveRows() {
    var rows = condRows();
    if (!rows.length) return [null].concat(condExtraChildren());
    var hasFirst = false;
    for (var i = 0; i < rows.length; i++) {
      if (!rows[i]._cwExtra) { hasFirst = true; break; }
    }
    return hasFirst ? rows : [null].concat(rows);
  }

  function rowQuery(row, sel) {
    try {
      if (row && row.querySelector) {
        var f = row.querySelector(sel);
        if (f) return f;
      }
    } catch (e) { /* selector lookup is best-effort */ }
    return null;
  }

  // Extra (non-first) rows are flagged at creation; the static first row
  // carries the legacy IDs, so only non-extra rows may fall back to globals
  // (this also covers stub DOMs whose querySelector never matches).
  function rowContainsLegacy(row) {
    if (!row) return true;
    if (row._cwExtra) return false;
    var f = rowQuery(row, "#flag-rules-field");
    if (f) return true;
    if (row._cwCondRow) return true;
    return false;
  }

  function rowCtl(row, cacheKey, sel, legacyId) {
    if (row && row._cwCtl && row._cwCtl[cacheKey]) return row._cwCtl[cacheKey];
    var f = rowQuery(row, sel);
    if (f) return f;
    if (rowContainsLegacy(row)) return CW.$(legacyId);
    return null;
  }

  function fieldSelFor(row) { return rowCtl(row, "field", "[data-c-field]", "flag-rules-field"); }
  function opSelFor(row) { return rowCtl(row, "op", "[data-c-op]", "flag-rules-op"); }
  function valueInputFor(row) { return rowCtl(row, "value", "[data-c-value]", "flag-rules-cond-value"); }
  function loInputFor(row) { return rowCtl(row, "lo", "[data-c-lo]", "flag-rules-cond-lo"); }
  function hiInputFor(row) { return rowCtl(row, "hi", "[data-c-hi]", "flag-rules-cond-hi"); }
  function customInputFor(row) { return rowCtl(row, "custom", "[data-c-custom]", "flag-rules-custom"); }
  function seedInputFor(row) { return rowCtl(row, "seed", "[data-c-seed]", "flag-rules-seed"); }
  function valueWrapFor(row) { return rowCtl(row, "valueWrap", "[data-c-value-wrap]", "flag-rules-cond-value-wrap"); }
  function loWrapFor(row) { return rowCtl(row, "loWrap", "[data-c-lo-wrap]", "flag-rules-cond-lo-wrap"); }
  function hiWrapFor(row) { return rowCtl(row, "hiWrap", "[data-c-hi-wrap]", "flag-rules-cond-hi-wrap"); }
  function customWrapFor(row) { return rowCtl(row, "customWrap", "[data-c-custom-wrap]", "flag-rules-custom-wrap"); }
  function seedWrapFor(row) { return rowCtl(row, "seedWrap", "[data-c-seed-wrap]", "flag-rules-seed-wrap"); }
  function removeBtnFor(row) { return rowCtl(row, "remove", "[data-cond-remove]", "flag-rules-remove"); }

  function populateRuleOpOptions(base, keepOp, opSel) {
    var opSelEl = opSel || CW.$("flag-rules-op");
    if (!opSelEl) return;
    var ops = CONDITION_OPS[base] || [];
    var cur = typeof keepOp === "string" && keepOp ? keepOp : opSelEl.value;
    opSelEl.innerHTML = ops.map(function (op) {
      return '<option value="' + CW.esc(op) + '">' + CW.esc(op) + "</option>";
    }).join("");
    if (ops.indexOf(cur) >= 0) opSelEl.value = cur;
    else if (ops.length) opSelEl.value = ops[0];
  }

  function setRowFromCond(row, cond) {
    if (!cond || typeof cond !== "object") return false;
    var base = ruleBaseField(cond.field);
    if (!base) return false;
    var fieldSel = fieldSelFor(row);
    if (!fieldSel) return false;
    fieldSel.value = base;
    var opSel = opSelFor(row);
    populateRuleOpOptions(base, cond.op, opSel);
    var op = (opSel && opSel.value) || cond.op;
    updateRuleBuilderVisibility(base, op, row);
    if (base === "custom.") {
      var customInput = customInputFor(row);
      if (customInput) customInput.value = String(cond.field || "").slice("custom.".length);
    }
    setBuilderValueInputs(base, op, cond.value, row);
    var seedInput = seedInputFor(row);
    if (seedInput) {
      seedInput.value = typeof cond.seed === "string" ? cond.seed : "";
    }
    return true;
  }

  function syncRuleBuilderFromCondition() {
    var parsed = parseRuleConditionInput();
    if (!parsed.ok || !parsed.conds || !parsed.conds.length) return false;
    var conds = parsed.conds.slice(0, MAX_CONDS);
    var i, guard;
    for (i = 0; i < conds.length; i++) {
      if (!ruleBaseField(conds[i] && conds[i].field)) return false;
    }
    var eff = effectiveRows();
    guard = 0;
    while (eff.length < conds.length && guard++ < MAX_CONDS + 1) {
      if (!addConditionRow(null, true)) break;
      eff = effectiveRows();
    }
    guard = 0;
    while (eff.length > conds.length && guard++ < MAX_CONDS + 1) {
      var last = eff[eff.length - 1];
      if (last === null) break;
      removeConditionRow(last, true);
      eff = effectiveRows();
    }
    if (eff.length !== conds.length) return false;
    for (i = 0; i < eff.length; i++) {
      if (!setRowFromCond(eff[i], conds[i])) return false;
    }
    updateCondRowChrome();
    return true;
  }

  function toFiniteNumber(raw, fallback) {
    var n = typeof raw === "number" ? raw : parseFloat(String(raw == null ? "" : raw).trim());
    return isFinite(n) ? n : fallback;
  }

  // Value typing mirrors eval.go: percentile is numeric, custom.* coerces
  // JSON scalars with string fallback, string fields stay raw strings.
  // root scopes the lookup to one row; null/legacy reads the global IDs.
  function builderValueFor(base, op, root) {
    function ctl(key, sel, legacyId) {
      if (root && root._cwCtl && root._cwCtl[key]) return root._cwCtl[key];
      try {
        if (root && root.querySelector) {
          var f = root.querySelector(sel);
          if (f) return f;
        }
      } catch (e) { /* selector lookup is best-effort */ }
      if (!root || !root._cwExtra) return CW.$(legacyId);
      return null;
    }
    if (base === "percentile") {
      if (op === "between") {
        var loEl = ctl("lo", "[data-c-lo]", "flag-rules-cond-lo");
        var hiEl = ctl("hi", "[data-c-hi]", "flag-rules-cond-hi");
        var lo = loEl ? toFiniteNumber(loEl.value, 0) : 0;
        var hi = hiEl ? toFiniteNumber(hiEl.value, 9999) : 9999;
        return [lo, hi];
      }
      var vEl = ctl("value", "[data-c-value]", "flag-rules-cond-value");
      var raw = vEl ? vEl.value : "";
      if (!String(raw == null ? "" : raw).trim()) return 5000;
      return toFiniteNumber(raw, 5000);
    }
    if (base === "custom.") {
      var cEl = ctl("value", "[data-c-value]", "flag-rules-cond-value");
      var text = cEl ? String(cEl.value == null ? "" : cEl.value) : "";
      try { return JSON.parse(text); }
      catch (e) {
        if (/^(true|false)$/.test(text.trim())) return text.trim() === "true";
        var num = parseFloat(text.trim());
        if (text.trim() !== "" && isFinite(num) && String(num) === text.trim()) return num;
        return text;
      }
    }
    var sEl = ctl("value", "[data-c-value]", "flag-rules-cond-value");
    return sEl ? String(sEl.value == null ? "" : sEl.value) : "";
  }

  function condValueToText(v) {
    if (v === undefined || v === null) return "";
    if (typeof v === "string") return v;
    if (typeof v === "number" || typeof v === "boolean") return String(v);
    try { return JSON.stringify(v); }
    catch (e) { return String(v); }
  }

  function setBuilderValueInputs(base, op, value, root) {
    var vEl = valueInputFor(root);
    var loEl = loInputFor(root);
    var hiEl = hiInputFor(root);
    if (base === "percentile" && op === "between") {
      var lo = 0, hi = 9999;
      if (Array.isArray(value)) {
        if (value.length > 0) lo = toFiniteNumber(value[0], 0);
        if (value.length > 1) hi = toFiniteNumber(value[1], 9999);
      } else if (value !== undefined) {
        lo = toFiniteNumber(value, 0);
      }
      if (loEl) loEl.value = String(lo);
      if (hiEl) hiEl.value = String(hi);
      return;
    }
    if (vEl) vEl.value = condValueToText(value === undefined ? "" : value);
  }

  function updateRuleBuilderVisibility(base, op, root) {
    var isBetween = base === "percentile" && op === "between";
    var isPercentile = base === "percentile";
    var isCustom = base === "custom.";
    var vWrap = valueWrapFor(root);
    var loWrap = loWrapFor(root);
    var hiWrap = hiWrapFor(root);
    var customWrap = customWrapFor(root);
    var seedWrap = seedWrapFor(root);
    if (vWrap) vWrap.hidden = !!isBetween;
    if (loWrap) loWrap.hidden = !isBetween;
    if (hiWrap) hiWrap.hidden = !isBetween;
    if (customWrap) customWrap.hidden = !isCustom;
    if (seedWrap) seedWrap.hidden = !isPercentile;
    var vInput = valueInputFor(root);
    if (vInput) {
      if (isPercentile && !isBetween) {
        vInput.setAttribute("type", "number");
        vInput.setAttribute("min", "0");
        vInput.setAttribute("max", "9999");
        if (!vInput.placeholder || vInput.placeholder === "ios") vInput.placeholder = "5000";
      } else {
        vInput.setAttribute("type", "text");
        vInput.removeAttribute("min");
        vInput.removeAttribute("max");
        if (base === "platform" && (!vInput.placeholder || vInput.placeholder === "5000")) vInput.placeholder = "ios";
      }
    }
  }

  function condFromRow(row, prevCond) {
    var fieldSel = fieldSelFor(row);
    var opSel = opSelFor(row);
    if (!fieldSel || !opSel) return { ok: false, cond: null };
    var base = fieldSel.value || "platform";
    if (!CONDITION_OPS[base]) return { ok: false, cond: null };
    var op = opSel.value || CONDITION_OPS[base][0];
    if (CONDITION_OPS[base].indexOf(op) < 0) op = CONDITION_OPS[base][0];
    var field = base;
    if (base === "custom.") {
      var customInput = customInputFor(row);
      var name = customInput && customInput.value ? customInput.value.trim() : "";
      field = "custom." + name;
    }
    var cond = { field: field, op: op, value: builderValueFor(base, op, row) };
    var seedInput = seedInputFor(row);
    if (seedInput && base === "percentile") {
      var seed = String(seedInput.value == null ? "" : seedInput.value).trim();
      if (seed) cond.seed = seed;
    } else if (prevCond && typeof prevCond.seed === "string" && prevCond.seed) {
      cond.seed = prevCond.seed;
    }
    return { ok: true, cond: cond };
  }

  function writeConditionInput(conds) {
    var input = CW.$("flag-rules-condition");
    if (!input) return false;
    try { input.value = JSON.stringify(conds.length > 1 ? conds : conds[0]); }
    catch (e) { return false; }
    if (CW.updateJsonHint) CW.updateJsonHint("flag-rules-condition");
    return true;
  }

  function applyRuleBuilderToCondition() {
    var input = CW.$("flag-rules-condition");
    if (!input) return false;
    var prev = null;
    try { prev = JSON.parse(input.value); } catch (e) { prev = null; }
    var eff = effectiveRows();
    if (!eff.length) eff = [null];
    var conds = [];
    for (var i = 0; i < eff.length; i++) {
      var prevCond = Array.isArray(prev) ? prev[i] : (i === 0 ? prev : null);
      var r = condFromRow(eff[i], prevCond);
      if (!r.ok) return false;
      conds.push(r.cond);
    }
    if (!conds.length) return false;
    if (!writeConditionInput(conds)) return false;
    updateCondRowChrome();
    return true;
  }

  function refreshCondRow(row, what) {
    var fieldSel = fieldSelFor(row);
    if (!fieldSel) return null;
    var base = fieldSel.value || "platform";
    var opSel = opSelFor(row);
    if (what === "field") populateRuleOpOptions(base, opSel && opSel.value, opSel);
    var op = (opSel && opSel.value) || "";
    updateRuleBuilderVisibility(base, op, row);
    return { base: base, op: op };
  }

  function mkBuilderControl(tag, cls, dataAttr, aria, props) {
    var el = document.createElement(tag);
    if (cls) el.className = cls;
    if (dataAttr && el.setAttribute) {
      try { el.setAttribute(dataAttr, ""); } catch (e) { /* best-effort */ }
    }
    if (aria && el.setAttribute) {
      try { el.setAttribute("aria-label", aria); } catch (e2) { /* best-effort */ }
    }
    if (props) {
      for (var k in props) {
        if (Object.prototype.hasOwnProperty.call(props, k)) el[k] = props[k];
      }
    }
    return el;
  }

  function buildConditionRow() {
    var row = document.createElement("div");
    row.className = "rule-cond-row";
    if (row.setAttribute) {
      try { row.setAttribute("data-cond-row", ""); } catch (e) { /* best-effort */ }
    }
    row._cwCondRow = true;
    row._cwExtra = true;
    var ctl = {};
    row._cwCtl = ctl;
    function addField(labelText, wrapAttr, input, wrapClass) {
      var label = document.createElement("label");
      if (wrapClass) label.className = wrapClass;
      if (wrapAttr && label.setAttribute) {
        try { label.setAttribute(wrapAttr, ""); } catch (e2) { /* best-effort */ }
      }
      var hidden = document.createElement("span");
      hidden.className = "rule-cond-label-text";
      if (hidden.setAttribute) {
        try { hidden.setAttribute("aria-hidden", "true"); } catch (e3) { /* best-effort */ }
      }
      hidden.textContent = labelText;
      label.appendChild(hidden);
      label.appendChild(input);
      row.appendChild(label);
      return label;
    }
    ctl.field = mkBuilderControl("select", "rule-cond-field", "data-c-field", "Condition field", { value: "platform" });
    ["platform", "appVersion", "locale", "country", "percentile", "custom."].forEach(function (name) {
      var opt = document.createElement("option");
      opt.value = name;
      opt.textContent = name === "custom." ? "custom.*" : name;
      ctl.field.appendChild(opt);
    });
    addField("Field", null, ctl.field, "rule-cond-field-wrap");
    ctl.op = mkBuilderControl("select", "rule-cond-op", "data-c-op", "Condition operator", { value: "" });
    addField("Operator", null, ctl.op, "rule-cond-op-wrap");
    ctl.value = mkBuilderControl("input", "rule-cond-value", "data-c-value", "Condition value",
      { type: "text", value: "", placeholder: "ios" });
    ctl.valueWrap = addField("Value", "data-c-value-wrap", ctl.value, "rule-cond-value-wrap");
    ctl.lo = mkBuilderControl("input", "rule-cond-lo", "data-c-lo", "Condition range min",
      { type: "number", value: "0", min: "0", max: "9999" });
    ctl.loWrap = addField("Min", "data-c-lo-wrap", ctl.lo, "rule-cond-lo-wrap");
    ctl.loWrap.hidden = true;
    ctl.hi = mkBuilderControl("input", "rule-cond-hi", "data-c-hi", "Condition range max",
      { type: "number", value: "9999", min: "0", max: "9999" });
    ctl.hiWrap = addField("Max", "data-c-hi-wrap", ctl.hi, "rule-cond-hi-wrap");
    ctl.hiWrap.hidden = true;
    ctl.custom = mkBuilderControl("input", "rule-cond-custom", "data-c-custom", "Custom attribute name",
      { type: "text", value: "", placeholder: "tier" });
    ctl.customWrap = addField("Custom attr", "data-c-custom-wrap", ctl.custom, "rule-cond-custom-wrap");
    ctl.customWrap.hidden = true;
    ctl.seed = mkBuilderControl("input", "rule-cond-seed", "data-c-seed", "Percentile seed",
      { type: "text", value: "", placeholder: "rollout-1" });
    ctl.seedWrap = addField("Seed", "data-c-seed-wrap", ctl.seed, "rule-cond-seed-wrap");
    ctl.seedWrap.hidden = true;
    ctl.remove = mkBuilderControl("button", "btn ghost rule-cond-remove", "data-cond-remove",
      "Remove condition", { type: "button", textContent: "×", hidden: false });
    row.appendChild(ctl.remove);
    return row;
  }

  function addConditionRow(cond, silent) {
    var c = condRowContainer();
    if (!c) return null;
    var n = condRows().length || 1;
    if (n >= MAX_CONDS) return null;
    var row = buildConditionRow();
    c.appendChild(row);
    if (cond) setRowFromCond(row, cond);
    else {
      populateRuleOpOptions("platform", "==", row._cwCtl.op);
      updateRuleBuilderVisibility("platform", "==", row);
    }
    updateCondRowChrome();
    return row;
  }

  function detachRow(row) {
    var c = condRowContainer();
    if (c && c.removeChild) {
      try { c.removeChild(row); return true; } catch (e) { /* already detached */ }
    }
    if (c && c.children) {
      for (var i = 0; i < c.children.length; i++) {
        if (c.children[i] === row) { c.children.splice(i, 1); return true; }
      }
    }
    return false;
  }

  function removeConditionRow(row, silent) {
    var eff = effectiveRows();
    if (eff.length <= 1) return false;
    var rows = condRows();
    if (rows.length) {
      var target = row;
      if (!target || rows.indexOf(target) < 0) target = rows[rows.length - 1];
      if (rows.indexOf(target) === 0 && rows.length > 1) target = rows[rows.length - 1];
      detachRow(target);
    } else {
      if (!row || !row._cwExtra) return false;
      detachRow(row);
    }
    updateCondRowChrome();
    return true;
  }

  function updateCondRowChrome() {
    var rows = condRows();
    var extras = condExtraChildren();
    var n = rows.length || (1 + extras.length);
    var i, btn;
    for (i = 0; i < rows.length; i++) {
      btn = removeBtnFor(rows[i]);
      if (btn) btn.hidden = n <= 1;
    }
    for (i = 0; i < extras.length; i++) {
      if (rows.indexOf(extras[i]) < 0) {
        btn = removeBtnFor(extras[i]);
        if (btn) btn.hidden = n <= 1;
      }
    }
    var add = CW.$("flag-rules-add-cond");
    if (add) add.disabled = n >= MAX_CONDS;
  }

  function resetRuleBuilder() {
    var rows = condRows();
    var i;
    for (i = rows.length - 1; i >= 1; i--) removeConditionRow(rows[i], true);
    var extras = condExtraChildren();
    for (i = 0; i < extras.length; i++) detachRow(extras[i]);
    var fieldSel = CW.$("flag-rules-field");
    if (fieldSel) fieldSel.value = "platform";
    var customInput = CW.$("flag-rules-custom");
    if (customInput) customInput.value = "";
    var vInput = CW.$("flag-rules-cond-value");
    if (vInput) vInput.value = "ios";
    var loInput = CW.$("flag-rules-cond-lo");
    if (loInput) loInput.value = "0";
    var hiInput = CW.$("flag-rules-cond-hi");
    if (hiInput) hiInput.value = "9999";
    var seedInput = CW.$("flag-rules-seed");
    if (seedInput) seedInput.value = "";
    populateRuleOpOptions("platform", "==");
    updateRuleBuilderVisibility("platform", "==");
    updateCondRowChrome();
  }

  CW.conditionHTML = conditionHTML;
  CW.valueHTML = valueHTML;
  CW.loadRules = loadRules;
  CW.deleteRule = deleteRule;
  CW.updateRuleConditionHint = updateRuleConditionHint;
  CW.updateRuleValueHint = updateRuleValueHint;
  CW.CONDITION_OPS = CONDITION_OPS;
  CW.populateRuleOpOptions = populateRuleOpOptions;
  CW.syncRuleBuilderFromCondition = syncRuleBuilderFromCondition;
  CW.applyRuleBuilderToCondition = applyRuleBuilderToCondition;
  CW.updateRuleBuilderVisibility = updateRuleBuilderVisibility;
  CW.resetRuleBuilder = resetRuleBuilder;
  CW.addConditionRow = addConditionRow;
  CW.removeConditionRow = removeConditionRow;
  CW.condRows = condRows;
  CW.refreshCondRow = refreshCondRow;
})();
