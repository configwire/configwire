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

  function setInputs(d) {
    if (!d) return;
    var map = {
      "limit-global": d.globalRps,
      "limit-burst": d.burst,
      "limit-fetch": d.fetchRps,
      "limit-ingest": d.ingestRps,
      "limit-admin": d.adminRps
    };
    Object.keys(map).forEach(function (id) {
      var el = CW.$(id);
      if (el && map[id] != null) el.value = map[id];
    });
    var hips = CW.$("limit-ip-headers");
    if (hips && d.ipHeaders !== undefined) hips.value = headersToString(d.ipHeaders);
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
        el.textContent = "current: global " + data.globalRps + "/s burst " +
          data.burst + " fetch " + data.fetchRps + "/s ingest " +
          data.ingestRps + "/s admin " + data.adminRps + "/s headers " +
          headersToString(data.ipHeaders);
      }
      return data;
    }, function (err) {
      var el = CW.$("limits-result");
      if (el) el.textContent = (err && err.message) || "load failed";
      throw err;
    });
  }

  function saveLimits(ev) {
    if (ev) ev.preventDefault();
    function num(id) {
      var el = CW.$(id);
      if (!el) return NaN;
      return parseInt(el.value, 10);
    }
    var body = {
      globalRps: num("limit-global"),
      burst: num("limit-burst"),
      fetchRps: num("limit-fetch"),
      ingestRps: num("limit-ingest"),
      adminRps: num("limit-admin"),
      ipHeaders: stringToHeaders(CW.$("limit-ip-headers") && CW.$("limit-ip-headers").value)
    };
    var fields = ["globalRps", "burst", "fetchRps", "ingestRps", "adminRps"];
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
    return CW.apiMut("PUT", "/api/v1/admin/limits", body).then(function (out) {
      var resEl = CW.$("limits-result");
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

  CW.renderLimits = renderLimits;
  CW.loadLimits = loadLimits;
  CW.saveLimits = saveLimits;
})();
