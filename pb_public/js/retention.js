/* ConfigWire admin shell — retention.js (retention windows, days). */
(function () {
  "use strict";

  var CW = window.CW;

  var MIN_DAYS = 1;
  var MAX_DAYS = 1825;

  function isValidDays(n) {
    return typeof n === "number" && isFinite(n) &&
      Math.floor(n) === n && n >= MIN_DAYS && n <= MAX_DAYS;
  }

  function validatePair(rawDays, rollupDays) {
    if (!isValidDays(rawDays) || !isValidDays(rollupDays)) {
      return "Retention must be 1–1825 days.";
    }
    if (rollupDays < rawDays) {
      return "Rollup must be >= raw.";
    }
    return "";
  }

  function setInputs(d) {
    if (!d) return;
    var map = {
      "retention-raw": d.rawDays,
      "retention-rollup": d.rollupDays
    };
    Object.keys(map).forEach(function (id) {
      var el = CW.$(id);
      if (el && map[id] != null) el.value = map[id];
    });
  }

  function renderRetention(data) {
    if (!data) return;
    CW.state.retention = data;
    setInputs(data);
  }

  function loadRetention() {
    return CW.api("/api/v1/admin/retention").then(function (data) {
      CW.state.retention = data;
      setInputs(data);
      return data;
    }, function (err) {
      var el = CW.$("retention-result");
      if (el) el.textContent = (err && err.message) || "Load failed.";
      throw err;
    });
  }

  function numOr(id, fallback) {
    var el = CW.$(id);
    if (!el || el.value === "" || el.value == null) return fallback;
    var v = parseInt(el.value, 10);
    return isFinite(v) ? v : NaN;
  }

  function currentVal(key, fallback) {
    if (CW.state.retention && CW.state.retention[key] != null) return CW.state.retention[key];
    return fallback;
  }

  function putRetention(body, resElId) {
    return CW.apiMut("PUT", "/api/v1/admin/retention", body).then(function (out) {
      var resEl = CW.$(resElId || "retention-result");
      if (out.status === 200) {
        renderRetention(out.data);
        if (resEl) resEl.textContent = "Saved.";
        CW.toast("Saved.", true);
        if (CW.markFormClean) {
          CW.markFormClean("retention-form");
        }
        return loadRetention().catch(function () {});
      }
      var fail = CW.apiError(out, "Save failed");
      if (resEl) resEl.textContent = fail;
      CW.toast(fail);
      return out;
    });
  }

  function saveRetention(ev) {
    if (ev) ev.preventDefault();
    var body = {
      rawDays: numOr("retention-raw", currentVal("rawDays", NaN)),
      rollupDays: numOr("retention-rollup", currentVal("rollupDays", NaN))
    };
    var msg = validatePair(body.rawDays, body.rollupDays);
    if (msg) {
      CW.$("retention-result").textContent = msg;
      CW.toast(msg);
      return Promise.resolve();
    }
    return putRetention(body, "retention-result");
  }

  CW.renderRetention = renderRetention;
  CW.loadRetention = loadRetention;
  CW.saveRetention = saveRetention;
  // Pure validators exported for js_tests (same precedent as
  // limits.js CW.isValidIP): no behavior change.
  CW.isValidRetentionDays = isValidDays;
  CW.validateRetentionPair = validatePair;
  CW.retentionMinDays = MIN_DAYS;
  CW.retentionMaxDays = MAX_DAYS;
})();
