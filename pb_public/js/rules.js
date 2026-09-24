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
    return CW.api("/api/collections/rules/records?perPage=200&sort=priority").then(function (data) {
      var items = data.items || [];
      CW.state.rules = items.slice().sort(function (a, b) { return (a.priority || 0) - (b.priority || 0); });
      CW.state.rulesLoaded = true;
      if (CW.renderFlags) CW.renderFlags();
      if (CW.renderFlagRulesList && CW.state.activeFlagRulesId) CW.renderFlagRulesList();
    });
  }

  function deleteRule(id) {
    return CW.apiMut("DELETE", "/api/collections/rules/records/" + encodeURIComponent(id)).then(function (out) {
      var ok = out.status === 200 || out.status === 201 || out.status === 204;
      CW.toast(ok ? "rule deleted" : "rule delete failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
      loadRules().catch(function () {});
      return out;
    });
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
    if (!input) return { ok: false, cond: null };
    var raw = input.value;
    if (!raw || !raw.trim()) return { ok: false, cond: null };
    try {
      var v = JSON.parse(raw);
      if (!v || typeof v !== "object" || Array.isArray(v)) return { ok: false, cond: null };
      return { ok: true, cond: v };
    } catch (e) { return { ok: false, cond: null }; }
  }

  function populateRuleOpOptions(base, keepOp) {
    var opSel = CW.$("flag-rules-op");
    if (!opSel) return;
    var ops = CONDITION_OPS[base] || [];
    var cur = typeof keepOp === "string" && keepOp ? keepOp : opSel.value;
    opSel.innerHTML = ops.map(function (op) {
      return '<option value="' + CW.esc(op) + '">' + CW.esc(op) + "</option>";
    }).join("");
    if (ops.indexOf(cur) >= 0) opSel.value = cur;
    else if (ops.length) opSel.value = ops[0];
  }

  function syncRuleBuilderFromCondition() {
    var fieldSel = CW.$("flag-rules-field");
    var customWrap = CW.$("flag-rules-custom-wrap");
    var customInput = CW.$("flag-rules-custom");
    if (!fieldSel) return false;
    var parsed = parseRuleConditionInput();
    if (!parsed.ok) return false;
    var base = ruleBaseField(parsed.cond.field);
    if (!base) return false;
    fieldSel.value = base;
    populateRuleOpOptions(base, parsed.cond.op);
    var isCustom = base === "custom.";
    if (customWrap) customWrap.hidden = !isCustom;
    if (isCustom && customInput) {
      customInput.value = String(parsed.cond.field || "").slice("custom.".length);
    }
    return true;
  }

  function applyRuleBuilderToCondition() {
    var fieldSel = CW.$("flag-rules-field");
    var opSel = CW.$("flag-rules-op");
    var input = CW.$("flag-rules-condition");
    var customInput = CW.$("flag-rules-custom");
    if (!fieldSel || !opSel || !input) return false;
    var base = fieldSel.value || "platform";
    if (!CONDITION_OPS[base]) return false;
    var op = opSel.value || CONDITION_OPS[base][0];
    if (CONDITION_OPS[base].indexOf(op) < 0) op = CONDITION_OPS[base][0];
    var field = base;
    if (base === "custom.") {
      var name = customInput && customInput.value ? customInput.value.trim() : "";
      field = "custom." + name;
    }
    var parsed = parseRuleConditionInput();
    var cond = { field: field, op: op, value: "" };
    if (parsed.ok) {
      cond.value = parsed.cond.value === undefined ? "" : parsed.cond.value;
      if (typeof parsed.cond.seed === "string" && parsed.cond.seed) cond.seed = parsed.cond.seed;
    } else if (base === "percentile") {
      cond.value = op === "between" ? [0, 9999] : 5000;
    }
    try { input.value = JSON.stringify(cond); }
    catch (e) { return false; }
    if (CW.updateJsonHint) CW.updateJsonHint("flag-rules-condition");
    return true;
  }

  function resetRuleBuilder() {
    var fieldSel = CW.$("flag-rules-field");
    if (fieldSel) fieldSel.value = "platform";
    var customWrap = CW.$("flag-rules-custom-wrap");
    if (customWrap) customWrap.hidden = true;
    var customInput = CW.$("flag-rules-custom");
    if (customInput) customInput.value = "";
    populateRuleOpOptions("platform", "==");
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
  CW.resetRuleBuilder = resetRuleBuilder;
})();
