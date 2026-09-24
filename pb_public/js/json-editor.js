/* ConfigWire admin shell — json-editor.js (JSON inputs + dialog editor). */
(function () {
  "use strict";

  var CW = window.CW;

  function parseJSONInput(raw, label) {
    try {
      return { ok: true, value: raw === "" ? null : JSON.parse(raw) };
    } catch (e) {
      return { ok: false, error: label + " is not valid JSON" };
    }
  }

  function jsonDetail(raw) {
    if (raw.trim() === "") return { ok: true, value: null };
    try {
      return { ok: true, value: JSON.parse(raw) };
    } catch (e) {
      return { ok: false, error: (e && e.message) ? e.message : "invalid JSON" };
    }
  }

  function setJsonHint(hintEl, inputEl, ok, msg) {
    if (!hintEl) return;
    hintEl.textContent = msg;
    hintEl.className = "json-hint" + (msg ? (ok ? " ok" : " err") : "");
    if (inputEl) {
      inputEl.classList.remove("valid", "invalid");
      if (msg) inputEl.classList.add(ok ? "valid" : "invalid");
    }
  }

  function updateJsonHint(inputId) {
    var input = CW.$(inputId);
    if (!input) return false;
    var parsed = jsonDetail(input.value);
    setJsonHint(
      CW.$(inputId + "-hint"),
      input,
      parsed.ok,
      parsed.ok ? (input.value.trim() === "" ? "" : "Valid JSON") : "Invalid JSON: " + parsed.error
    );
    return parsed.ok;
  }

  function updateAllJsonHints() {
    updateJsonHint("flag-default");
    updateJsonHint("rule-condition");
    updateJsonHint("rule-value");
    updateJsonHint("exp-variants");
  }

  var jsonEditorTarget = "flag-default";
  var jsonEditorLabels = {
    "flag-default": "defaultValue",
    "rule-condition": "condition",
    "rule-value": "value",
    "exp-variants": "variants",
  };

  function getJsonEditorTarget() { return jsonEditorTarget; }
  function setJsonEditorTarget(t) { jsonEditorTarget = t; }

  function updateEditorStatus() {
    var ta = CW.$("json-editor-text");
    var parsed = jsonDetail(ta.value);
    setJsonHint(
      CW.$("json-editor-status"),
      ta,
      parsed.ok,
      parsed.ok ? "Valid JSON" : "Invalid JSON: " + parsed.error
    );
    var save = CW.$("json-editor-save");
    if (save) save.disabled = !parsed.ok;
    return parsed.ok;
  }

  function openJsonEditorFor(targetId) {
    var input = CW.$(targetId);
    if (!input) return;
    jsonEditorTarget = targetId;
    var label = jsonEditorLabels[targetId] || targetId;
    var title = CW.$("json-editor-title");
    if (title) title.textContent = "Edit " + label + " (JSON)";
    var ta = CW.$("json-editor-text");
    if (!ta) return;
    var parsed = jsonDetail(input.value);
    ta.value = parsed.ok && input.value.trim() !== ""
      ? JSON.stringify(parsed.value, null, 2)
      : input.value;
    updateEditorStatus();
    var dlg = CW.$("json-editor-dialog");
    if (!dlg) return;
    if (dlg.showModal) {
      try {
        if (!dlg.open) dlg.showModal();
      } catch (e) { /* already open or unsupported — editor still usable inline */ }
    }
    try {
      ta.focus();
      ta.setSelectionRange(ta.value.length, ta.value.length);
    } catch (e2) { /* focus/selection is best-effort */ }
  }

  function openJsonEditor(evOrId) {
    if (typeof evOrId === "string" && CW.$(evOrId)) return openJsonEditorFor(evOrId);
    if (evOrId) {
      var src = (evOrId.target && evOrId.target.closest &&
          evOrId.target.closest("[data-target]")) ||
        evOrId.currentTarget;
      if (src && src.getAttribute) {
        var t = src.getAttribute("data-target");
        if (t && CW.$(t)) return openJsonEditorFor(t);
      }
    }
    return openJsonEditorFor(jsonEditorTarget);
  }

  function closeJsonEditor(save) {
    var dlg = CW.$("json-editor-dialog");
    if (save) {
      if (!updateEditorStatus()) return;
      var input = CW.$(jsonEditorTarget);
      if (input) {
        input.value = CW.$("json-editor-text").value;
        updateJsonHint(jsonEditorTarget);
      }
    }
    if (dlg && dlg.open) dlg.close();
  }

  CW.parseJSONInput = parseJSONInput;
  CW.jsonDetail = jsonDetail;
  CW.setJsonHint = setJsonHint;
  CW.updateJsonHint = updateJsonHint;
  CW.updateAllJsonHints = updateAllJsonHints;
  CW.getJsonEditorTarget = getJsonEditorTarget;
  CW.setJsonEditorTarget = setJsonEditorTarget;
  CW.updateEditorStatus = updateEditorStatus;
  CW.openJsonEditorFor = openJsonEditorFor;
  CW.openJsonEditor = openJsonEditor;
  CW.closeJsonEditor = closeJsonEditor;
  CW.jsonEditorLabels = jsonEditorLabels;
})();
