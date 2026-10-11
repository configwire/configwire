/* ConfigWire admin shell — auth.js (login, logout). */
(function () {
  "use strict";

  var CW = window.CW;

  function login(email, password) {
    CW.showLoginError("");
    return fetch("/api/collections/_superusers/auth-with-password", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ identity: email, password: password }),
    }).then(function (res) {
      if (!res.ok) {
        CW.clearToken();
        throw new Error("login failed: HTTP " + res.status);
      }
      return res.json();
    }).then(function (data) {
      CW.saveToken(data.token); // bare token, memory-first
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

  CW.login = login;
  CW.logout = logout;
})();
