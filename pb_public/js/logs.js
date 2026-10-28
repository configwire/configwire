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
      : (levelEl && levelEl.value != null ? levelEl.value : currentFilter("level", ""));
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

  function levelClass(lv) {
    return lv === 0 ? "is-debug" : lv === 1 ? "is-info" : lv === 2 ? "is-warn" :
      lv === 3 ? "is-err" : "is-unknown";
  }

  // ---- Structured message parsing ----
  // Server messages are flat space-separated k=v lines, e.g.
  //   POST /api/v1/env/prod/events reason=accepted status=202 key_prefix=cw-OAtni project=abc123
  // (project= is only on rows written after project-tagged logging
  // rolled out; older rows carry no project token and fall back to the
  // env-slug lookup below.)
  function parseLogMessage(raw) {
    var out = { method: "", path: "", reason: "", status: "", keyPrefix: "",
      limiter: "", projectId: "", extra: [] };
    var parts = String(raw == null ? "" : raw).split(/\s+/).filter(function (p) { return p !== ""; });
    var i = 0;
    if (parts.length && /^[A-Z]+$/.test(parts[0])) { out.method = parts[0]; i = 1; }
    if (parts.length > i && parts[i].charAt(0) === "/") { out.path = parts[i]; i++; }
    for (; i < parts.length; i++) {
      var tok = parts[i];
      var eq = tok.indexOf("=");
      if (eq <= 0) { out.extra.push(tok); continue; }
      var k = tok.slice(0, eq), v = tok.slice(eq + 1);
      if (k === "reason") out.reason = v;
      else if (k === "status") out.status = v;
      else if (k === "key_prefix") out.keyPrefix = v;
      else if (k === "limiter") out.limiter = v;
      else if (k === "project") out.projectId = v;
      else out.extra.push(tok);
    }
    var m = out.path.match(/^\/api\/v1\/env\/([^\/]+)\//);
    out.envSlug = m ? m[1] : "";
    return out;
  }

  function projectByIdLocal(id) {
    var list = (CW.state && CW.state.projects) || [];
    for (var i = 0; i < list.length; i++) {
      if (list[i] && list[i].id === id) return list[i];
    }
    return null;
  }

  // Best-effort scope preload for project names: fills the projects list
  // (when empty) so project IDs resolve to names. Never rejects — logs
  // must render even when the read fails.
  function ensureLogScope() {
    var list = (CW.state && CW.state.projects) || [];
    if (list.length || !CW.apiAll) return Promise.resolve();
    return CW.apiAll("/api/collections/projects/records?perPage=200").then(function (items) {
      CW.state.projects = (items || []).slice().sort(function (a, b) {
        return (a.name || "") < (b.name || "") ? -1 : 1;
      });
    }, function () { /* best-effort: render without names */ });
  }

  // Resolve a display project for a parsed message. Only the project=
  // token (new rows) yields a pill — always by name, falling back to the
  // raw ID when its project record is gone. Old rows without the token
  // show no project pill at all (the env slug in the path is not unique
  // per project, so guessing would misattribute).
  function projectForLog(p) {
    if (!p.projectId) return null;
    var rec = projectByIdLocal(p.projectId);
    if (rec) return { name: rec.name || rec.id };
    return { name: p.projectId, missing: true };
  }

  function reasonClass(reason) {
    if (reason === "accepted" || reason === "served" || reason === "cached") return "is-ok";
    if (reason === "rate-limited") return "is-warn";
    if (reason) return "is-err";
    return "";
  }

  function statusClass(status) {
    var c = String(status == null ? "" : status).charAt(0);
    return c === "2" ? "is-ok" : c === "3" ? "is-info" : c === "4" ? "is-warn" :
      c === "5" ? "is-err" : "";
  }

  function methodClass(method) {
    return method === "GET" ? "is-get" : method === "POST" ? "is-post" :
      (method === "PUT" || method === "PATCH") ? "is-put" :
      method === "DELETE" ? "is-del" : "";
  }

  var LOG_REASON_HINTS = {
    "accepted": "Events accepted and queued for storage",
    "served": "Config snapshot served to the SDK",
    "cached": "Snapshot unchanged — client cache is still fresh",
    "missing-header": "X-ConfigWire-Key header missing",
    "unknown-key": "SDK key not recognized",
    "revoked-key": "SDK key has been revoked",
    "wrong-env": "Key is not scoped to this environment",
    "unknown-env": "Environment does not exist",
    "ambiguous-env": "Environment slug matches more than one project",
    "rate-limited": "Rate limit exceeded — retry shortly",
    "storage-error": "Server storage error",
    "resolve-error": "Failed to resolve the environment",
    "ip-blocked": "IP blocked by rate limiting",
    "admin-denied": "Admin access denied for this IP"
  };

  var LOG_STATUS_HINTS = {
    "200": "OK — config served",
    "202": "Accepted — events queued for storage",
    "304": "Not Modified — client cache is still fresh",
    "400": "Bad Request — check path, query and body",
    "401": "Unauthorized — missing, unknown or revoked SDK key",
    "403": "Forbidden",
    "404": "Not Found — unknown environment or route",
    "429": "Too Many Requests — back off and retry",
    "500": "Internal Server Error",
    "503": "Service Unavailable — retry shortly"
  };

  function reasonHint(reason) {
    return LOG_REASON_HINTS[reason] || ("Reason code: " + reason);
  }

  function statusHint(status) {
    var s = String(status == null ? "" : status);
    return LOG_STATUS_HINTS[s] || ("HTTP status " + s);
  }

  function messageCellHtml(raw) {
    var p = parseLogMessage(raw);
    if (!p.method && !p.path) {
      return '<div class="log-msg"><span class="log-raw">' + CW.esc(raw) + "</span></div>";
    }
    var html = '<div class="log-msg"><div class="log-msg-top">';
    if (p.method) html += '<span class="log-method ' + methodClass(p.method) + '">' + CW.esc(p.method) + "</span>";
    if (p.path) html += '<code class="log-path" title="' + CW.esc(raw) + '">' + CW.esc(p.path) + "</code>";
    html += "</div>";
    var meta = "";
    var proj = projectForLog(p);
    if (proj) {
      var cls = "log-chip log-project" + (proj.missing ? " is-missing" : "");
      var title = proj.missing ? "Project record not found (deleted?)" : "Project";
      meta += '<span class="' + cls + '" title="' + CW.esc(title) + '">' + CW.esc(proj.name) + "</span>";
    } else if (p.envSlug) {
      meta += '<span class="log-chip log-env" title="Environment">' + CW.esc(p.envSlug) + "</span>";
    }
    if (p.keyPrefix) meta += '<span class="log-chip log-key" title="SDK key prefix">' + CW.esc(p.keyPrefix) + "</span>";
    if (p.status) meta += '<span class="log-chip log-status ' + statusClass(p.status) + '" title="' + CW.esc(statusHint(p.status)) + '">' + CW.esc(p.status) + "</span>";
    if (p.reason) meta += '<span class="log-chip log-reason ' + reasonClass(p.reason) + '" title="' + CW.esc(reasonHint(p.reason)) + '">' + CW.esc(p.reason) + "</span>";
    if (p.limiter) meta += '<span class="log-chip log-limiter" title="Rate-limit budget">' + CW.esc(p.limiter) + "</span>";
    for (var i = 0; i < p.extra.length; i++) {
      meta += '<span class="log-chip log-extra">' + CW.esc(p.extra[i]) + "</span>";
    }
    if (meta) html += '<div class="log-msg-meta">' + meta + "</div>";
    html += "</div>";
    return html;
  }

  // Parse a server UTC timestamp robustly. The API renders PocketBase
  // autodates as "2026-10-28 12:36:35.861Z" (space, not T); assume UTC
  // when no offset marker is present (server stores UTC).
  function parseLogUTC(s) {
    s = String(s == null ? "" : s).trim();
    if (!s) return null;
    var m = s.match(/^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?)\s*(Z|UTC|[+-]\d{2}:?\d{2}|[+-]\d{4})?/);
    var d;
    if (m) {
      var tz = m[3] || "Z";
      if (tz === "UTC") tz = "Z";
      else if (/^[+-]\d{4}$/.test(tz)) tz = tz.slice(0, 3) + ":" + tz.slice(3);
      d = new Date(m[1] + "T" + m[2] + tz);
    } else {
      d = new Date(s);
    }
    return isNaN(d.getTime()) ? null : d;
  }

  function pad2(n) { return (n < 10 ? "0" : "") + n; }

  function createdCellHtml(raw) {
    var s = String(raw == null ? "" : raw);
    var d = parseLogUTC(s);
    if (!d) return CW.esc(s);
    var text = d.getFullYear() + "-" + pad2(d.getMonth() + 1) + "-" + pad2(d.getDate()) +
      "<br>" + pad2(d.getHours()) + ":" + pad2(d.getMinutes()) + ":" + pad2(d.getSeconds());
    return '<span class="log-created" title="UTC: ' + CW.esc(d.toISOString()) + '">' + text + "</span>";
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
        html += "<tr><td>" + CW.esc(it.id) + "</td>" +
          '<td><span class="log-level ' + levelClass(it.level) + '">' + CW.esc(levelName(it.level)) + "</span></td>" +
          '<td class="log-msg-cell">' + messageCellHtml(it.message) + "</td>" +
          '<td><span class="log-src">' + CW.esc(it.src) + "</span></td>" +
          '<td class="log-created-cell">' + createdCellHtml(it.created) + "</td></tr>";
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
    function fetchLogs() {
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
    // Resolve project names in parallel with the log fetch; render as
    // soon as both settle (scope never blocks or fails the render).
    return Promise.all([
      ensureLogScope(),
      fetchLogs()
    ]).then(function (both) { return both[1]; });
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
  CW.parseLogMessage = parseLogMessage;
  CW.parseLogUTC = parseLogUTC;
  CW.logRetentionMinDays = MIN_DAYS;
  CW.logRetentionMaxDays = MAX_DAYS;
})();
