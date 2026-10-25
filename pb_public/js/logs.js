/* ConfigWire admin shell — logs.js (superuser log viewer + log settings). */
(function () {
  "use strict";

  var CW = window.CW;

  var MIN_DAYS = 1;
  var MAX_DAYS = 1825;
  var PER_PAGE = 50;

  function isValidLogDays(n) {
    return typeof n === "number" && isFinite(n) &&
      Math.floor(n) === n && n >= MIN_DAYS && n <= MAX_DAYS;
  }

  function validateLogSettings(maxDays) {
    if (!isValidLogDays(maxDays)) {
      return "Log retention must be 1–1825 days.";
    }
    return "";
  }

  function setLogEnabledSwitch(on) {
    var sw = CW.$("log-enabled");
    if (sw && sw.setAttribute) sw.setAttribute("aria-checked", on ? "true" : "false");
  }

  function isLogEnabledSwitchOn() {
    var sw = CW.$("log-enabled");
    if (sw && sw.getAttribute) return sw.getAttribute("aria-checked") === "true";
    return currentVal("enabled", false);
  }

  function onLogEnabledSwitch() {
    setLogEnabledSwitch(!isLogEnabledSwitchOn());
    if (CW.markFormDirty) CW.markFormDirty("log-settings-form");
    else if (CW.refreshFormSubmit) CW.refreshFormSubmit("log-settings-form");
  }

  function setSettingsInputs(d) {
    if (!d) return;
    if (d.enabled != null) setLogEnabledSwitch(!!d.enabled);
    var daysEl = CW.$("log-maxdays");
    if (daysEl && d.maxDays != null) daysEl.value = d.maxDays;
  }

  function renderLogSettings(data) {
    if (!data) return;
    CW.state.logSettings = data;
    setSettingsInputs(data);
  }

  function loadLogSettings() {
    return CW.api("/api/v1/admin/log-settings").then(function (data) {
      CW.state.logSettings = data;
      setSettingsInputs(data);
      return data;
    }, function (err) {
      var el = CW.$("log-settings-result");
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
    if (CW.state.logSettings && CW.state.logSettings[key] != null) return CW.state.logSettings[key];
    return fallback;
  }

  function putLogSettings(body, resElId) {
    return CW.apiMut("PUT", "/api/v1/admin/log-settings", body).then(function (out) {
      var resEl = CW.$(resElId || "log-settings-result");
      if (out.status === 200) {
        renderLogSettings(out.data);
        if (resEl) resEl.textContent = "Saved.";
        CW.toast("Saved.", true);
        if (CW.markFormClean) {
          CW.markFormClean("log-settings-form");
        }
        closeLogSettingsDialog();
        return loadLogSettings().catch(function (e) { console.warn('logs: save list refresh', e); });
      }
      var fail = CW.apiError(out, "Save failed");
      if (resEl) resEl.textContent = fail;
      CW.toast(fail);
      return out;
    });
  }

  function saveLogSettings(ev) {
    if (ev) ev.preventDefault();
    var enabled = isLogEnabledSwitchOn();
    var body = {
      enabled: enabled,
      maxDays: numOr("log-maxdays", currentVal("maxDays", NaN))
    };
    var msg = validateLogSettings(body.maxDays);
    if (msg) {
      CW.$("log-settings-result").textContent = msg;
      CW.toast(msg);
      return Promise.resolve();
    }
    return putLogSettings(body, "log-settings-result");
  }

  function openLogSettingsDialog() {
    setSettingsInputs(CW.state.logSettings);
    var res = CW.$("log-settings-result");
    if (res) res.textContent = "";
    if (CW.markFormClean) CW.markFormClean("log-settings-form");
    else if (CW.refreshFormSubmit) CW.refreshFormSubmit("log-settings-form");
    var dlg = CW.$("log-settings-dialog");
    if (!dlg) return;
    if (dlg.showModal) {
      try { if (!dlg.open) dlg.showModal(); } catch (e) { /* already open */ }
    }
  }

  function closeLogSettingsDialog() {
    var dlg = CW.$("log-settings-dialog");
    if (dlg && dlg.open) dlg.close();
  }

  function currentFilter(key, fallback) {
    var f = CW.state.logFilters || {};
    if (f[key] != null && f[key] !== "") return f[key];
    return fallback;
  }

  function readFilters(page, level, src) {
    var levelEl = CW.$("logs-level");
    var srcEl = CW.$("logs-src");
    var lv = level != null && level !== "" ? level
      : (levelEl && levelEl.value != null && levelEl.value !== "" ? levelEl.value : currentFilter("level", ""));
    var sc = src != null ? src
      : (srcEl && srcEl.value != null ? srcEl.value : currentFilter("src", ""));
    var pg = page != null ? page : currentFilter("page", 1);
    pg = parseInt(pg, 10);
    if (!isFinite(pg) || pg < 1) pg = 1;
    return { level: lv, src: sc, page: pg };
  }

  function buildLogsPath(f) {
    var parts = ["perPage=" + PER_PAGE, "page=" + f.page];
    var lv = String(f.level == null ? "" : f.level).replace(/^\s+|\s+$/g, "");
    if (lv !== "") parts.push("level=" + encodeURIComponent(lv));
    var sc = String(f.src == null ? "" : f.src).replace(/^\s+|\s+$/g, "");
    if (sc !== "") parts.push("src=" + encodeURIComponent(sc));
    return "/api/v1/admin/logs?" + parts.join("&");
  }

  function totalPagesOf(data) {
    var per = data && data.perPage ? data.perPage : PER_PAGE;
    var total = data && typeof data.total === "number" ? data.total : 0;
    return Math.max(1, Math.ceil(total / per));
  }

  // Numeric levels render as names (0..3 mirror logx/logstore levels);
  // unknown values fall back to the raw value, never blank.
  function levelName(lv) {
    return lv === 0 ? "DEBUG" : lv === 1 ? "INFO" : lv === 2 ? "WARN" : lv === 3 ? "ERROR" : lv;
  }

  function renderLogs(data) {
    if (!data) return;
    CW.state.logs = data;
    var items = Array.isArray(data.items) ? data.items : [];
    var page = data.page || 1;
    CW.state.logFilters = CW.state.logFilters || {};
    CW.state.logFilters.page = page;
    var box = CW.$("logs-list");
    if (box) {
      var html = "";
      for (var i = 0; i < items.length; i++) {
        var it = items[i] || {};
        html += "<tr><td>" + CW.esc(it.id) + "</td><td>" + CW.esc(levelName(it.level)) +
          "</td><td>" + CW.esc(it.message) + "</td><td>" + CW.esc(it.src) +
          "</td><td>" + CW.esc(it.created) + "</td></tr>";
      }
      if (!html) {
        html = '<tr><td colspan="5">No logs.</td></tr>';
      }
      box.innerHTML = html;
    }
    var info = CW.$("logs-page-info");
    if (info) {
      var total = typeof data.total === "number" ? data.total : items.length;
      info.textContent = "Page " + page + " of " + totalPagesOf(data) +
        " (" + total + " total)";
    }
    var prev = CW.$("logs-prev");
    if (prev) prev.disabled = page <= 1;
    var next = CW.$("logs-next");
    if (next) next.disabled = page >= totalPagesOf(data);
  }

  function loadLogs(page, level, src) {
    var f = readFilters(page, level, src);
    CW.state.logFilters = { level: f.level, src: f.src, page: f.page };
    return CW.api(buildLogsPath(f)).then(function (data) {
      renderLogs(data);
      var resEl = CW.$("logs-result");
      if (resEl) resEl.textContent = "";
      return data;
    }, function (err) {
      var el = CW.$("logs-result");
      if (el) el.textContent = (err && err.message) || "Load failed.";
      throw err;
    });
  }

  function filterLogs(ev) {
    if (ev) ev.preventDefault();
    return loadLogs(1).catch(function (e) { console.warn('logs: filter refresh', e); });
  }

  function pageLogs(delta) {
    var cur = (CW.state.logs && CW.state.logs.page) ||
      (CW.state.logFilters && CW.state.logFilters.page) || 1;
    var next = cur + delta;
    if (next < 1) next = 1;
    return loadLogs(next).catch(function (e) { console.warn('logs: page refresh', e); });
  }

  CW.renderLogSettings = renderLogSettings;
  CW.setLogEnabledSwitch = setLogEnabledSwitch;
  CW.onLogEnabledSwitch = onLogEnabledSwitch;
  CW.loadLogSettings = loadLogSettings;
  CW.saveLogSettings = saveLogSettings;
  CW.openLogSettingsDialog = openLogSettingsDialog;
  CW.closeLogSettingsDialog = closeLogSettingsDialog;
  CW.renderLogs = renderLogs;
  CW.loadLogs = loadLogs;
  CW.filterLogs = filterLogs;
  CW.pageLogs = pageLogs;
  // Pure validators exported for js_tests (same precedent as
  // retention.js CW.isValidRetentionDays): no behavior change.
  CW.isValidLogDays = isValidLogDays;
  CW.validateLogSettings = validateLogSettings;
  CW.logRetentionMinDays = MIN_DAYS;
  CW.logRetentionMaxDays = MAX_DAYS;
})();
