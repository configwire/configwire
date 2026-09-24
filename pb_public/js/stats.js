/* ConfigWire admin shell — stats.js. */
(function () {
  "use strict";

  var CW = window.CW;

  function renderStats(data) {
    // Percentages are display-only: counts are never recomputed from rates.
    // Charts are display-only too: div widths + conic-gradient stops derived
    // from perVariant/exposures shares, never recomputed counts.
    CW.state.lastStats = data;
    try {
      CW.state.lastStatsText = JSON.stringify(data, null, 2);
    } catch (e) { CW.state.lastStatsText = String(data); }
    var exposures = Number(data.exposures) || 0;
    var perVariant = data.perVariant || {};
    var keys = Object.keys(perVariant);
    var palette = ["var(--accent)", "var(--ok)", "var(--warn)", "var(--danger)", "var(--muted)"];
    var entries = keys.map(function (v, i) {
      var label = v === "" ? "(empty)" : v;
      var count = Number(perVariant[v]) || 0;
      var pct = exposures > 0 ? (count / exposures * 100) : 0;
      return { label: label, pct: pct, color: palette[i % palette.length] };
    });
    var split = keys.map(function (v) {
      var label = v === "" ? "(empty)" : v;
      var count = perVariant[v];
      if (exposures > 0) {
        var pct = (Number(count) / exposures * 100).toFixed(1);
        return CW.esc(label) + ": " + CW.esc(count) + " (" + CW.esc(pct) + "%)";
      }
      return CW.esc(label) + ": " + CW.esc(count);
    }).join(", ") || "(no exposures)";
    var chart = "";
    if (exposures > 0 && entries.length) {
      var bars = entries.map(function (e) {
        var pctText = e.pct.toFixed(1);
        return '<div class="stats-bar-row"><span class="stats-bar-label">' + CW.esc(e.label) +
          '</span><span class="stats-bar-track" role="img" aria-label="' + CW.esc(e.label) + " " + CW.esc(pctText) + '%">' +
          '<span class="stats-bar-fill" style="width: ' + pctText + '%; background: ' + e.color + ';"></span></span>' +
          '<span class="stats-bar-pct">' + CW.esc(pctText) + '%</span></div>';
      }).join("");
      var acc = 0;
      var stops = entries.map(function (e) {
        var s = acc;
        acc += e.pct;
        return e.color + " " + s.toFixed(1) + "% " + acc.toFixed(1) + "%";
      }).join(", ");
      var donutLabel = entries.map(function (e) { return e.label + " " + e.pct.toFixed(1) + "%"; }).join(", ");
      chart = '<div class="stats-chart"><div class="stats-bars">' + bars + "</div>" +
        '<div class="stats-donut" role="img" aria-label="Variant split: ' + CW.esc(donutLabel) +
        '" style="background: conic-gradient(' + stops + ');"></div></div>';
    } else {
      chart = '<div class="stats-chart is-empty"><p class="muted stats-empty">No exposures yet — chart appears after the first exposure.</p></div>';
    }
    var echo = data.echo || {};
    var echoFlag = echo.flag !== undefined && echo.flag !== null && echo.flag !== "" ? echo.flag : "(all)";
    var echoSince = echo.since || echo.horizon || "";
    var echoHtml = "flag " + CW.esc(echoFlag) + " · since " + CW.esc(echoSince) +
      " · cutoff " + CW.esc(echo.cutoff || "");
    // True empty (no exposures AND flag known): deliberate onboarding state —
    // muted tiles + one guidance line, quiet echo, secondary copy. Unknown-flag
    // zeros (flagFound:false) keep the legacy zero wall + prominent warning so
    // the two states never look alike; populated markup below is byte-identical.
    var isEmpty = exposures === 0 && data.flagFound !== false;
    var html;
    if (isEmpty) {
      html =
        '<div class="stats-tiles" role="group" aria-label="Current totals">' +
        '<div class="stats-tile"><span class="stats-tile-num">' + CW.esc(data.version) + '</span><span class="stats-tile-label">version</span></div>' +
        '<div class="stats-tile"><span class="stats-tile-num">' + CW.esc(data.fetches) + '</span><span class="stats-tile-label">fetches</span></div>' +
        '<div class="stats-tile"><span class="stats-tile-num">' + CW.esc(data.exposures) + '</span><span class="stats-tile-label">exposures</span></div>' +
        "</div>" +
        '<p class="stats-guide">No stats yet — publish a release, fetch via SDK, then post an exposure event.</p>' +
        '<p class="muted stats-echo-quiet">' + echoHtml + "</p>";
    } else {
      html =
        '<p class="stats-count">version: <strong>' + CW.esc(data.version) + "</strong></p>" +
        '<p class="stats-count">fetches: <strong>' + CW.esc(data.fetches) + "</strong></p>" +
        '<p class="stats-count">exposures: <strong>' + CW.esc(data.exposures) + "</strong></p>" +
        '<p class="stats-split">split: ' + split + "</p>" +
        chart +
        '<p class="muted">' + echoHtml + "</p>";
    }
    if (data.approximate) html += '<p class="muted">approximate</p>';
    if (data.flagFound === false) {
      html += "<p role=\"alert\">warning: unknown flag — showing zeros (flagFound:false).</p>";
    }
    html += '<p class="stats-copy-row' + (isEmpty ? " is-secondary" : "") + '"><button type="button" id="stats-copy" class="btn ghost">Copy JSON</button> ' +
      '<span id="stats-copy-status" class="muted" role="status"></span></p>';
    if (CW.state.lastStatsError) html += "<p role=\"alert\">" + CW.esc(CW.state.lastStatsError) + "</p>";
    CW.$("stats-view").innerHTML = html;
  }

  function copyStatsJson() {
    var status = CW.$("stats-copy-status");
    function say(msg) {
      if (status) status.textContent = msg;
      else CW.toast(msg);
    }
    var text = CW.state.lastStatsText || "";
    if (!text) { say("nothing to copy yet"); return; }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () { say("copied"); },
        function () { say("copy failed — select and copy manually"); });
    } else {
      var ta = document.createElement("textarea");
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand("copy"); say("copied"); }
      catch (e) { say("copy failed — select and copy manually"); }
      document.body.removeChild(ta);
    }
  }

  // Stats flag dropdown: populated from the already-loaded flags collection
  // (same perPage=200 superuser read as loadFlags — no new endpoint).
  function renderStatsFlagOptions() {
    var sel = CW.$("stats-flag");
    if (!sel) return;
    var cur = sel.value;
    var keys = CW.state.flags.map(function (f) { return f.key; })
      .filter(function (k) { return !!k; }).sort();
    var html = '<option value="">All flags</option>' + keys.map(function (k) {
      return '<option value="' + CW.esc(k) + '">' + CW.esc(k) + "</option>";
    }).join("");
    // Keep a stale/unknown selection visible so its warning round-trips.
    if (cur && keys.indexOf(cur) === -1) {
      html += '<option value="' + CW.esc(cur) + '" selected>' + CW.esc(cur) + "</option>";
    }
    sel.innerHTML = html;
    if (!cur) sel.value = "";
    else if (keys.indexOf(cur) !== -1) sel.value = cur;
  }

  function loadStats() {
    var flagEl = CW.$("stats-flag");
    var sinceEl = CW.$("stats-since");
    var flag = flagEl && flagEl.value != null ? String(flagEl.value) : "";
    var sinceRaw = sinceEl && sinceEl.value != null ? String(sinceEl.value) : "";
    var since = sinceRaw !== "" ? sinceRaw : "7d";
    var url = "/api/v1/admin/env/" + encodeURIComponent(CW.envSlug()) + "/stats?since=" +
      encodeURIComponent(since);
    if (flag) url += "&flag=" + encodeURIComponent(flag);
    if (CW.state.projectId) url += "&project=" + encodeURIComponent(CW.state.projectId);
    if (!CW.state.lastStats) CW.$("stats-view").textContent = "Loading…";
    CW.state.lastStatsError = "";
    return CW.api(url).then(function (data) {
      CW.state.lastStatsError = "";
      renderStats(data);
      return data;
    }, function (err) {
      var msg = err && err.message ? String(err.message).split("\n")[0] : "stats load failed";
      CW.state.lastStatsError = msg;
      if (CW.state.lastStats) renderStats(CW.state.lastStats);
      else CW.$("stats-view").innerHTML = "<p>Loading… failed.</p><p role=\"alert\">" + CW.esc(msg) + "</p>";
      throw err;
    });
  }

  CW.renderStats = renderStats;
  CW.copyStatsJson = copyStatsJson;
  CW.loadStats = loadStats;
  CW.renderStatsFlagOptions = renderStatsFlagOptions;
})();
