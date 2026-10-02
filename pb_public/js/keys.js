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
    if (!CW.state.keys.length) { list.innerHTML = "<li>No keys.</li>"; return; }
    var lim = CW.state.limits || {};
    list.innerHTML = CW.state.keys.map(function (k) {
      var effFetch = effectiveRps(k.fetchRps, lim.fetchRps);
      var effIngest = effectiveRps(k.ingestRps, lim.ingestRps);
      return '<li class="key-row"><code>' + CW.esc(k.prefix) + "…</code>" +
        (k.revoked
          ? ' <span class="badge revoked">revoked</span>'
          : ' <span class="badge ok">active</span>') +
        ' <span class="key-limits muted">fetch ' + CW.esc(effFetch) + "/s · ingest " + CW.esc(effIngest) + "/s</span>" +
        ' <span class="key-actions">' +
        '<button type="button" data-edit-key="' + CW.esc(k.id) + '">Edit limits</button>' +
        ' <button type="button" data-revoke-key="' + CW.esc(k.id) + '"' +
        (k.revoked ? " disabled" : "") + ">revoke</button></span></li>";
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
    if (!CW.state.envId) { CW.$("key-result").textContent = "Pick an environment first."; return Promise.resolve(); }
    var fetchParsed = readRpsValidated("key-fetch-rps");
    if (!fetchParsed.ok) {
      var fetchMsg = "Limits: empty or 1–10000.";
      CW.$("key-result").textContent = fetchMsg;
      CW.toast(fetchMsg);
      return Promise.resolve();
    }
    var ingestParsed = readRpsValidated("key-ingest-rps");
    if (!ingestParsed.ok) {
      var ingestMsg = "Limits: empty or 1–10000.";
      CW.$("key-result").textContent = ingestMsg;
      CW.toast(ingestMsg);
      return Promise.resolve();
    }
    var body = { env: CW.state.envId };
    var fetchRps = fetchParsed.value;
    var ingestRps = ingestParsed.value;
    if (fetchRps !== undefined) body.fetchRps = fetchRps;
    if (ingestRps !== undefined) body.ingestRps = ingestRps;
    return CW.apiMut("POST", "/api/v1/admin/keys", body).then(function (out) {
      var data = out.data || {};
      if ((out.status === 200 || out.status === 201) && data.key) {
        CW.$("key-result").textContent = "Key created: " + data.prefix;
        CW.$("key-once-value").textContent = data.key;
        CW.$("key-once").hidden = false;
      } else {
        CW.$("key-result").textContent = "Key create failed (" + out.status + "): " + CW.serverMessage(out.data);
      }
      loadKeys().catch(function () {});
      return out;
    });
  }

  function revokeKey(id) {
    return CW.apiMut("PATCH", "/api/collections/sdk_keys/records/" + encodeURIComponent(id), { revoked: true })
      .then(function (out) {
        var keyOk = out.status === 200 || out.status === 204;
        CW.toast(keyOk
          ? "Key revoked."
          : "Revoke failed (" + out.status + "): " + CW.serverMessage(out.data), keyOk);
        loadKeys().catch(function () {});
        return out;
      });
  }

  function parseRpsOrNull(raw) {
    var s = raw == null ? "" : String(raw).trim();
    if (s === "") return { ok: true, value: null };
    var n = parseInt(s, 10);
    if (!isFinite(n) || n < 1 || n > 10000) return { ok: false, value: null };
    return { ok: true, value: n };
  }

  function openKeyLimitsDialog(id) {
    var found = null;
    for (var i = 0; i < CW.state.keys.length; i++) {
      if (CW.state.keys[i].id === id) { found = CW.state.keys[i]; break; }
    }
    var idEl = CW.$("key-limits-id");
    if (idEl) idEl.value = id == null ? "" : String(id);
    var fetchEl = CW.$("key-limits-fetch");
    if (fetchEl) fetchEl.value = found && found.fetchRps ? String(found.fetchRps) : "";
    var ingestEl = CW.$("key-limits-ingest");
    if (ingestEl) ingestEl.value = found && found.ingestRps ? String(found.ingestRps) : "";
    var res = CW.$("key-limits-result");
    if (res) res.textContent = "";
    if (CW.markFormClean) CW.markFormClean("key-limits-form");
    var dlg = CW.$("key-limits-dialog");
    if (!dlg) return;
    if (dlg.showModal) {
      try { if (!dlg.open) dlg.showModal(); } catch (e) { /* already open */ }
    } else if (dlg.setAttribute) {
      try { dlg.setAttribute("open", ""); } catch (e2) { /* stub DOM */ }
    }
  }

  function closeKeyLimitsDialog() {
    var dlg = CW.$("key-limits-dialog");
    if (dlg && dlg.open) dlg.close();
  }

  function saveKeyLimits(ev) {
    if (ev) ev.preventDefault();
    var idEl = CW.$("key-limits-id");
    var id = idEl ? idEl.value : "";
    var fetchEl = CW.$("key-limits-fetch");
    var ingestEl = CW.$("key-limits-ingest");
    var f = parseRpsOrNull(fetchEl ? fetchEl.value : "");
    var g = parseRpsOrNull(ingestEl ? ingestEl.value : "");
    if (!f.ok || !g.ok) {
      var msg = "Limits: empty or 1–10000.";
      var res = CW.$("key-limits-result");
      if (res) res.textContent = msg;
      CW.toast(msg);
      return Promise.resolve(null);
    }
    return CW.apiMut("PATCH", "/api/collections/sdk_keys/records/" + encodeURIComponent(id), {
      fetchRps: f.value,
      ingestRps: g.value,
    }).then(function (out) {
      var ok = out.status === 200 || out.status === 204;
      CW.toast(ok
        ? "Key limits updated."
        : "Update failed (" + out.status + "): " + CW.serverMessage(out.data), ok);
      if (ok) closeKeyLimitsDialog();
      loadKeys().catch(function () {});
      return out;
    });
  }

  function editKeyLimits(id) {
    openKeyLimitsDialog(id);
  }

  CW.renderKeys = renderKeys;
  CW.loadKeys = loadKeys;
  CW.createKey = createKey;
  CW.revokeKey = revokeKey;
  CW.openKeyLimitsDialog = openKeyLimitsDialog;
  CW.closeKeyLimitsDialog = closeKeyLimitsDialog;
  CW.saveKeyLimits = saveKeyLimits;
  CW.editKeyLimits = editKeyLimits;
  CW.randomKey = randomKey;
  CW.sha256Hex = sha256Hex;
})();
