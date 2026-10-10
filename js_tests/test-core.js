"use strict";
// test-core.js — core.js ONLY (Wave 2, todo 2).
// Covers core.js helpers/auth/fetch/scope per .omo/plans/configwire-js-tests.md.
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-core.js
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function fresh(files, overrides) {
  return h.load(files === undefined ? ["core.js"] : files, overrides);
}

function jsonRes(status, data, ok) {
  return {
    status: status,
    ok: ok === undefined ? status >= 200 && status < 300 : ok,
    json: function () { return Promise.resolve(data); },
  };
}

function jsonFailRes(status) {
  return {
    status: status,
    ok: false,
    json: function () { return Promise.reject(new Error("bad json")); },
  };
}

function jsonEq(actual, expected, msg) {
  assert.equal(JSON.stringify(actual), JSON.stringify(expected), msg);
}

// --- esc (core.js:122-126) ---

test("esc escapes amp/lt/gt/quot", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.esc('<a href="x&y">'), "&lt;a href=&quot;x&amp;y&quot;&gt;");
  assert.equal(ctx.CW.esc("a&b<c>d\"e"), "a&amp;b&lt;c&gt;d&quot;e");
});

test("esc null/undefined coerce to empty string, numbers stringified", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.esc(null), "");
  assert.equal(ctx.CW.esc(undefined), "");
  assert.equal(ctx.CW.esc(42), "42");
  assert.equal(ctx.CW.esc(""), "");
});

test("esc escapes single quotes for attribute safety", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.esc("a'b"), "a&#39;b");
  assert.equal(ctx.CW.esc("'\"<>&"), "&#39;&quot;&lt;&gt;&amp;");
});

// --- scope generation (core.js bumpScopeGen/scopeStale) ---

test("scopeGen: bump invalidates older generations", function () {
  var ctx = fresh();
  var g0 = ctx.CW.state.scopeGen;
  var g1 = ctx.CW.bumpScopeGen();
  assert.ok(g1 !== g0);
  assert.equal(ctx.CW.scopeStale(g0), true);
  assert.equal(ctx.CW.scopeStale(g1), false);
  assert.equal(ctx.CW.scopeStale(ctx.CW.bumpScopeGen()), false);
});

// --- serverMessage (core.js:128-133) ---

test("serverMessage null/undefined to empty string", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.serverMessage(null), "");
  assert.equal(ctx.CW.serverMessage(undefined), "");
});

test("serverMessage string passthrough", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.serverMessage("boom"), "boom");
  assert.equal(ctx.CW.serverMessage(""), "");
});

test("serverMessage message-object wins over JSON", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.serverMessage({ message: "pb says no", code: 400 }), "pb says no");
  assert.equal(ctx.CW.serverMessage({ message: 500 }), "500");
});

test("serverMessage falls back to JSON.stringify", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.serverMessage({ code: 400, data: {} }), JSON.stringify({ code: 400, data: {} }));
});

test("serverMessage circular object never throws", function () {
  var ctx = fresh();
  var o = {};
  o.self = o;
  assert.equal(ctx.CW.serverMessage(o), "request failed");
});

// --- withPage (core.js:150-172) ---

test("withPage strips page+perPage, forces perPage=200, preserves other params", function () {
  // withPage is NOT exported (core.js:238-256), so exercise it through apiAll
  // fetchImpl URL capture (core.js:150-172 + apiAll core.js:192).
  var seen = [];
  var hx = h.load(["core.js"], {
    fetchImpl: function (u) {
      seen.push(u);
      return Promise.resolve(jsonRes(200, { items: [{ id: 1 }], perPage: 200, totalPages: 1 }));
    },
  });
  return hx.CW.apiAll("/api/collections/flags/records?project=p1&page=9&perPage=5").then(function (items) {
    jsonEq(items, [{ id: 1 }]);
    assert.equal(seen.length, 1);
    assert.ok(seen[0].indexOf("project=p1") >= 0, "preserves other params: " + seen[0]);
    assert.ok(seen[0].indexOf("perPage=200") >= 0, "forces perPage=200: " + seen[0]);
    assert.ok(seen[0].indexOf("page=1") >= 0, "sets page=1: " + seen[0]);
    assert.ok(seen[0].indexOf("page=9") < 0, "strips old page: " + seen[0]);
    assert.ok(seen[0].indexOf("perPage=5") < 0, "strips old perPage: " + seen[0]);
  });
});

test("withPage preserves hash and strips encoded page keys", function () {
  var seen = [];
  var hx = h.load(["core.js"], {
    fetchImpl: function (u) {
      seen.push(u);
      return Promise.resolve(jsonRes(200, { items: [], perPage: 200, totalPages: 1 }));
    },
  });
  return hx.CW.apiAll("/api/collections/flags/records?x=1&%70age=3#view").then(function () {
    assert.equal(seen.length, 1);
    assert.ok(seen[0].slice(-5) === "#view", "preserves hash: " + seen[0]);
    assert.ok(seen[0].indexOf("x=1") >= 0, "preserves x=1: " + seen[0]);
    assert.ok(seen[0].indexOf("perPage=200") >= 0, "forces perPage=200: " + seen[0]);
  });
});

// --- authHeaders (core.js:43-46) ---

test("authHeaders returns bare token, empty object when logged out", function () {
  var ctx = fresh();
  jsonEq(ctx.CW.authHeaders(), {});
  ctx.CW.state.token = "tok123";
  jsonEq(ctx.CW.authHeaders(), { Authorization: "tok123" });
  // Bare token: no Bearer prefix (core.js:44).
  assert.ok(ctx.CW.authHeaders().Authorization.indexOf("Bearer") < 0);
  ctx.CW.state.token = null;
  jsonEq(ctx.CW.authHeaders(), {});
});

// --- selectedEnv / envSlug (core.js:94-104) ---

test("selectedEnv hit and miss", function () {
  var ctx = fresh();
  ctx.CW.state.envs = [{ id: "e1", slug: "dev" }, { id: "e2", slug: "prod" }];
  ctx.CW.state.envId = "e2";
  jsonEq(ctx.CW.selectedEnv(), { id: "e2", slug: "prod" });
  ctx.CW.state.envId = "nope";
  assert.equal(ctx.CW.selectedEnv(), null);
  ctx.CW.state.envs = [];
  ctx.CW.state.envId = "e1";
  assert.equal(ctx.CW.selectedEnv(), null);
});

test("envSlug prefers selected env slug, falls back to state default then dev", function () {
  var ctx = fresh();
  ctx.CW.state.envs = [{ id: "e1", slug: "staging" }];
  ctx.CW.state.envId = "e1";
  ctx.CW.state.envSlug = "dev";
  assert.equal(ctx.CW.envSlug(), "staging");
  ctx.CW.state.envId = "missing";
  ctx.CW.state.envSlug = "fallback";
  assert.equal(ctx.CW.envSlug(), "fallback");
  ctx.CW.state.envSlug = "";
  assert.equal(ctx.CW.envSlug(), "dev");
  ctx.CW.state.envSlug = null;
  assert.equal(ctx.CW.envSlug(), "dev");
});

// --- loadPersistedScope / persistScope (core.js:106-120) ---

test("persistScope/loadPersistedScope round-trip", function () {
  var ctx = fresh();
  ctx.CW.state.projectId = "p1";
  ctx.CW.state.envId = "e1";
  ctx.CW.persistScope();
  assert.equal(ctx.store[ctx.CW.LS_PROJECT], "p1");
  assert.equal(ctx.store[ctx.CW.LS_ENV], "e1");
  // Clear in-memory, reload from store.
  ctx.CW.state.projectId = null;
  ctx.CW.state.envId = null;
  ctx.CW.loadPersistedScope();
  assert.equal(ctx.CW.state.projectId, "p1");
  assert.equal(ctx.CW.state.envId, "e1");
  // Null scope removes keys.
  ctx.CW.state.projectId = null;
  ctx.CW.state.envId = null;
  ctx.CW.persistScope();
  assert.ok(!Object.prototype.hasOwnProperty.call(ctx.store, ctx.CW.LS_PROJECT));
  assert.ok(!Object.prototype.hasOwnProperty.call(ctx.store, ctx.CW.LS_ENV));
  ctx.CW.loadPersistedScope();
  assert.equal(ctx.CW.state.projectId, null);
  assert.equal(ctx.CW.state.envId, null);
});

test("scope helpers swallow private-mode localStorage throws", function () {
  var ctx = fresh();
  var throwing = {
    getItem: function () { throw new Error("denied"); },
    setItem: function () { throw new Error("denied"); },
    removeItem: function () { throw new Error("denied"); },
  };
  var orig = ctx.context.localStorage;
  ctx.context.localStorage = throwing;
  try {
    ctx.CW.state.projectId = "p9";
    ctx.CW.state.envId = "e9";
    ctx.CW.persistScope(); // must not throw (core.js:113-120)
    ctx.CW.state.projectId = null;
    ctx.CW.state.envId = null;
    ctx.CW.loadPersistedScope(); // must not throw (core.js:106-111)
  } finally {
    ctx.context.localStorage = orig;
  }
});

// --- api (core.js:135-145) ---

test("api sends auth header and resolves json on 200", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function (url, opts) {
      assert.equal(url, "/api/collections/projects/records");
      jsonEq(opts.headers, { Authorization: "tok" });
      return Promise.resolve(jsonRes(200, { items: [] }));
    },
  });
  ctx.CW.state.token = "tok";
  return ctx.CW.api("/api/collections/projects/records").then(function (data) {
    jsonEq(data, { items: [] });
    assert.equal(ctx.fetchCalls.length, 1);
  });
});

test("api 401 clears token and rejects session expired", function () {
  var ctx = h.load(["core.js"], {
    localStorageSeed: { cw_admin_token: "tok" },
    fetchImpl: function () {
      return Promise.resolve(jsonRes(401, { message: "unauth" }));
    },
  });
  ctx.CW.state.token = "tok";
  return ctx.CW.api("/x").then(
    function () { throw new Error("should reject"); },
    function (err) {
      assert.equal(err.message, "session expired — please log in");
      assert.equal(ctx.CW.state.token, null);
      assert.ok(!Object.prototype.hasOwnProperty.call(ctx.store, "cw_admin_token"));
    }
  );
});

test("api 403 clears token and rejects session expired", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function () {
      return Promise.resolve(jsonRes(403, { message: "forbidden" }));
    },
  });
  ctx.CW.state.token = "tok";
  return ctx.CW.api("/x").then(
    function () { throw new Error("should reject"); },
    function (err) {
      assert.equal(err.message, "session expired — please log in");
      assert.equal(ctx.CW.state.token, null);
    }
  );
});

test("api non-401 error includes server message", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function () {
      return Promise.resolve(jsonRes(500, { message: "kaput" }));
    },
  });
  ctx.CW.state.token = "tok";
  return ctx.CW.api("/boom").then(
    function () { throw new Error("should reject"); },
    function (err) {
      assert.equal(err.message, "request failed (500): kaput");
      // Non-auth failure keeps the session.
      assert.equal(ctx.CW.state.token, "tok");
    }
  );
});

test("api non-json error body falls back to path+status", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function () {
      return Promise.resolve(jsonFailRes(502));
    },
  });
  ctx.CW.state.token = null;
  return ctx.CW.api("/nobody").then(
    function () { throw new Error("should reject"); },
    function (err) {
      assert.equal(err.message, "request failed: /nobody HTTP 502");
    }
  );
});

// --- apiAll (core.js:174-211) ---

test("apiAll non-list path passes through to api", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function (url) {
      assert.equal(url, "/api/v1/admin/env/dev/stats?flag=a");
      return Promise.resolve(jsonRes(200, { fetches: 3 }));
    },
  });
  return ctx.CW.apiAll("/api/v1/admin/env/dev/stats?flag=a").then(function (data) {
    jsonEq(data, { fetches: 3 });
    assert.equal(ctx.fetchCalls.length, 1);
  });
});

test("apiAll merges exactly 3 pages", function () {
  var seen = [];
  var pages = {
    1: { items: [1, 2], perPage: 2, totalPages: 3 },
    2: { items: [3, 4], perPage: 2, totalPages: 3 },
    3: { items: [5], perPage: 2, totalPages: 3 },
  };
  var ctx = h.load(["core.js"], {
    fetchImpl: function (url) {
      seen.push(url);
      var m = /page=(\d+)/.exec(url);
      var n = Number(m[1]);
      return Promise.resolve(jsonRes(200, pages[n]));
    },
  });
  return ctx.CW.apiAll("/api/collections/flags/records").then(function (out) {
    jsonEq(out, [1, 2, 3, 4, 5]);
    assert.equal(seen.length, 3);
    seen.forEach(function (u) {
      assert.ok(u.indexOf("perPage=200") >= 0, "each page forces perPage=200: " + u);
    });
    assert.ok(/page=1/.test(seen[0]) && /page=2/.test(seen[1]) && /page=3/.test(seen[2]));
  });
});

test("apiAll trips paging guard past 1100 pages", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function () {
      // Single-item pages with perPage=1 never short-circuit (core.js:199-207),
      // so pagination runs until PAGE_GUARD=1100 trips.
      return Promise.resolve(jsonRes(200, { items: [{ x: 1 }], perPage: 1, totalPages: 99999 }));
    },
  });
  return ctx.CW.apiAll("/api/collections/flags/records").then(
    function () { throw new Error("should reject"); },
    function (err) {
      assert.ok(err.message.indexOf(">1100 pages") >= 0, "guard mentions >1100 pages: " + err.message);
      assert.equal(ctx.fetchCalls.length, 1100);
    }
  );
});

test("apiAll rejects when a list page is missing items", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function () {
      return Promise.resolve(jsonRes(200, { perPage: 200, totalPages: 1 }));
    },
  });
  return ctx.CW.apiAll("/api/collections/flags/records").then(
    function () { throw new Error("should reject"); },
    function (err) {
      assert.ok(err.message.indexOf("missing items") >= 0, err.message);
    }
  );
});

test("apiAll rejects when items is not an array", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function () {
      return Promise.resolve(jsonRes(200, { items: "nope", perPage: 200, totalPages: 1 }));
    },
  });
  return ctx.CW.apiAll("/api/collections/flags/records").then(
    function () { throw new Error("should reject"); },
    function (err) {
      assert.ok(err.message.indexOf("not an array") >= 0, err.message);
    }
  );
});

// --- apiMut (core.js:221-236) ---

test("apiMut normalizes status/data and sends JSON body", function () {
  var seen = [];
  var ctx = h.load(["core.js"], {
    fetchImpl: function (url, opts) {
      seen.push({ url: url, opts: opts });
      return Promise.resolve(jsonRes(200, { id: "n1" }));
    },
  });
  ctx.CW.state.token = "tok";
  return ctx.CW.apiMut("POST", "/api/collections/flags/records", { key: "f" }).then(function (out) {
    jsonEq(out, { status: 200, data: { id: "n1" } });
    assert.equal(seen[0].opts.method, "POST");
    assert.equal(seen[0].opts.headers["Content-Type"], "application/json");
    assert.equal(seen[0].opts.headers.Authorization, "tok");
    assert.equal(seen[0].opts.body, JSON.stringify({ key: "f" }));
  });
});

test("apiMut normalizes non-json body to HTTP-status message", function () {
  var ctx = h.load(["core.js"], {
    fetchImpl: function () {
      return Promise.resolve(jsonFailRes(201));
    },
  });
  ctx.CW.state.token = "tok";
  return ctx.CW.apiMut("POST", "/x", {}).then(function (out) {
    jsonEq(out, { status: 201, data: { message: "HTTP 201" } });
    assert.equal(ctx.CW.state.token, "tok");
  });
});

test("apiMut calls logoutRef on 401 but still resolves", function () {
  var ctx = h.load(["core.js"], {
    localStorageSeed: { cw_admin_token: "tok" },
    fetchImpl: function () {
      return Promise.resolve(jsonRes(401, { message: "expired" }));
    },
  });
  ctx.CW.state.token = "tok";
  return ctx.CW.apiMut("GET", "/x").then(function (out) {
    jsonEq(out, { status: 401, data: { message: "expired" } });
    assert.equal(ctx.CW.state.token, null);
    assert.ok(!Object.prototype.hasOwnProperty.call(ctx.store, "cw_admin_token"));
  });
});
