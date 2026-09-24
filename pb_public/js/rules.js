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

  function renderRules() {
    var list = CW.$("rule-list");
    if (!CW.state.rules.length) { list.innerHTML = "<li>No rules for the selected flag.</li>"; return; }
    list.innerHTML = CW.state.rules.map(function (r) {
      var flagKey = CW.flagKeyById(r.flag) || r.flag || "";
      return '<li class="rule-item">' +
        '<span class="rule-prio">P' + CW.esc(r.priority) + "</span>" +
        conditionHTML(r.condition) +
        '<span class="rule-arrow" aria-hidden="true">→</span>' +
        valueHTML(r.value) +
        '<span class="rule-flag">' + CW.esc(flagKey) + "</span>" +
        ' <button type="button" data-edit-rule="' + CW.esc(r.id) + '">edit</button>' +
        ' <button type="button" data-delete-rule="' + CW.esc(r.id) + '">delete</button>' +
        "</li>";
    }).join("");
  }

  function loadRules() {
    var flagId = CW.$("rule-flag-select").value;
    var q = "/api/collections/rules/records?perPage=200&sort=priority";
    if (flagId) q += "&filter=" + encodeURIComponent('(flag="' + flagId + '")');
    return CW.api(q).then(function (data) {
      var items = data.items || [];
      if (flagId) items = items.filter(function (r) { return r.flag === flagId; });
      CW.state.rules = items.slice().sort(function (a, b) { return (a.priority || 0) - (b.priority || 0); });
      renderRules();
    }, function () {
      return CW.api("/api/collections/rules/records?perPage=200&sort=priority").then(function (data) {
        var items = data.items || [];
        if (flagId) items = items.filter(function (r) { return r.flag === flagId; });
        CW.state.rules = items;
        renderRules();
      });
    });
  }

  function saveRule(ev) {
    if (ev) ev.preventDefault();
    var flagId = CW.$("rule-flag-select").value;
    if (!flagId) { CW.$("rule-result").textContent = "pick a flag first"; return Promise.resolve(); }
    var cond = CW.parseJSONInput(CW.$("rule-condition").value, "condition");
    if (!cond.ok) { CW.$("rule-result").textContent = cond.error; return Promise.resolve(); }
    var val = CW.parseJSONInput(CW.$("rule-value").value, "value");
    if (!val.ok) { CW.$("rule-result").textContent = val.error; return Promise.resolve(); }
    var body = {
      flag: flagId,
      priority: parseInt(CW.$("rule-priority").value, 10) || 0,
      condition: cond.value,
      value: val.value,
    };
    var idEl = typeof document !== "undefined" ? document.getElementById("rule-id") : null;
    var id = idEl ? String(idEl.value || "").trim() : "";
    var req = id
      ? CW.apiMut("PATCH", "/api/collections/rules/records/" + encodeURIComponent(id), body)
      : CW.apiMut("POST", "/api/collections/rules/records", body);
    return req.then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("rule-result").textContent = ok
        ? (id ? "rule saved: " + id : "rule created")
        : "rule save failed (" + out.status + "): " + CW.serverMessage(out.data);
      loadRules().catch(function () {});
      return out;
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
    return CW.updateJsonHint("rule-condition");
  }

  function updateRuleValueHint() {
    return CW.updateJsonHint("rule-value");
  }

  CW.renderRules = renderRules;
  CW.loadRules = loadRules;
  CW.saveRule = saveRule;
  CW.createRule = saveRule;
  CW.deleteRule = deleteRule;
  CW.updateRuleConditionHint = updateRuleConditionHint;
  CW.updateRuleValueHint = updateRuleValueHint;
})();
