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
        CW.state.token = null;
        try { localStorage.removeItem(CW.LS_KEY); } catch (e) { /* private mode */ }
        throw new Error("login failed: HTTP " + res.status);
      }
      return res.json();
    }).then(function (data) {
      CW.state.token = data.token; // bare token, memory-first
      try { localStorage.setItem(CW.LS_KEY, data.token); } catch (e) { /* private mode */ }
      CW.setLoggedIn(true);
      CW.refreshAll();
      return data;
    }).catch(function (err) {
      CW.showLoginError(err.message);
      throw err;
    });
  }

  function logout() {
    CW.state.token = null;
    try { localStorage.removeItem(CW.LS_KEY); } catch (e) { /* private mode */ }
    CW.setLoggedIn(false);
  }

  CW.login = login;
  CW.logout = logout;
})();
