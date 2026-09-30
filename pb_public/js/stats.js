/* ConfigWire admin shell — stats.js. */
(function () {
  "use strict";

  var CW = window.CW;

  var SERIES_W = 560;
  var SERIES_H = 220;
  var SERIES_PAD_L = 36;
  var SERIES_PAD_R = 8;
  var SERIES_PAD_T = 8;
  var SERIES_PAD_B = 22;

  var lastGeom = null;

  function r1(v) {
    return Math.round(v * 10) / 10;
  }

  function fmtDay(d) {
    var s = String(d == null ? "" : d);
    var m = /^(\d{4})-(\d{2})-(\d{2})/.exec(s);
    if (m) return m[2] + "-" + m[3];
    return s.slice(0, 5) || s;
  }

  function getMonotoneCubicSegments(pts, baseY) {
    var n = pts.length;
    if (n < 2) return [];
    if (n === 2) {
      var dx = pts[1].x - pts[0].x;
      return [{
        p1: pts[0],
        c1: { x: pts[0].x + dx / 3, y: pts[0].y + (pts[1].y - pts[0].y) / 3 },
        c2: { x: pts[0].x + 2 * dx / 3, y: pts[0].y + 2 * (pts[1].y - pts[0].y) / 3 },
        p2: pts[1]
      }];
    }
    var dx = [];
    var dy = [];
    var secants = [];
    for (var i = 0; i < n - 1; i++) {
      var h = pts[i + 1].x - pts[i].x;
      var d = pts[i + 1].y - pts[i].y;
      dx.push(h);
      dy.push(d);
      secants.push(h === 0 ? 0 : d / h);
    }
    var m = [secants[0]];
    for (var j = 1; j < n - 1; j++) {
      if (secants[j - 1] * secants[j] <= 0) {
        m.push(0);
      } else {
        m.push((secants[j - 1] + secants[j]) / 2);
      }
    }
    m.push(secants[n - 2]);

    for (var k = 0; k < n - 1; k++) {
      if (dy[k] === 0) {
        m[k] = 0;
        m[k + 1] = 0;
      } else {
        var alpha = m[k] / secants[k];
        var beta = m[k + 1] / secants[k];
        if (alpha < 0) m[k] = 0;
        if (beta < 0) m[k + 1] = 0;
        var dist = alpha * alpha + beta * beta;
        if (dist > 9) {
          var tau = 3 / Math.sqrt(dist);
          m[k] = tau * alpha * secants[k];
          m[k + 1] = tau * beta * secants[k];
        }
      }
    }

    var segments = [];
    for (var s = 0; s < n - 1; s++) {
      var p1 = pts[s];
      var p2 = pts[s + 1];
      var sh = dx[s];
      var c1x = p1.x + sh / 3;
      var c1y = p1.y + m[s] * sh / 3;
      var c2x = p2.x - sh / 3;
      var c2y = p2.y - m[s + 1] * sh / 3;

      if (baseY !== undefined) {
        if (c1y > baseY) c1y = baseY;
        if (c2y > baseY) c2y = baseY;
        if (c1y < SERIES_PAD_T) c1y = SERIES_PAD_T;
        if (c2y < SERIES_PAD_T) c2y = SERIES_PAD_T;
      }
      segments.push({
        p1: p1,
        c1: { x: c1x, y: c1y },
        c2: { x: c2x, y: c2y },
        p2: p2
      });
    }
    return segments;
  }

  function segmentsToPath(pts, segments) {
    if (!pts || !pts.length) return "";
    var d = "M" + r1(pts[0].x) + " " + r1(pts[0].y);
    if (!segments || !segments.length) return d;
    if (pts.length < 3) {
      for (var k = 1; k < pts.length; k++) d += "L" + r1(pts[k].x) + " " + r1(pts[k].y);
      return d;
    }
    for (var i = 0; i < segments.length; i++) {
      var seg = segments[i];
      d += "C" + r1(seg.c1.x) + " " + r1(seg.c1.y) + " " + r1(seg.c2.x) + " " + r1(seg.c2.y) + " " + r1(seg.p2.x) + " " + r1(seg.p2.y);
    }
    return d;
  }

  function segmentsToReversePath(pts, segments) {
    if (!pts || !pts.length) return "";
    var d = "";
    if (pts.length < 3) {
      for (var k = pts.length - 2; k >= 0; k--) d += "L" + r1(pts[k].x) + " " + r1(pts[k].y);
      return d;
    }
    for (var i = segments.length - 1; i >= 0; i--) {
      var seg = segments[i];
      d += "C" + r1(seg.c2.x) + " " + r1(seg.c2.y) + " " + r1(seg.c1.x) + " " + r1(seg.c1.y) + " " + r1(seg.p1.x) + " " + r1(seg.p1.y);
    }
    return d;
  }

  function verHue(i) {
    var h = (275 + (i < 0 ? 0 : i) * 137.508) % 360;
    if (h > 160 && h < 230) h = (h + 70) % 360;
    return Math.round(h);
  }

  function verColor(i) {
    return "hsl(" + verHue(i) + ", 75%, 62%)";
  }

  function verGradient(i) {
    var h = verHue(i);
    return "linear-gradient(90deg, hsl(" + h + ", 75%, 58%), hsl(" + h + ", 85%, 72%))";
  }

  function seriesTipText(p) {
    var tot = (p.fetches || 0) + (p.exposures || 0);
    var fetchPart = p.fetches + " fetches";
    if (p && p.versions && typeof p.versions === "object") {
      var ks = Object.keys(p.versions).sort(function (a, b) { return Number(a) - Number(b); });
      if (ks.length) {
        fetchPart += " (" + ks.map(function (k) { return "v" + k + ": " + p.versions[k]; }).join(", ") + ")";
      }
    }
    return p.day + " — " + fetchPart + " · " + p.exposures + " exposures · total " + tot;
  }

  function renderSeries(series) {
    lastGeom = null;
    if (!series || !series.length) {
      return '<p class="muted stats-series-empty">No daily activity in this window.</p>';
    }
    var pts = series.map(function (p) {
      var f = Number(p && p.fetches) || 0;
      var e = Number(p && p.exposures) || 0;
      if (f < 0) f = 0;
      if (e < 0) e = 0;
      var day = p && p.day !== undefined && p.day !== null ? String(p.day) : "";
      var vers = {};
      var hasVer = false;
      if (p && p.versions && typeof p.versions === "object") {
        Object.keys(p.versions).forEach(function (k) {
          var c = Number(p.versions[k]) || 0;
          if (c < 0) c = 0;
          vers[String(k)] = c;
          hasVer = true;
        });
      }
      return { day: day, fetches: f, exposures: e, versions: hasVer ? vers : null };
    });
    var vKeys = [];
    var seen = {};
    pts.forEach(function (p) {
      if (!p.versions) return;
      Object.keys(p.versions).forEach(function (k) {
        if (!seen[k]) { seen[k] = true; vKeys.push(k); }
      });
    });
    vKeys.sort(function (a, b) { return Number(a) - Number(b); });
    var max = 1;
    var has = false;
    var sumF = 0;
    var sumE = 0;
    pts.forEach(function (p) {
      var t = p.fetches + p.exposures;
      if (t > max) max = t;
      if (t > 0) has = true;
      sumF += p.fetches;
      sumE += p.exposures;
    });
    if (!has) {
      return '<p class="muted stats-series-empty">No daily activity in this window.</p>';
    }
    var n = pts.length;
    var plotW = SERIES_W - SERIES_PAD_L - SERIES_PAD_R;
    var plotH = SERIES_H - SERIES_PAD_T - SERIES_PAD_B;
    var baseY = SERIES_PAD_T + plotH;
    function sx(i) {
      if (n === 1) return SERIES_PAD_L + plotW / 2;
      return SERIES_PAD_L + plotW * i / (n - 1);
    }
    function sy(v) {
      return SERIES_PAD_T + plotH - v / max * plotH;
    }
    // Single-day windows paint a flat band across the plot so the lone
    // point still fills an area instead of collapsing to zero width.
    var gx = n === 1 ? [SERIES_PAD_L, SERIES_PAD_L + plotW] : pts.map(function (p, i) { return sx(i); });
    var fVals = n === 1 ? [pts[0].fetches, pts[0].fetches] : pts.map(function (p) { return p.fetches; });
    var tVals = n === 1 ? [pts[0].fetches + pts[0].exposures, pts[0].fetches + pts[0].exposures] :
      pts.map(function (p) { return p.fetches + p.exposures; });
    var fetchPts = gx.map(function (x, i) { return { x: x, y: sy(fVals[i]) }; });
    var topPts = gx.map(function (x, i) { return { x: x, y: sy(tVals[i]) }; });
    var fetchSegs = getMonotoneCubicSegments(fetchPts, baseY);
    var topSegs = getMonotoneCubicSegments(topPts, baseY);
    var fetchLine = segmentsToPath(fetchPts, fetchSegs);
    var topLine = segmentsToPath(topPts, topSegs);
    var fetchD = fetchLine + "L" + r1(gx[gx.length - 1]) + " " + r1(baseY) +
      "L" + r1(gx[0]) + " " + r1(baseY) + "Z";
    var back = segmentsToReversePath(fetchPts, fetchSegs);
    var expoD = topLine + "L" + r1(fetchPts[fetchPts.length - 1].x) + " " + r1(fetchPts[fetchPts.length - 1].y) +
      back + "Z";
    var ticks = [0, Math.ceil(max / 3), Math.ceil(max * 2 / 3), max].filter(function (v, i, a) {
      return a.indexOf(v) === i;
    });
    var grid = ticks.map(function (v) {
      var y = r1(sy(v));
      return '<line class="stats-grid" x1="' + SERIES_PAD_L + '" x2="' + (SERIES_W - SERIES_PAD_R) +
        '" y1="' + y + '" y2="' + y + '"></line>' +
        '<text class="stats-tick" x="' + (SERIES_PAD_L - 6) + '" y="' + r1(y + 3) +
        '" text-anchor="end">' + CW.esc(v) + "</text>";
    }).join("");
    var step = n <= 7 ? 1 : 7;
    var xIdx = [];
    for (var xi = 0; xi < n; xi += step) xIdx.push(xi);
    if (xIdx[xIdx.length - 1] !== n - 1) xIdx.push(n - 1);
    var xLabels = xIdx.map(function (i) {
      var anchor = (i === 0 && n > 1) ? "start" : (i === n - 1 && n > 1) ? "end" : "middle";
      return '<text class="stats-x" x="' + r1(sx(i)) + '" y="' + (SERIES_H - 6) +
        '" text-anchor="' + anchor + '">' + CW.esc(fmtDay(pts[i].day)) + "</text>";
    }).join("");
    var firstL = fmtDay(pts[0].day);
    var lastL = fmtDay(pts[n - 1].day);
    var range = n === 1 ? firstL : firstL + " to " + lastL;
    var aria = "Daily fetches and exposures for " + n + (n === 1 ? " day, " : " days, ") + range + ", peak " + max;
    if (vKeys.length) aria += ", versions " + vKeys.map(function (k) { return "v" + k; }).join(", ");
    var dayWord = n === 1 ? " day" : " days";
    var hoverX = r1(sx(0));
    var defs =
      '<defs>' +
      '<linearGradient id="cw-stat-fetch-grad" x1="0" y1="0" x2="0" y2="1">' +
      '<stop offset="0%" stop-color="#3b82f6" stop-opacity="0.35"/>' +
      '<stop offset="100%" stop-color="#3b82f6" stop-opacity="0.02"/>' +
      '</linearGradient>' +
      '<linearGradient id="cw-stat-expo-grad" x1="0" y1="0" x2="0" y2="1">' +
      '<stop offset="0%" stop-color="#5eead4" stop-opacity="0.35"/>' +
      '<stop offset="100%" stop-color="#5eead4" stop-opacity="0.02"/>' +
      '</linearGradient>' +
      '</defs>';
    var verPaths = vKeys.map(function (vk, vi) {
      var color = verColor(vi);
      var vVals = n === 1 ?
        [pts[0].versions ? (pts[0].versions[vk] || 0) : 0, pts[0].versions ? (pts[0].versions[vk] || 0) : 0] :
        pts.map(function (p) { return p.versions ? (p.versions[vk] || 0) : 0; });
      var vPts = gx.map(function (x, i) { return { x: x, y: sy(vVals[i]) }; });
      var vSegs = getMonotoneCubicSegments(vPts, baseY);
      var vLine = segmentsToPath(vPts, vSegs);
      var vTotal = vVals.reduce(function (a, b) { return a + b; }, 0);
      if (n === 1) vTotal = vVals[0];
      return '<path class="stats-line-ver stats-line-ver-' + CW.esc(String(vi)) + '" data-version="' + CW.esc(String(vk)) +
        '" d="' + vLine + '" fill="none" stroke="' + color + '" stroke-width="1.5" opacity=".85"><title>' +
        CW.esc("v" + vk + " " + vTotal + " total") + "</title></path>";
    }).join("");
    var svg = '<svg class="stats-series" role="img" aria-label="' + CW.esc(aria) +
      '" viewBox="0 0 ' + SERIES_W + " " + SERIES_H + '" preserveAspectRatio="xMidYMid meet">' +
      defs + grid + xLabels +
      '<path class="stats-area-fetch" d="' + fetchD + '" fill="url(#cw-stat-fetch-grad)" stroke="none"><title>' +
      CW.esc("fetches " + sumF + " total over " + n + dayWord) + "</title></path>" +
      '<path class="stats-area-exposure" d="' + expoD + '" fill="url(#cw-stat-expo-grad)" stroke="none"><title>' +
      CW.esc("exposures " + sumE + " total over " + n + dayWord) + "</title></path>" +
      '<path class="stats-line-fetch" d="' + fetchLine + '" fill="none" stroke="#3b82f6" stroke-width="3.5" opacity=".9"></path>' +
      '<path class="stats-line-exposure" d="' + topLine + '" fill="none" stroke="#5eead4" stroke-width="3.5" opacity="' + (sumE > 0 ? '.9' : '0') + '"></path>' +
      verPaths +
      '<line class="stats-hover" x1="' + hoverX + '" y1="' + SERIES_PAD_T + '" x2="' + hoverX + '" y2="' + baseY + '" visibility="hidden"></line>' +
      '<circle class="stats-dot-fetch" r="4" cx="' + hoverX + '" cy="' + r1(sy(pts[0].fetches)) + '" visibility="hidden"></circle>' +
      '<circle class="stats-dot-exposure" r="4" cx="' + hoverX + '" cy="' + r1(sy(pts[0].fetches + pts[0].exposures)) + '" visibility="hidden"></circle>' +
      '<rect class="stats-hit" x="' + SERIES_PAD_L + '" y="' + SERIES_PAD_T + '" width="' + plotW + '" height="' + plotH + '" fill="transparent"></rect>' +
      "</svg>";
    lastGeom = { pts: pts, n: n, max: max };
    var verLegend = vKeys.map(function (vk, vi) {
      return '<span class="stats-legend-item stats-legend-ver"><span class="stats-legend-dot" style="background: ' +
        verColor(vi) + ';"></span>' + CW.esc("v" + vk) + "</span>";
    }).join("");
    var verTotals = vKeys.map(function (vk) {
      var tot = 0;
      pts.forEach(function (p) { if (p.versions) tot += Number(p.versions[vk]) || 0; });
      return tot;
    });
    var legend =
      '<div class="stats-series-legend">' +
      '<span class="stats-legend-item"><span class="stats-legend-dot stats-dot-fetch-bg"></span>fetches: <strong>' + sumF + '</strong></span>' +
      '<span class="stats-legend-item"><span class="stats-legend-dot stats-dot-exposure-bg"></span>exposures: <strong>' + sumE + '</strong></span>' +
      verLegend +
      '</div>';
    var noteText = "Thick blue = total fetches per day (" + sumF + " total)";
    if (sumE > 0) noteText += " · Teal = exposures stacked on fetches (" + sumE + " total)";
    else noteText += " · No exposures in this window";
    if (vKeys.length) {
      noteText += " · Thin lines = fetches per version (" +
        vKeys.map(function (vk, vi) { return "v" + vk + ": " + verTotals[vi]; }).join(", ") +
        "); total fetches = sum of versions";
    }
    var noteHtml = '<p class="muted stats-series-note">' + CW.esc(noteText) + "</p>";
    return '<div class="stats-series-wrap" tabindex="0" data-count="' + n + '" data-peak="' + max + '">' +
      legend + svg + '<div class="stats-tip" hidden></div>' + noteHtml + "</div>";
  }

  function bindSeriesTip() {
    var view = CW.$("stats-view");
    if (!view || !view.querySelector) return;
    var wrap = view.querySelector(".stats-series-wrap");
    if (!wrap || !wrap.querySelector || !wrap.addEventListener) return;
    var g = lastGeom;
    if (!g || !g.pts || !g.pts.length) return;
    var svg = wrap.querySelector("svg");
    var tip = wrap.querySelector(".stats-tip");
    var line = wrap.querySelector(".stats-hover");
    var dotF = wrap.querySelector(".stats-dot-fetch");
    var dotE = wrap.querySelector(".stats-dot-exposure");
    var hit = wrap.querySelector(".stats-hit");
    if (!svg || !tip || !hit) return;
    var n = g.pts.length;
    var plotW = SERIES_W - SERIES_PAD_L - SERIES_PAD_R;
    var plotH = SERIES_H - SERIES_PAD_T - SERIES_PAD_B;
    var cur = -1;
    function hx(i) {
      if (n === 1) return SERIES_PAD_L + plotW / 2;
      return SERIES_PAD_L + plotW * i / (n - 1);
    }
    function hy(v) {
      return SERIES_PAD_T + plotH - v / g.max * plotH;
    }
    function setVis(el, vis) {
      if (el && el.setAttribute) el.setAttribute("visibility", vis ? "visible" : "hidden");
    }
    function showAt(i) {
      if (i < 0) i = 0;
      if (i > n - 1) i = n - 1;
      cur = i;
      var p = g.pts[i];
      var x = hx(i);
      if (line && line.setAttribute) {
        line.setAttribute("x1", String(r1(x)));
        line.setAttribute("x2", String(r1(x)));
        line.setAttribute("visibility", "visible");
      }
      if (dotF && dotF.setAttribute) {
        dotF.setAttribute("cx", String(r1(x)));
        dotF.setAttribute("cy", String(r1(hy(p.fetches))));
        dotF.setAttribute("visibility", "visible");
      }
      if (dotE && dotE.setAttribute) {
        dotE.setAttribute("cx", String(r1(x)));
        dotE.setAttribute("cy", String(r1(hy(p.fetches + p.exposures))));
        dotE.setAttribute("visibility", "visible");
      }
      tip.textContent = seriesTipText(p);
      tip.hidden = false;
      if (tip.style) {
        var pct = x / SERIES_W * 100;
        if (x > SERIES_W / 2) {
          tip.style.left = "auto";
          tip.style.right = (100 - pct) + "%";
        } else {
          tip.style.right = "auto";
          tip.style.left = pct + "%";
        }
        tip.style.top = "0";
      }
    }
    function hide() {
      cur = -1;
      tip.hidden = true;
      setVis(line, false);
      setVis(dotF, false);
      setVis(dotE, false);
    }
    function idxFromEvent(ev) {
      try {
        var r = svg.getBoundingClientRect ? svg.getBoundingClientRect() : null;
        if (!r || !r.width) return 0;
        var s = (ev.clientX - r.left) * SERIES_W / r.width;
        if (n === 1) return 0;
        return Math.round((s - SERIES_PAD_L) / plotW * (n - 1));
      } catch (e) { return 0; }
    }
    hit.addEventListener("mousemove", function (ev) { showAt(idxFromEvent(ev)); });
    hit.addEventListener("mouseleave", hide);
    svg.addEventListener("mouseleave", hide);
    wrap.addEventListener("focus", function () { if (cur < 0) showAt(0); });
    wrap.addEventListener("blur", hide);
    wrap.addEventListener("keydown", function (ev) {
      var k = ev && ev.key;
      if (k === "ArrowRight") { showAt(cur < 0 ? 0 : cur + 1); if (ev.preventDefault) ev.preventDefault(); }
      else if (k === "ArrowLeft") { showAt(cur < 0 ? 0 : cur - 1); if (ev.preventDefault) ev.preventDefault(); }
      else if (k === "Home") { showAt(0); if (ev.preventDefault) ev.preventDefault(); }
      else if (k === "End") { showAt(n - 1); if (ev.preventDefault) ev.preventDefault(); }
      else if (k === "Escape") { hide(); }
    });
  }

  function renderVerBars(perVersion, vKeys) {
    if (!vKeys || !vKeys.length) return "";
    var counts = vKeys.map(function (v) { return Number(perVersion[v]) || 0; });
    var maxV = 0;
    counts.forEach(function (c) { if (c > maxV) maxV = c; });
    if (maxV <= 0) return "";
    var rows = vKeys.map(function (v, i) {
      var count = counts[i] < 0 ? 0 : counts[i];
      var pctText = (count / maxV * 100).toFixed(1);
      var color = verGradient(i);
      return '<div class="stats-ver-row stats-bar-row"><span class="stats-ver-label stats-bar-label">' + CW.esc("v" + v) +
        '</span><span class="stats-ver-track stats-bar-track" role="img" aria-label="' + CW.esc("v" + v) + " " + CW.esc(pctText) + '%">' +
        '<span class="stats-ver-fill stats-bar-fill" style="width: ' + pctText + '%; background: ' + color + ';"></span></span>' +
        '<span class="stats-ver-pct stats-bar-pct">' + CW.esc(count) + " (" + CW.esc(pctText) + '%)</span></div>';
    }).join("");
    return '<div class="stats-ver"><h4 class="stats-ver-title">versions</h4>' + rows + "</div>";
  }

  function renderStats(data) {
    // Percentages are display-only: counts are never recomputed from rates.
    // Charts are display-only too: div widths + conic-gradient stops derived
    // from perVariant/exposures shares, never recomputed counts.
    CW.state.lastStats = data;
    try {
      CW.state.lastStatsText = JSON.stringify(data, null, 2);
    } catch (e) { CW.state.lastStatsText = String(data); }
    var exposures = Number(data.exposures) || 0;
    var fetches = Number(data.fetches) || 0;
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
    // True empty (no fetches AND no exposures): deliberate onboarding
    // state — muted tiles + one guidance line, secondary copy.
    // Fetch is env-wide.
    var isEmpty = fetches === 0 && exposures === 0;
    var html;
    if (isEmpty) {
      html =
        '<div class="stats-tiles" role="group" aria-label="Current totals">' +
        '<div class="stats-tile"><span class="stats-tile-num">' + CW.esc(data.version) + '</span><span class="stats-tile-label">version</span></div>' +
        '<div class="stats-tile"><span class="stats-tile-num">' + CW.esc(data.fetches) + '</span><span class="stats-tile-label">fetches</span></div>' +
        '<div class="stats-tile"><span class="stats-tile-num">' + CW.esc(data.exposures) + '</span><span class="stats-tile-label">exposures</span></div>' +
        "</div>" +
        '<p class="stats-guide">No stats yet — publish a release, fetch via SDK, then post an exposure event.</p>';
    } else {
      var perVersion = data.perVersion || {};
      var vKeys = Object.keys(perVersion).sort(function (a, b) { return Number(a) - Number(b); });
      var vSplit = vKeys.map(function (v) {
        return "v" + CW.esc(v) + ": " + CW.esc(perVersion[v]);
      }).join(", ") || "(no version data)";
      var seriesHtml = renderSeries(data.series);
      var verHtml = renderVerBars(perVersion, vKeys);
      html =
        '<div class="stats-counts">' +
        '<p class="stats-count">version: <strong>' + CW.esc(data.version) + "</strong></p>" +
        '<p class="stats-count">fetches: <strong>' + CW.esc(data.fetches) + "</strong></p>" +
        '<p class="stats-count">exposures: <strong>' + CW.esc(data.exposures) + "</strong></p>" +
        '</div>' +
        '<p class="stats-split">split: ' + split + "</p>" +
        '<p class="stats-split">versions: ' + vSplit + "</p>" +
        chart +
        seriesHtml +
        verHtml;
    }
    if (data.approximate) html += '<p class="muted">approximate</p>';
    html += '<p class="stats-copy-row' + (isEmpty ? " is-secondary" : "") + '"><button type="button" id="stats-copy" class="btn ghost">Copy JSON</button> ' +
      '<span id="stats-copy-status" class="muted" role="status"></span></p>';
    if (CW.state.lastStatsError) html += "<p role=\"alert\">" + CW.esc(CW.state.lastStatsError) + "</p>";
    CW.$("stats-view").innerHTML = html;
    bindSeriesTip();
  }

  function copyStatsJson() {
    var status = CW.$("stats-copy-status");
    function say(msg) {
      if (status) status.textContent = msg;
      else CW.toast(msg);
    }
    var text = CW.state.lastStatsText || "";
    if (!text) { say("nothing to copy"); return; }
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

  function loadStats() {
    var sinceEl = CW.$("stats-since");
    var sinceRaw = sinceEl && sinceEl.value != null ? String(sinceEl.value) : "";
    var since = sinceRaw !== "" ? sinceRaw : "7d";
    var url = "/api/v1/admin/env/" + encodeURIComponent(CW.envSlug()) + "/stats?since=" +
      encodeURIComponent(since);
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
  CW.bindSeriesTip = bindSeriesTip;
  CW.seriesTipText = seriesTipText;
  CW.copyStatsJson = copyStatsJson;
  CW.loadStats = loadStats;
})();
