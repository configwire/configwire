/* ConfigWire admin shell — rules.js. */
(function () {
  "use strict";

  var CW = window.CW;

  function renderRules() {
    var list = CW.$("rule-list");
    if (!CW.state.rules.length) { list.innerHTML = "<li>No rules for the selected flag.</li>"; return; }
    list.innerHTML = CW.state.rules.map(function (r) {
      var flagKey = CW.flagKeyById(r.flag) || r.flag || "";
      return "<li>p" + CW.esc(r.priority) + " flag " + CW.esc(flagKey) +
        " <code>" + CW.esc(JSON.stringify(r.condition)) + "</code> → <code>" +
        CW.esc(JSON.stringify(r.value)) + "</code></li>";
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

  function createRule(ev) {
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
    return CW.apiMut("POST", "/api/collections/rules/records", body).then(function (out) {
      CW.$("rule-result").textContent = out.status === 200 || out.status === 201
        ? "rule created"
        : "rule create failed (" + out.status + "): " + CW.serverMessage(out.data);
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
  CW.createRule = createRule;
  CW.updateRuleConditionHint = updateRuleConditionHint;
  CW.updateRuleValueHint = updateRuleValueHint;
})();
