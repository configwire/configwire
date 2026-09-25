/* ConfigWire admin shell — experiments.js. */
(function () {
  "use strict";

  var CW = window.CW;

  function isExpUnpub(id) {
    try { if (CW.drafts && CW.drafts.isDraft("experiment", id)) return true; } catch (e) { /* ignore */ }
    try { return !!(CW.isUnpublished && CW.isUnpublished("experiment", id)); }
    catch (e) { return false; }
  }

  function expNameById(id) {
    var list = CW.drafts ? CW.drafts.mergedExperiments() : CW.state.experiments;
    if (Array.isArray(list)) {
      for (var i = 0; i < list.length; i++) {
        if (list[i] && list[i].id === id) {
          return list[i].name || id;
        }
      }
    }
    return id;
  }

  function renderExperiments() {
    var list = CW.$("experiment-list");
    var items = CW.drafts ? CW.drafts.mergedExperiments() : CW.state.experiments;
    if (!items.length) { list.innerHTML = "<li>No experiments.</li>"; return; }
    list.innerHTML = items.map(function (x) {
      var variants = Array.isArray(x.variants) ? x.variants : [];
      var summary = variants.map(function (v) {
        var w = (v && typeof v.weightBps === "number" && isFinite(v.weightBps)) ? v.weightBps : 0;
        return '<span class="exp-variant-pill">' + CW.esc(v.name) + " <b>" + CW.esc(String(bpsToPercent(w))) + "%</b></span>";
      }).join("");
      var st = x.status || "draft";
      var badgeClass = st === "running"
        ? "badge ok exp-status-running"
        : st === "stopped" ? "badge revoked exp-status-stopped" : "badge exp-status-draft";
      var statuses = ["draft", "running", "stopped"];
      var opts = statuses.map(function (s) {
        return '<option value="' + s + '"' + (st === s ? " selected" : "") + ">" + s + "</option>";
      }).join("");
      return '<li class="exp-card' + (isExpUnpub(x.id) ? " is-unpublished" : "") + '">' +
        '<div class="exp-card-head"><strong class="exp-name">' + CW.esc(x.name) + "</strong> " +
        '<span class="' + badgeClass + '">' + CW.esc(st) + "</span>" +
        (isExpUnpub(x.id) ? ' <span class="badge unpublished">Unpublished</span>' : "") + "</div>" +
        '<div class="exp-meta">flag <code>' + CW.esc(CW.flagKeyById(x.flag) || x.flag || "(none)") +
        "</code> · seed <code>" + CW.esc(x.seed) + "</code></div>" +
        '<div class="exp-variants">' + summary + "</div>" +
        '<div class="exp-actions"><label>status <select data-exp-status="' + CW.esc(x.id) + '" aria-label="Experiment status">' + opts + "</select></label> " +
        '<span class="exp-actions-buttons"><button type="button" class="btn ghost" data-edit-experiment="' + CW.esc(x.id) + '">Edit</button> ' +
        '<button type="button" class="btn ghost" data-delete-experiment="' + CW.esc(x.id) + '">Delete</button></span></div></li>';
    }).join("");
  }

  function loadExperiments() {
    return CW.apiAll("/api/collections/experiments/records").then(function (items) {
      items = items || [];
      if (CW.state.projectId) {
        var flagIds = {};
        CW.state.flags.forEach(function (f) { flagIds[f.id] = true; });
        items = items.filter(function (x) { return flagIds[x.flag]; });
      }
      CW.state.experiments = items;
      if (CW.drafts) {
        try { CW.drafts.restoreDrafts(); } catch (e) { /* best-effort */ }
      }
      renderExperiments();
      if (CW.drafts && CW.setPublishState) {
        try { CW.setPublishState(CW.drafts.hasDrafts()); } catch (e2) { /* best-effort */ }
      }
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
    // Local-only: stage the full POST/PATCH body as a draft.
    if (id) {
      CW.drafts.draftStage("experiment", { op: "update", body: body, baseId: id, label: body.name });
    } else {
      CW.drafts.draftStage("experiment", { op: "create", body: body, label: body.name });
    }
    CW.toast("draft staged: " + body.name, true);
    var resEl = CW.$("experiment-result");
    if (resEl) resEl.textContent = "";
    if (CW.markFormClean) CW.markFormClean("experiment-form");
    closeExperimentDialog();
    CW.drafts.refreshDraftChrome();
    return Promise.resolve({ status: 200, data: {} });
  }

  function createExperiment(ev) {
    return saveExperiment(ev);
  }

  function deleteExperiment(id) {
    var delName = expNameById(id);
    CW.drafts.draftStage("experiment", { op: "delete", body: {}, baseId: id, label: delName });
    CW.toast("draft staged: " + delName + " deleted", true);
    CW.drafts.refreshDraftChrome();
    return Promise.resolve({ status: 200, data: {} });
  }

  function setExperimentStatus(id, status) {
    CW.drafts.draftStage("experiment", {
      op: "update",
      body: { status: status },
      baseId: id,
      label: expNameById(id),
    });
    CW.toast("draft staged: experiment status: " + status, true);
    CW.drafts.refreshDraftChrome();
    return Promise.resolve({ status: 200, data: {} });
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

  function clearVariantFieldMarks() {
    var rows = variantRows();
    var i, j, els;
    for (i = 0; i < rows.length; i++) {
      els = rows[i] && rows[i].querySelectorAll
        ? rows[i].querySelectorAll(".exp-variant-name,.exp-variant-weight,.exp-variant-values")
        : [];
      for (j = 0; j < els.length; j++) {
        els[j].classList.remove("invalid");
        els[j].classList.remove("valid");
        els[j].removeAttribute("aria-invalid");
      }
    }
  }

  function markVariantInvalid(el) {
    if (!el || !el.classList) return;
    el.classList.remove("valid");
    el.classList.add("invalid");
    el.setAttribute("aria-invalid", "true");
  }

  // Single source of row-level validation. Marks the offending field so
  // errors show inside the variant rows instead of a JSON summary line.
  // Does not mutate row values (apply does the last-row auto-fix).
  function readAndValidateVariantRows(mark) {
    var rows = variantRows();
    function fail(el, msg) {
      if (mark !== false) markVariantInvalid(el);
      return { ok: false, error: msg };
    }
    if (!rows.length) return { ok: false, error: "add at least one variant" };
    var seen = {};
    var names = [];
    var pcts = [];
    var valuesList = [];
    var i, nameEl, weightEl, valuesEl, name, p, text;
    for (i = 0; i < rows.length; i++) {
      nameEl = rowField(rows[i], "exp-variant-name");
      weightEl = rowField(rows[i], "exp-variant-weight");
      valuesEl = rowField(rows[i], "exp-variant-values");
      name = nameEl && nameEl.value ? nameEl.value.trim() : "";
      if (!name) return fail(nameEl, "Variant " + (i + 1) + ": name is required");
      if (seen[name]) return fail(nameEl, "Variant " + (i + 1) + ": duplicate variant name: " + name);
      seen[name] = true;
      p = weightEl ? parseFloat(String(weightEl.value)) : NaN;
      if (!isFinite(p) || p < 0 || p > 100) {
        return fail(weightEl, "Variant " + (i + 1) + ": weight must be a number 0-100 (%)");
      }
      text = valuesEl ? String(valuesEl.value == null ? "" : valuesEl.value).trim() : "";
      if (text) {
        try {
          valuesList[i] = JSON.parse(text);
        } catch (e) {
          return fail(valuesEl, "Variant " + (i + 1) + ": values is not valid JSON" +
            ((e && e.message) ? ": " + e.message : ""));
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
      return { ok: false, error: "weights exceed 100% — lower the other rows so the last row stays >= 0" };
    }
    return { ok: true, error: "", names: names, pcts: pcts, valuesList: valuesList };
  }

  function applyVariantsBuilderToVariants() {
    var input = CW.$("exp-variants");
    var hint = CW.$("exp-variants-hint");
    var rows = variantRows();
    if (!input || !rows.length) return false;
    clearVariantFieldMarks();
    var checked = readAndValidateVariantRows(true);
    if (!checked.ok) {
      CW.setJsonHint(hint, null, false, checked.error);
      return false;
    }
    var pcts = checked.pcts;
    var names = checked.names;
    var valuesList = checked.valuesList;
    var sumOthers = 0;
    var j;
    for (j = 0; j < pcts.length - 1; j++) sumOthers = round2(sumOthers + pcts[j]);
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
    clearVariantFieldMarks();
    CW.setJsonHint(CW.$("exp-variants-hint"), null, true, "");
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
    var flagList = CW.drafts ? CW.drafts.mergedFlags() : (CW.state && CW.state.flags);
    if (Array.isArray(flagList) && sel && sel.value) {
      var i, f;
      for (i = 0; i < flagList.length; i++) {
        f = flagList[i];
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
    var hint = CW.$("exp-variants-hint");
    if (!hint) return false;
    if (!variantRows().length) {
      CW.setJsonHint(hint, null, false, "add at least one variant");
      return false;
    }
    clearVariantFieldMarks();
    var checked = readAndValidateVariantRows(true);
    if (checked.ok) {
      CW.setJsonHint(hint, null, true, "");
    } else {
      CW.setJsonHint(hint, null, false, checked.error);
    }
    return checked.ok;
  }

  // Canonical event wiring lives in boot.js (mirrors flag-rules pattern).
  // This module only defines logic + CW.* exports, no self-binding.

  function resetExperimentForm() {
    var idEl = CW.$("exp-id");
    if (idEl) idEl.value = "";
    var form = CW.$("experiment-form");
    if (form) form.reset();
    if (CW.resetVariantsBuilder) CW.resetVariantsBuilder();
    if (CW.updateVariantPlaceholders) CW.updateVariantPlaceholders();
    if (CW.updateExpVariantsHint) CW.updateExpVariantsHint();
    var res = CW.$("experiment-result");
    if (res) res.textContent = "";
  }

  function fillExperimentForm(found) {
    if (!found) return false;
    var idEl = CW.$("exp-id");
    if (idEl) idEl.value = found.id || "";
    var nameEl = CW.$("exp-name");
    if (nameEl) nameEl.value = found.name || "";
    var seedEl = CW.$("exp-seed");
    if (seedEl) seedEl.value = found.seed || "";
    var flagSel = CW.$("exp-flag-select");
    if (flagSel) flagSel.value = found.flag || "";
    var statusSel = CW.$("exp-status");
    if (statusSel) statusSel.value = found.status || "draft";
    try {
      CW.$("exp-variants").value = JSON.stringify(found.variants || []);
    } catch (e) { CW.$("exp-variants").value = "[]"; }
    if (CW.syncVariantsBuilderFromInput) CW.syncVariantsBuilderFromInput();
    if (CW.updateVariantPlaceholders) CW.updateVariantPlaceholders();
    if (CW.updateExpVariantsHint) CW.updateExpVariantsHint();
    var res = CW.$("experiment-result");
    if (res) res.textContent = "editing " + (found.name || found.id || "");
    return true;
  }

  function openExperimentDialog(exp) {
    var title = CW.$("experiment-dialog-title");
    if (!exp) {
      resetExperimentForm();
      if (title) title.textContent = "Add experiment";
    } else {
      fillExperimentForm(exp);
      if (title) title.textContent = exp.name ? "Edit " + exp.name : "Edit experiment";
    }
    var dlg = CW.$("experiment-dialog");
    if (!dlg) return;
    if (dlg.showModal) {
      try { if (!dlg.open) dlg.showModal(); } catch (e) { /* already open */ }
    }
  }

  function closeExperimentDialog() {
    var dlg = CW.$("experiment-dialog");
    if (dlg && dlg.open) dlg.close();
  }

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
  CW.clearVariantFieldMarks = clearVariantFieldMarks;
  CW.balanceVariantsBuilder = balanceVariantsBuilder;
  CW.recalcLastVariantWeight = recalcLastVariantWeight;
  CW.refreshLastRowLock = refreshLastRowLock;
  CW.resetExperimentForm = resetExperimentForm;
  CW.fillExperimentForm = fillExperimentForm;
  CW.openExperimentDialog = openExperimentDialog;
  CW.closeExperimentDialog = closeExperimentDialog;
  CW.bpsToPercent = bpsToPercent;
  CW.percentToBps = percentToBps;
})();
