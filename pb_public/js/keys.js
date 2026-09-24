/* ConfigWire admin shell — keys.js (SDK keys). */
(function () {
  "use strict";

  var CW = window.CW;

  function renderKeys() {
    var list = CW.$("key-list");
    if (!CW.state.keys.length) { list.innerHTML = "<li>No keys for this env.</li>"; return; }
    list.innerHTML = CW.state.keys.map(function (k) {
      return "<li><code>" + CW.esc(k.prefix) + "…</code>" +
        (k.revoked ? " revoked" : " active") +
        ' <button type="button" data-revoke-key="' + CW.esc(k.id) + '"' +
        (k.revoked ? " disabled" : "") + ">revoke</button></li>";
    }).join("");
  }

  function loadKeys() {
    return CW.api("/api/collections/sdk_keys/records?perPage=200").then(function (data) {
      var items = data.items || [];
      if (CW.state.envId) items = items.filter(function (k) { return k.env === CW.state.envId; });
      else if (CW.state.projectId) {
        var envIds = {};
        CW.state.envs.forEach(function (e) { envIds[e.id] = true; });
        items = items.filter(function (k) { return envIds[k.env]; });
      }
      CW.state.keys = items;
      renderKeys();
    });
  }

  function randomKey() {
    var bytes = new Uint8Array(24);
    if (window.crypto && window.crypto.getRandomValues) {
      window.crypto.getRandomValues(bytes);
    } else {
      for (var i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
    }
    var chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
    var out = "";
    for (var j = 0; j < bytes.length; j++) out += chars[bytes[j] % chars.length];
    return "cw-" + out;
  }

  function sha256Hex(s) {
    if (window.crypto && window.crypto.subtle) {
      var bytes = new TextEncoder().encode(s);
      return window.crypto.subtle.digest("SHA-256", bytes).then(function (buf) {
        return Array.prototype.map.call(new Uint8Array(buf), function (b) {
          return ("0" + b.toString(16)).slice(-2);
        }).join("");
      });
    }
    return Promise.reject(new Error("WebCrypto unavailable — cannot hash key"));
  }

  function createKey(ev) {
    if (ev) ev.preventDefault();
    if (!CW.state.envId) { CW.$("key-result").textContent = "pick an environment first"; return Promise.resolve(); }
    var full = randomKey();
    var prefix = full.slice(0, 8);
    return sha256Hex(full).then(function (hash) {
      var body = {
        prefix: prefix,
        hash: hash,
        env: CW.state.envId,
        rateLimit: parseInt(CW.$("key-ratelimit").value, 10) || 60,
      };
      return CW.apiMut("POST", "/api/collections/sdk_keys/records", body).then(function (out) {
        if (out.status === 200 || out.status === 201) {
          CW.$("key-result").textContent = "key created (prefix " + prefix + ")";
          CW.$("key-once-value").textContent = full;
          CW.$("key-once").hidden = false;
        } else {
          CW.$("key-result").textContent = "key create failed (" + out.status + "): " + CW.serverMessage(out.data);
        }
        loadKeys().catch(function () {});
        return out;
      });
    }, function (err) {
      CW.$("key-result").textContent = err.message;
    });
  }

  function revokeKey(id) {
    return CW.apiMut("PATCH", "/api/collections/sdk_keys/records/" + encodeURIComponent(id), { revoked: true })
      .then(function (out) {
        var keyOk = out.status === 200 || out.status === 204;
        CW.toast(keyOk
          ? "key revoked"
          : "revoke failed (" + out.status + "): " + CW.serverMessage(out.data), keyOk);
        loadKeys().catch(function () {});
        return out;
      });
  }

  CW.renderKeys = renderKeys;
  CW.loadKeys = loadKeys;
  CW.createKey = createKey;
  CW.revokeKey = revokeKey;
  CW.randomKey = randomKey;
  CW.sha256Hex = sha256Hex;
})();
