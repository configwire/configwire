/* ConfigWire admin shell — router.js (hash router + home/detail views). */
(function () {
  "use strict";

  var CW = window.CW;

  // ---- hash router: #/ => home, #/p/<id>[#anchor] => detail ----

  function parseHash() {
    var h = window.location.hash || "";
    if (h === "#/p/settings" || h.indexOf("#/p/settings#") === 0 || h.indexOf("#/p/settings/") === 0) {
      var srest = h.slice("#/p/settings".length);
      var sanchor = "";
      if (srest.charAt(0) === "#") sanchor = srest.slice(1);
      else if (srest.charAt(0) === "/") sanchor = srest.slice(1);
      return { view: "settings", id: "", anchor: sanchor };
    }
    if (h === "#/p/account" || h.indexOf("#/p/account#") === 0 || h.indexOf("#/p/account/") === 0) {
      var arest = h.slice("#/p/account".length);
      var aanchor = "";
      if (arest.charAt(0) === "#") aanchor = arest.slice(1);
      else if (arest.charAt(0) === "/") aanchor = arest.slice(1);
      return { view: "account", id: "", anchor: aanchor };
    }
    if (h.indexOf("#/p/") === 0) {
      var rest = h.slice("#/p/".length);
      var anchor = "";
      var hi = rest.indexOf("#");
      if (hi !== -1) { anchor = rest.slice(hi + 1); rest = rest.slice(0, hi); }
      // Allow a trailing "/<anchor>" form too.
      var si = rest.indexOf("/");
      if (si !== -1 && !anchor) { anchor = rest.slice(si + 1); rest = rest.slice(0, si); }
      return { view: "detail", id: decodeURIComponent(rest), anchor: anchor };
    }
    return { view: "home", id: "", anchor: "" };
  }

  function projectById(id) {
    for (var i = 0; i < CW.state.projects.length; i++) {
      if (CW.state.projects[i].id === id) return CW.state.projects[i];
    }
    return null;
  }

  function syncSidebar() {
    var ts = CW.$("sidebar-scope");
    if (ts) {
      ts.hidden = !CW.state.token;
    }
    var nav = CW.$("sidebar-nav");
    if (!nav) return;
    var links = nav.querySelectorAll("a");
    var mods = ["flags", "releases", "publish", "keys", "stats", "project-settings"];
    var settingsLink = CW.$("settings-nav-link");
    var logoutBtn = CW.$("logout-btn");
    if (!CW.state.token) {
      nav.setAttribute("aria-hidden", "true");
      for (var k = 0; k < links.length; k++) {
        links[k].setAttribute("tabindex", "-1");
        links[k].setAttribute("aria-disabled", "true");
      }
      nav.style.display = "none";
      if (settingsLink) { settingsLink.hidden = true; settingsLink.style.display = "none"; }
      if (logoutBtn) { logoutBtn.hidden = true; logoutBtn.style.display = "none"; }
      return;
    }
    if (settingsLink) {
      settingsLink.hidden = false;
      settingsLink.style.display = "";
      var active = CW.state.view === "settings" || CW.state.view === "account";
      if (active) settingsLink.setAttribute("aria-current", "page");
      else if (settingsLink.removeAttribute) settingsLink.removeAttribute("aria-current");
    }
    if (logoutBtn) {
      logoutBtn.hidden = false;
      logoutBtn.style.display = "";
    }
    // SSO mode keeps the logout control like standard (tap clears the
    // session and routes home to the cloud dashboard); only the
    // login/setup/account surfaces stay gated.
    try {
      if (CW.isSsoMode && CW.isSsoMode()) {
        if (CW.applySsoUiGating) CW.applySsoUiGating();
      }
    } catch (e) { /* best-effort */ }
    var showNav = !!CW.state.projectId && (CW.state.view === "detail" || CW.state.view === "settings" || CW.state.view === "account");
    if (!showNav) {
      nav.setAttribute("aria-hidden", "true");
      for (var i = 0; i < links.length; i++) {
        links[i].setAttribute("tabindex", "-1");
        links[i].setAttribute("aria-disabled", "true");
      }
      nav.style.display = "none";
      return;
    }
    nav.removeAttribute("aria-hidden");
    nav.style.display = "";
    for (var j = 0; j < links.length; j++) {
      var m = mods[j] || "";
      if (!CW.state.projectId) {
        links[j].setAttribute("tabindex", "-1");
        links[j].setAttribute("aria-disabled", "true");
        links[j].setAttribute("href", "#/");
        continue;
      }
      links[j].removeAttribute("tabindex");
      links[j].removeAttribute("aria-disabled");
      links[j].setAttribute("href", "#/p/" + encodeURIComponent(CW.state.projectId) + "#" + m);
    }
  }

  function showView(name) {
    CW.state.view = name;
    var home = CW.$("view-home");
    var detail = CW.$("view-detail");
    var account = CW.$("view-account");
    var settings = CW.$("view-settings");
    if (home) home.hidden = name !== "home";
    if (detail) detail.hidden = name !== "detail";
    if (settings) settings.hidden = name !== "settings" && name !== "account";
    if (account) account.hidden = true;
    syncSidebar();
  }

  // Home sparkline helpers (pure, tested via node js_tests): series points
  // carry {day, fetches, exposures}; merged values are per-day totals.

  // Merge a list of stats series arrays into one SeriesPoint array,
  // keyed by `day` string (union of days, sorted ascending YYYY-MM-DD
  // lexicographic), summing fetches+exposures per day and merging
  // versions maps by summing counts. Envs missing a day contribute zero.
  function homeMergePoints(list) {
    var byDay = {};
    var i, j;
    if (!list || !list.length) return [];
    for (i = 0; i < list.length; i++) {
      var s = list[i];
      if (!s || !s.length) continue;
      for (j = 0; j < s.length; j++) {
        var p = s[j] || {};
        var day = p.day !== undefined && p.day !== null ? String(p.day) : "";
        if (!day) continue;
        var f = Number(p.fetches) || 0;
        var e = Number(p.exposures) || 0;
        if (f < 0) f = 0;
        if (e < 0) e = 0;
        var slot = byDay[day];
        if (!slot) slot = byDay[day] = { day: day, fetches: 0, exposures: 0, versions: null };
        slot.fetches += f;
        slot.exposures += e;
        if (p.versions && typeof p.versions === "object") {
          if (!slot.versions) slot.versions = {};
          Object.keys(p.versions).forEach(function (k) {
            var c = Number(p.versions[k]) || 0;
            if (c < 0) c = 0;
            var key = String(k);
            slot.versions[key] = (Number(slot.versions[key]) || 0) + c;
          });
        }
      }
    }
    return Object.keys(byDay).sort().map(function (d) {
      var slot = byDay[d];
      return { day: slot.day, fetches: slot.fetches, exposures: slot.exposures, versions: slot.versions };
    });
  }

  function homePointsTotals(points) {
    if (!points || !points.length) return [];
    return points.map(function (p) {
      var f = Number(p && p.fetches) || 0;
      var e = Number(p && p.exposures) || 0;
      if (f < 0) f = 0;
      if (e < 0) e = 0;
      return f + e;
    });
  }

  // Headline stability: the series runs through the current (partial)
  // UTC day, so averaging it in drags the headline down every morning.
  // The headline averages the *complete* UTC days instead; the chart
  // keeps the full series.
  function homeTrimPartialDay(points) {
    if (!points || !points.length) return [];
    var today = "";
    try { today = new Date().toISOString().slice(0, 10); } catch (e) { today = ""; }
    var last = points[points.length - 1] || {};
    var lastDay = last.day !== undefined && last.day !== null ? String(last.day) : "";
    if (today && lastDay && lastDay === today) return points.slice(0, -1);
    return points.slice();
  }

  // Merge a list of stats series arrays into one number[] of per-day
  // totals (fetches + exposures). Day-keyed inputs delegate to
  // homeMergePoints; day-less inputs keep the legacy right-aligned
  // index merge so old callers/stubs stay green.
  function homeMergeSeries(list) {
    var i, j;
    var hasDay = false;
    if (list && list.length) {
      for (i = 0; i < list.length && !hasDay; i++) {
        var s = list[i];
        if (!s || !s.length) continue;
        for (j = 0; j < s.length; j++) {
          var q = s[j] || {};
          if (q.day !== undefined && q.day !== null && String(q.day) !== "") { hasDay = true; break; }
        }
      }
    }
    if (hasDay) return homePointsTotals(homeMergePoints(list));
    var maxLen = 0;
    if (!list || !list.length) return [];
    for (i = 0; i < list.length; i++) {
      var a = list[i];
      if (a && a.length && a.length > maxLen) maxLen = a.length;
    }
    if (!maxLen) return [];
    var out = [];
    for (i = 0; i < maxLen; i++) out.push(0);
    for (i = 0; i < list.length; i++) {
      var t = list[i];
      if (!t || !t.length) continue;
      var off = maxLen - t.length;
      for (j = 0; j < t.length; j++) {
        var p = t[j] || {};
        var f = Number(p.fetches) || 0;
        var e = Number(p.exposures) || 0;
        if (f < 0) f = 0;
        if (e < 0) e = 0;
        out[off + j] += f + e;
      }
    }
    return out;
  }

  function homeDailyAverage(values) {
    var n = values ? values.length : 0;
    if (!n) return 0;
    var sum = 0;
    for (var i = 0; i < n; i++) {
      var v = Number(values[i]) || 0;
      if (v > 0) sum += v;
    }
    return Math.round(sum / n);
  }

  // Home range selector (?since= param). Only backend-supported day-grain
  // windows within 90d retention — no month/all-time/custom options.
  var homeSinceWhitelist = ["1d", "2d", "7d", "30d", "90d"];
  CW.homeSinceWhitelist = homeSinceWhitelist;

  function getHomeSince() {
    var v = CW.state && CW.state.homeSince;
    for (var i = 0; i < homeSinceWhitelist.length; i++) {
      if (v === homeSinceWhitelist[i]) return v;
    }
    return "7d";
  }

  function setHomeSince(v) {
    var ok = false;
    for (var i = 0; i < homeSinceWhitelist.length; i++) {
      if (v === homeSinceWhitelist[i]) { ok = true; break; }
    }
    if (!ok) return getHomeSince();
    CW.state.homeSince = v;
    try {
      if (CW.persistScope) CW.persistScope();
      else if (typeof localStorage !== "undefined" && localStorage.setItem) {
        localStorage.setItem(CW.LS_HOME_SINCE || "cw_home_since", v);
      }
    } catch (e) { /* private mode */ }
    return v;
  }

  function homeSparklineSVG(values, opts) {
    var W = 100;
    var H = 32;
    var PAD = 2;
    var BASE = H - 4;
    var rawId = (opts && opts.id != null) ? String(opts.id) : "home";
    var safeId = rawId.replace(/[^A-Za-z0-9_-]/g, "") || "home";
    var gid = "cw-spark-" + safeId;
    var vals = [];
    var max = 0;
    var i;
    if (values && values.length) {
      for (i = 0; i < values.length; i++) {
        var v = Number(values[i]) || 0;
        if (v < 0) v = 0;
        vals.push(v);
        if (v > max) max = v;
      }
    }
    function r1(v) { return Math.round(v * 10) / 10; }
    function yOf(v) {
      if (max <= 0) return BASE;
      return r1(2 + (1 - v / max) * (BASE - 2));
    }
    function xOf(idx, count) {
      if (count <= 1) return r1(W / 2);
      return r1(PAD + idx * (W - PAD * 2) / (count - 1));
    }
    var n = vals.length;
    var pts = [];
    var k;
    if (n === 0) {
      pts = [{ x: PAD, y: BASE }, { x: W - PAD, y: BASE }];
    } else if (n === 1) {
      var y1 = yOf(vals[0]);
      pts = [{ x: PAD, y: y1 }, { x: W - PAD, y: y1 }];
    } else {
      for (k = 0; k < n; k++) pts.push({ x: xOf(k, n), y: yOf(vals[k]) });
    }
    var line = "";
    for (k = 0; k < pts.length; k++) line += (k ? " " : "") + pts[k].x + "," + pts[k].y;
    var area = "M" + pts[0].x + " " + pts[0].y;
    for (k = 1; k < pts.length; k++) area += "L" + pts[k].x + " " + pts[k].y;
    area += "L" + pts[pts.length - 1].x + " " + BASE + "L" + pts[0].x + " " + BASE + "Z";
    var title = "30-day activity" + (n ? ", latest " + vals[n - 1] : "");
    return '<svg class="pc-spark-svg" role="img" aria-label="' + CW.esc(title) +
      '" viewBox="0 0 ' + W + " " + H + '" preserveAspectRatio="none">' +
      "<title>" + CW.esc(title) + "</title>" +
      '<defs><linearGradient id="' + CW.esc(gid) + '" x1="0" y1="0" x2="0" y2="1">' +
      '<stop offset="0%" stop-color="#3b82f6" stop-opacity="0.35"/>' +
      '<stop offset="100%" stop-color="#3b82f6" stop-opacity="0.02"/>' +
      "</linearGradient></defs>" +
      '<path d="' + area + '" fill="url(#' + CW.esc(gid) + ')" stroke="none"></path>' +
      '<polyline points="' + line + '" fill="none" stroke="#6ea8fe" stroke-width="1.5"' +
      ' stroke-linejoin="round" stroke-linecap="round"></polyline></svg>';
  }

  // Dashed trailing card that opens the project dialog (replaces the
  // former header button, so the header keeps only the range selector).
  // Rendered here so grid rewrites never drop it; boot.js owns the
  // click/key handling via [data-register-project].
  function registerCardHTML() {
    return '<article class="card project-card register-card" role="listitem" tabindex="0" data-register-project aria-label="Add a new project">' +
      '<span class="register-icon" aria-hidden="true"><svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round" aria-hidden="true"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/><path d="M12 10v6M9 13h6"/></svg></span>' +
      '<span class="register-text">Add a new project</span></article>';
  }

  function renderProjectCards() {
    var grid = CW.$("project-grid");
    var empty = CW.$("project-empty");
    if (!grid) return;
    if (!CW.state.projects.length) {
      grid.innerHTML = registerCardHTML();
      if (empty) empty.hidden = false;
      return;
    }
    if (empty) empty.hidden = true;
    if (!CW.state.homeSeries) CW.state.homeSeries = {};
    grid.innerHTML = CW.state.projects.map(function (p) {
      var s = CW.state.homeStats[p.id] || { flags: 0, envs: 0, keys: 0 };
      var hs = CW.state.homeSeries[p.id] || { daily: 0, values: [] };
      var points = (hs && hs.points) || null;
      if (!points && hs && hs.values && hs.values.length && typeof hs.values[0] === "object") points = hs.values;
      var totals;
      if (points && points.length) totals = homePointsTotals(homeTrimPartialDay(points));
      else totals = (hs && hs.values) || [];
      if (totals && totals.length && typeof totals[0] === "object") totals = homePointsTotals(totals);
      var daily = (hs && typeof hs.daily === "number") ? hs.daily : homeDailyAverage(totals);
      if (daily < 0) daily = 0;
      var label = (p.name || p.id) + ", average " + daily + " events per day";
      var chartHtml;
      if (typeof CW.renderSeries === "function") {
        if (points && points.length) chartHtml = CW.renderSeries(points);
        else chartHtml = '<p class="muted stats-series-empty">No activity.</p>';
      } else if (totals && totals.length) {
        chartHtml = homeSparklineSVG((typeof totals[0] === "object") ? homePointsTotals(totals) : totals, { id: p.id });
      } else {
        chartHtml = homeSparklineSVG([], { id: p.id });
      }
      return '<article class="card project-card has-spark" role="listitem" tabindex="0" data-project-id="' +
        CW.esc(p.id) + '" aria-label="' + CW.esc(label) + '">' +
        '<div class="pc-head">' +
        '<span class="pc-icon" aria-hidden="true"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round" aria-hidden="true"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg></span>' +
        '<span class="pc-name">' + CW.esc(p.name || p.id) + "</span>" +
        '<span class="pc-nums">' +
        '<span class="pc-daily">' + CW.esc(String(daily)) + "</span></span></div>" +
        '<div class="pc-sub">avg daily events</div>' +
        '<p class="pc-meta project-card-stats"><span>' + CW.esc(s.flags) + " flags</span>" +
        "<span> · </span><span>" + CW.esc(s.envs) + " envs</span>" +
        "<span> · </span><span>" + CW.esc(s.keys) + " active keys</span></p>" +
        '<div class="pc-spark pc-full">' + chartHtml + "</div>" +
        "</article>";
    }).join("") + registerCardHTML();
  }

  // Home aggregates GLOBAL stats via a per-project paged loop (CW.apiAll)
  // merged client-side, so the grid stays correct past 200 rows per
  // collection. Bucketing is unchanged: flags/envs count by project,
  // keys are owned per-project through env->project, same as loadKeys,
  // and revoked keys are excluded.
  function loadHomeStats() {
    var gen = CW.state.scopeGen;
    return CW.apiAll("/api/collections/environments/records?perPage=200").then(function (eItems) {
      var envs = eItems || [];
      var envProject = {};
      envs.forEach(function (env) { envProject[env.id] = env.project; });
      var stats = {};
      var projects = CW.state.projects.slice();
      projects.forEach(function (p) { stats[p.id] = { flags: 0, envs: 0, keys: 0 }; });
      envs.forEach(function (env) {
        if (stats[env.project]) stats[env.project].envs++;
      });
      var chain = Promise.resolve();
      projects.forEach(function (p) {
        chain = chain.then(function () {
          var f = "?perPage=200&filter=" + encodeURIComponent('(project="' + p.id + '")');
          return CW.apiAll("/api/collections/flags/records" + f).then(function (fItems) {
            (fItems || []).forEach(function (fl) {
              if (stats[fl.project]) stats[fl.project].flags++;
            });
          });
        });
      });
      return chain.then(function () {
        return CW.apiAll("/api/collections/sdk_keys/records?perPage=200").then(function (kItems) {
          (kItems || []).forEach(function (key) {
            var pid = envProject[key.env];
            if (pid && stats[pid] && !key.revoked) stats[pid].keys++;
          });
          if (CW.scopeStale(gen)) return;
          CW.state.homeStats = stats;
          renderProjectCards();
          return loadHomeSeries(gen, envs, projects).then(function () {
            if (CW.scopeStale(gen)) return;
            renderProjectCards();
          });
        });
      });
    }, function () { renderProjectCards(); });
  }

  // Per-project activity over the selected home range merged across the
  // project's own envs.
  // Best-effort: per-env failures resolve null and fall back to the
  // flags/envs/keys card (flat baseline), never rejecting home grid.
  function loadHomeSeries(gen, envs, projects) {
    if (!CW.state.homeSeries) CW.state.homeSeries = {};
    var byProject = {};
    var i;
    for (i = 0; i < projects.length; i++) byProject[projects[i].id] = [];
    (envs || []).forEach(function (env) {
      if (env && env.slug && byProject[env.project]) byProject[env.project].push(env);
    });
    var jobs = projects.map(function (p) {
      var list = byProject[p.id] || [];
      if (!list.length) {
        if (!CW.scopeStale(gen)) CW.state.homeSeries[p.id] = { points: [], values: [], daily: 0 };
        return Promise.resolve();
      }
      var fetches = list.map(function (env) {
        var url = "/api/v1/admin/env/" + encodeURIComponent(env.slug) +
          "/stats?since=" + encodeURIComponent(getHomeSince()) + "&project=" + encodeURIComponent(p.id);
        return CW.api(url).then(function (data) {
          return (data && data.series) || [];
        }, function () { return null; });
      });
      return Promise.all(fetches).then(function (parts) {
        if (CW.scopeStale(gen)) return;
        var ok = [];
        for (var k = 0; k < parts.length; k++) {
          if (parts[k]) ok.push(parts[k]);
        }
        var merged = homeMergePoints(ok);
        var avg = homeDailyAverage(homePointsTotals(homeTrimPartialDay(merged)));
        CW.state.homeSeries[p.id] = { points: merged, values: merged, daily: avg };
      });
    });
    return Promise.all(jobs).then(function () { /* best-effort: never reject */ },
      function () { /* best-effort: never reject */ });
  }

  function loadDetailScope() {
    CW.bumpScopeGen();
    CW.loadEnvs().then(function () {
      renderDetailHeader();
      CW.loadReleases().catch(function (e) { CW.$("release-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
      CW.loadKeys().catch(function (e) { CW.$("key-list").innerHTML = "<li>" + CW.esc(e.message) + "</li>"; });
      CW.loadStats().catch(function () { /* inline in stats card */ });
      CW.loadFlags().then(loadScopedAfterFlags, function (e) {
        CW.$("flag-folders").innerHTML = "<p class=\"muted\">" + CW.esc(e.message) + "</p>";
        loadScopedAfterFlags();
      });
      function loadScopedAfterFlags() {
        CW.loadRules().catch(CW.toastErr);
        CW.loadExperiments().catch(CW.toastErr);
      }
    }).catch(CW.toastErr);
  }

  function renderDetailHeader() {
    var p = projectById(CW.state.projectId);
    var el = CW.$("detail-project-name");
    if (el) el.textContent = p ? (p.name || p.id) : "Project";
    var sel = null;
    try { sel = CW.selectedEnv ? CW.selectedEnv() : null; } catch (e) { sel = null; }
    var eg = CW.$("env-group-name");
    if (eg) eg.textContent = (sel && sel.slug) || (CW.state && CW.state.envSlug) || "env";
    syncSidebar();
  }

  function showHome() {
    CW.bumpScopeGen();
    showView("home");
    renderProjectCards();
    try {
      var sel = CW.$("home-since");
      if (sel) sel.value = getHomeSince();
    } catch (e) { /* test harness without the select */ }
    if (CW.state.token) loadHomeStats().catch(function (e) { console.warn('router: home stats', e); });
  }

  function scrollBelowSticky(card) {
    // Sidebar anchors land on a section card, but the sticky topbar would
    // cover its head (scope bars are static). Measure live and land the
    // card just below in one jump.
    if (!card || !card.getBoundingClientRect) {
      if (card && card.scrollIntoView) card.scrollIntoView();
      return;
    }
    var y = 0;
    try {
      y = card.getBoundingClientRect().top +
        (window.pageYOffset || document.documentElement.scrollTop || 0);
    } catch (e) { card.scrollIntoView(); return; }
    var off = 36;
    try {
      var tb = document.querySelector(".topbar");
      if (tb && tb.getBoundingClientRect) off += tb.getBoundingClientRect().height || 0;
      ["env-bar"].forEach(function (id) {
        var bar = CW.$(id);
        if (bar && bar.getBoundingClientRect) {
          var pos = "";
          if (window.getComputedStyle) pos = window.getComputedStyle(bar).position || "";
          if (pos === "sticky" || pos === "fixed") off += bar.getBoundingClientRect().height || 0;
        }
      });
      // Sticky project header (#view-detail .detail-head, position:sticky)
      // floats over section cards in detail view — same live-measure
      // pattern as above. Hidden views report height 0, so this is a
      // no-op outside detail.
      try {
        var dh = document.querySelector ? document.querySelector("#view-detail .detail-head") : null;
        if (dh && dh.getBoundingClientRect) {
          var dpos = "";
          if (window.getComputedStyle) dpos = window.getComputedStyle(dh).position || "";
          if (dpos === "sticky" || dpos === "fixed") off += dh.getBoundingClientRect().height || 0;
        }
      } catch (e3) { /* best-effort: header stays uncovered by base+topbar */ }
    } catch (e2) { off = 212; }
    y = Math.max(0, y - off);
    try { window.scrollTo(0, y); }
    catch (e3) { card.scrollIntoView(); }
  }

  function openProject(id, anchor) {
    if (!projectById(id)) { showHome(); return; }
    CW.state.projectId = id;
    CW.persistScope();
    var want = "#/p/" + encodeURIComponent(id);
    if (window.location.hash !== want && !anchor) window.location.hash = want;
    showView("detail");
    renderDetailHeader();
    loadDetailScope();
    if (anchor) {
      if (anchor === "experiments") anchor = "flags";
      var t = CW.$(anchor);
      var card = t && t.closest ? t.closest("section") : null;
      if (card) scrollBelowSticky(card);
    }
  }

  // Global scope (All/home) implies no selected project.
  function clearStaleAllScope() {
    if (CW.state.view === "home" && CW.state.projectId) {
      CW.state.projectId = null;
      CW.state.envId = null;
      CW.persistScope();
    }
  }

  function showAccount() {
    clearStaleAllScope();
    showView("account");
    try { if (CW.isSsoMode && CW.isSsoMode() && CW.applySsoUiGating) CW.applySsoUiGating(); } catch (e) { /* best-effort */ }
    if (CW.loadAccounts) CW.loadAccounts().catch(CW.toastErr);
    if (CW.loadLimits) CW.loadLimits().catch(function () { /* inline in settings card */ });
    if (CW.loadRetention) CW.loadRetention().catch(function () { /* inline in retention card */ });
  }

  function showSettings() {
    clearStaleAllScope();
    showView("settings");
    try { if (CW.isSsoMode && CW.isSsoMode() && CW.applySsoUiGating) CW.applySsoUiGating(); } catch (e) { /* best-effort */ }
    if (CW.loadAccounts) CW.loadAccounts().catch(CW.toastErr);
    if (CW.loadLimits) CW.loadLimits().catch(function () { /* inline in settings card */ });
    if (CW.loadRetention) CW.loadRetention().catch(function () { /* inline in retention card */ });
    if (CW.loadBackups) CW.loadBackups().catch(function () { /* inline in backups card */ });
    if (CW.loadBackupS3) CW.loadBackupS3().catch(function () { /* inline in backups card */ });
    if (CW.loadBackupSchedule) CW.loadBackupSchedule().catch(function () { /* inline in backups card */ });
  }

  function route() {
    if (!CW.state.token) return;
    var r = parseHash();
    if (r.view === "settings") {
      showSettings();
    } else if (r.view === "account") {
      showSettings();
    } else if (r.view === "detail" && r.id && projectById(r.id)) {
      // Avoid re-loading on pure in-page anchor hops to the same project.
      if (CW.state.view === "detail" && CW.state.projectId === r.id) {
        showView("detail");
        renderDetailHeader();
        if (r.anchor) {
          var anchor = r.anchor === "experiments" ? "flags" : r.anchor;
          var t = CW.$(anchor);
          var card = t && t.closest ? t.closest("section") : null;
          if (card) scrollBelowSticky(card);
        }
        return;
      }
      openProject(r.id, r.anchor);
    } else if (r.view === "detail" && r.id) {
      // Unknown id (stale reload): fall back home rather than blank detail.
      showHome();
    } else {
      showHome();
    }
  }

  CW.parseHash = parseHash;
  CW.projectById = projectById;
  CW.syncSidebar = syncSidebar;
  CW.showView = showView;
  CW.renderProjectCards = renderProjectCards;
  CW.loadHomeStats = loadHomeStats;
  CW.loadHomeSeries = loadHomeSeries;
  CW.homeMergeSeries = homeMergeSeries;
  CW.homeMergePoints = homeMergePoints;
  CW.homePointsTotals = homePointsTotals;
  CW.homeDailyAverage = homeDailyAverage;
  CW.homeTrimPartialDay = homeTrimPartialDay;
  CW.getHomeSince = getHomeSince;
  CW.setHomeSince = setHomeSince;
  CW.homeSparklineSVG = homeSparklineSVG;
  CW.loadDetailScope = loadDetailScope;
  CW.renderDetailHeader = renderDetailHeader;
  CW.showHome = showHome;
  CW.showAccount = showAccount;
  CW.showSettings = showSettings;
  CW.openProject = openProject;
  CW.route = route;
})();
