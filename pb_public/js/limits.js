/* ConfigWire admin shell — limits.js (global rate limits, req/sec). */
(function () {
  "use strict";

  var CW = window.CW;

  var IP_HEADER_RE = /^[A-Za-z0-9-]{1,64}$/;

  function headersToString(list) {
    if (!Array.isArray(list)) return "";
    return list.join(", ");
  }

  function stringToHeaders(raw) {
    var out = [];
    String(raw == null ? "" : raw).split(",").forEach(function (part) {
      var name = String(part).replace(/^\s+|\s+$/g, "");
      if (name !== "") out.push(name);
    });
    return out;
  }

  function ipsToString(list) {
    if (!Array.isArray(list)) return "";
    return list.join(", ");
  }

  function stringToIPs(raw) {
    var out = [];
    String(raw == null ? "" : raw).split(",").forEach(function (part) {
      var ip = String(part).replace(/^\s+|\s+$/g, "");
      if (ip !== "") out.push(ip);
    });
    return out;
  }

  function isValidIP(s) {
    var v = String(s == null ? "" : s).replace(/^\s+|\s+$/g, "");
    if (v === "") return false;
    var v4 = /^(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9][0-9]|[0-9])(\.(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9][0-9]|[0-9])){3}$/;
    if (v4.test(v)) return true;
    if (v.indexOf(":") === -1) return false;
    return /^[0-9A-Fa-f:.%]+$/.test(v);
  }

  function ipv4ToInt(v) {
    var parts = String(v).split(".");
    if (parts.length !== 4) return null;
    var n = 0;
    for (var i = 0; i < 4; i++) {
      var b = parseInt(parts[i], 10);
      if (!isFinite(b) || b < 0 || b > 255 || !/^\d+$/.test(parts[i])) return null;
      n = n * 256 + b;
    }
    return n >>> 0;
  }

  function ipv4CidrContains(cidr, ip) {
    var slash = cidr.indexOf("/");
    var base = cidr.slice(0, slash);
    var bits = parseInt(cidr.slice(slash + 1), 10);
    if (!isFinite(bits) || bits < 0 || bits > 32) return false;
    var net = ipv4ToInt(base);
    var addr = ipv4ToInt(ip);
    if (net === null || addr === null) return false;
    if (bits === 0) return true;
    var mask = bits === 32 ? 0xFFFFFFFF : (0xFFFFFFFF << (32 - bits)) >>> 0;
    return (net & mask) === (addr & mask);
  }

  function isAllowedByList(ip, list) {
    if (!Array.isArray(list) || list.length === 0) return true;
    var v = String(ip == null ? "" : ip).replace(/^\s+|\s+$/g, "");
    if (v === "") return false;
    for (var i = 0; i < list.length; i++) {
      var entry = String(list[i] == null ? "" : list[i]).replace(/^\s+|\s+$/g, "");
      if (entry === "") continue;
      if (entry.indexOf("/") !== -1) {
        if (ipv4CidrContains(entry, v)) return true;
        continue;
      }
      if (entry.toLowerCase() === v.toLowerCase()) return true;
    }
    return false;
  }

  function renderCurrentIP(el, clientIp, remoteAddr, allowlist) {
    var textEl = CW.$("admin-current-ip-text") || el;
    var ip = String(clientIp == null ? "" : clientIp).replace(/^\s+|\s+$/g, "");
    var ok = isValidIP(ip) && isAllowedByList(ip, allowlist);
    var msg;
    if (ip === "") {
      msg = "Your current IP (via IP headers): …";
    } else {
      msg = "Your current IP (via IP headers): " + ip +
        (remoteAddr ? " (remote " + remoteAddr + ")" : "");
      if (!ok) msg += " — blocked by allowlist";
    }
    // textContent only (IP is operator-influenced via headers).
    textEl.textContent = msg;
    if (el.classList) {
      el.classList.remove("is-valid");
      el.classList.remove("is-invalid");
      if (ip !== "") el.classList.add(ok ? "is-valid" : "is-invalid");
    }
  }

  function copyCurrentIP() {
    var d = CW.state.limits || {};
    var ip = String(d.clientIp == null ? "" : d.clientIp).replace(/^\s+|\s+$/g, "");
    if (!ip) {
      CW.toast("nothing to copy");
      return;
    }
    if (typeof navigator !== "undefined" && navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(ip).then(function () { CW.toast("copied", true); }, function () { CW.toast("copy failed"); });
    } else {
      var tmp = document.createElement("textarea");
      tmp.value = ip;
      document.body.appendChild(tmp);
      tmp.select();
      try { document.execCommand("copy"); CW.toast("copied", true); }
      catch (e) { CW.toast("copy failed"); }
      document.body.removeChild(tmp);
    }
  }

  function setInputs(d) {
    if (!d) return;
    var map = {
      "limit-global": d.globalRps,
      "limit-burst": d.burst,
      "limit-fetch": d.fetchRps,
      "limit-ingest": d.ingestRps
    };
    Object.keys(map).forEach(function (id) {
      var el = CW.$(id);
      if (el && map[id] != null) el.value = map[id];
    });
    var hips = CW.$("limit-ip-headers");
    if (hips && d.ipHeaders !== undefined) hips.value = headersToString(d.ipHeaders);
    var allowEl = CW.$("admin-allowed-ips");
    if (allowEl && d.adminAllowedIPs !== undefined) allowEl.value = ipsToString(d.adminAllowedIPs);
    var curIp = CW.$("admin-current-ip");
    if (curIp) {
      renderCurrentIP(curIp, d.clientIp, d.remoteAddr, d.adminAllowedIPs);
    }
  }

  function renderLimits(data) {
    if (!data) return;
    CW.state.limits = data;
    setInputs(data);
  }

  function loadLimits() {
    return CW.api("/api/v1/admin/limits").then(function (data) {
      CW.state.limits = data;
      setInputs(data);
      var el = CW.$("limits-result");
      // textContent only (no innerHTML), so no CW.esc needed here.
      if (el) {
        el.textContent = "current: global " + data.globalRps + "/s ceiling " +
          data.burst + " fetch " + data.fetchRps + "/s ingest " +
          data.ingestRps + "/s allowlist " +
          ipsToString(data.adminAllowedIPs) + " headers " +
          headersToString(data.ipHeaders) +
          ((data.clientIp != null && data.clientIp !== "") ? " your IP " + data.clientIp : "");
      }
      return data;
    }, function (err) {
      var el = CW.$("limits-result");
      if (el) el.textContent = (err && err.message) || "load failed";
      throw err;
    });
  }

  function numOr(id, fallback) {
    var el = CW.$(id);
    if (!el || el.value === "" || el.value == null) return fallback;
    var v = parseInt(el.value, 10);
    return isFinite(v) ? v : NaN;
  }

  function currentVal(key, fallback) {
    if (CW.state.limits && CW.state.limits[key] != null) return CW.state.limits[key];
    return fallback;
  }

  function putLimits(body, resElId) {
    return CW.apiMut("PUT", "/api/v1/admin/limits", body).then(function (out) {
      var resEl = CW.$(resElId || "limits-result");
      if (out.status === 200) {
        renderLimits(out.data);
        if (resEl) resEl.textContent = "limits saved";
        CW.toast("limits saved", true);
        return loadLimits().catch(function () {});
      }
      var fail = "save failed (" + out.status + "): " + CW.serverMessage(out.data);
      if (resEl) resEl.textContent = fail;
      CW.toast(fail);
      return out;
    });
  }

  function saveLimits(ev) {
    if (ev) ev.preventDefault();
    var hipsEl = CW.$("limit-ip-headers");
    var hipsInForm = hipsEl && hipsEl.closest ? hipsEl.closest("#limits-form") : null;
    var stateHeaders = currentVal("ipHeaders", null);
    var stateAllowed = currentVal("adminAllowedIPs", null);
    var allowEl = CW.$("admin-allowed-ips");
    var allowInForm = allowEl && allowEl.closest ? allowEl.closest("#limits-form") : null;
    var body = {
      globalRps: numOr("limit-global", currentVal("globalRps", NaN)),
      burst: numOr("limit-burst", currentVal("burst", NaN)),
      fetchRps: numOr("limit-fetch", currentVal("fetchRps", NaN)),
      ingestRps: numOr("limit-ingest", currentVal("ingestRps", NaN)),
      adminAllowedIPs: allowInForm ? stringToIPs(allowEl.value) : (Array.isArray(stateAllowed) ? stateAllowed : stringToIPs(allowEl && allowEl.value)),
      ipHeaders: hipsInForm ? stringToHeaders(hipsEl.value) : (Array.isArray(stateHeaders) ? stateHeaders : stringToHeaders(hipsEl && hipsEl.value))
    };
    var fields = ["globalRps", "burst", "fetchRps", "ingestRps"];
    for (var i = 0; i < fields.length; i++) {
      var v = body[fields[i]];
      if (!isFinite(v) || v < 1 || v > 10000) {
        var msg = fields[i] + " must be 1..10000";
        CW.$("limits-result").textContent = msg;
        CW.toast(msg);
        return Promise.resolve();
      }
    }
    // Empty header list is allowed: the server resets to defaults.
    if (body.ipHeaders.length > 10) {
      var tooMany = "ipHeaders must hold 1..10 entries";
      CW.$("limits-result").textContent = tooMany;
      CW.toast(tooMany);
      return Promise.resolve();
    }
    for (var h = 0; h < body.ipHeaders.length; h++) {
      if (!IP_HEADER_RE.test(body.ipHeaders[h])) {
        var bad = "ipHeaders entry " + body.ipHeaders[h] + " must be 1..64 chars of [A-Za-z0-9-]";
        CW.$("limits-result").textContent = bad;
        CW.toast(bad);
        return Promise.resolve();
      }
    }
    return putLimits(body, "limits-result");
  }

  function saveAdminLimit(ev) {
    if (ev) ev.preventDefault();
    var allowEl = CW.$("admin-allowed-ips");
    var allowed = stringToIPs(allowEl && allowEl.value);
    var resEl = CW.$("settings-limits-result");
    function fail(msg) {
      if (resEl) resEl.textContent = msg;
      CW.toast(msg);
      return Promise.resolve();
    }
    if (allowed.length > 32) {
      return fail("adminAllowedIPs must hold ≤32 entries");
    }
    for (var a = 0; a < allowed.length; a++) {
      if (!/^[ -~]+$/.test(allowed[a])) {
        return fail("adminAllowedIPs entry must be non-empty printable");
      }
    }
    var headers = stringToHeaders(CW.$("limit-ip-headers") && CW.$("limit-ip-headers").value);
    if (headers.length > 10) {
      return fail("ipHeaders must hold 1..10 entries");
    }
    for (var h = 0; h < headers.length; h++) {
      if (!IP_HEADER_RE.test(headers[h])) {
        return fail("ipHeaders entry " + headers[h] + " must be 1..64 chars of [A-Za-z0-9-]");
      }
    }
    function send() {
      var cur = CW.state.limits || {};
      var body = {
        globalRps: cur.globalRps,
        burst: cur.burst,
        fetchRps: cur.fetchRps,
        ingestRps: cur.ingestRps,
        adminAllowedIPs: allowed,
        ipHeaders: headers
      };
      var fields = ["globalRps", "burst", "fetchRps", "ingestRps"];
      for (var i = 0; i < fields.length; i++) {
        var v = body[fields[i]];
        if (!isFinite(v) || v < 1 || v > 10000) {
          return loadLimits().then(function (fresh) {
            return putLimits({
              globalRps: fresh.globalRps,
              burst: fresh.burst,
              fetchRps: fresh.fetchRps,
              ingestRps: fresh.ingestRps,
              adminAllowedIPs: allowed,
              ipHeaders: headers
            }, "settings-limits-result");
          });
        }
      }
      return putLimits(body, "settings-limits-result");
    }
    if (!CW.state.limits) {
      return loadLimits().then(send, send);
    }
    return send();
  }

  CW.renderLimits = renderLimits;
  CW.loadLimits = loadLimits;
  CW.saveLimits = saveLimits;
  CW.saveAdminLimit = saveAdminLimit;
  CW.copyCurrentIP = copyCurrentIP;
})();
