/* ConfigWire admin shell — experiments.js. */
(function () {
  "use strict";

  var CW = window.CW;

  function renderExperiments() {
    var list = CW.$("experiment-list");
    if (!CW.state.experiments.length) { list.innerHTML = "<li>No experiments.</li>"; return; }
    list.innerHTML = CW.state.experiments.map(function (x) {
      return "<li>" + CW.esc(x.name) + " [" + CW.esc(x.status) + "] flag " +
        CW.esc(CW.flagKeyById(x.flag) || x.flag || "(none)") +
        " seed <code>" + CW.esc(x.seed) + "</code></li>";
    }).join("");
  }

  function loadExperiments() {
    return CW.api("/api/collections/experiments/records?perPage=200").then(function (data) {
      var items = data.items || [];
      if (CW.state.projectId) {
        var flagIds = {};
        CW.state.flags.forEach(function (f) { flagIds[f.id] = true; });
        items = items.filter(function (x) { return flagIds[x.flag]; });
      }
      CW.state.experiments = items;
      renderExperiments();
    });
  }

  function createExperiment(ev) {
    if (ev) ev.preventDefault();
    var parsed = CW.parseJSONInput(CW.$("exp-variants").value, "variants");
    if (!parsed.ok) { CW.$("experiment-result").textContent = parsed.error; return Promise.resolve(); }
    var variants = parsed.value;
    var body = {
      name: CW.$("exp-name").value.trim(),
      seed: CW.$("exp-seed").value.trim(),
      variants: variants,
      status: CW.$("exp-status").value,
    };
    var flagId = CW.$("exp-flag-select").value;
    if (flagId) body.flag = flagId;
    return CW.apiMut("POST", "/api/collections/experiments/records", body).then(function (out) {
      CW.$("experiment-result").textContent = out.status === 200 || out.status === 201
        ? "experiment created: " + (out.data.name || out.data.id)
        : "experiment create failed (" + out.status + "): " + CW.serverMessage(out.data);
      loadExperiments().catch(function () {});
      return out;
    });
  }

  function updateExpVariantsHint() {
    return CW.updateJsonHint("exp-variants");
  }

  CW.renderExperiments = renderExperiments;
  CW.loadExperiments = loadExperiments;
  CW.createExperiment = createExperiment;
  CW.updateExpVariantsHint = updateExpVariantsHint;
})();
