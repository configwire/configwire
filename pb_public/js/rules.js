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
    return CW.apiAll("/api/collections/rules/records?sort=priority").then(function (items) {
      items = items || [];
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
    var opSel = CW.$("flag-rules-op");
    var op = (opSel && opSel.value) || parsed.cond.op;
    updateRuleBuilderVisibility(base, op);
    var isCustom = base === "custom.";
    if (isCustom && customInput) {
      customInput.value = String(parsed.cond.field || "").slice("custom.".length);
    }
    setBuilderValueInputs(base, op, parsed.cond.value);
    var seedInput = CW.$("flag-rules-seed");
    if (seedInput) {
      seedInput.value = typeof parsed.cond.seed === "string" ? parsed.cond.seed : "";
    }
    return true;
  }

  function toFiniteNumber(raw, fallback) {
    var n = typeof raw === "number" ? raw : parseFloat(String(raw == null ? "" : raw).trim());
    return isFinite(n) ? n : fallback;
  }

  // Value typing mirrors eval.go: percentile is numeric, custom.* coerces
  // JSON scalars with string fallback, string fields stay raw strings.
  function builderValueFor(base, op) {
    if (base === "percentile") {
      if (op === "between") {
        var loEl = CW.$("flag-rules-cond-lo");
        var hiEl = CW.$("flag-rules-cond-hi");
        var lo = loEl ? toFiniteNumber(loEl.value, 0) : 0;
        var hi = hiEl ? toFiniteNumber(hiEl.value, 9999) : 9999;
        return [lo, hi];
      }
      var vEl = CW.$("flag-rules-cond-value");
      var raw = vEl ? vEl.value : "";
      if (!String(raw == null ? "" : raw).trim()) return 5000;
      return toFiniteNumber(raw, 5000);
    }
    if (base === "custom.") {
      var cEl = CW.$("flag-rules-cond-value");
      var text = cEl ? String(cEl.value == null ? "" : cEl.value) : "";
      try { return JSON.parse(text); }
      catch (e) {
        if (/^(true|false)$/.test(text.trim())) return text.trim() === "true";
        var num = parseFloat(text.trim());
        if (text.trim() !== "" && isFinite(num) && String(num) === text.trim()) return num;
        return text;
      }
    }
    var sEl = CW.$("flag-rules-cond-value");
    return sEl ? String(sEl.value == null ? "" : sEl.value) : "";
  }

  function condValueToText(v) {
    if (v === undefined || v === null) return "";
    if (typeof v === "string") return v;
    if (typeof v === "number" || typeof v === "boolean") return String(v);
    try { return JSON.stringify(v); }
    catch (e) { return String(v); }
  }

  function setBuilderValueInputs(base, op, value) {
    var vEl = CW.$("flag-rules-cond-value");
    var loEl = CW.$("flag-rules-cond-lo");
    var hiEl = CW.$("flag-rules-cond-hi");
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

  function updateRuleBuilderVisibility(base, op) {
    var isBetween = base === "percentile" && op === "between";
    var isPercentile = base === "percentile";
    var isCustom = base === "custom.";
    var vWrap = CW.$("flag-rules-cond-value-wrap");
    var loWrap = CW.$("flag-rules-cond-lo-wrap");
    var hiWrap = CW.$("flag-rules-cond-hi-wrap");
    var customWrap = CW.$("flag-rules-custom-wrap");
    var seedWrap = CW.$("flag-rules-seed-wrap");
    if (vWrap) vWrap.hidden = !!isBetween;
    if (loWrap) loWrap.hidden = !isBetween;
    if (hiWrap) hiWrap.hidden = !isBetween;
    if (customWrap) customWrap.hidden = !isCustom;
    if (seedWrap) seedWrap.hidden = !isPercentile;
    var vInput = CW.$("flag-rules-cond-value");
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
    var cond = { field: field, op: op, value: builderValueFor(base, op) };
    var seedInput = CW.$("flag-rules-seed");
    if (seedInput && base === "percentile") {
      var seed = String(seedInput.value == null ? "" : seedInput.value).trim();
      if (seed) cond.seed = seed;
    } else {
      var parsed = parseRuleConditionInput();
      if (parsed.ok && typeof parsed.cond.seed === "string" && parsed.cond.seed) cond.seed = parsed.cond.seed;
    }
    try { input.value = JSON.stringify(cond); }
    catch (e) { return false; }
    if (CW.updateJsonHint) CW.updateJsonHint("flag-rules-condition");
    return true;
  }

  function resetRuleBuilder() {
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
})();
