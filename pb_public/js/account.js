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
  function checkSetup() {
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
    if (!email) { setSetupStatus("", "email is required"); return Promise.resolve(); }
    if (pw.length < 8) { setSetupStatus("", "password must be at least 8 characters"); return Promise.resolve(); }
    if (pw !== confirm) { setSetupStatus("", "passwords do not match"); return Promise.resolve(); }
    setSetupStatus("", "");
    return CW.apiMut("POST", "/api/v1/admin/setup", { email: email, password: pw }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      if (!ok) {
        setSetupStatus("", "setup failed (" + out.status + "): " + CW.serverMessage(out.data));
        return out;
      }
      var res = CW.$("setup-result");
      if (res) { res.textContent = "admin created — signing in…"; res.hidden = false; }
      // Auto-log-in via the standard superuser auth (CW.login flips the shell).
      return CW.login(email, pw).then(function (data) {
        setSetupStatus("", "");
        return data;
      }, function (err) {
        setSetupStatus("", "created, but auto-login failed: " + (err && err.message ? err.message : err));
        throw err;
      });
    });
  }

  function renderAccounts() {
    var list = CW.$("account-list");
    if (!list) return;
    if (!CW.state.accounts.length) { list.innerHTML = "<li>No admin accounts.</li>"; return; }
    list.innerHTML = CW.state.accounts.map(function (a) {
      return "<li><code>" + CW.esc(a.email || a.id) + "</code>" +
        (a.created ? ' <span class="muted">' + CW.esc(a.created) + "</span>" : "") +
        ' <button type="button" class="btn ghost" data-account-password="' + CW.esc(a.id) + '">Change password</button>' +
        ' <button type="button" class="btn danger" data-account-delete="' + CW.esc(a.id) + '">Delete</button></li>';
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
    if (!email) { CW.$("account-result").textContent = "email is required"; return Promise.resolve(); }
    if (pw.length < 8) { CW.$("account-result").textContent = "password must be at least 8 characters"; return Promise.resolve(); }
    return CW.apiMut("POST", "/api/v1/admin/account/create", { email: email, password: pw }).then(function (out) {
      var ok = out.status === 200 || out.status === 201;
      CW.$("account-result").textContent = ok
        ? "admin created: " + (out.data.email || out.data.id || email)
        : "admin create failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (ok) {
        CW.toast("admin created: " + (out.data.email || email), true);
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
        CW.$("account-result").textContent = "password must be at least 8 characters";
        return;
      }
      return CW.apiMut("POST", "/api/v1/admin/account/" + encodeURIComponent(id) + "/password", { password: pw })
        .then(function (out) {
          var ok = out.status === 200 || out.status === 204;
          CW.$("account-result").textContent = ok
            ? "password changed"
            : "password change failed (" + out.status + "): " + CW.serverMessage(out.data);
          CW.toast(CW.$("account-result").textContent, ok);
          if (ok) loadAccounts().catch(function () {});
          return out;
        });
    });
  }

  function deleteAccount(id) {
    return CW.confirmDialog("Delete this admin account?", { title: "Delete admin", okText: "Delete", danger: true }).then(function (ok) {
      if (!ok) return;
      return CW.apiMut("DELETE", "/api/v1/admin/account/" + encodeURIComponent(id))
        .then(function (out) {
          var ok = out.status === 200 || out.status === 204;
          // Blocked for last user: surface the server message verbatim.
          CW.$("account-result").textContent = ok
            ? "admin deleted"
            : "delete failed (" + out.status + "): " + CW.serverMessage(out.data);
          CW.toast(CW.$("account-result").textContent, ok);
          if (ok) loadAccounts().catch(function () {});
          return out;
        });
    });
  }

  CW.checkSetup = checkSetup;
  CW.createSetup = createSetup;
  CW.renderAccounts = renderAccounts;
  CW.loadAccounts = loadAccounts;
  CW.createAccount = createAccount;
  CW.changeAccountPassword = changeAccountPassword;
  CW.deleteAccount = deleteAccount;
})();
