/* ConfigWire admin shell — auth.js (login, logout). */
(function () {
  "use strict";

  var CW = window.CW;

  function login(email, password) {
    CW.showLoginError("");
    // Cookie-only session: the server sets an HttpOnly cookie on success.
    // Nothing is kept in JS memory or persisted: reloads re-probe the cookie.
    return fetch("/api/v1/admin/session", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: email, password: password }),
    }).then(function (res) {
      if (!res.ok) {
        CW.clearToken();
        throw new Error("login failed: HTTP " + res.status);
      }
      return res.json();
    }).then(function (data) {
      CW.clearToken();
      CW.setLoggedIn(true);
      CW.refreshAll();
      return data;
    }).catch(function (err) {
      CW.showLoginError(err.message);
      throw err;
    });
  }

  // Logout-before-mode-resolves race (plan cloud-sso-bridge item 7): on
  // the stored-token boot path setLoggedIn(true) runs before the
  // background getAuthMode() revalidation returns, so a logout tap in that
  // window would take the standard branch (login page) even on an sso
  // tenant. When the mode is already resolved, run today's lines verbatim
  // and synchronously (no added latency on the common path). When
  // unresolved, await the cached CW.getAuthMode() promise FIRST (it caches
  // + falls back to standard on failure — no second mechanism), then run
  // the same lines. Mode-fetch failure stays standard (login) by
  // construction — the documented residual case.
  function doLogout() {
    // Fire-and-forget: the cookie is HttpOnly so only the server can clear
    // it; never block the UI flip on this call.
    try {
      fetch("/api/v1/admin/session", { method: "DELETE", credentials: "same-origin" })
        .then(function () {}, function () {});
    } catch (e) { /* sync stub throw — the UI flip below still runs */ }
    CW.clearToken();
    CW.setLoggedIn(false);
    if (CW.syncSidebar) CW.syncSidebar();
    // SSO logout event: leave for the cloud dashboard when its URL is
    // known (setLoggedIn already routed the shell to the managed notice,
    // which stays as the fallback when no cloud URL was provided).
    try { if (CW.ssoLogoutHome) CW.ssoLogoutHome(); } catch (e2) { /* notice already shown */ }
  }

  function logout() {
    try {
      if (CW.state && CW.state.authMode) { doLogout(); return; }
      if (CW.getAuthMode) return CW.getAuthMode().then(function () { doLogout(); }, function () { doLogout(); });
    } catch (e) { /* fall through to the sync lines */ }
    doLogout();
  }

  // Sliding refresh: cookie-only equivalent of PocketBase auth-refresh.
  // POSTs to /api/v1/admin/session/refresh which re-mints the JWT and
  // re-sets the cw_admin cookie with a fresh 3d window. Silent on
  // failure: an expired session surfaces via the next api() 401 which
  // drives the normal logout flow. Throttled to one call per 60s so
  // overlapping boot paths share a single mint.
  var lastRefresh = 0;
  function refreshSession() {
    try {
      var now = Date.now();
      if (now - lastRefresh < 60 * 1000) return Promise.resolve(false);
      lastRefresh = now;
      return fetch("/api/v1/admin/session/refresh", {
        method: "POST",
        credentials: "same-origin",
      }).then(function (res) {
        return !!(res && res.ok);
      }, function () { return false; });
    } catch (e) { return Promise.resolve(false); }
  }

  CW.login = login;
  CW.logout = logout;
  CW.refreshSession = refreshSession;
})();
