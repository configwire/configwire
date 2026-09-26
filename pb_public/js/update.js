/* ConfigWire admin shell — update.js (stale-asset watcher). Loads LAST.
 * Polls same-origin /api/v1/meta and compares the {version, assets}
 * fingerprint against the load-time baseline. On mismatch (redeploy with
 * or without a VERSION bump) it reveals #cw-reload-bar; the user reloads
 * explicitly so in-memory drafts are never wiped by a silent refresh.
 * All failures are silent: offline, file://, or old servers without the
 * assets field simply never show the banner. */
(function () {
  "use strict";

  if (window.__cwUpdateWatch) return;
  window.__cwUpdateWatch = true;

  var POLL_MS = 5 * 60 * 1000;

  var baseline = null; // {version, assets} from first successful poll
  var timer = null;

  function metaUrl() {
    return "/api/v1/meta?_=" + Date.now();
  }

  function readMeta() {
    return fetch(metaUrl(), { headers: { Accept: "application/json" }, cache: "no-store" })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (d) {
        if (!d || typeof d !== "object") return null;
        return {
          version: typeof d.version === "string" ? d.version : "",
          assets: typeof d.assets === "string" ? d.assets : ""
        };
      });
  }

  function showBanner() {
    var bar = document.getElementById("cw-reload-bar");
    if (!bar) return;
    try { bar.removeAttribute("hidden"); } catch (e) { /* never break the shell */ }
  }

  function isStale(cur) {
    if (!baseline || !cur) return false;
    if (baseline.assets && cur.assets && baseline.assets !== cur.assets) return true;
    if (baseline.version && cur.version && baseline.version !== cur.version) return true;
    return false;
  }

  function checkNow() {
    var p = null;
    try {
      p = readMeta();
    } catch (e) {
      return Promise.resolve(false);
    }
    return p.then(function (cur) {
      if (!cur) return false;
      if (!baseline) {
        baseline = cur;
        return false;
      }
      if (isStale(cur)) {
        showBanner();
        return true;
      }
      return false;
    }).catch(function () { return false; });
  }

  function tick() {
    if (document.hidden) return;
    try { checkNow(); } catch (e) { /* silent */ }
  }

  function start() {
    try { checkNow(); } catch (e) { /* silent */ }
    if (timer) return;
    try {
      timer = setInterval(tick, POLL_MS);
    } catch (e) { timer = null; }
    try {
      document.addEventListener("visibilitychange", function () {
        if (!document.hidden) tick();
      });
      window.addEventListener("online", tick);
    } catch (e) { /* silent */ }
    try {
      var btn = document.getElementById("cw-reload-btn");
      if (btn) {
        btn.addEventListener("click", function () {
          try { window.location.reload(); } catch (e) { /* silent */ }
        });
      }
    } catch (e) { /* silent */ }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }

  try {
    if (window.cwAdmin) window.cwAdmin.checkAssetUpdate = checkNow;
  } catch (e) { /* silent */ }
})();
