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
    scopeGen: 0, // bumped on every scope change; loaders drop older generations
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

  var toastTimer = null;

  function hideToast() {
    var el = $("toast");
    if (toastTimer) { clearTimeout(toastTimer); toastTimer = null; }
    if (!el) return;
    el.textContent = "";
    el.hidden = true;
    el.classList.remove("ok");
  }

  function toast(msg, ok) {
    var el = $("toast");
    if (!el) return;
    if (toastTimer) { clearTimeout(toastTimer); toastTimer = null; }
    // After auto-logout there is no session: never leave a stale API error
    // (e.g. "request failed (403)") visible on the login screen.
    if (!state.token && ok !== true) { el.textContent = ""; el.hidden = true; return; }
    el.textContent = msg;
    el.hidden = !msg;
    // Success (ok===true) renders green via #toast.ok; everything else
    // stays danger-red so errors are never mistaken for success.
    el.classList.toggle("ok", ok === true);
    if (msg) {
      toastTimer = setTimeout(hideToast, 3500);
    }
  }

  // J3: single toast-error helper for the `.catch(function (e) {
  // CW.toast(e.message); })` fanout (~30 sites). Null-safe: shows
  // (e && e.message) || "Failed". No toast copy/behavior change —
  // plain error toast (ok undefined, danger-red), identical output
  // for normal Errors.
  function toastErr(e) {
    toast((e && e.message) || "Failed");
  }

  // J3: promise guard — attaches the shared toastErr handler so call
  // sites stay one token (p.catch(CW.toastErr) or CW.guard(p)).
  function guard(p) {
    if (p && typeof p.catch === "function") return p.catch(toastErr);
    return p;
  }

  // Click-to-dismiss: single delegated listener, registered once at load.
  document.addEventListener("click", function (ev) {
    if (ev && ev.target && ev.target.closest && ev.target.closest("#toast")) hideToast();
  });

  function showLoginError(msg) {
    var el = $("login-error");
    el.textContent = msg;
    el.hidden = !msg;
  }

  // isSsoMode reads the cached mode signal only (CW.state.authMode set by
  // CW.getAuthMode in account.js; any fetch failure resolves standard, so
  // an unknown mode is never sso). No fetch here — callers already resolved.
  function isSsoMode() {
    try { return !!(state && state.authMode === "sso"); } catch (e) { return false; }
  }

  // applySsoUiGating hides every auth-related UI surface that must not
  // reach the user in sso mode. Hiding (not detaching): the login/
  // account elements stay in the DOM with hidden=true and no clickable
  // path reaches them, while CW.login/CW.logout/account helpers stay
  // callable as code. The logout control is NOT gated: it mirrors
  // standard visibility (shown when logged in) so the user can tap it to
  // clear the session and route home to the cloud dashboard. No-op
  // unless sso (standard untouched).
  function applySsoUiGating() {
    if (!isSsoMode()) return false;
    var ids = ["login-section", "setup-section",
      "account-card", "account-list", "account-create-form", "account-result"];
    for (var i = 0; i < ids.length; i++) {
      try {
        var el = $(ids[i]);
        if (el) {
          el.hidden = true;
        }
      } catch (e) { /* best-effort */ }
    }
    return true;
  }

  function setLoggedIn(on) {
    // Cloud SSO bridge: in sso mode there is no login dialog and no
    // password/account UI whatsoever — entry goes through the Cloud
    // dashboard. The logout control mirrors standard (visible when logged
    // in); tapping it clears the session and routes home to the cloud
    // dashboard via ssoLogoutHome. Every sso branch below is strictly
    // behind isSsoMode() (cached CW.state.authMode from getAuthMode);
    // standard mode runs the exact lines it always ran.
    var sso = isSsoMode();
    if (sso) {
      if ($("login-section")) $("login-section").hidden = true;
      if ($("setup-section")) $("setup-section").hidden = true;
      if ($("login-error")) { $("login-error").textContent = ""; $("login-error").hidden = true; }
      if ($("admin-section")) $("admin-section").hidden = !on;
      var lb = $("logout-btn");
      if (lb) { lb.hidden = !on; lb.style.display = on ? "" : "none"; }
      if (on) {
        var sn = $("sso-section");
        if (sn) sn.hidden = true;
      } else {
        // Logged out (or session expired) in sso mode: never fall back to
        // the classic login form — route through the managed notice so the
        // shell is never stranded blank. Late-bound: account.js owns the
        // notice (link + error plumbing); without it, reveal the section
        // with the link hidden rather than leaking login.
        if (typeof CW.showSsoNotice === "function") { CW.showSsoNotice(""); }
        else {
          var sn2 = $("sso-section");
          if (sn2) sn2.hidden = false;
        }
      }
      applySsoUiGating();
      var ts = $("sidebar-scope");
      if (ts) ts.hidden = !on;
      var snl = $("settings-nav-link");
      if (snl) { snl.hidden = !on; snl.style.display = on ? "" : "none"; }
      var nav = $("sidebar-nav");
      if (on === false) {
        if (nav) {
          nav.setAttribute("aria-hidden", "true");
          nav.style.display = "none";
        }
      } else if (on === true) {
        if (typeof CW.syncSidebar === "function") {
          CW.syncSidebar();
        } else if (nav) {
          nav.removeAttribute("aria-hidden");
          nav.style.display = "";
        }
      }
      return;
    }
    if ($("login-section")) $("login-section").hidden = on;
    if ($("setup-section")) $("setup-section").hidden = on ? true : $("setup-section").hidden;
    if ($("admin-section")) $("admin-section").hidden = !on;
    var lb = $("logout-btn");
    if (lb) { lb.hidden = !on; lb.style.display = on ? "" : "none"; }
    var ts = $("sidebar-scope");
    if (ts) ts.hidden = !on;
    var snl = $("settings-nav-link");
    if (snl) { snl.hidden = !on; snl.style.display = on ? "" : "none"; }
    var nav = $("sidebar-nav");
    if (on === false) {
      if (nav) {
        nav.setAttribute("aria-hidden", "true");
        nav.style.display = "none";
      }
    } else if (on === true) {
      if (typeof CW.syncSidebar === "function") {
        CW.syncSidebar();
      } else if (nav) {
        nav.removeAttribute("aria-hidden");
        nav.style.display = "";
      }
    }
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
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  // Scope generation: bumped on every scope change that fans out loads
  // (project/env switch, home/detail navigation). In-flight loaders
  // capture the generation at call time and drop their response when it
  // no longer matches, so a slow response from scope A can never paint
  // over scope B.
  function bumpScopeGen() { state.scopeGen = (state.scopeGen || 0) + 1; return state.scopeGen; }
  function scopeStale(gen) { return gen !== state.scopeGen; }

  function serverMessage(data) {
    if (data == null) return "";
    if (typeof data === "string") return data;
    if (data.message) return String(data.message);
    try { return JSON.stringify(data); } catch (e) { return "request failed"; }
  }

  // J2: single formatter for the `"X failed (" + out.status + "): " +
  // serverMessage(out.data)` pattern (15+ sites). Identical output, no
  // contract change — api vs apiMut vs apiAll call sites untouched.
  function apiError(out, prefix) {
    out = out || {};
    return String(prefix) + " (" + out.status + "): " + serverMessage(out.data);
  }

  // J4: single clipboard helper (boot.js x2, limits.js, stats.js pattern).
  // Promise<boolean> — callers keep their own UX strings ("Copied" /
  // "Copy failed" / "Nothing to copy." toasts or status lines), so no UX
  // copy changes. Clipboard API first, textarea+execCommand fallback.
  function copyText(v) {
    var text = v == null ? "" : String(v);
    try {
      if (typeof navigator !== "undefined" && navigator && navigator.clipboard &&
          navigator.clipboard.writeText) {
        try {
          var p = navigator.clipboard.writeText(text);
          if (p && typeof p.then === "function") {
            return p.then(function () { return true; }, function () { return false; });
          }
          return Promise.resolve(true);
        } catch (e) { /* fall through to textarea fallback */ }
      }
    } catch (e2) { /* fall through to textarea fallback */ }
    var ok = false;
    var tmp = null;
    try {
      tmp = document.createElement("textarea");
      tmp.value = text;
      document.body.appendChild(tmp);
      tmp.select();
      try { document.execCommand("copy"); ok = true; }
      catch (e3) { ok = false; }
    } catch (e4) { ok = false; }
    try { document.body.removeChild(tmp); } catch (e5) { /* best-effort */ }
    return Promise.resolve(ok);
  }

  // J5: single token-storage helper (auth.js x3, account.js sso-redeem,
  // boot.js boot-restore, core.js logoutFallback). Memory-first + guarded
  // localStorage copy, exactly like the inline lines they replace.
  function saveToken(tok) {
    state.token = tok;
    try { localStorage.setItem(LS_KEY, tok); } catch (e) { /* private mode */ }
    return tok;
  }

  function loadToken() {
    try { state.token = localStorage.getItem(LS_KEY); }
    catch (e) { state.token = null; }
    return state.token;
  }

  function clearToken() {
    state.token = null;
    try { localStorage.removeItem(LS_KEY); } catch (e) { /* private mode */ }
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

  // ssoLogoutHome navigates a logged-out sso session home to the cloud
  // dashboard instead of showing any login form. Strictly sso-only AND
  // only when the mode endpoint provided a cloud URL (standalone/legacy
  // tenants fall back to the managed notice). Returns true when it fired.
  function ssoLogoutHome() {
    try {
      if (!isSsoMode()) return false;
      var url = state && state.cloudUrl;
      if (typeof url !== "string" || !url) return false;
      if (typeof window !== "undefined" && window.location) window.location.href = url;
      else return false;
      return true;
    } catch (e) { return false; }
  }

  function logoutFallback() {
    clearToken();
    setLoggedIn(false);
    try { ssoLogoutHome(); } catch (e2) { /* notice already shown */ }
  }

  function logoutRef() {
    // Late-bound to avoid load-order coupling: auth.js registers CW.logout
    // (itself mode-aware — see auth.js — so the 401/auto-logout paths via
    // api/apiMut are covered transitively with no change at their call
    // sites). The fallback below only runs when auth.js is absent; it keeps
    // the same mode-aware shape over its own lines.
    if (CW.logout) return CW.logout();
    try {
      if (!state.authMode && typeof CW.getAuthMode === "function") {
        return CW.getAuthMode().then(function () { logoutFallback(); }, function () { logoutFallback(); });
      }
    } catch (e) { /* fall through to the sync lines */ }
    logoutFallback();
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
  CW.toastErr = toastErr;
  CW.guard = guard;
  CW.showLoginError = showLoginError;
  CW.setLoggedIn = setLoggedIn;
  CW.isSsoMode = isSsoMode;
  CW.applySsoUiGating = applySsoUiGating;
  CW.ssoLogoutHome = ssoLogoutHome;
  CW.selectedEnv = selectedEnv;
  CW.envSlug = envSlug;
  CW.loadPersistedScope = loadPersistedScope;
  CW.persistScope = persistScope;
  CW.esc = esc;
  CW.bumpScopeGen = bumpScopeGen;
  CW.scopeStale = scopeStale;
  CW.serverMessage = serverMessage;
  CW.apiError = apiError;
  CW.copyText = copyText;
  CW.saveToken = saveToken;
  CW.loadToken = loadToken;
  CW.clearToken = clearToken;
  CW.api = api;
  CW.apiAll = apiAll;
  CW.apiMut = apiMut;
})();
