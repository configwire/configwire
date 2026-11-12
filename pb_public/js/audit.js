/* ConfigWire admin shell — audit.js (superuser audit-trail viewer). */
(function () {
  "use strict";

  var CW = window.CW;

  var PER_PAGE = 50;

  function currentAuditFilter(key, fallback) {
    var f = CW.state.auditFilters || {};
    if (f[key] != null && f[key] !== "") return f[key];
    return fallback;
  }

  function readAuditFilters(page) {
    function val(id, key, fallback) {
      var el = CW.$(id);
      if (el && el.value != null && el.value !== "") return el.value;
      return currentAuditFilter(key, fallback);
    }
    var pg = page != null ? page : currentAuditFilter("page", 1);
    pg = parseInt(pg, 10);
    if (!isFinite(pg) || pg < 1) pg = 1;
    var pp = val("audit-perpage", "perPage", PER_PAGE);
    pp = parseInt(pp, 10);
    if (!isFinite(pp) || pp < 1) pp = PER_PAGE;
    if (pp > 200) pp = 200;
    return {
      action: val("audit-action", "action", ""),
      actor: val("audit-actor", "actor", ""),
      targetType: val("audit-target-type", "targetType", ""),
      targetId: val("audit-target-id", "targetId", ""),
      status: val("audit-status", "status", ""),
      since: val("audit-since", "since", ""),
      page: pg,
      perPage: pp
    };
  }

  function buildAuditPath(f) {
    var pp = parseInt(f.perPage, 10);
    if (!isFinite(pp) || pp < 1) pp = PER_PAGE;
    if (pp > 200) pp = 200;
    var parts = ["perPage=" + pp, "page=" + f.page];
    var kinds = ["action", "actor", "targetType", "targetId", "status", "since"];
    for (var ki = 0; ki < kinds.length; ki++) {
      var kv = String(f[kinds[ki]] == null ? "" : f[kinds[ki]]).replace(/^\s+|\s+$/g, "");
      if (kv !== "") parts.push(kinds[ki] + "=" + encodeURIComponent(kv));
    }
    return "/api/v1/admin/audit?" + parts.join("&");
  }

  function auditStatusClass(status) {
    var n = typeof status === "number" ? status : parseInt(status, 10);
    if (!isFinite(n)) return "is-info";
    if (n >= 200 && n < 300) return "is-ok";
    if (n >= 300 && n < 400) return "is-info";
    if (n >= 400 && n < 500) return "is-warn";
    if (n >= 500) return "is-err";
    return "is-info";
  }

  function totalAuditPagesOf(data) {
    var per = data && data.perPage ? data.perPage : PER_PAGE;
    var total = data && typeof data.total === "number" ? data.total : 0;
    return Math.max(1, Math.ceil(total / per));
  }

  function pad2(n) { return (n < 10 ? "0" : "") + n; }

  function auditCreatedCell(raw) {
    var s = String(raw == null ? "" : raw);
    var d = new Date(s);
    if (isNaN(d.getTime())) return '<span class="log-created">' + CW.esc(s) + "</span>";
    var text = d.getFullYear() + "-" + pad2(d.getMonth() + 1) + "-" + pad2(d.getDate()) +
      " " + pad2(d.getHours()) + ":" + pad2(d.getMinutes()) + ":" + pad2(d.getSeconds());
    return '<span class="log-created" title="UTC: ' + CW.esc(d.toISOString()) + '">' + text + "</span>";
  }

  function auditDetailCell(detail) {
    if (detail == null) return '<span class="muted">-</span>';
    var text;
    try {
      text = JSON.stringify(detail);
    } catch (e) {
      text = String(detail);
    }
    if (text == null) return '<span class="muted">-</span>';
    var full = CW.esc(text);
    var short = text.length > 240 ? text.slice(0, 240) : text;
    return '<span title="' + full + '">' + CW.esc(short) + "</span>";
  }

  function renderAudit(data) {
    if (!data) return;
    CW.state.audit = data;
    var items = Array.isArray(data.items) ? data.items : [];
    var page = data.page || 1;
    var perPage = data.perPage > 0 ? data.perPage : PER_PAGE;
    CW.state.auditFilters = CW.state.auditFilters || {};
    CW.state.auditFilters.page = page;
    var box = CW.$("audit-list");
    if (box) {
      var html = "";
      for (var i = 0; i < items.length; i++) {
        var it = items[i] || {};
        var label = it.targetLabel || it.targetId || "";
        var target = ((it.targetType || "") + " " + label).replace(/^\s+|\s+$/g, "");
        html += '<tr><td class="log-rownum-cell" title="' + CW.esc(it.id) + '">' + (((page - 1) * perPage) + i + 1) + "</td>" +
          '<td class="log-created-cell">' + auditCreatedCell(it.created) + "</td>" +
          '<td class="audit-actor-cell">' + CW.esc(it.actor) + "</td>" +
          '<td class="audit-action-cell">' + CW.esc(it.action) + "</td>" +
          '<td class="audit-target-cell">' + CW.esc(target) + "</td>" +
          '<td><span class="log-chip log-status ' + auditStatusClass(it.status) + '">' + CW.esc(it.status) + "</span></td>" +
          '<td class="log-msg-cell">' + auditDetailCell(it.detail) + "</td></tr>";
      }
      if (!html) {
        html = '<tr><td colspan="7">No audit entries.</td></tr>';
      }
      box.innerHTML = html;
    }
    var info = CW.$("audit-page-info");
    var total = typeof data.total === "number" ? data.total : items.length;
    var pages = totalAuditPagesOf(data);
    if (info) {
      info.textContent = "of " + pages + (pages === 1 ? " page" : " pages") +
        " · " + total + (total === 1 ? " entry" : " entries");
    }
    var sel = CW.$("audit-page");
    if (sel) {
      var opts = "";
      for (var pg = 1; pg <= pages; pg++) {
        opts += '<option value="' + pg + '"' + (pg === page ? " selected" : "") + ">" + pg + "</option>";
      }
      sel.innerHTML = opts;
      sel.disabled = pages <= 1;
    }
    var prev = CW.$("audit-prev");
    if (prev) prev.disabled = page <= 1;
    var next = CW.$("audit-next");
    if (next) next.disabled = page >= pages;
  }

  function loadAudit(page) {
    var f = readAuditFilters(page);
    CW.state.auditFilters = {
      action: f.action, actor: f.actor, targetType: f.targetType,
      targetId: f.targetId, status: f.status, since: f.since,
      page: f.page, perPage: f.perPage
    };
    return CW.api(buildAuditPath(f)).then(function (data) {
      renderAudit(data);
      var resEl = CW.$("audit-result");
      if (resEl) resEl.textContent = "";
      return data;
    }, function (err) {
      var el = CW.$("audit-result");
      if (el) el.textContent = (err && err.message) || "Load failed.";
      throw err;
    });
  }

  function refreshAudit() {
    var page = (CW.state.auditFilters && CW.state.auditFilters.page) || 1;
    return loadAudit(page).catch(function (e) { console.warn("audit: refresh", e); });
  }

  function filterAudit(ev) {
    if (ev) ev.preventDefault();
    return loadAudit(1).catch(function (e) { console.warn("audit: filter refresh", e); });
  }

  function pageAudit(delta) {
    var cur = (CW.state.audit && CW.state.audit.page) ||
      (CW.state.auditFilters && CW.state.auditFilters.page) || 1;
    var next = cur + delta;
    if (next < 1) next = 1;
    return loadAudit(next).catch(function (e) { console.warn("audit: page refresh", e); });
  }

  CW.renderAudit = renderAudit;
  CW.loadAudit = loadAudit;
  CW.filterAudit = filterAudit;
  CW.refreshAudit = refreshAudit;
  CW.pageAudit = pageAudit;
  CW.buildAuditPath = buildAuditPath;
  CW.auditStatusClass = auditStatusClass;
})();
