"use strict";
// test-audit.js — audit.js admin audit-trail viewer (dedicated Audit screen
// at #/p/audit, superuser-only). Loads core.js + logs.js + audit.js only via
// js_tests/harness.js (router.js / boot.js added per-suite below for the
// showAudit + wiring tests — never the full order, so the suite stays
// hermetic). node:test + node:assert/strict only. Run from configwire/:
//   node --test js_tests/test-audit.js
//
// Covers CW.buildAuditPath (defaults, full filter encoding, empty omitted,
// perPage clamp, since passthrough), CW.auditStatusClass (2xx/3xx/4xx/5xx+
// mapping), renderAudit (2 rows with object + null detail, escaped content,
// status chips, truncated detail, entry-style page info), the empty-list
// row, the error path (#audit-result set, prior rows kept), pagination
// (120 total / 50 per page -> 3 pages, prev/next disabled ends), filter
// submit reloading page 1 (fixed-set action/target-type/status selects +
// actor text), router parseHash/showView/showAudit (dedicated #/p/audit view,
// showLogs no longer loads audit), boot wiring submit/prev/next, the
// index.html markup contract (view-audit section, sidebar link, select
// options), and the no-third-party contract.
var test = require("node:test");
var assert = require("node:assert/strict");
var fs = require("node:fs");
var path = require("node:path");
var h = require("./harness.js");

function freshAudit(overrides) {
  return h.load(["core.js", "logs.js", "audit.js"], overrides);
}

function okRes(data) {
  return { ok: true, status: 200, json: function () { return Promise.resolve(data); } };
}

function flushed() {
  return new Promise(function (resolve) { setImmediate(resolve); });
}

function auditPage(items, page, perPage, total) {
  return { items: items, page: page, perPage: perPage, total: total };
}

// fetchImpl serving one fixed audit page; every other URL is denied.
function auditImpl(items, page, perPage, total) {
  return function (url) {
    if (String(url).indexOf("/api/v1/admin/audit") === 0) {
      return Promise.resolve(okRes(auditPage(items, page, perPage, total)));
    }
    return Promise.reject(new Error("harness fetch denied: " + url));
  };
}

// fetchImpl echoing the requested page back (total fixed at 120).
function pagedAuditImpl() {
  return function (url) {
    var u = String(url);
    if (u.indexOf("/api/v1/admin/audit") === 0) {
      var m = /[?&]page=(\d+)/.exec(u);
      var pg = m ? parseInt(m[1], 10) : 1;
      return Promise.resolve(okRes(auditPage([], pg, 50, 120)));
    }
    return Promise.reject(new Error("harness fetch denied: " + url));
  };
}

function auditUrls(ctx) {
  return ctx.fetchCalls.map(function (c) { return String(c.url); })
    .filter(function (u) { return u.indexOf("/api/v1/admin/audit") === 0; });
}

// --- buildAuditPath (audit.js:40-51) ---

test("buildAuditPath defaults to perPage 50, same-origin path", function () {
  var ctx = freshAudit();
  assert.equal(ctx.CW.buildAuditPath({ page: 1 }), "/api/v1/admin/audit?perPage=50&page=1");
});

test("buildAuditPath encodes every filter", function () {
  var ctx = freshAudit();
  var u = ctx.CW.buildAuditPath({
    action: "release.publish", actor: "admin@example.com",
    targetType: "environment", targetId: "env 1",
    status: "200", since: "7d", page: 2, perPage: 25,
  });
  assert.ok(u.indexOf("/api/v1/admin/audit?") === 0, "same-origin, got " + u);
  assert.ok(u.indexOf("perPage=25") !== -1, "got " + u);
  assert.ok(u.indexOf("page=2") !== -1, "got " + u);
  assert.ok(u.indexOf("action=release.publish") !== -1, "got " + u);
  assert.ok(u.indexOf("actor=admin%40example.com") !== -1, "got " + u);
  assert.ok(u.indexOf("targetType=environment") !== -1, "got " + u);
  assert.ok(u.indexOf("targetId=env%201") !== -1, "got " + u);
  assert.ok(u.indexOf("status=200") !== -1, "got " + u);
  assert.ok(u.indexOf("since=7d") !== -1, "got " + u);
});

test("buildAuditPath omits empty and whitespace-only filters", function () {
  var ctx = freshAudit();
  assert.equal(
    ctx.CW.buildAuditPath({
      action: "", actor: "   ", targetType: "", targetId: "",
      status: "", since: "", page: 1, perPage: 50,
    }),
    "/api/v1/admin/audit?perPage=50&page=1"
  );
});

test("buildAuditPath clamps perPage to 1..200, default 50", function () {
  var ctx = freshAudit();
  assert.ok(ctx.CW.buildAuditPath({ page: 1, perPage: 500 }).indexOf("perPage=200") !== -1);
  assert.ok(ctx.CW.buildAuditPath({ page: 1, perPage: 0 }).indexOf("perPage=50") !== -1);
  assert.ok(ctx.CW.buildAuditPath({ page: 1, perPage: -3 }).indexOf("perPage=50") !== -1);
  assert.ok(ctx.CW.buildAuditPath({ page: 1, perPage: "abc" }).indexOf("perPage=50") !== -1);
  assert.ok(ctx.CW.buildAuditPath({ page: 1 }).indexOf("perPage=50") !== -1);
});

test("buildAuditPath passes since through, omits when empty", function () {
  var ctx = freshAudit();
  assert.ok(ctx.CW.buildAuditPath({ page: 1, since: "all" }).indexOf("since=all") !== -1);
  assert.ok(ctx.CW.buildAuditPath({ page: 1, since: "365d" }).indexOf("since=365d") !== -1);
  assert.ok(ctx.CW.buildAuditPath({ page: 1, since: "" }).indexOf("since=") === -1);
});

// --- auditStatusClass (audit.js:53-61) ---

test("auditStatusClass maps 2xx/3xx/4xx/5xx+, else is-info", function () {
  var ctx = freshAudit();
  var cls = ctx.CW.auditStatusClass;
  assert.equal(cls(200), "is-ok");
  assert.equal(cls(201), "is-ok");
  assert.equal(cls("200"), "is-ok");
  assert.equal(cls(301), "is-info");
  assert.equal(cls(304), "is-info");
  assert.equal(cls(404), "is-warn");
  assert.equal(cls(429), "is-warn");
  assert.equal(cls(500), "is-err");
  assert.equal(cls(503), "is-err");
  assert.equal(cls(""), "is-info");
  assert.equal(cls(undefined), "is-info");
  assert.equal(cls("xyz"), "is-info");
  assert.equal(cls(99), "is-info");
});

// --- renderAudit rows (audit.js:83-131) ---

function twoRows() {
  return [
    { id: "a1", actor: "admin@example.com", action: "release.publish",
      targetType: "environment", targetId: "e1", targetLabel: "production",
      status: 200, detail: { env: "production", version: 3 },
      created: "2026-11-10T12:00:00Z" },
    { id: "a2", actor: "<script>alert(1)</script>", action: "key.revoke",
      targetType: "sdk_key", targetId: "k9", targetLabel: "",
      status: 500, detail: null, created: "2026-11-10T12:01:00Z" },
  ];
}

test("loadAudit renders 2 rows: escaped content, status chips, detail, page info", async function () {
  var ctx = freshAudit({ fetchImpl: auditImpl(twoRows(), 1, 50, 2) });
  await ctx.CW.loadAudit(1);
  var html = String(ctx.CW.$("audit-list").innerHTML);
  assert.ok(html.indexOf("release.publish") !== -1, "got " + html);
  assert.ok(html.indexOf("environment production") !== -1, "got " + html);
  assert.ok(html.indexOf("sdk_key k9") !== -1, "targetId fallback, got " + html);
  assert.ok(html.indexOf("&lt;script&gt;") !== -1, "actor escaped, got " + html);
  assert.ok(html.indexOf("<script>") === -1, "no raw markup, got " + html);
  assert.ok(html.indexOf("log-status is-ok") !== -1, "got " + html);
  assert.ok(html.indexOf("log-status is-err") !== -1, "got " + html);
  assert.ok(html.indexOf("&quot;env&quot;") !== -1, "detail JSON escaped, got " + html);
  assert.ok(html.indexOf('<span class="muted">-</span>') !== -1, "null detail dash, got " + html);
  assert.ok(html.indexOf('title="a1">1</td>') !== -1, "row 1 numbered, got " + html);
  assert.ok(html.indexOf('title="a2">2</td>') !== -1, "row 2 numbered, got " + html);
  assert.equal(ctx.CW.$("audit-page-info").textContent, "of 1 page · 2 entries");
  assert.equal(ctx.CW.$("audit-result").textContent, "");
});

test("loadAudit truncates long detail JSON, full text in title", async function () {
  var long = "";
  for (var i = 0; i < 300; i++) long += "x";
  var items = [{ id: "a9", actor: "system", action: "purge.run",
    targetType: "maintenance", targetId: "", targetLabel: "",
    status: 200, detail: { note: long }, created: "2026-11-10T12:00:00Z" }];
  var ctx = freshAudit({ fetchImpl: auditImpl(items, 1, 50, 1) });
  await ctx.CW.loadAudit(1);
  var html = String(ctx.CW.$("audit-list").innerHTML);
  var full = JSON.stringify({ note: long });
  assert.ok(full.length > 240, "fixture exceeds the 240-char cut, got " + full.length);
  assert.ok(html.indexOf("title=") !== -1, "full detail kept in title, got " + html);
  assert.ok(html.indexOf(ctx.CW.esc(full.slice(0, 240))) !== -1, "short slice rendered");
  assert.ok(html.indexOf(full) === -1, "full text not inlined, got " + html.length);
});

test("loadAudit renders the no-entries row when items is empty", async function () {
  var ctx = freshAudit({ fetchImpl: auditImpl([], 1, 50, 0) });
  await ctx.CW.loadAudit(1);
  assert.ok(String(ctx.CW.$("audit-list").innerHTML).indexOf("No audit entries.") !== -1);
  assert.equal(ctx.CW.$("audit-page-info").textContent, "of 1 page · 0 entries");
});

// --- error path (audit.js:145-149) ---

test("loadAudit failure sets #audit-result and keeps prior rows", async function () {
  var fail = false;
  var ctx = freshAudit({ fetchImpl: function (url) {
    if (String(url).indexOf("/api/v1/admin/audit") === 0) {
      if (fail) return Promise.reject(new Error("boom"));
      return Promise.resolve(okRes(auditPage(twoRows(), 1, 50, 2)));
    }
    return Promise.reject(new Error("harness fetch denied: " + url));
  } });
  await ctx.CW.loadAudit(1);
  var before = String(ctx.CW.$("audit-list").innerHTML);
  assert.ok(before.indexOf("release.publish") !== -1);
  fail = true;
  await assert.rejects(ctx.CW.loadAudit(1), /boom/);
  assert.ok(ctx.CW.$("audit-result").textContent.indexOf("boom") !== -1,
    "got " + JSON.stringify(ctx.CW.$("audit-result").textContent));
  assert.equal(String(ctx.CW.$("audit-list").innerHTML), before);
});

// --- pagination (audit.js:63-67,111-130) ---

test("120 total / 50 per page -> 3 pages, prev disabled on page 1", async function () {
  var ctx = freshAudit({ fetchImpl: pagedAuditImpl() });
  await ctx.CW.loadAudit(1);
  assert.equal(ctx.CW.$("audit-page-info").textContent, "of 3 pages · 120 entries");
  assert.equal(ctx.CW.$("audit-prev").disabled, true);
  assert.equal(ctx.CW.$("audit-next").disabled, false);
  assert.equal(ctx.CW.$("audit-page").disabled, false);
});

test("last page disables next, keeps prev enabled", async function () {
  var ctx = freshAudit({ fetchImpl: pagedAuditImpl() });
  await ctx.CW.loadAudit(3);
  assert.equal(ctx.CW.$("audit-page-info").textContent, "of 3 pages · 120 entries");
  assert.equal(ctx.CW.$("audit-prev").disabled, false);
  assert.equal(ctx.CW.$("audit-next").disabled, true);
});

test("pageAudit steps pages and clamps at page 1", async function () {
  var ctx = freshAudit({ fetchImpl: pagedAuditImpl() });
  await ctx.CW.loadAudit(1);
  await ctx.CW.pageAudit(1);
  var urls = auditUrls(ctx);
  assert.equal(urls.length, 2);
  assert.ok(urls[1].indexOf("page=2") !== -1, "got " + urls[1]);
  await ctx.CW.pageAudit(-5);
  urls = auditUrls(ctx);
  assert.equal(urls.length, 3);
  assert.ok(urls[2].indexOf("page=1") !== -1, "clamped, got " + urls[2]);
});

// --- filter submit (audit.js:157-160) ---

test("filterAudit reads selects + inputs and reloads page 1", async function () {
  var ctx = freshAudit({ fetchImpl: pagedAuditImpl() });
  await ctx.CW.loadAudit(3);
  assert.equal(auditUrls(ctx).length, 1);
  ctx.CW.$("audit-action").value = "release.publish";
  ctx.CW.$("audit-target-type").value = "environment";
  ctx.CW.$("audit-status").value = "200";
  ctx.CW.$("audit-since").value = "7d";
  await ctx.CW.filterAudit();
  var urls = auditUrls(ctx);
  assert.equal(urls.length, 2);
  assert.ok(urls[1].indexOf("page=1") !== -1, "got " + urls[1]);
  assert.ok(urls[1].indexOf("action=release.publish") !== -1, "got " + urls[1]);
  assert.ok(urls[1].indexOf("targetType=environment") !== -1, "got " + urls[1]);
  assert.ok(urls[1].indexOf("status=200") !== -1, "got " + urls[1]);
  assert.ok(urls[1].indexOf("since=7d") !== -1, "got " + urls[1]);
});

// --- router audit view (router.js parseHash/showView/showAudit/route) ---

function showAuditCtx(files) {
  return h.load(files, {
    fetchImpl: function (url) {
      var u = String(url);
      if (u.indexOf("/api/v1/admin/audit") === 0) {
        return Promise.resolve(okRes(auditPage([], 1, 50, 0)));
      }
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
  });
}

test("router parseHash maps #/p/audit (and suffix forms) to the audit view", function () {
  var ctx = showAuditCtx(["core.js", "logs.js", "audit.js", "router.js"]);
  ctx.context.location.hash = "#/p/audit";
  assert.equal(ctx.CW.parseHash().view, "audit");
  ctx.context.location.hash = "#/p/audit#audit";
  assert.equal(ctx.CW.parseHash().view, "audit");
  ctx.context.location.hash = "#/p/audit/audit";
  assert.equal(ctx.CW.parseHash().view, "audit");
});

test("router showView(audit) unhides #view-audit and hides #view-logs", function () {
  var ctx = showAuditCtx(["core.js", "logs.js", "audit.js", "router.js"]);
  ctx.CW.showView("audit");
  assert.equal(ctx.CW.$("view-audit").hidden, false);
  assert.equal(ctx.CW.$("view-logs").hidden, true);
  ctx.CW.showView("logs");
  assert.equal(ctx.CW.$("view-audit").hidden, true);
  assert.equal(ctx.CW.$("view-logs").hidden, false);
});

test("router showAudit calls CW.loadAudit(1)", function () {
  var ctx = showAuditCtx(["core.js", "logs.js", "audit.js", "router.js"]);
  ctx.CW.showAudit();
  return flushed().then(function () {
    var urls = auditUrls(ctx);
    assert.equal(urls.length, 1, "got " + JSON.stringify(urls));
    assert.ok(urls[0].indexOf("page=1") !== -1, "got " + urls[0]);
  });
});

test("router showAudit stays guarded when audit.js is absent", function () {
  var ctx = showAuditCtx(["core.js", "logs.js", "router.js"]);
  assert.equal(typeof ctx.CW.loadAudit, "undefined");
  ctx.CW.showAudit();
  return flushed().then(function () {
    assert.equal(auditUrls(ctx).length, 0);
  });
});

test("router showLogs no longer loads the audit trail", function () {
  var ctx = showAuditCtx(["core.js", "logs.js", "audit.js", "router.js"]);
  ctx.CW.showLogs();
  return flushed().then(function () {
    assert.equal(auditUrls(ctx).length, 0);
  });
});

// --- index.html markup contract: dedicated view, sidebar link, selects ---

function indexHtml() {
  return fs.readFileSync(path.join(__dirname, "..", "pb_public", "index.html"), "utf8");
}

function selectBlock(html, id) {
  var open = '<select id="' + id + '"';
  var start = html.indexOf(open);
  assert.ok(start !== -1, "expected <select id=\"" + id + "\"> in index.html");
  var end = html.indexOf("</select>", start);
  assert.ok(end !== -1, "expected closing </select> for #" + id);
  return html.slice(start, end);
}

test("index.html hosts the Audit card in #view-audit with a sidebar link", function () {
  var html = indexHtml();
  assert.ok(html.indexOf('<section id="view-audit" class="page" aria-label="Audit" hidden>') !== -1);
  assert.ok(html.indexOf('id="audit-nav-link"') !== -1);
  assert.ok(html.indexOf('href="#/p/audit"') !== -1);
  var logsStart = html.indexOf('id="view-logs"');
  var auditStart = html.indexOf('id="view-audit"');
  assert.ok(logsStart !== -1 && auditStart !== -1 && logsStart < auditStart);
  assert.ok(html.slice(logsStart, auditStart).indexOf("audit-filter-form") === -1,
    "audit form must not live inside #view-logs");
  assert.ok(html.slice(auditStart).indexOf("audit-filter-form") !== -1);
});

test("index.html audit selects carry the fixed-set options", function () {
  var html = indexHtml();
  var actions = ["setup.claim", "session.login",
    "session.login_failed", "session.logout", "account.create",
    "account.password", "account.delete", "release.publish",
    "release.rollback", "release.promote", "key.mint",
    "key.revoke", "key.unrevoke", "key.update", "key.delete",
    "limits.update", "retention.update", "log_settings.update",
    "purge.run", "sso.redeem"];
  var targetTypes = ["account", "environment", "sdk_key", "flag", "rule",
    "experiment", "project", "backup", "settings", "maintenance"];
  var statuses = ["200", "400", "401", "403", "404", "409", "429", "500"];
  var actionSel = selectBlock(html, "audit-action");
  assert.ok(actionSel.indexOf('<option value="">') !== -1, "action select keeps an Any option");
  actions.forEach(function (a) {
    assert.ok(actionSel.indexOf('<option value="' + a + '">' + a + "</option>") !== -1,
      "expected action option " + a);
  });
  var typeSel = selectBlock(html, "audit-target-type");
  assert.ok(typeSel.indexOf('<option value="">') !== -1, "target-type select keeps an Any option");
  targetTypes.forEach(function (t) {
    assert.ok(typeSel.indexOf('<option value="' + t + '">' + t + "</option>") !== -1,
      "expected targetType option " + t);
  });
  var statusSel = selectBlock(html, "audit-status");
  assert.ok(statusSel.indexOf('<option value="">') !== -1, "status select keeps an Any option");
  statuses.forEach(function (s) {
    assert.ok(statusSel.indexOf('<option value="' + s + '">' + s + "</option>") !== -1,
      "expected status option " + s);
  });
});

// --- boot wiring (boot.js:739-744) ---

function listenersFor(ctx, id, type) {
  var el = ctx.elementsById[id];
  assert.ok(el, "expected element #" + id);
  return el.listeners.filter(function (l) { return l.type === type; }).map(function (l) { return l.fn; });
}

test("boot wires audit submit/prev/next; submit reloads page 1, paging steps", async function () {
  // json-editor.js: boot.js:382 calls CW.updateAllJsonHints() unguarded
  // during the DOMContentLoaded wiring pass.
  var ctx = h.load(["core.js", "audit.js", "json-editor.js", "boot.js"], { fetchImpl: pagedAuditImpl() });
  ctx.dispatch("DOMContentLoaded");
  var submits = listenersFor(ctx, "audit-filter-form", "submit");
  var prevs = listenersFor(ctx, "audit-prev", "click");
  var nexts = listenersFor(ctx, "audit-next", "click");
  assert.equal(submits.length, 1);
  assert.equal(prevs.length, 1);
  assert.equal(nexts.length, 1);

  ctx.CW.$("audit-actor").value = "admin@example.com";
  await submits[0]({ preventDefault: function () {} });
  var urls = auditUrls(ctx);
  assert.equal(urls.length, 1);
  assert.ok(urls[0].indexOf("page=1") !== -1, "got " + urls[0]);
  assert.ok(urls[0].indexOf("actor=admin%40example.com") !== -1, "got " + urls[0]);

  nexts[0]();
  await flushed();
  urls = auditUrls(ctx);
  assert.equal(urls.length, 2);
  assert.ok(urls[1].indexOf("page=2") !== -1, "got " + urls[1]);

  prevs[0]();
  await flushed();
  urls = auditUrls(ctx);
  assert.equal(urls.length, 3);
  assert.ok(urls[2].indexOf("page=1") !== -1, "got " + urls[2]);
});

// --- no-third-party contract ---

test("audit.js has no third-party references and fetches same-origin only", async function () {
  var src = fs.readFileSync(path.join(__dirname, "..", "pb_public", "js", "audit.js"), "utf8");
  assert.ok(src.indexOf("api.github.com") === -1, "audit.js must not reference api.github.com");
  var ctx = freshAudit({ fetchImpl: auditImpl(twoRows(), 1, 50, 2) });
  await ctx.CW.loadAudit(1);
  await ctx.CW.pageAudit(1);
  var urls = ctx.fetchCalls.map(function (c) { return String(c.url); });
  assert.ok(urls.length > 0, "expected audit fetches");
  assert.ok(urls.every(function (u) { return u.charAt(0) === "/"; }), "same-origin only, got " + JSON.stringify(urls));
  assert.ok(urls.every(function (u) { return u.indexOf("api.github.com") === -1; }), "got " + JSON.stringify(urls));
});
