/* ConfigWire admin shell — account.js (first-run setup wizard + admin accounts). */
(function () {
  "use strict";

  var CW = window.CW;

  CW.state.accounts = CW.state.accounts || [];

  function setSetupStatus(okMsg, errMsg) {
    var ok = CW.$("setup-result");
    var err = CW.$("setup-error");
    if (ok) { ok.textContent = okMsg || ""; ok.hidden = !okMsg; }
    if (err) { err.textContent = errMsg || ""; err.hidden = !errMsg; }
  }

  // Unauthenticated: does this server need a first superuser?
  // Shows setup-section (hides login) when needsSetup, else normal login.
  // SSO guard: in sso mode there is no setup-password or login form at
  // all, so never probe setup and never reveal either section here — the
  // managed notice owns the pre-auth surface (prevents a login leak if a
  // caller reaches checkSetup while the cached mode is sso).
  function checkSetup() {
    try {
      if (CW.isSsoMode && CW.isSsoMode()) {
        var sLogin = CW.$("login-section");
        if (sLogin) sLogin.hidden = true;
        var sSetup = CW.$("setup-section");
        if (sSetup) sSetup.hidden = true;
        return Promise.resolve({ needsSetup: false });
      }
    } catch (e) { /* fall through to today's behavior */ }
    if (CW.state.token) return Promise.resolve({ needsSetup: false });
    return CW.api("/api/v1/admin/setup/status").then(function (data) {
      var needs = !!(data && data.needsSetup);
      var setup = CW.$("setup-section");
      var login = CW.$("login-section");
      if (setup) setup.hidden = !needs;
      if (login) login.hidden = needs;
      if (!needs) setSetupStatus("", "");
      return { needsSetup: needs };
    }, function () {
      // Backend predates the setup contract (or unreachable): keep normal login.
      var setup = CW.$("setup-section");
      var login = CW.$("login-section");
      if (setup) setup.hidden = true;
      if (login) login.hidden = false;
      return { needsSetup: false };
    });
  }

  function createSetup(ev) {
    if (ev) ev.preventDefault();
    var email = CW.$("setup-email") ? CW.$("setup-email").value.trim() : "";
    var pw = CW.$("setup-password") ? CW.$("setup-password").value : "";
    var confirm = CW.$("setup-password-confirm") ? CW.$("setup-password-confirm").value : "";
    if (!email) { setSetupStatus("", "Email required."); return Promise.resolve(); }
    if (pw.length < 8) { setSetupStatus("", "Password needs 8+ characters."); return Promise.resolve(); }
    if (pw !== confirm) { setSetupStatus("", "Passwords don't match."); return Promise.resolve(); }
    setSetupStatus("", "");
    return CW.apiMut("POST", "/api/v1/admin/setup", { email: email, password: pw }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      if (!ok) {
        setSetupStatus("", CW.apiError(out, "Setup failed"));
        return out;
      }
      var res = CW.$("setup-result");
      if (res) { res.textContent = "Account created. Signing in…"; res.hidden = false; }
      // Auto-log-in via the standard superuser auth (CW.login flips the shell).
      return CW.login(email, pw).then(function (data) {
        setSetupStatus("", "");
        return data;
      }, function (err) {
        setSetupStatus("", "Created, but auto-login failed: " + (err && err.message ? err.message : err));
        throw err;
      });
    });
  }

  function renderAccounts() {
    var list = CW.$("account-list");
    if (!list) return;
    if (!CW.state.accounts.length) { list.innerHTML = "<li>No admin accounts.</li>"; return; }
    var single = CW.state.accounts.length === 1;
    list.innerHTML = CW.state.accounts.map(function (a) {
      return "<li><code>" + CW.esc(a.email || a.id) + "</code>" +
        (a.created ? ' <span class="muted">' + CW.esc(a.created) + "</span>" : "") +
        ' <button type="button" class="btn ghost" data-account-password="' + CW.esc(a.id) + '">Change password</button>' +
        ' <button type="button" class="btn danger" data-account-delete="' + CW.esc(a.id) + '"' +
        (single ? ' disabled title="Cannot delete the last admin account."' : '') + '>Delete</button></li>';
    }).join("");
  }

  function loadAccounts() {
    return CW.api("/api/v1/admin/account/list").then(function (data) {
      CW.state.accounts = data.items || [];
      renderAccounts();
      return CW.state.accounts;
    }, function (err) {
      var list = CW.$("account-list");
      if (list) list.innerHTML = "<li>" + CW.esc(err.message) + "</li>";
      throw err;
    });
  }

  function createAccount(ev) {
    if (ev) ev.preventDefault();
    var email = CW.$("account-email") ? CW.$("account-email").value.trim() : "";
    var pw = CW.$("account-password") ? CW.$("account-password").value : "";
    if (!email) { CW.$("account-result").textContent = "Email required."; return Promise.resolve(); }
    if (pw.length < 8) { CW.$("account-result").textContent = "Password needs 8+ characters."; return Promise.resolve(); }
    return CW.apiMut("POST", "/api/v1/admin/account/create", { email: email, password: pw }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("account-result").textContent = ok
        ? "Account created: " + (out.data.email || out.data.id || email)
        : CW.apiError(out, "Account create failed");
      if (ok) {
        CW.toast("Account created: " + (out.data.email || email), true);
        CW.$("account-email").value = "";
        CW.$("account-password").value = "";
        if (CW.markFormClean) CW.markFormClean("account-create-form");
      } else {
        CW.toast(CW.$("account-result").textContent);
      }
      loadAccounts().catch(function () {});
      return out;
    });
  }

  function changeAccountPassword(id) {
    return CW.promptDialog("New password for this admin (min 8 characters):", "", { title: "Change password", okText: "Change", inputType: "password", required: true, minLength: 8 }).then(function (pw) {
      if (pw === null) return; // cancelled
      if (pw.length < 8) {
        CW.$("account-result").textContent = "Password needs 8+ characters.";
        return;
      }
      return CW.apiMut("POST", "/api/v1/admin/account/" + encodeURIComponent(id) + "/password", { password: pw })
        .then(function (out) {
          var ok = out.status === 200 || out.status === 204;
          CW.$("account-result").textContent = ok
            ? "Password changed."
            : CW.apiError(out, "Password change failed");
          CW.toast(CW.$("account-result").textContent, ok);
          if (ok) loadAccounts().catch(function () {});
          return out;
        });
    });
  }

  function deleteAccount(id) {
    if (CW.state.accounts.length === 1) {
      try { CW.$("account-result").textContent = "Cannot delete the last admin account."; } catch (e) { /* non-browser harness */ }
      CW.toast("Cannot delete the last admin account.");
      return Promise.resolve();
    }
    return CW.confirmDialog("Delete this admin account?", { title: "Delete admin", okText: "Delete", danger: true }).then(function (ok) {
      if (!ok) return;
      return CW.apiMut("DELETE", "/api/v1/admin/account/" + encodeURIComponent(id))
        .then(function (out) {
          var ok = out.status === 200 || out.status === 204;
          // Blocked for last user: surface the server message verbatim.
          CW.$("account-result").textContent = ok
            ? "Admin deleted."
            : CW.apiError(out, "Delete failed");
          CW.toast(CW.$("account-result").textContent, ok);
          if (ok) loadAccounts().catch(function () {});
          return out;
        });
    });
  }

  // ---- Cloud SSO bridge (plan cloud-sso-bridge P1) ----
  // No load-time side effects: these are pure helpers + fetch wrappers
  // called from boot.js only. In standard mode (or when the backend
  // predates the contract) every path below falls back to today's
  // login/setup behavior.

  // getSsoTicket reads ?sso=<ticket> from the current URL (href-parsed so
  // it works without window.location.search). Percent-decoding is
  // best-effort; "" means absent.
  function getSsoTicket() {
    var href = "";
    try { href = String((window.location && window.location.href) || ""); } catch (e) { return ""; }
    var h = href.split("#")[0];
    var q = h.indexOf("?");
    if (q < 0) return "";
    var pairs = h.slice(q + 1).split("&");
    for (var i = 0; i < pairs.length; i++) {
      var kv = pairs[i].split("=");
      var k = kv[0];
      try { k = decodeURIComponent(k); } catch (e) { /* keep raw */ }
      if (k === "sso") {
        var v = kv.slice(1).join("=");
        try { v = decodeURIComponent(v); } catch (e2) { /* keep raw */ }
        return v || "";
      }
    }
    return "";
  }

  // stripSsoParam returns url without the sso query param (others kept).
  function stripSsoParam(url) {
    var hash = "";
    var hi = String(url).indexOf("#");
    if (hi >= 0) { hash = String(url).slice(hi); url = String(url).slice(0, hi); }
    else url = String(url);
    var qi = url.indexOf("?");
    if (qi < 0) return url + hash;
    var base = url.slice(0, qi);
    var kept = [];
    var pairs = url.slice(qi + 1).split("&");
    for (var i = 0; i < pairs.length; i++) {
      if (pairs[i] === "") continue;
      var k = pairs[i].split("=")[0];
      try { k = decodeURIComponent(k); } catch (e) { /* keep raw */ }
      if (k === "sso") continue;
      kept.push(pairs[i]);
    }
    return base + (kept.length ? "?" + kept.join("&") : "") + hash;
  }

  // getAuthMode resolves the server mode once ("sso" or "standard") and
  // caches it on CW.state. Any failure (backend predates the contract,
  // offline) resolves "standard" so the shell keeps today's behavior.
  function getAuthMode() {
    if (CW.state.authMode) return Promise.resolve(CW.state.authMode);
    return CW.api("/api/v1/admin/auth/mode").then(function (data) {
      var m = (data && data.mode === "sso") ? "sso" : "standard";
      CW.state.authMode = m;
      if (data && typeof data.cloudUrl === "string" && data.cloudUrl) CW.state.cloudUrl = data.cloudUrl;
      return m;
    }, function () { return "standard"; });
  }

  // redeemSsoTicket redeems a ticket same-origin and stores the token
  // exactly like CW.login success: bare token memory-first + localStorage
  // + shell flip + refresh. On success it strips ?sso= via replaceState.
  function redeemSsoTicket(ticket) {
    CW.showLoginError("");
    return fetch("/api/v1/admin/sso/redeem", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ticket: ticket }),
    }).then(function (res) {
      if (!res.ok) throw new Error("sso redeem failed: HTTP " + res.status);
      return res.json();
    }).then(function (data) {
      if (!data || !data.token) throw new Error("sso redeem failed: no token");
      CW.saveToken(data.token); // bare token, memory-first
      var sso = CW.$("sso-section");
      if (sso) sso.hidden = true;
      CW.setLoggedIn(true);
      if (typeof CW.refreshAll === "function") CW.refreshAll();
      try {
        if (window.history && window.history.replaceState && window.location && window.location.href) {
          window.history.replaceState(null, "", stripSsoParam(window.location.href));
        }
      } catch (e) { /* best-effort URL cleanup */ }
      return data;
    });
  }

  // showSsoNotice hides the native login AND setup password forms and
  // shows the managed-by-cloud notice (with the dashboard link only when
  // the server provided a cloud URL). errMsg (redeem failure) renders in
  // sso-error; "" clears it. It also hides every other auth surface —
  // logout control and the whole account-management card — so no password
  // path stays reachable; CW.login/CW.logout/account helpers stay intact.
  function showSsoNotice(errMsg) {
    var login = CW.$("login-section");
    if (login) login.hidden = true;
    var setup = CW.$("setup-section");
    if (setup) setup.hidden = true;
    var le = CW.$("login-error");
    if (le) { le.textContent = ""; le.hidden = true; }
    var lb = CW.$("logout-btn");
    if (lb) { lb.hidden = true; try { lb.style.display = "none"; } catch (e) { /* stub */ } }
    if (CW.applySsoUiGating) { try { CW.applySsoUiGating(); } catch (e2) { /* best-effort */ } }
    else {
      var card = CW.$("account-card");
      if (card) card.hidden = true;
      var al = CW.$("account-list");
      if (al) al.hidden = true;
      var af = CW.$("account-create-form");
      if (af) af.hidden = true;
      var ar = CW.$("account-result");
      if (ar) ar.hidden = true;
    }
    var admin = CW.$("admin-section");
    if (admin) admin.hidden = true;
    var sso = CW.$("sso-section");
    if (sso) sso.hidden = false;
    var link = CW.$("sso-cloud-link");
    if (link) {
      if (CW.state.cloudUrl) {
        try { link.href = CW.state.cloudUrl; } catch (e) { /* stub */ }
        link.hidden = false;
      } else {
        link.hidden = true;
      }
    }
    var err = CW.$("sso-error");
    if (err) { err.textContent = errMsg || ""; err.hidden = !errMsg; }
  }

  CW.checkSetup = checkSetup;
  CW.getSsoTicket = getSsoTicket;
  CW.stripSsoParam = stripSsoParam;
  CW.getAuthMode = getAuthMode;
  CW.redeemSsoTicket = redeemSsoTicket;
  CW.showSsoNotice = showSsoNotice;
  CW.createSetup = createSetup;
  CW.renderAccounts = renderAccounts;
  CW.loadAccounts = loadAccounts;
  CW.createAccount = createAccount;
  CW.changeAccountPassword = changeAccountPassword;
  CW.deleteAccount = deleteAccount;
})();
