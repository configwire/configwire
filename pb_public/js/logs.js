/* ConfigWire admin shell — logs.js (superuser log viewer + log settings). */
(function () {
  "use strict";

  var CW = window.CW;

  var MIN_DAYS = 1;
  var MAX_DAYS = 1825;
  var PER_PAGE = 10;

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
    var ppEl = CW.$("logs-perpage");
    var pp = (ppEl && ppEl.value != null && ppEl.value !== "") ? ppEl.value : currentFilter("perPage", PER_PAGE);
    pp = parseInt(pp, 10);
    if (!isFinite(pp) || pp < 1) pp = PER_PAGE;
    if (pp > 200) pp = 200;
    return {
      level: lv, src: sc, page: pg, perPage: pp,
      project: currentFilter("project", ""), key: currentFilter("key", ""),
      status: currentFilter("status", ""), reason: currentFilter("reason", ""),
      limiter: currentFilter("limiter", "")
    };
  }

  function buildLogsPath(f) {
    var pp = parseInt(f.perPage, 10);
    if (!isFinite(pp) || pp < 1) pp = PER_PAGE;
    if (pp > 200) pp = 200;
    var parts = ["perPage=" + pp, "page=" + f.page];
    var lv = String(f.level == null ? "" : f.level).replace(/^\s+|\s+$/g, "");
    if (lv !== "") parts.push("level=" + encodeURIComponent(lv));
    var sc = String(f.src == null ? "" : f.src).replace(/^\s+|\s+$/g, "");
    if (sc !== "") parts.push("src=" + encodeURIComponent(sc));
    var kinds = ["project", "key", "status", "reason", "limiter"];
    for (var ki = 0; ki < kinds.length; ki++) {
      var kv = String(f[kinds[ki]] == null ? "" : f[kinds[ki]]).replace(/^\s+|\s+$/g, "");
      if (kv !== "") parts.push(kinds[ki] + "=" + encodeURIComponent(kv));
    }
    return "/api/v1/admin/logs?" + parts.join("&");
  }

  var LOG_FILTER_KINDS = ["level", "src", "project", "key", "status", "reason", "limiter"];

  function logFilterKindLabel(kind) {
    return kind === "project" ? "Project" : kind === "key" ? "Key" :
      kind === "src" ? "Source" : kind === "status" ? "Status" :
      kind === "reason" ? "Reason" : kind === "limiter" ? "Limiter" :
      kind === "level" ? "Level" : kind;
  }

  function logFilterLabel(kind, val) {
    if (kind === "project") {
      var rec = projectByIdLocal(val);
      return rec ? (rec.name || rec.id) : val;
    }
    if (kind === "level") {
      var n = parseInt(val, 10);
      return levelName(isFinite(n) ? n : val);
    }
    return val;
  }

  function syncLogFilterInputs() {
    var f = CW.state.logFilters || {};
    var levelEl = CW.$("logs-level");
    if (levelEl && levelEl.value != null) levelEl.value = f.level == null ? "" : f.level;
    var srcEl = CW.$("logs-src");
    if (srcEl && srcEl.value != null) srcEl.value = f.src == null ? "" : f.src;
    var ppEl = CW.$("logs-perpage");
    if (ppEl && ppEl.value != null) ppEl.value = String(f.perPage == null ? PER_PAGE : f.perPage);
  }

  function reloadLogsFirstPage() {
    return loadLogs(1).catch(function (e) { console.warn('logs: filter refresh', e); });
  }

  function toggleLogFilter(kind, val) {
    CW.state.logFilters = CW.state.logFilters || {};
    var f = CW.state.logFilters;
    f[kind] = (f[kind] === val) ? "" : val;
    syncLogFilterInputs();
    return reloadLogsFirstPage();
  }

  function clearLogFilter(kind) {
    CW.state.logFilters = CW.state.logFilters || {};
    CW.state.logFilters[kind] = "";
    syncLogFilterInputs();
    return reloadLogsFirstPage();
  }

  function clearAllLogFilters() {
    var keepPerPage = (CW.state.logFilters && CW.state.logFilters.perPage) || PER_PAGE;
    CW.state.logFilters = {
      level: "", src: "", project: "", key: "",
      status: "", reason: "", limiter: "", page: 1, perPage: keepPerPage
    };
    syncLogFilterInputs();
    return reloadLogsFirstPage();
  }

  function renderActiveLogFilters() {
    var box = CW.$("logs-active-filters");
    if (!box) return;
    var f = CW.state.logFilters || {};
    var html = "";
    for (var i = 0; i < LOG_FILTER_KINDS.length; i++) {
      var kind = LOG_FILTER_KINDS[i];
      var val = f[kind];
      if (val == null || val === "") continue;
      html += '<button type="button" class="log-filter-pill" data-f-clear="' + CW.esc(kind) +
        '" title="Remove filter">' + CW.esc(logFilterKindLabel(kind) + ": " + logFilterLabel(kind, val)) +
        ' <span class="log-filter-x" aria-hidden="true"><svg width="8" height="8" viewBox="0 0 8 8"><path d="M1.5 1.5l5 5M6.5 1.5l-5 5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg></span></button>';
    }
    if (html) html += '<button type="button" class="log-filter-clear" data-f-clearall="1">Clear all</button>';
    box.innerHTML = html;
    if (box.hidden != null) box.hidden = !html;
  }

  function wireLogFilterClicks() {
    var list = CW.$("logs-list");
    if (list && !list.__cwLogChipsWired && list.addEventListener) {
      list.addEventListener("click", function (ev) {
        var t = ev && ev.target ? ev.target : null;
        var chip = t && t.closest ? t.closest("[data-f-kind]") : null;
        if (!chip || !chip.getAttribute) return;
        var kind = chip.getAttribute("data-f-kind");
        var val = chip.getAttribute("data-f-val");
        if (kind && val != null) toggleLogFilter(kind, val);
      });
      list.__cwLogChipsWired = true;
    }
    var bar = CW.$("logs-active-filters");
    if (bar && !bar.__cwLogPillsWired && bar.addEventListener) {
      bar.addEventListener("click", function (ev) {
        var t = ev && ev.target ? ev.target : null;
        if (!t || !t.closest) return;
        if (t.closest("[data-f-clearall]")) { clearAllLogFilters(); return; }
        var pill = t.closest("[data-f-clear]");
        if (pill && pill.getAttribute) clearLogFilter(pill.getAttribute("data-f-clear"));
      });
      bar.__cwLogPillsWired = true;
    }
    var pp = CW.$("logs-perpage");
    if (pp && !pp.__cwLogPerPageWired && pp.addEventListener) {
      pp.addEventListener("change", function () { reloadLogsFirstPage(); });
      pp.__cwLogPerPageWired = true;
    }
    var pgSel = CW.$("logs-page");
    if (pgSel && !pgSel.__cwLogPageWired && pgSel.addEventListener) {
      pgSel.addEventListener("change", function () {
        var n = parseInt(pgSel.value, 10);
        if (isFinite(n) && n >= 1) loadLogs(n).catch(function (e) { console.warn('logs: page refresh', e); });
      });
      pgSel.__cwLogPageWired = true;
    }
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
    if (rec) return { id: p.projectId, name: rec.name || rec.id };
    return { id: p.projectId, name: p.projectId, missing: true };
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

  function filterChip(kind, val, cls, title, label) {
    return '<button type="button" class="' + cls + '" data-f-kind="' + CW.esc(kind) +
      '" data-f-val="' + CW.esc(val) + '" title="' + CW.esc(title) + '">' + CW.esc(label) + "</button>";
  }

  function messageCellHtml(raw, src) {
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
      var pcls = "log-chip log-project" + (proj.missing ? " is-missing" : "");
      var ptitle = proj.missing ? "Project record not found (deleted?)" : "Project";
      meta += filterChip("project", proj.id, pcls, ptitle, proj.name);
    }
    if (p.keyPrefix) meta += filterChip("key", p.keyPrefix, "log-chip log-key", "SDK key prefix", p.keyPrefix);
    if (src) meta += filterChip("src", src, "log-chip log-source", "Source", src);
    if (p.status) meta += filterChip("status", p.status, "log-chip log-status " + statusClass(p.status), statusHint(p.status), p.status);
    if (p.reason) meta += filterChip("reason", p.reason, "log-chip log-reason " + reasonClass(p.reason), reasonHint(p.reason), p.reason);
    if (p.limiter) meta += filterChip("limiter", p.limiter, "log-chip log-limiter", "Rate-limit budget", p.limiter);
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
      " " + pad2(d.getHours()) + ":" + pad2(d.getMinutes()) + ":" + pad2(d.getSeconds());
    return '<span class="log-created" title="UTC: ' + CW.esc(d.toISOString()) + '">' + text + "</span>";
  }

  function renderLogs(data) {
    if (!data) return;
    CW.state.logs = data;
    var items = Array.isArray(data.items) ? data.items : [];
    var page = data.page || 1;
    var perPage = data.perPage > 0 ? data.perPage : 50;
    CW.state.logFilters = CW.state.logFilters || {};
    CW.state.logFilters.page = page;
    var box = CW.$("logs-list");
    if (box) {
      var html = "";
      for (var i = 0; i < items.length; i++) {
        var it = items[i] || {};
        html += '<tr><td class="log-rownum-cell" title="' + CW.esc(it.id) + '">' + (((page - 1) * perPage) + i + 1) + "</td>" +
          '<td><span class="log-level ' + levelClass(it.level) + '">' + CW.esc(levelName(it.level)) + "</span></td>" +
          '<td class="log-msg-cell">' + messageCellHtml(it.message, it.src) + "</td>" +
          '<td class="log-created-cell">' + createdCellHtml(it.created) + "</td></tr>";
      }
      if (!html) {
        html = '<tr><td colspan="4">No logs.</td></tr>';
      }
      box.innerHTML = html;
    }
    renderActiveLogFilters();
    wireLogFilterClicks();
    var info = CW.$("logs-page-info");
    var total = typeof data.total === "number" ? data.total : items.length;
    var pages = totalPagesOf(data);
    if (info) {
      info.textContent = "of " + pages + (pages === 1 ? " page" : " pages") +
        " · " + total + (total === 1 ? " log" : " logs");
    }
    var sel = CW.$("logs-page");
    if (sel) {
      var opts = "";
      for (var pg = 1; pg <= pages; pg++) {
        opts += '<option value="' + pg + '"' + (pg === page ? " selected" : "") + ">" + pg + "</option>";
      }
      sel.innerHTML = opts;
      sel.disabled = pages <= 1;
    }
    var prev = CW.$("logs-prev");
    if (prev) prev.disabled = page <= 1;
    var next = CW.$("logs-next");
    if (next) next.disabled = page >= pages;
  }

  function loadLogs(page, level, src) {
    var f = readFilters(page, level, src);
    CW.state.logFilters = {
      level: f.level, src: f.src, project: f.project, key: f.key,
      status: f.status, reason: f.reason, limiter: f.limiter, page: f.page,
      perPage: f.perPage
    };
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
