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

  // Variants builder (explicit Apply only): the rows edit a working copy;
  // exp-variants JSON is the source of truth and is only rewritten when
  // the Apply button runs. Never auto-apply on keystroke, so manual JSON
  // edits are never clobbered. All DOM lookups are null-guarded so the
  // page keeps working when the builder markup has not landed yet.
  var MAX_VARIANT_ROWS = 8;
  var VARIANTS_TOTAL = 10000;

  // Mirrors eval/experiment.go ValidateExperiment (non-empty, weightBps
  // >= 0, sum == 10000) plus duplicate-name and empty-name checks.
  function validateVariants(v) {
    if (!Array.isArray(v)) return { ok: false, sum: 0, error: "variants must be an array" };
    if (!v.length) return { ok: false, sum: 0, error: "experiment has no variants" };
    var seen = {};
    var sum = 0;
    var i, item, name, key, w;
    for (i = 0; i < v.length; i++) {
      item = v[i];
      if (!item || typeof item !== "object" || Array.isArray(item)) {
        return { ok: false, sum: sum, error: "variant " + (i + 1) + " must be an object" };
      }
      name = item.name;
      if (typeof name !== "string" || !name.trim()) {
        return { ok: false, sum: sum, error: "variant " + (i + 1) + " has an empty name" };
      }
      key = name.trim();
      if (seen[key]) return { ok: false, sum: sum, error: "duplicate variant name: " + key };
      seen[key] = true;
      w = item.weightBps;
      if (typeof w !== "number" || !isFinite(w) || Math.floor(w) !== w) {
        return { ok: false, sum: sum, error: 'variant "' + key + '" weightBps must be an integer' };
      }
      if (w < 0) {
        return { ok: false, sum: sum, error: 'variant "' + key + '" has negative weightBps' };
      }
      sum += w;
    }
    if (sum !== VARIANTS_TOTAL) {
      return { ok: false, sum: sum, error: "weights sum " + sum + "/10000 — must total 10000" };
    }
    return { ok: true, sum: sum, error: "" };
  }

  function variantRows() {
    var wrap = CW.$("exp-variants-builder-rows");
    if (!wrap) return [];
    var out = [];
    var kids = wrap.children || [];
    var i;
    for (i = 0; i < kids.length; i++) out.push(kids[i]);
    return out;
  }

  function rowField(row, cls) {
    if (!row || !row.querySelector) return null;
    return row.querySelector("." + cls);
  }

  function valuesTextFor(values) {
    if (values === undefined || values === null) return "";
    if (typeof values === "string") return values;
    try {
      var s = JSON.stringify(values);
      return s === undefined ? "" : s;
    } catch (e) { return ""; }
  }

  function addVariantRow(name, weightBps, valuesText, index) {
    var wrap = CW.$("exp-variants-builder-rows");
    if (!wrap) return null;
    if (wrap.children && wrap.children.length >= MAX_VARIANT_ROWS) return null;
    var n = typeof index === "number" && isFinite(index) && index > 0
      ? Math.floor(index)
      : (wrap.children ? wrap.children.length + 1 : 1);
    var row = document.createElement("div");
    row.className = "rule-builder exp-variant-row";
    row.setAttribute("aria-label", "Variant " + n);
    row.innerHTML =
      '<strong>Variant ' + n + '</strong>' +
      '<label><span>Name</span> <input class="exp-variant-name" type="text" spellcheck="false" aria-label="Variant ' + n + ' name" value="' + CW.esc(name || "") + '"></label>' +
      '<label>Weight (bps) <input class="exp-variant-weight" type="number" min="0" max="10000" step="1" aria-label="Variant ' + n + ' weight bps" value="' + CW.esc(String(weightBps == null ? "" : weightBps)) + '"></label>' +
      "<label>Values (JSON) <input class=\"exp-variant-values\" type=\"text\" spellcheck=\"false\" placeholder='{\"launch_flag\": true}' aria-label=\"Variant " + n + " values JSON\" value=\"" + CW.esc(valuesText || "") + "\"></label>" +
      '<button type="button" class="btn ghost exp-variant-remove" aria-label="Remove variant ' + n + '">Remove</button>';
    wrap.appendChild(row);
    updateVariantPlaceholders();
    return row;
  }

  function applyVariantsBuilderToVariants() {
    var input = CW.$("exp-variants");
    var rows = variantRows();
    if (!input || !rows.length) return false;
    var variants = [];
    var i, nameEl, weightEl, valuesEl, name, w, text, v;
    for (i = 0; i < rows.length; i++) {
      nameEl = rowField(rows[i], "exp-variant-name");
      weightEl = rowField(rows[i], "exp-variant-weight");
      valuesEl = rowField(rows[i], "exp-variant-values");
      name = nameEl && nameEl.value ? nameEl.value.trim() : "";
      w = weightEl ? parseInt(String(weightEl.value), 10) : 0;
      if (!isFinite(w)) w = 0;
      v = { name: name, weightBps: w };
      text = valuesEl ? String(valuesEl.value == null ? "" : valuesEl.value).trim() : "";
      if (text) {
        try { v.values = JSON.parse(text); }
        catch (e) {
          CW.setJsonHint(CW.$("exp-variants-hint"), input, false,
            "row " + (i + 1) + " values is not valid JSON: " + ((e && e.message) ? e.message : "invalid JSON"));
          return false;
        }
      }
      variants.push(v);
    }
    try { input.value = JSON.stringify(variants); }
    catch (e) { return false; }
    updateExpVariantsHint();
    return true;
  }

  function syncVariantsBuilderFromInput() {
    var wrap = CW.$("exp-variants-builder-rows");
    var input = CW.$("exp-variants");
    if (!wrap || !input) return false;
    var raw = input.value;
    if (!raw || !raw.trim()) return false;
    var parsed;
    try { parsed = JSON.parse(raw); }
    catch (e) { return false; }
    if (!Array.isArray(parsed)) return false;
    wrap.innerHTML = "";
    var n = Math.min(parsed.length, MAX_VARIANT_ROWS);
    var i, item, w;
    for (i = 0; i < n; i++) {
      item = parsed[i] || {};
      w = typeof item.weightBps === "number" && isFinite(item.weightBps) ? item.weightBps : 0;
      addVariantRow(typeof item.name === "string" ? item.name : "", w, valuesTextFor(item.values), i + 1);
    }
    updateVariantPlaceholders();
    return true;
  }

  function resetVariantsBuilder() {
    var wrap = CW.$("exp-variants-builder-rows");
    if (!wrap) return false;
    wrap.innerHTML = "";
    addVariantRow("control", 5000, "");
    addVariantRow("treatment", 5000, "");
    return true;
  }

  function balanceVariantsBuilder() {
    var rows = variantRows();
    if (!rows.length) return false;
    var each = Math.floor(VARIANTS_TOTAL / rows.length);
    var rest = VARIANTS_TOTAL - each * rows.length;
    var i, weightEl;
    for (i = 0; i < rows.length; i++) {
      weightEl = rowField(rows[i], "exp-variant-weight");
      if (weightEl) weightEl.value = String(each + (i === 0 ? rest : 0));
    }
    return true;
  }

  function updateVariantPlaceholders() {
    var sel = CW.$("exp-flag-select");
    var key = "";
    if (sel && sel.value && CW.flagKeyById) {
      try { key = CW.flagKeyById(sel.value) || ""; } catch (e) { key = ""; }
    }
    if (!key) key = "launch_flag";
    var example = "true";
    if (CW.state && Array.isArray(CW.state.flags) && sel && sel.value) {
      var i, f;
      for (i = 0; i < CW.state.flags.length; i++) {
        f = CW.state.flags[i];
        if (f && f.id === sel.value) {
          if (f.type === "number") example = "1";
          else if (f.type === "string") example = '"on"';
          break;
        }
      }
    }
    var ph = '{"' + key + '": ' + example + '}';
    var rows = variantRows();
    var j, el;
    for (j = 0; j < rows.length; j++) {
      el = rowField(rows[j], "exp-variant-values");
      if (el) el.placeholder = ph;
    }
    return ph;
  }

  function updateExpVariantsHint() {
    var ok = CW.updateJsonHint("exp-variants");
    var hint = CW.$("exp-variants-hint");
    var input = CW.$("exp-variants");
    if (!hint || !input) return ok;
    if (!ok) return false;
    var raw = input.value;
    if (!raw || !raw.trim()) return ok;
    var parsed;
    try { parsed = JSON.parse(raw); }
    catch (e) { return false; }
    if (parsed === null) return ok;
    var res = validateVariants(parsed);
    if (res.ok) {
      CW.setJsonHint(hint, input, true, "Valid JSON — sum " + res.sum + "/10000");
    } else {
      CW.setJsonHint(hint, input, false, res.error);
    }
    return res.ok;
  }

  // Canonical event wiring lives in boot.js (mirrors flag-rules pattern).
  // This module only defines logic + CW.* exports, no self-binding.

  CW.renderExperiments = renderExperiments;
  CW.loadExperiments = loadExperiments;
  CW.createExperiment = createExperiment;
  CW.updateExpVariantsHint = updateExpVariantsHint;
  CW.validateVariants = validateVariants;
  CW.addVariantRow = addVariantRow;
  CW.applyVariantsBuilderToVariants = applyVariantsBuilderToVariants;
  CW.syncVariantsBuilderFromInput = syncVariantsBuilderFromInput;
  CW.updateVariantPlaceholders = updateVariantPlaceholders;
  CW.resetVariantsBuilder = resetVariantsBuilder;
  CW.balanceVariantsBuilder = balanceVariantsBuilder;
})();
