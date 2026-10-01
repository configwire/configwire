/* ConfigWire admin shell — keys.js (SDK keys). */
(function () {
  "use strict";

  var CW = window.CW;

  function effectiveRps(value, globalVal) {
    if (value) return value;
    if (globalVal) return globalVal;
    return "global";
  }

  function renderKeys() {
    var list = CW.$("key-list");
    if (!CW.state.keys.length) { list.innerHTML = "<li>No keys for this env.</li>"; return; }
    var lim = CW.state.limits || {};
    list.innerHTML = CW.state.keys.map(function (k) {
      var effFetch = effectiveRps(k.fetchRps, lim.fetchRps);
      var effIngest = effectiveRps(k.ingestRps, lim.ingestRps);
      return "<li><code>" + CW.esc(k.prefix) + "…</code>" +
        (k.revoked ? " revoked" : " active") +
        " fetch " + CW.esc(effFetch) + "/s ingest " + CW.esc(effIngest) + "/s" +
        ' <button type="button" data-edit-key="' + CW.esc(k.id) + '">Edit limits</button>' +
        ' <button type="button" data-revoke-key="' + CW.esc(k.id) + '"' +
        (k.revoked ? " disabled" : "") + ">revoke</button></li>";
    }).join("");
  }

  function loadKeys() {
    return CW.apiAll("/api/collections/sdk_keys/records?perPage=200").then(function (items) {
      items = items || [];
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

  function readRpsValidated(id) {
    var el = CW.$(id);
    if (!el || el.value == null || String(el.value).trim() === "") return { ok: true, value: undefined };
    var n = parseInt(String(el.value).trim(), 10);
    if (!isFinite(n) || n < 1 || n > 10000) return { ok: false, value: undefined };
    return { ok: true, value: n };
  }

  function createKey(ev) {
    if (ev) ev.preventDefault();
    if (!CW.state.envId) { CW.$("key-result").textContent = "pick an environment first"; return Promise.resolve(); }
    var fetchParsed = readRpsValidated("key-fetch-rps");
    if (!fetchParsed.ok) {
      var fetchMsg = "fetchRps must be empty or 1..10000";
      CW.$("key-result").textContent = fetchMsg;
      CW.toast(fetchMsg);
      return Promise.resolve();
    }
    var ingestParsed = readRpsValidated("key-ingest-rps");
    if (!ingestParsed.ok) {
      var ingestMsg = "ingestRps must be empty or 1..10000";
      CW.$("key-result").textContent = ingestMsg;
      CW.toast(ingestMsg);
      return Promise.resolve();
    }
    var full = randomKey();
    var prefix = full.slice(0, 8);
    return sha256Hex(full).then(function (hash) {
      var body = {
        prefix: prefix,
        hash: hash,
        env: CW.state.envId,
      };
      var fetchRps = fetchParsed.value;
      var ingestRps = ingestParsed.value;
      if (fetchRps !== undefined) body.fetchRps = fetchRps;
      if (ingestRps !== undefined) body.ingestRps = ingestRps;
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

  function promptRps(message, defVal, opts) {
    var fallbackDefault = defVal == null ? "" : String(defVal);
    if (CW.promptDialog) {
      return CW.promptDialog(message, fallbackDefault, opts);
    }
    try {
      return Promise.resolve(window.prompt(message, fallbackDefault));
    } catch (e) {
      return Promise.resolve(null);
    }
  }

  function parseRpsOrNull(raw) {
    var s = raw == null ? "" : String(raw).trim();
    if (s === "") return { ok: true, value: null };
    var n = parseInt(s, 10);
    if (!isFinite(n) || n < 1 || n > 10000) return { ok: false, value: null };
    return { ok: true, value: n };
  }

  function editKeyLimits(id) {
    var found = null;
    for (var i = 0; i < CW.state.keys.length; i++) {
      if (CW.state.keys[i].id === id) { found = CW.state.keys[i]; break; }
    }
    var defFetch = found && found.fetchRps ? String(found.fetchRps) : "";
    var defIngest = found && found.ingestRps ? String(found.ingestRps) : "";
    return promptRps("fetchRps (req/s, empty=global, 1..10000)", defFetch, { title: "Edit key limits" })
      .then(function (fetchRaw) {
        if (fetchRaw == null) return null;
        return promptRps("ingestRps (req/s, empty=global, 1..10000)", defIngest, { title: "Edit key limits" })
          .then(function (ingestRaw) {
            if (ingestRaw == null) return null;
            return { fetchRaw: fetchRaw, ingestRaw: ingestRaw };
          });
      })
      .then(function (pair) {
        if (!pair) return null;
        var f = parseRpsOrNull(pair.fetchRaw);
        var g = parseRpsOrNull(pair.ingestRaw);
        if (!f.ok || !g.ok) {
          CW.toast("fetchRps/ingestRps must be empty or 1..10000");
          return null;
        }
        return CW.apiMut("PATCH", "/api/collections/sdk_keys/records/" + encodeURIComponent(id), {
          fetchRps: f.value,
          ingestRps: g.value,
        }).then(function (out) {
          var ok = out.status === 200 || out.status === 204;
          CW.toast(ok
            ? "key limits updated"
            : "update failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
          loadKeys().catch(function () {});
          return out;
        });
      });
  }

  CW.renderKeys = renderKeys;
  CW.loadKeys = loadKeys;
  CW.createKey = createKey;
  CW.revokeKey = revokeKey;
  CW.editKeyLimits = editKeyLimits;
  CW.randomKey = randomKey;
  CW.sha256Hex = sha256Hex;
})();
