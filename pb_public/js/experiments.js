/* ConfigWire admin shell — experiments.js. */
(function () {
  "use strict";

  var CW = window.CW;

  function renderExperiments() {
    var list = CW.$("experiment-list");
    if (!CW.state.experiments.length) { list.innerHTML = "<li>No experiments.</li>"; return; }
    list.innerHTML = CW.state.experiments.map(function (x) {
      var variants = Array.isArray(x.variants) ? x.variants : [];
      var summary = variants.map(function (v) {
        return "<span>" + CW.esc(v.name) + " " + CW.esc(v.weightBps) + "</span>";
      }).join(" ");
      var st = x.status || "draft";
      var statuses = ["draft", "running", "stopped"];
      var opts = statuses.map(function (s) {
        return '<option value="' + s + '"' + (st === s ? " selected" : "") + ">" + s + "</option>";
      }).join("");
      return "<li>" + CW.esc(x.name) + " [" + CW.esc(st) + "] flag " +
        CW.esc(CW.flagKeyById(x.flag) || x.flag || "(none)") +
        " seed <code>" + CW.esc(x.seed) + "</code>" +
        ' <span class="exp-variants">' + summary + "</span> " +
        '<select data-exp-status="' + CW.esc(x.id) + '" aria-label="Experiment status">' + opts + "</select> " +
        '<button type="button" data-edit-experiment="' + CW.esc(x.id) + '">Edit</button> ' +
        '<button type="button" data-delete-experiment="' + CW.esc(x.id) + '">Delete</button></li>';
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

  function saveExperiment(ev) {
    if (ev) ev.preventDefault();
    // Builder is the source of truth: auto-apply percent rows to the
    // hidden exp-variants transport before reading it. Abort on invalid
    // rows (apply already surfaced the reason in the visible hint).
    if (!applyVariantsBuilderToVariants()) {
      var hintEl = CW.$("exp-variants-hint");
      var resEl = CW.$("experiment-result");
      if (resEl) resEl.textContent = (hintEl && hintEl.textContent) || "invalid variants";
      return Promise.resolve();
    }
    var transport = CW.$("exp-variants");
    var parsed = CW.parseJSONInput(transport ? transport.value : "", "variants");
    if (!parsed.ok) { CW.$("experiment-result").textContent = parsed.error; return Promise.resolve(); }
    var variants = parsed.value;
    var idEl = CW.$("exp-id");
    var id = idEl ? String(idEl.value || "").trim() : "";
    var body = {
      name: CW.$("exp-name").value.trim(),
      seed: CW.$("exp-seed").value.trim(),
      variants: variants,
      status: CW.$("exp-status").value,
    };
    var flagId = CW.$("exp-flag-select").value;
    if (flagId) body.flag = flagId;
    var req = id
      ? CW.apiMut("PATCH", "/api/collections/experiments/records/" + encodeURIComponent(id), body)
      : CW.apiMut("POST", "/api/collections/experiments/records", body);
    return req.then(function (out) {
      CW.$("experiment-result").textContent = out.status === 200 || out.status === 201
        ? (id ? "experiment saved: " : "experiment created: ") + (out.data.name || out.data.id)
        : "experiment save failed (" + out.status + "): " + CW.serverMessage(out.data);
      loadExperiments().catch(function () {});
      return out;
    });
  }

  function createExperiment(ev) {
    return saveExperiment(ev);
  }

  function deleteExperiment(id) {
    return CW.apiMut("DELETE", "/api/collections/experiments/records/" + encodeURIComponent(id)).then(function (out) {
      var ok = out.status === 200 || out.status === 201 || out.status === 204;
      CW.toast(ok ? "experiment deleted" : "experiment delete failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
      loadExperiments().catch(function () {});
      return out;
    });
  }

  function setExperimentStatus(id, status) {
    return CW.apiMut("PATCH", "/api/collections/experiments/records/" + encodeURIComponent(id), { status: status }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.toast(ok ? "experiment status: " + status : "experiment status failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
      loadExperiments().catch(function () {});
      return out;
    });
  }

  // Variants builder is the source of truth: saveExperiment auto-applies
  // the rows to the hidden exp-variants transport before reading it.
  // The Apply/Validate button only re-validates and refreshes the hint.
  // Never auto-apply on keystroke (weight inputs recalc the last row
  // only). All DOM lookups are null-guarded so the page keeps working
  // when the builder markup has not landed yet.
  var MAX_VARIANT_ROWS = 8;
  var VARIANTS_TOTAL = 10000;

  function round2(n) {
    return Math.round(n * 100) / 100;
  }

  // Percent <-> bps conversions. Backend contract stays integer bps
  // summing to 10000; the builder only displays/inputs percent doubles.
  function bpsToPercent(bps) {
    return round2(bps / 100);
  }

  function percentToBps(pct) {
    return Math.round(pct * 100);
  }

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

  function addVariantRow(name, weightPercent, valuesText, index) {
    var wrap = CW.$("exp-variants-builder-rows");
    if (!wrap) return null;
    if (wrap.children && wrap.children.length >= MAX_VARIANT_ROWS) return null;
    var n = typeof index === "number" && isFinite(index) && index > 0
      ? Math.floor(index)
      : (wrap.children ? wrap.children.length + 1 : 1);
    var pct = weightPercent == null || weightPercent === "" ? "" : String(weightPercent);
    var row = document.createElement("div");
    row.className = "rule-builder exp-variant-row";
    row.setAttribute("aria-label", "Variant " + n);
    row.innerHTML =
      '<strong>Variant ' + n + '</strong>' +
      '<label><span>Name</span> <input class="exp-variant-name" type="text" spellcheck="false" aria-label="Variant ' + n + ' name" value="' + CW.esc(name || "") + '"></label>' +
      '<label>Weight (%) <input class="exp-variant-weight" type="number" min="0" max="100" step="0.01" aria-label="Variant ' + n + ' weight percent" value="' + CW.esc(pct) + '"></label>' +
      "<label>Values (JSON) <input class=\"exp-variant-values\" type=\"text\" spellcheck=\"false\" placeholder='{\"launch_flag\": true}' aria-label=\"Variant " + n + " values JSON\" value=\"" + CW.esc(valuesText || "") + "\"></label>" +
      '<button type="button" class="btn ghost exp-variant-remove" aria-label="Remove variant ' + n + '">Remove</button>';
    wrap.appendChild(row);
    updateVariantPlaceholders();
    refreshLastRowLock();
    return row;
  }

  function refreshLastRowLock() {
    var rows = variantRows();
    var i, weightEl;
    for (i = 0; i < rows.length; i++) {
      weightEl = rowField(rows[i], "exp-variant-weight");
      if (!weightEl) continue;
      if (i === rows.length - 1) {
        weightEl.readOnly = true;
        weightEl.setAttribute("readonly", "readonly");
        weightEl.classList.add("auto-weight");
        weightEl.title = "auto-calculated";
      } else {
        weightEl.readOnly = false;
        weightEl.removeAttribute("readonly");
        if (weightEl.classList) weightEl.classList.remove("auto-weight");
        weightEl.title = "";
      }
    }
    return true;
  }

  function recalcLastVariantWeight() {
    var rows = variantRows();
    if (!rows.length) return false;
    var sum = 0;
    var i, weightEl, v;
    for (i = 0; i < rows.length - 1; i++) {
      weightEl = rowField(rows[i], "exp-variant-weight");
      v = weightEl ? parseFloat(String(weightEl.value)) : 0;
      if (!isFinite(v)) v = 0;
      sum += v;
    }
    var lastPct = round2(100 - sum);
    if (!isFinite(lastPct) || lastPct < 0) lastPct = 0;
    var lastEl = rowField(rows[rows.length - 1], "exp-variant-weight");
    if (lastEl) lastEl.value = String(lastPct);
    refreshLastRowLock();
    return true;
  }

  function applyVariantsBuilderToVariants() {
    var input = CW.$("exp-variants");
    var rows = variantRows();
    if (!input || !rows.length) return false;
    var pcts = [];
    var names = [];
    var valuesList = [];
    var i, nameEl, weightEl, valuesEl, name, p, text;
    for (i = 0; i < rows.length; i++) {
      nameEl = rowField(rows[i], "exp-variant-name");
      weightEl = rowField(rows[i], "exp-variant-weight");
      valuesEl = rowField(rows[i], "exp-variant-values");
      name = nameEl && nameEl.value ? nameEl.value.trim() : "";
      p = weightEl ? parseFloat(String(weightEl.value)) : NaN;
      if (!isFinite(p) || p < 0 || p > 100) {
        CW.setJsonHint(CW.$("exp-variants-hint"), input, false,
          "row " + (i + 1) + " weight must be a number 0-100 (percent)");
        return false;
      }
      text = valuesEl ? String(valuesEl.value == null ? "" : valuesEl.value).trim() : "";
      if (text) {
        try {
          valuesList[i] = JSON.parse(text);
        } catch (e) {
          CW.setJsonHint(CW.$("exp-variants-hint"), input, false,
            "row " + (i + 1) + " values is not valid JSON: " + ((e && e.message) ? e.message : "invalid JSON"));
          return false;
        }
      } else {
        valuesList[i] = undefined;
      }
      names.push(name);
      pcts.push(p);
    }
    var sumOthers = 0;
    var j;
    for (j = 0; j < pcts.length - 1; j++) sumOthers = round2(sumOthers + pcts[j]);
    if (sumOthers > 100) {
      CW.setJsonHint(CW.$("exp-variants-hint"), input, false,
        "weights exceed 100% — lower the other rows so the last row stays >= 0");
      return false;
    }
    var lastPct = round2(100 - sumOthers);
    pcts[pcts.length - 1] = lastPct;
    var lastEl = rowField(rows[rows.length - 1], "exp-variant-weight");
    if (lastEl) lastEl.value = String(lastPct);
    refreshLastRowLock();
    var bpsList = pcts.map(function (x) { return percentToBps(x); });
    var bpsSum = 0;
    var k;
    for (k = 0; k < bpsList.length; k++) bpsSum += bpsList[k];
    bpsList[bpsList.length - 1] += (VARIANTS_TOTAL - bpsSum);
    var variants = [];
    var m, v;
    for (m = 0; m < rows.length; m++) {
      v = { name: names[m], weightBps: bpsList[m] };
      if (valuesList[m] !== undefined) v.values = valuesList[m];
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
      w = typeof item.weightBps === "number" && isFinite(item.weightBps) ? bpsToPercent(item.weightBps) : 0;
      addVariantRow(typeof item.name === "string" ? item.name : "", w, valuesTextFor(item.values), i + 1);
    }
    updateVariantPlaceholders();
    refreshLastRowLock();
    return true;
  }

  function resetVariantsBuilder() {
    var wrap = CW.$("exp-variants-builder-rows");
    if (!wrap) return false;
    wrap.innerHTML = "";
    addVariantRow("control", 50, "");
    addVariantRow("treatment", 50, "");
    recalcLastVariantWeight();
    return true;
  }

  function balanceVariantsBuilder() {
    var rows = variantRows();
    if (!rows.length) return false;
    var each = round2(100 / rows.length);
    var last = round2(100 - each * (rows.length - 1));
    var i, weightEl;
    for (i = 0; i < rows.length; i++) {
      weightEl = rowField(rows[i], "exp-variant-weight");
      if (weightEl) weightEl.value = String(i === rows.length - 1 ? last : each);
    }
    refreshLastRowLock();
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
      CW.setJsonHint(hint, input, true, "Valid JSON — sum " + res.sum + "/10000 (" + bpsToPercent(res.sum) + "%)");
    } else {
      CW.setJsonHint(hint, input, false, res.error);
    }
    return res.ok;
  }

  // Canonical event wiring lives in boot.js (mirrors flag-rules pattern).
  // This module only defines logic + CW.* exports, no self-binding.

  CW.renderExperiments = renderExperiments;
  CW.loadExperiments = loadExperiments;
  CW.saveExperiment = saveExperiment;
  CW.createExperiment = createExperiment;
  CW.deleteExperiment = deleteExperiment;
  CW.setExperimentStatus = setExperimentStatus;
  CW.updateExpVariantsHint = updateExpVariantsHint;
  CW.validateVariants = validateVariants;
  CW.addVariantRow = addVariantRow;
  CW.applyVariantsBuilderToVariants = applyVariantsBuilderToVariants;
  CW.syncVariantsBuilderFromInput = syncVariantsBuilderFromInput;
  CW.updateVariantPlaceholders = updateVariantPlaceholders;
  CW.resetVariantsBuilder = resetVariantsBuilder;
  CW.balanceVariantsBuilder = balanceVariantsBuilder;
  CW.recalcLastVariantWeight = recalcLastVariantWeight;
  CW.refreshLastRowLock = refreshLastRowLock;
  CW.bpsToPercent = bpsToPercent;
  CW.percentToBps = percentToBps;
})();
