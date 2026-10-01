/* ConfigWire js_tests — DOM/fetch-backed ops for router/scope/flags/rules/
 * experiments/keys/releases (Wave 3, todo 10).
 * Loads core+router+scope+drafts+flags+rules+experiments+keys+releases+stats
 * IN ORDER via harness (never boot.js/update.js — todo 11 owns full-order).
 * Patterns reused (cited): seeded drafts via real CW.drafts.draftStage from
 * test-drafts.js; render-stub style from test-flags-pure.js; the recording
 * CW.apiMut double from test-releases-pure.js.
 * dialog.js + json-editor.js are NOT loaded: CW.promptDialog/CW.confirmDialog
 * (dialog.js:10-286) and CW.parseJSONInput/CW.updateJsonHint/CW.setJsonHint
 * (json-editor.js:7-145) are stubbed below. ALL network flows through the
 * harness fetchImpl with canned payloads — no real sockets/ports/timers.
 * DOM assertions are stub-observable only (innerHTML contains, textContent
 * equals, recorded calls/listeners). node:test + node:assert/strict only.
 * Run from configwire/: node --test js_tests/test-dom-ops.js
 */
"use strict";

var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

var FILES = ["core.js", "router.js", "scope.js", "drafts.js", "flags.js",
  "rules.js", "experiments.js", "keys.js", "releases.js", "stats.js"];

// Canned collections served through fetchImpl. Tests mutate DB per case.
var DB = {
  projects: [], environments: [], flags: [], groups: [],
  rules: [], experiments: [], sdk_keys: [], releases: [],
};
var failAll = false; // when true fetchImpl rejects (failure-fallback paths)
var bypassFilter = false; // when true the server-side filter is ignored,
// proving the client-side strict filters (flags.js:221, scope.js:66)

function jsonRes(status, data) {
  return {
    status: status,
    ok: status >= 200 && status < 300,
    json: function () { return Promise.resolve(data); },
  };
}

function paged(items) {
  return jsonRes(200, { items: items, perPage: 200, totalPages: 1 });
}

// Extract (project="<id>") from an encoded filter query, if present.
function projectFilter(url) {
  try {
    var m = /\(project="([^"]+)"\)/.exec(decodeURIComponent(url));
    return m ? m[1] : null;
  } catch (e) { return null; }
}

var h = harness.load(FILES, {
  fetchImpl: async function (url, opts) {
    if (failAll) throw new Error("boom (forced failure): " + url);
    var method = (opts && opts.method) || "GET";
    var mc = /\/api\/collections\/([^/?#]+)\/records/.exec(url);
    if (mc && DB[mc[1]] !== undefined && method === "GET") {
      var items = DB[mc[1]].slice();
      var pid = projectFilter(url);
      if (pid && !bypassFilter) {
        items = items.filter(function (it) { return it && it.project === pid; });
      }
      return paged(items);
    }
    if (method === "POST" && url.indexOf("/api/collections/projects/records") === 0) {
      var bp = JSON.parse(opts.body || "{}");
      return jsonRes(201, { id: "p-new", name: bp.name || "p-new" });
    }
    if (method === "POST" && url.indexOf("/api/collections/environments/records") === 0) {
      var be = JSON.parse(opts.body || "{}");
      return jsonRes(201, { id: "e-new", slug: be.slug || "e-new", project: be.project });
    }
    if (method === "POST" && (url.indexOf("/api/collections/sdk_keys/records") === 0 ||
        url.indexOf("/api/collections/groups/records") === 0 ||
        url.indexOf("/api/collections/flags/records") === 0)) {
      return jsonRes(201, { id: "k-new" });
    }
    if (url.indexOf("/publish") >= 0) return jsonRes(200, { version: 1 });
    if (url.indexOf("/rollback") >= 0) return jsonRes(200, { version: 2 });
    if (url.indexOf("/api/v1/admin/env/") >= 0 && url.indexOf("/stats") >= 0) {
      return jsonRes(200, {
        version: 1, fetches: 1, exposures: 0, flagFound: true,
        perVariant: {}, perVersion: {}, echo: {},
      });
    }
    return jsonRes(200, {});
  },
});

// Stubs for the two modules deliberately NOT loaded here.
h.CW.parseJSONInput = function (raw) { // minimal json-editor.js:7-145 shape
  if (raw == null || String(raw).trim() === "") return { ok: true, value: null };
  try { return { ok: true, value: JSON.parse(raw) }; }
  catch (e) { return { ok: false, error: "invalid JSON: " + e.message }; }
};
h.CW.updateJsonHint = function () { return true; };
h.CW.setJsonHint = function (el, v, ok, msg) {
  if (el) el.textContent = msg || "";
  return !!ok;
};
var promptQueue = []; // queued CW.promptDialog resolutions (dialog.js stub)
var confirmQueue = []; // queued CW.confirmDialog resolutions (dialog.js stub)
h.CW.promptDialog = function () {
  return Promise.resolve(promptQueue.length ? promptQueue.shift() : null);
};
h.CW.confirmDialog = function () {
  return Promise.resolve(confirmQueue.length ? confirmQueue.shift() : false);
};

// Fan-out loaders stubbed only inside the refreshAll test (scope.js:140-147).
var ORIG = {
  apiMut: h.CW.apiMut,
  loadReleases: h.CW.loadReleases,
  loadKeys: h.CW.loadKeys,
  loadStats: h.CW.loadStats,
  loadFlags: h.CW.loadFlags,
  loadRules: h.CW.loadRules,
  loadExperiments: h.CW.loadExperiments,
  loadHomeStats: h.CW.loadHomeStats,
};

function plain(o) { return JSON.parse(JSON.stringify(o)); } // cross-realm compare
function tick(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
function el(id) { return h.CW.$(id); }
function setVal(id, v) { el(id).value = v; }

function reset() {
  var s = h.CW.state;
  s.token = "tok";
  s.projects = []; s.envs = []; s.projectId = null; s.envId = null; s.envSlug = "dev";
  s.flags = []; s.groups = {}; s.releases = [];
  s.rules = []; s.rulesLoaded = false;
  s.experiments = []; s.experimentsLoaded = false;
  s.keys = []; s.lastStats = null; s.lastStatsText = ""; s.lastStatsError = "";
  s.homeStats = {}; s.view = "home"; s.unpublishedChanges = false;
  s.activeFlagRulesId = null; s.activeFlagExperimentsId = null;
  s.collapsedGroups = {}; s.dirtyForms = {};
  h.CW.drafts.draftClearAll();
  h.localStorage.clear();
  h.fetchCalls.length = 0;
  failAll = false; bypassFilter = false;
  Object.keys(DB).forEach(function (k) { DB[k] = []; });
  promptQueue.length = 0; confirmQueue.length = 0;
  h.context.location.hash = "";
  h.CW.apiMut = ORIG.apiMut;
  h.CW.loadReleases = ORIG.loadReleases;
  h.CW.loadKeys = ORIG.loadKeys;
  h.CW.loadStats = ORIG.loadStats;
  h.CW.loadFlags = ORIG.loadFlags;
  h.CW.loadRules = ORIG.loadRules;
  h.CW.loadExperiments = ORIG.loadExperiments;
  h.CW.loadHomeStats = ORIG.loadHomeStats;
  // Builder rows accumulate in the stub (innerHTML="" never clears stub
  // children), so drop them plus volatile dialog/result fields explicitly.
  var wrap = el("exp-variants-builder-rows");
  wrap.children = []; wrap.innerHTML = "";
  ["project-name", "project-result", "env-slug", "env-result", "flag-id",
    "flag-key", "flag-description", "flag-default", "flag-result",
    "flag-rules-condition", "flag-rules-result", "flag-rules-cond-value",
    "flag-rules-cond-lo", "flag-rules-cond-hi", "flag-rules-seed",
    "flag-rules-custom", "exp-name", "exp-seed", "exp-variants",
    "experiment-result", "exp-variants-hint", "key-result", "key-once-value",
    "publish-hint", "flag-folders", "project-grid",
  ].forEach(function (id) {
    var e = el(id);
    if ("value" in e) e.value = "";
    e.textContent = ""; e.innerHTML = "";
  });
}

// Recording CW.apiMut double (pattern from test-releases-pure.js): POST
// /groups -> live-g1, POST /flags -> live-f1, live-f2 in call order.
function stubApiMut(calls) {
  var flagSeq = 0;
  h.CW.apiMut = function (method, path, body) {
    calls.push({ method: method, path: path, body: body });
    if (method === "POST" && path === "/api/collections/groups/records") {
      return Promise.resolve({ status: 201, data: { id: "live-g1" } });
    }
    if (method === "POST" && path === "/api/collections/flags/records") {
      flagSeq++;
      return Promise.resolve({ status: 201, data: { id: "live-f" + flagSeq } });
    }
    return Promise.resolve({ status: 200, data: {} });
  };
}

// ---- router.js: syncSidebar/showView (router.js:38-84) ----

function sidebar() {
  var nav = el("sidebar-nav");
  var links = [];
  for (var i = 0; i < 6; i++) {
    var a = h.document.createElement("a");
    a.attrs = {};
    a.setAttribute = function (k, v) { this.attrs[k] = v; };
    a.removeAttribute = function (k) { delete this.attrs[k]; };
    links.push(a);
  }
  nav.querySelectorAll = function () { return links; };
  return { nav: nav, links: links };
}

test("syncSidebar: home hides nav, detail shows project hrefs", function () {
  reset();
  var sb = sidebar();
  h.CW.state.view = "home"; h.CW.state.projectId = null;
  h.CW.syncSidebar();
  assert.equal(sb.nav.style.display, "none");
  h.CW.state.view = "detail"; h.CW.state.projectId = "p1";
  h.CW.syncSidebar();
  assert.equal(sb.nav.style.display, "");
  assert.equal(sb.links[0].attrs.href, "#/p/p1#flags");
  assert.equal(sb.links[5].attrs.href, "#/p/account");
});

test("showView: toggles hidden across home/detail/account", function () {
  reset();
  sidebar();
  h.CW.showView("detail");
  assert.equal(h.CW.state.view, "detail");
  assert.equal(el("view-home").hidden, true);
  assert.equal(el("view-detail").hidden, false);
  assert.equal(el("view-account").hidden, true);
  h.CW.showView("home");
  assert.equal(el("view-home").hidden, false);
  assert.equal(el("view-detail").hidden, true);
});

test("renderProjectCards: empty grid + populated stats strings", function () {
  reset();
  h.CW.state.projects = [];
  h.CW.renderProjectCards();
  assert.equal(el("project-grid").innerHTML, "");
  assert.equal(el("project-empty").hidden, false);
  h.CW.state.projects = [{ id: "p1", name: "Alpha" }];
  h.CW.state.homeStats = { p1: { flags: 2, envs: 1, keys: 3 } };
  h.CW.renderProjectCards();
  var html = el("project-grid").innerHTML;
  assert.ok(html.indexOf("Alpha") >= 0);
  assert.ok(html.indexOf("2 flags") >= 0);
  assert.ok(html.indexOf("1 envs") >= 0);
  assert.equal(el("project-empty").hidden, true);
});

test("renderDetailHeader: names project, falls back to Project", function () {
  reset();
  sidebar();
  h.CW.state.projects = [{ id: "p1", name: "Alpha" }];
  h.CW.state.projectId = "p1";
  h.CW.renderDetailHeader();
  assert.equal(el("detail-project-name").textContent, "Alpha");
  h.CW.state.projectId = "ghost";
  h.CW.renderDetailHeader();
  assert.equal(el("detail-project-name").textContent, "Project");
});

test("loadHomeStats: aggregates envs/flags/keys, drops revoked+unknown", async function () {
  reset();
  h.CW.state.projects = [{ id: "p1", name: "A" }, { id: "p2", name: "B" }];
  DB.environments = [
    { id: "e1", project: "p1", slug: "dev" },
    { id: "e2", project: "p1", slug: "prod" },
    { id: "e3", project: "p2", slug: "dev" },
    { id: "ex", project: "ghost", slug: "dev" },
  ];
  DB.flags = [
    { id: "f1", project: "p1", key: "a" },
    { id: "f2", project: "p1", key: "b" },
    { id: "f3", project: "p2", key: "c" },
  ];
  DB.sdk_keys = [
    { id: "k1", env: "e1" },
    { id: "k2", env: "e1", revoked: true },
    { id: "k3", env: "e3" },
    { id: "k9", env: "nope" },
  ];
  await h.CW.loadHomeStats();
  assert.deepEqual(plain(h.CW.state.homeStats), {
    p1: { flags: 2, envs: 2, keys: 1 },
    p2: { flags: 1, envs: 1, keys: 1 },
  });
  assert.ok(el("project-grid").innerHTML.indexOf("A") >= 0);
});

test("loadHomeStats: failure still renders cards", async function () {
  reset();
  h.CW.state.projects = [{ id: "p1", name: "A" }];
  failAll = true;
  await h.CW.loadHomeStats(); // failure branch (router.js:145) resolves
  assert.deepEqual(plain(h.CW.state.homeStats), {});
  assert.ok(el("project-grid").innerHTML.indexOf("A") >= 0);
});

test("route: unknown project id falls back home without throw", async function () {
  reset();
  h.CW.state.projects = [{ id: "a", name: "A" }];
  h.context.location.hash = "#/p/zzz";
  h.CW.route(); // router.js:249-251 stale-id fallback
  assert.equal(h.CW.state.view, "home");
  await tick(20);
  assert.equal(h.CW.state.view, "home");
});

test("route: known project opens detail scope", async function () {
  reset();
  sidebar();
  h.CW.state.projects = [{ id: "a", name: "A" }];
  DB.environments = [{ id: "e1", project: "a", slug: "dev" }];
  h.context.location.hash = "#/p/a";
  h.CW.route();
  assert.equal(h.CW.state.projectId, "a");
  assert.equal(h.CW.state.view, "detail");
  await tick(30); // loadDetailScope fan-out settles on canned payloads
  assert.equal(h.CW.state.envId, "e1");
  assert.equal(el("detail-project-name").textContent, "A");
});

test("route: no token returns early", function () {
  reset();
  h.CW.state.token = null;
  h.CW.state.projects = [{ id: "a", name: "A" }];
  h.context.location.hash = "#/p/a";
  h.CW.route();
  assert.equal(h.CW.state.view, "home");
  assert.equal(h.fetchCalls.length, 0);
});

// ---- scope.js: renderProjectEnv/createProject/createEnv/refreshAll ----

test("renderProjectEnv: project/env options + envSlug", function () {
  reset();
  h.CW.state.projects = [{ id: "p1", name: "One" }];
  h.CW.state.envs = [{ id: "e1", slug: "dev" }, { id: "e2", slug: "prod" }];
  h.CW.renderProjectEnv();
  assert.ok(el("project-select").innerHTML.indexOf("One") >= 0);
  assert.ok(el("env-select").innerHTML.indexOf("dev") >= 0);
  assert.equal(h.CW.state.envSlug, "dev");
});

test("createProject: blank name guards with zero fetch", async function () {
  reset();
  setVal("project-name", "   ");
  await h.CW.createProject();
  assert.equal(el("project-result").textContent, "project name is required");
  assert.equal(h.fetchCalls.length, 0);
});

test("createProject: success reports, clears, navigates hash", async function () {
  reset();
  DB.projects = [{ id: "p0", name: "Old" }];
  setVal("project-name", "demo");
  var out = await h.CW.createProject();
  assert.equal(out.status, 201);
  assert.equal(el("project-result").textContent, "project created: demo");
  assert.equal(el("project-name").value, "");
  await tick(30); // inner loadProjects/loadHomeStats/loadEnvs chain (scope.js:92-101)
  assert.equal(h.context.location.hash, "#/p/p-new");
  assert.ok(h.fetchCalls.some(function (c) {
    return c.url.indexOf("/api/collections/projects/records") === 0 && c.opts.method === "POST";
  }));
});

test("createEnv: guards + success", async function () {
  reset();
  await h.CW.createEnv();
  assert.equal(el("env-result").textContent, "pick a project first");
  h.CW.state.projectId = "p1";
  setVal("env-slug", "  ");
  await h.CW.createEnv();
  assert.equal(el("env-result").textContent, "env slug is required");
  setVal("env-slug", "dev");
  var out = await h.CW.createEnv();
  assert.equal(out.status, 201);
  assert.equal(el("env-result").textContent, "env created: dev");
});

test("refreshAll: loads projects, routes, fans out to stubbed loaders", async function () {
  reset();
  sidebar();
  DB.projects = [{ id: "p1", name: "One" }];
  var seen = [];
  ["loadReleases", "loadKeys", "loadStats", "loadFlags", "loadRules", "loadExperiments"].forEach(function (n) {
    h.CW[n] = function () { seen.push(n); return Promise.resolve(); };
  });
  h.CW.refreshAll(); // deeper loaders stubbed per plan; loadProjects/route stay real
  await tick(40);
  assert.equal(h.CW.state.projectId, "p1");
  assert.equal(h.CW.state.view, "home");
  assert.ok(h.fetchCalls.some(function (c) { return c.url.indexOf("/api/collections/projects/records") >= 0; }));
  ["loadReleases", "loadKeys", "loadStats", "loadFlags", "loadRules", "loadExperiments"].forEach(function (n) {
    assert.ok(seen.indexOf(n) >= 0, n + " fanned out");
  });
});

// ---- flags.js: loadFlags strict filter + draft-backed ops (flags.js:183-450) ----

test("loadFlags: strict project filter, no cross-project leak", async function () {
  reset();
  bypassFilter = true; // server returns everything; client must filter (flags.js:221,230)
  DB.flags = [
    { id: "f1", project: "p1", key: "a" },
    { id: "f2", project: "p2", key: "b" },
    { id: "f3", key: "legacy" },
  ];
  DB.groups = [
    { id: "g1", project: "p1", name: "G" },
    { id: "g2", project: "p2", name: "H" },
  ];
  h.CW.state.projectId = "p1";
  await h.CW.loadFlags();
  assert.deepEqual(plain(h.CW.state.flags).map(function (f) { return f.id; }), ["f1"]);
  assert.deepEqual(plain(h.CW.state.groups), { g1: "G" });
  assert.ok(h.fetchCalls[0].url.indexOf("filter=") >= 0);
});

test("saveFlag: create + update stage local-only drafts resolving {status:200}", async function () {
  reset();
  h.CW.state.projectId = "p1";
  setVal("flag-id", ""); setVal("flag-key", "k1"); setVal("flag-description", "");
  setVal("flag-type", "bool"); setVal("flag-default", "false"); setVal("flag-group", "");
  var out = await h.CW.saveFlag();
  assert.deepEqual(plain(out), { status: 200, data: {} });
  assert.equal(h.fetchCalls.length, 0);
  var keys = Object.keys(h.CW.state.drafts.flag);
  assert.equal(keys.length, 1);
  assert.equal(h.CW.state.drafts.flag[keys[0]].body.key, "k1");
  // Update path preserves the group-omission quirk (flags.js:261-263).
  h.CW.state.flags = [{ id: "f1", key: "k1", type: "bool", defaultValue: false, project: "p1" }];
  setVal("flag-id", "f1");
  out = await h.CW.saveFlag();
  assert.deepEqual(plain(out), { status: 200, data: {} });
  assert.equal(h.CW.state.drafts.flag.f1.op, "update");
  assert.equal(h.CW.state.drafts.flag.f1.body.group, null);
});

test("deleteFlag/moveFlag: local-only drafts resolving {status:200}", async function () {
  reset();
  h.CW.state.flags = [{ id: "f1", key: "k1", type: "bool", defaultValue: false }];
  assert.deepEqual(plain(await h.CW.deleteFlag("f1")), { status: 200, data: {} });
  assert.deepEqual(plain(await h.CW.moveFlag("f1", "g1")), { status: 200, data: {} });
  assert.equal(h.CW.state.drafts.flag.f1.body.group, "g1"); // moveFlag (flags.js:169-181)
  assert.equal(h.fetchCalls.length, 0);
});

test("promptCreateGroup/renameGroup: staged creates/updates", async function () {
  reset();
  h.CW.state.projectId = "p1";
  promptQueue.push("G1");
  assert.deepEqual(plain(await h.CW.promptCreateGroup()), { status: 200, data: {} });
  var gkeys = Object.keys(h.CW.state.drafts.group);
  assert.equal(gkeys.length, 1);
  assert.equal(h.CW.state.drafts.group[gkeys[0]].body.name, "G1");
  h.CW.state.groups = { g1: "Old" };
  promptQueue.push("New");
  assert.deepEqual(plain(await h.CW.renameGroup("g1")), { status: 200, data: {} });
  assert.equal(h.CW.state.drafts.group.g1.body.name, "New");
  assert.equal(h.fetchCalls.length, 0);
});

test("deleteGroup: confirm stages delete with memberIds; cancel stages nothing", async function () {
  reset();
  h.CW.state.groups = { g1: "G" };
  h.CW.state.flags = [{ id: "f1", key: "a", type: "bool", defaultValue: false, group: "g1" }];
  confirmQueue.push(true);
  assert.deepEqual(plain(await h.CW.deleteGroup("g1")), { status: 200, data: {} });
  assert.deepEqual(plain(h.CW.state.drafts.group.g1.memberIds), ["f1"]); // flags.js:332-335
  h.CW.drafts.draftClearAll();
  confirmQueue.push(false);
  await h.CW.deleteGroup("g1");
  assert.equal(h.CW.drafts.hasDrafts(), false);
  assert.equal(h.fetchCalls.length, 0);
});

// ---- rules.js: loadRules/deleteRule + builder ops (rules.js:90-330) ----

test("loadRules: sorts by priority, marks loaded; deleteRule stages draft", async function () {
  reset();
  DB.rules = [
    { id: "r1", flag: "f", priority: 5, condition: {}, value: 1 },
    { id: "r2", flag: "f", priority: 1, condition: {}, value: 2 },
  ];
  await h.CW.loadRules();
  assert.deepEqual(plain(h.CW.state.rules).map(function (r) { return r.id; }), ["r2", "r1"]);
  assert.equal(h.CW.state.rulesLoaded, true);
  assert.deepEqual(plain(await h.CW.deleteRule("r1")), { status: 200, data: {} });
  assert.equal(h.CW.state.drafts.rule.r1.op, "delete");
});

test("populateRuleOpOptions: fills ops, keeps valid op", function () {
  reset();
  h.CW.populateRuleOpOptions("platform", "==");
  assert.ok(el("flag-rules-op").innerHTML.indexOf("contains") >= 0);
  assert.equal(el("flag-rules-op").value, "==");
});

test("rule builder: sync round-trips condition, apply writes JSON", function () {
  reset();
  setVal("flag-rules-condition", '{"field":"platform","op":"==","value":"ios"}');
  assert.equal(h.CW.syncRuleBuilderFromCondition(), true); // rules.js:165-189
  assert.equal(el("flag-rules-field").value, "platform");
  setVal("flag-rules-cond-value", "android");
  assert.equal(h.CW.applyRuleBuilderToCondition(), true); // rules.js:284-312
  var cond = JSON.parse(el("flag-rules-condition").value);
  assert.equal(cond.field, "platform");
  assert.equal(cond.op, "==");
  assert.equal(cond.value, "android");
});

test("rule builder: visibility toggles + reset defaults", function () {
  reset();
  h.CW.updateRuleBuilderVisibility("percentile", "between"); // rules.js:254-282
  assert.equal(el("flag-rules-cond-value-wrap").hidden, true);
  assert.equal(el("flag-rules-cond-lo-wrap").hidden, false);
  assert.equal(el("flag-rules-seed-wrap").hidden, false);
  h.CW.updateRuleBuilderVisibility("custom.", "==");
  assert.equal(el("flag-rules-custom-wrap").hidden, false);
  h.CW.resetRuleBuilder();
  assert.equal(el("flag-rules-field").value, "platform");
  assert.equal(el("flag-rules-cond-value").value, "ios");
  assert.equal(el("flag-rules-cond-lo").value, "0");
});

// ---- experiments.js: filtered load + draft-backed ops (experiments.js:56-263) ----

test("loadExperiments: filtered by loaded flags", async function () {
  reset();
  h.CW.state.projectId = "p1";
  h.CW.state.flags = [{ id: "f1", key: "a" }];
  DB.experiments = [
    { id: "x1", flag: "f1", name: "E" },
    { id: "x2", flag: "zzz", name: "Stray" },
  ];
  await h.CW.loadExperiments(); // experiments.js:175-193
  assert.deepEqual(plain(h.CW.state.experiments).map(function (x) { return x.id; }), ["x1"]);
  assert.equal(h.CW.state.experimentsLoaded, true);
});

function builderField(v) {
  var e = h.document.createElement("input");
  e.value = v == null ? "" : String(v);
  return e;
}

function fakeVariantRow(name, weight, values) {
  var map = {
    "exp-variant-name": builderField(name),
    "exp-variant-weight": builderField(weight),
    "exp-variant-values": builderField(values),
  };
  return {
    querySelector: function (sel) {
      var k = sel.charAt(0) === "." ? sel.slice(1) : sel;
      return map[k] || null;
    },
  };
}

function twoGoodRows() {
  el("exp-variants-builder-rows").children = [
    fakeVariantRow("control", "50", ""),
    fakeVariantRow("treatment", "50", ""),
  ];
}

function seedExpForm() {
  twoGoodRows();
  setVal("exp-name", "E1"); setVal("exp-seed", "s1");
  setVal("exp-status", "draft"); setVal("exp-flag-select", "f1");
  setVal("exp-variants", ""); setVal("experiment-result", "");
}

test("saveExperiment/createExperiment: stage drafts resolving {status:200}", async function () {
  reset();
  seedExpForm();
  var out = await h.CW.saveExperiment(); // experiments.js:195-239
  assert.deepEqual(plain(out), { status: 200, data: {} });
  var keys = Object.keys(h.CW.state.drafts.experiment);
  assert.equal(keys.length, 1);
  assert.equal(h.CW.state.drafts.experiment[keys[0]].body.name, "E1");
  assert.equal(h.CW.state.drafts.experiment[keys[0]].body.flag, "f1");
  var transport = JSON.parse(el("exp-variants").value);
  assert.equal(transport.reduce(function (a, v) { return a + v.weightBps; }, 0), 10000);
  h.CW.drafts.draftClearAll();
  seedExpForm();
  assert.deepEqual(plain(await h.CW.createExperiment()), { status: 200, data: {} });
  assert.equal(h.fetchCalls.length, 0);
});

test("deleteExperiment/setExperimentStatus: staged drafts", async function () {
  reset();
  h.CW.state.experiments = [{ id: "x1", name: "X", flag: "f1" }];
  assert.deepEqual(plain(await h.CW.deleteExperiment("x1")), { status: 200, data: {} });
  assert.equal(h.CW.state.drafts.experiment.x1.op, "delete");
  assert.deepEqual(plain(await h.CW.setExperimentStatus("x1", "running")), { status: 200, data: {} });
  assert.equal(h.CW.state.drafts.experiment.x1.body.status, "running");
});

test("variants builder: add/balance/recalc/validate/apply/sync", function () {
  reset();
  var wrap = el("exp-variants-builder-rows");
  wrap.children = []; wrap.innerHTML = "";
  var row = h.CW.addVariantRow("a", 25, ""); // experiments.js:381-402
  assert.ok(row);
  assert.equal(wrap.children.length, 1);
  assert.ok(row.innerHTML.indexOf("Variant 1") >= 0);
  wrap.children = [fakeVariantRow("a", "10", ""), fakeVariantRow("b", "90", "")];
  assert.equal(h.CW.balanceVariantsBuilder(), true); // experiments.js:596-608
  assert.equal(wrap.children[0].querySelector(".exp-variant-weight").value, "50");
  assert.equal(wrap.children[1].querySelector(".exp-variant-weight").value, "50");
  wrap.children[0].querySelector(".exp-variant-weight").value = "30";
  assert.equal(h.CW.recalcLastVariantWeight(), true); // experiments.js:425-443
  assert.equal(wrap.children[1].querySelector(".exp-variant-weight").value, "70");
  assert.equal(h.CW.applyVariantsBuilderToVariants(), true); // experiments.js:521-559
  var applied = JSON.parse(el("exp-variants").value);
  assert.equal(applied.length, 2);
  assert.equal(applied[0].weightBps + applied[1].weightBps, 10000);
  wrap.children = [fakeVariantRow("", "50", "")];
  assert.equal(h.CW.applyVariantsBuilderToVariants(), false);
  assert.ok(el("exp-variants-hint").textContent.indexOf("name is required") >= 0);
  wrap.children = []; wrap.innerHTML = "";
  setVal("exp-variants", JSON.stringify([
    { name: "c", weightBps: 6000 }, { name: "t", weightBps: 4000 },
  ]));
  assert.equal(h.CW.syncVariantsBuilderFromInput(), true); // experiments.js:561-582
  assert.equal(wrap.children.length, 2);
});

// ---- keys.js: createKey/revokeKey (keys.js:57-95) ----

test("createKey: no envId guards with zero fetch", async function () {
  reset();
  h.CW.state.envId = null;
  await h.CW.createKey(); // keys.js:59 guard
  assert.equal(el("key-result").textContent, "pick an environment first");
  assert.equal(h.fetchCalls.length, 0);
});

test("createKey: success shows prefix + one-time value", async function () {
  reset();
  h.CW.state.envId = "e1";
  setVal("key-fetch-rps", "1");
  setVal("key-ingest-rps", "1");
  await h.CW.createKey();
  assert.ok(el("key-result").textContent.indexOf("key created") >= 0);
  assert.ok(el("key-once-value").textContent.indexOf("cw-") === 0);
  assert.ok(h.fetchCalls.some(function (c) {
    return c.url.indexOf("/api/collections/sdk_keys/records") === 0 && c.opts.method === "POST";
  }));
});

test("revokeKey: PATCH url shape", async function () {
  reset();
  var out = await h.CW.revokeKey("k1");
  assert.equal(out.status, 200);
  assert.ok(h.fetchCalls.some(function (c) {
    return c.url === "/api/collections/sdk_keys/records/k1" && c.opts.method === "PATCH";
  }));
});

// ---- releases.js: env filter, publish/rollback shapes, applyDrafts ----

test("loadReleases: env filter + desc sort + publish-base fill", async function () {
  reset();
  h.CW.state.envId = "e1";
  DB.releases = [
    { id: "r1", env: "e1", version: 2 },
    { id: "r2", env: "e9", version: 5 },
    { id: "r3", env: "e1", version: 1 },
  ];
  await h.CW.loadReleases(); // releases.js:709-722
  assert.deepEqual(plain(h.CW.state.releases).map(function (r) { return r.version; }), [2, 1]);
  assert.equal(String(el("publish-base").value), "2");
});

test("publish/rollback: URL shapes via fetch stub", async function () {
  reset();
  h.CW.state.envSlug = "dev"; h.CW.state.projectId = "p1";
  await h.CW.publish("note", 3); // releases.js:724-736
  assert.equal(h.fetchCalls[0].url, "/api/v1/admin/env/dev/publish?project=p1");
  assert.equal(JSON.parse(h.fetchCalls[0].opts.body).baseVersion, 3);
  await h.CW.rollback(2, "back"); // releases.js:738-751
  assert.equal(h.fetchCalls[1].url, "/api/v1/admin/env/dev/releases/2/rollback?project=p1");
});

test("applyDrafts: group-create + flag-create consumed in order with remap", async function () {
  reset();
  var gt = h.CW.drafts.draftStage("group", {
    op: "create", body: { name: "G", project: "p1" }, label: "G",
  });
  var ft = h.CW.drafts.draftStage("flag", {
    op: "create",
    body: { key: "f", type: "bool", defaultValue: false, project: "p1", group: gt },
    label: "f",
  });
  assert.ok(ft.indexOf("draft-flag-") === 0);
  var calls = [];
  stubApiMut(calls);
  var out = await h.CW.applyDrafts();
  assert.deepEqual(plain(out), { applied: 2, total: 2 });
  assert.deepEqual(calls.map(function (c) { return c.method + " " + c.path; }), [
    "POST /api/collections/groups/records",
    "POST /api/collections/flags/records",
  ]);
  assert.equal(calls[1].body.group, "live-g1"); // remapGroupRef (releases.js:418-423)
  assert.equal(h.CW.drafts.hasDrafts(), false);
  assert.equal(h.fetchCalls.length, 0); // apiMut double never touches fetch
});

test("applyDrafts: dangling temp rule ref rejects with kept-count error", async function () {
  reset();
  h.CW.drafts.draftStage("rule", {
    op: "create", body: { flag: "draft-flag-9", priority: 0, condition: {}, value: true },
    label: "rule for ghost",
  });
  var calls = [];
  stubApiMut(calls);
  var err = null;
  try { await h.CW.applyDrafts(); } catch (e) { err = e; }
  assert.ok(err instanceof Error);
  assert.equal(err.code, "DANGLING_TEMP"); // releases.js:551-554
  assert.ok(err.message.indexOf("1 draft kept") >= 0); // releases.js:641-643
  assert.equal(err.kept, 1);
  assert.equal(calls.length, 0);
  assert.equal(h.CW.drafts.hasDrafts(), true);
});

test("applyDrafts: group-delete ungroups members first", async function () {
  reset();
  h.CW.state.flags = [{ id: "f1", key: "a", type: "bool", defaultValue: false, group: "g1" }];
  h.CW.drafts.draftStage("group", { op: "delete", body: {}, baseId: "g1", label: "G" });
  var calls = [];
  stubApiMut(calls);
  var out = await h.CW.applyDrafts(); // releases.js:491-508 clear-then-delete
  assert.deepEqual(plain(out), { applied: 1, total: 1 });
  assert.deepEqual(calls.map(function (c) { return c.method + " " + c.path; }), [
    "PATCH /api/collections/flags/records/f1",
    "DELETE /api/collections/groups/records/g1",
  ]);
  assert.deepEqual(plain(calls[0].body), { group: null });
});

test("publish state + dirty forms: mark/set/arm", function () {
  reset();
  h.CW.markUnpublished("flag", "f1"); // releases.js:305-307
  assert.ok(el("publish-hint").textContent.indexOf("Unpublished changes") >= 0);
  h.CW.setPublishState(false); // releases.js:281-303
  assert.equal(el("publish-hint").textContent, "No unpublished changes");
  h.CW.drafts.draftStage("flag", { op: "create", body: { key: "f" }, label: "f" });
  h.CW.markPublished(); // releases.js:337-348 consumes the store
  assert.equal(h.CW.drafts.hasDrafts(), false);
  assert.equal(el("publish-hint").textContent, "No unpublished changes");
  h.CW.armDirtyForm("flag-form"); // releases.js:699-707
  var form = el("flag-form");
  var inputFns = form.listeners.filter(function (l) { return l.type === "input"; });
  assert.equal(inputFns.length, 1);
  inputFns[0].fn();
  assert.equal(h.CW.state.dirtyForms["flag-form"], true);
  h.CW.markFormClean("flag-form");
  assert.equal(h.CW.state.dirtyForms["flag-form"], false);
});

test("no stray network: fetch only where asserted", function () {
  // Every suite above serves network through fetchImpl; pure-DOM suites
  // record nothing. This pins the final count after a DOM-only op.
  reset();
  h.CW.renderProjectCards();
  h.CW.populateRuleOpOptions("platform", "==");
  h.CW.updateRuleBuilderVisibility("platform", "==");
  assert.equal(h.fetchCalls.length, 0);
});
