/* ConfigWire admin shell — core.js (shared state + helpers).
 * Loaded FIRST. Defines window.CW namespace used by all later files.
 */
(function () {
  "use strict";

  var CW = window.CW = window.CW || {};

  var LS_KEY = "cw_admin_token";
  var LS_PROJECT = "cw_admin_project";
  var LS_ENV = "cw_admin_env";

  var state = {
    token: null, // memory-first copy
    projects: [],
    envs: [],
    projectId: null,
    envId: null,
    envSlug: "dev",
    flags: [],
    groups: {}, // group id -> name
    releases: [],
    rules: [],
    experiments: [],
    keys: [],
    lastStats: null, // last successful stats payload (kept across errors)
    lastStatsText: "", // JSON text of lastStats for copy-JSON
    lastStatsError: "", // inline error line rendered under the numbers
    homeStats: {}, // projectId -> {flags, envs, keys} for the home grid
    view: "home", // "home" | "detail" (hash-routed)
    unpublishedChanges: false, // publish-global dirty (any unsaved/unpublished edit)
    unpublished: { flag: {}, rule: {}, experiment: {}, group: {} }, // kind -> id -> {label, at}
  };

  function $(id) { return document.getElementById(id); }

  function on(id, ev, fn) {
    var el = $(id);
    if (el && el.addEventListener) el.addEventListener(ev, fn);
    return el;
  }

  function authHeaders() {
    // BARE token. Do NOT prefix with "TOKEN " (PocketBase data API 403s).
    return state.token ? { Authorization: state.token } : {};
  }

  function toast(msg, ok) {
    var el = $("toast");
    if (!el) return;
    // After auto-logout there is no session: never leave a stale API error
    // (e.g. "request failed (403)") visible on the login screen.
    if (!state.token && ok !== true) { el.textContent = ""; el.hidden = true; return; }
    el.textContent = msg;
    el.hidden = !msg;
    // Success (ok===true) renders green via #toast.ok; everything else
    // stays danger-red so errors are never mistaken for success.
    el.classList.toggle("ok", ok === true);
  }

  function showLoginError(msg) {
    var el = $("login-error");
    el.textContent = msg;
    el.hidden = !msg;
  }

  function setLoggedIn(on) {
    if ($("login-section")) $("login-section").hidden = on;
    if ($("setup-section")) $("setup-section").hidden = on ? true : $("setup-section").hidden;
    if ($("admin-section")) $("admin-section").hidden = !on;
    if ($("logout-btn")) $("logout-btn").hidden = !on;
  }

  function selectedEnv() {
    for (var i = 0; i < state.envs.length; i++) {
      if (state.envs[i].id === state.envId) return state.envs[i];
    }
    return null;
  }

  function envSlug() {
    var e = selectedEnv();
    return (e && e.slug) || state.envSlug || "dev";
  }

  function loadPersistedScope() {
    try {
      state.projectId = localStorage.getItem(LS_PROJECT);
      state.envId = localStorage.getItem(LS_ENV);
    } catch (e) { /* private mode */ }
  }

  function persistScope() {
    try {
      if (state.projectId) localStorage.setItem(LS_PROJECT, state.projectId);
      else localStorage.removeItem(LS_PROJECT);
      if (state.envId) localStorage.setItem(LS_ENV, state.envId);
      else localStorage.removeItem(LS_ENV);
    } catch (e) { /* private mode */ }
  }

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;")
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  function serverMessage(data) {
    if (data == null) return "";
    if (typeof data === "string") return data;
    if (data.message) return String(data.message);
    try { return JSON.stringify(data); } catch (e) { return "request failed"; }
  }

  function api(path) {
    return fetch(path, { headers: authHeaders() }).then(function (res) {
      if (res.status === 401 || res.status === 403) { logoutRef(); throw new Error("session expired — please log in"); }
      if (!res.ok) {
        return res.json().then(function (data) {
          throw new Error("request failed (" + res.status + "): " + serverMessage(data));
        }, function () { throw new Error("request failed: " + path + " HTTP " + res.status); });
      }
      return res.json();
    });
  }

  var LIST_RE = /\/api\/collections\/[^/?#]+\/records(?:[?#]|$)/;
  var PAGE_GUARD = 1100;

  function withPage(path, page) {
    var hash = "";
    var hIdx = path.indexOf("#");
    if (hIdx >= 0) { hash = path.slice(hIdx); path = path.slice(0, hIdx); }
    var qIdx = path.indexOf("?");
    var base = qIdx >= 0 ? path.slice(0, qIdx) : path;
    var qs = qIdx >= 0 ? path.slice(qIdx + 1) : "";
    var parts = [];
    if (qs) {
      var pairs = qs.split("&");
      for (var i = 0; i < pairs.length; i++) {
        if (pairs[i] === "") continue;
        var eq = pairs[i].indexOf("=");
        var k = eq >= 0 ? pairs[i].slice(0, eq) : pairs[i];
        try { k = decodeURIComponent(k); } catch (e) { /* keep raw */ }
        if (k === "page" || k === "perPage") continue;
        parts.push(pairs[i]);
      }
    }
    parts.push("perPage=200");
    parts.push("page=" + page);
    return base + "?" + parts.join("&") + hash;
  }

  function apiAll(path) {
    if (!LIST_RE.test(path)) return api(path);
    var out = [];
    var page = 0;
    var totalPages = Infinity;
    var guard = 0;
    function failWithStatus(err, url) {
      var m = /\((\d{3})\)/.exec(String(err && err.message));
      if (m) throw new Error("request failed (" + m[1] + "): " + url);
      throw err;
    }
    function next() {
      guard++;
      if (guard > PAGE_GUARD) {
        return Promise.reject(new Error("request failed: paging guard tripped (>1100 pages): " + path));
      }
      page++;
      if (page > totalPages) return Promise.resolve(out);
      var url = withPage(path, page);
      return api(url).then(function (data) {
        var items = data && data.items;
        if (items == null) throw new Error("request failed: list page missing items: " + url);
        if (!Array.isArray(items)) {
          throw new Error("request failed: list page items not an array: " + url);
        }
        var pp = (data && data.perPage) || 200;
        for (var i = 0; i < items.length; i++) out.push(items[i]);
        if (data && typeof data.totalPages === "number" && isFinite(data.totalPages)) {
          totalPages = data.totalPages;
        } else if (items.length < pp) {
          totalPages = page;
        }
        if (items.length < pp || page >= totalPages) return out;
        return next();
      }, function (err) { failWithStatus(err, url); });
    }
    return next();
  }

  function logoutRef() {
    // Late-bound to avoid load-order coupling: auth.js registers CW.logout.
    if (CW.logout) return CW.logout();
    state.token = null;
    try { localStorage.removeItem(LS_KEY); } catch (e) { /* private mode */ }
    setLoggedIn(false);
  }

  function apiMut(method, path, body) {
    return fetch(path, {
      method: method,
      headers: Object.assign({ "Content-Type": "application/json" }, authHeaders()),
      body: body === undefined ? undefined : JSON.stringify(body),
    }).then(function (res) {
      return res.json().then(function (data) {
        return { status: res.status, data: data };
      }, function () {
        return { status: res.status, data: { message: "HTTP " + res.status } };
      }).then(function (out) {
        if (out.status === 401 || out.status === 403) logoutRef();
        return out;
      });
    });
  }

  CW.LS_KEY = LS_KEY;
  CW.LS_PROJECT = LS_PROJECT;
  CW.LS_ENV = LS_ENV;
  CW.state = state;
  CW.$ = $;
  CW.on = on;
  CW.authHeaders = authHeaders;
  CW.toast = toast;
  CW.showLoginError = showLoginError;
  CW.setLoggedIn = setLoggedIn;
  CW.selectedEnv = selectedEnv;
  CW.envSlug = envSlug;
  CW.loadPersistedScope = loadPersistedScope;
  CW.persistScope = persistScope;
  CW.esc = esc;
  CW.serverMessage = serverMessage;
  CW.api = api;
  CW.apiAll = apiAll;
  CW.apiMut = apiMut;
})();
