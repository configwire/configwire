"use strict";
// test-update.js — update.js version math, staleness + hang-guard (Wave 2, todo 8),
// plus boot.js topbar version wiring (Phase 5: same-origin meta only).
// Loads core.js + boot.js + update.js via harness.js; node:test + node:assert/strict only.
// Run from configwire/: node --test js_tests/test-update.js
//
// Why boot.js too: update.js:102-104 exposes checkNow ONLY as
// window.cwAdmin.checkAssetUpdate, and cwAdmin is defined in boot.js:711.
// Both boot IIFEs defer to a captured DOMContentLoaded listener under the
// default harness readyState "loading" (boot.js:13, boot.js:882-886,
// update.js:96-100), so this suite NEVER calls ctx.dispatch() — it invokes
// update's own start listener directly (last DOMContentLoaded entry) and
// boot's wiring/updateVersion stay dormant, keeping fetchCalls pure-update.
// The boot tests below are the exception: they dispatch on purpose.
var test = require("node:test");
var assert = require("node:assert/strict");
var fs = require("node:fs");
var path = require("node:path");
var h = require("./harness.js");

function fresh(overrides) {
  return h.load(["core.js", "boot.js", "update.js"], overrides);
}

function metaRes(data, ok) {
  return {
    ok: ok === undefined ? true : ok,
    json: function () { return Promise.resolve(data); },
  };
}

// update's start is the LAST DOMContentLoaded listener on document
// (boot.js:13 first, boot.js:883 second, update.js:97 last). Calling it
// directly tests start() deterministically without waking boot handlers.
function updateStart(ctx) {
  var fns = ctx.document.listeners
    .filter(function (l) { return l.type === "DOMContentLoaded"; })
    .map(function (l) { return l.fn; });
  assert.ok(fns.length >= 2, "expected boot+update DOMContentLoaded listeners, got " + fns.length);
  return fns[fns.length - 1];
}

function checkNow(ctx) {
  assert.equal(typeof ctx.cwAdmin.checkAssetUpdate, "function", "update.js:103 exposes checkNow");
  return ctx.cwAdmin.checkAssetUpdate;
}

// --- metaUrl (update.js:19-21) ---

test("metaUrl appends Date.now cache-buster", function () {
  var seen = [];
  var ctx = fresh({
    fetchImpl: function (u) {
      seen.push(u);
      return Promise.resolve(metaRes({ version: "v1", assets: "a1" }));
    },
  });
  var realNow = Date.now;
  ctx.context.Date.now = function () { return 1700000000000; };
  var p;
  try {
    p = checkNow(ctx)();
  } finally {
    ctx.context.Date.now = realNow;
  }
  return p.then(function () {
    assert.equal(seen.length, 1);
    assert.equal(seen[0], "/api/v1/meta?_=1700000000000");
  });
});

// --- readMeta normalization (update.js:23-33) ---

test("readMeta: !ok response normalizes to null (checkNow false, no baseline)", function () {
  var ctx = fresh({ fetchImpl: function () { return Promise.resolve(metaRes({ x: 1 }, false)); } });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
    // Baseline still unset: a later good poll is "first success", also false.
    return checkNow(ctx)().then(function (out2) {
      assert.equal(out2, false);
    });
  });
});

test("readMeta: sync fetch throw resolves false (offline/file:// silent)", function () {
  var ctx = fresh({
    fetchImpl: function () { throw new Error("offline"); },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
    assert.ok(!ctx.elementsById["cw-reload-bar"], "no banner element touched on failure");
  });
});

test("readMeta: rejected json resolves false", function () {
  var ctx = fresh({
    fetchImpl: function () {
      return Promise.resolve({ ok: true, json: function () { return Promise.reject(new Error("bad json")); } });
    },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
  });
});

test("readMeta: non-object json normalizes to null", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes("just-a-string")); },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
  });
});

test("readMeta: non-string version/assets coerce to empty string", function () {
  var calls = 0;
  var ctx = fresh({
    fetchImpl: function () {
      calls++;
      return Promise.resolve(metaRes({ version: 5, assets: null }));
    },
  });
  var check = checkNow(ctx);
  return check().then(function (first) {
    assert.equal(first, false); // first success sets baseline (update.js:57-60)
    return check().then(function (second) {
      // {version:"",assets:""} vs {version:"",assets:""}: both guards in
      // isStale (update.js:43-44) skip on falsy fields, so never stale.
      assert.equal(second, false);
      assert.equal(calls, 2);
    });
  });
});

// --- isStale matrix via checkNow (update.js:41-46 + 55-66) ---

test("isStale: null baseline is false — first successful poll sets baseline, no banner", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes({ version: "v1", assets: "a1" })); },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
    assert.ok(!ctx.elementsById["cw-reload-bar"], "no banner on first poll (update.js:57-60)");
  });
});

test("isStale: null current is false", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes(null)); },
  });
  return checkNow(ctx)().then(function (out) {
    assert.equal(out, false);
  });
});

test("isStale: assets-differ is true + banner shown exactly once", function () {
  var polls = [{ version: "v1", assets: "aaa" }, { version: "v1", assets: "bbb" }];
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes(polls.shift() || polls[0])); },
  });
  var bar = ctx.document.getElementById("cw-reload-bar");
  bar.hidden = true;
  var removed = [];
  bar.removeAttribute = function (name) {
    removed.push(name);
    if (name === "hidden") bar.hidden = false;
  };
  var check = checkNow(ctx);
  return check().then(function (first) {
    assert.equal(first, false);
    assert.equal(bar.hidden, true);
    return check().then(function (second) {
      assert.equal(second, true);
      assert.equal(bar.hidden, false);
      assert.deepEqual(removed, ["hidden"]); // update.js:35-39 runs once per stale poll
    });
  });
});

test("isStale: version-differ is true", function () {
  var polls = [{ version: "v1", assets: "" }, { version: "v2", assets: "" }];
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes(polls.shift() || polls[0])); },
  });
  var check = checkNow(ctx);
  return check().then(function (first) {
    assert.equal(first, false);
    return check().then(function (second) {
      assert.equal(second, true);
    });
  });
});

test("isStale: equal fingerprint is false", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes({ version: "v1", assets: "aaa" })); },
  });
  var check = checkNow(ctx);
  return check()
    .then(function (a) { assert.equal(a, false); return check(); })
    .then(function (b) { assert.equal(b, false); return check(); })
    .then(function (c) { assert.equal(c, false); });
});

test("checkNow: post-mismatch failure resolves false (never throws)", function () {
  var mode = "good";
  var ctx = fresh({
    fetchImpl: function () {
      if (mode === "bad") throw new Error("went offline");
      return Promise.resolve(metaRes({ version: "v1", assets: "a1" }));
    },
  });
  var check = checkNow(ctx);
  return check().then(function (first) {
    assert.equal(first, false);
    mode = "bad";
    return check().then(function (out) {
      assert.equal(out, false); // update.js:66 catch swallows
    });
  });
});

// --- tick (update.js:69-72) ---

test("tick skips the poll when document.hidden", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes({ version: "v1", assets: "a1" })); },
  });
  updateStart(ctx)(); // arms the single interval; tick = intervals[0].fn (update.js:78)
  assert.equal(ctx.intervals.length, 1);
  var tick = ctx.intervals[0].fn;
  var base = ctx.fetchCalls.length;
  ctx.document.hidden = true;
  tick();
  assert.equal(ctx.fetchCalls.length, base, "hidden tick must not fetch (update.js:70)");
  ctx.document.hidden = false;
  tick();
  assert.equal(ctx.fetchCalls.length, base + 1, "visible tick polls");
});

// --- start (update.js:74-94) ---

test("start arms a single interval plus visibilitychange/online/reload-button listeners", function () {
  var ctx = fresh({
    fetchImpl: function () { return Promise.resolve(metaRes({ version: "v1", assets: "a1" })); },
  });
  var start = updateStart(ctx);
  assert.equal(ctx.intervals.length, 0, "nothing armed before start (readyState loading defers)");
  start();
  assert.equal(ctx.intervals.length, 1);
  assert.equal(ctx.intervals[0].ms, 5 * 60 * 1000, "POLL_MS 5min (update.js:14)");
  start(); // update.js:76 guard: timer set, second start must not re-arm
  assert.equal(ctx.intervals.length, 1, "single interval only");
  var types = ctx.listeners.map(function (l) { return l.target + ":" + l.type; });
  assert.ok(types.indexOf("document:visibilitychange") >= 0, "visibilitychange armed (update.js:81), got " + JSON.stringify(types));
  assert.ok(types.indexOf("window:online") >= 0, "online armed (update.js:84), got " + JSON.stringify(types));
  var btn = ctx.elementsById["cw-reload-btn"];
  assert.ok(btn, "reload button looked up (update.js:87)");
  var clicks = btn.listeners.filter(function (l) { return l.type === "click"; });
  assert.equal(clicks.length, 1, "exactly one reload click listener (update.js:89)");
});

// --- boot topbar version (Phase 5): same-origin meta, no third party ---

function bootVersion(ctx) {
  return ctx.document.getElementById("cw-version");
}

function flushed() {
  return new Promise(function (resolve) { setImmediate(resolve); });
}

test("boot: /api/v1/meta version sets #cw-version and boot never fetches GitHub", function () {
  var ctx = h.load(undefined, {
    fetchImpl: function (url) {
      if (String(url).indexOf("/api/v1/meta") === 0) {
        return Promise.resolve(metaRes({ version: "v9.8.7" }));
      }
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
  });
  ctx.dispatch("DOMContentLoaded");
  var recorded = ctx.fetchCalls.map(function (c) { return String(c.url); });
  assert.ok(
    recorded.indexOf("/api/v1/meta") !== -1,
    "boot requests the same-origin meta endpoint, got " + JSON.stringify(recorded)
  );
  assert.ok(
    recorded.every(function (u) { return u.indexOf("api.github.com") === -1; }),
    "boot must not call api.github.com, got " + JSON.stringify(recorded)
  );
  return flushed().then(function () {
    assert.equal(bootVersion(ctx).textContent, "v9.8.7");
  });
});

test("boot: meta failure keeps the static fallback version", function () {
  var ctx = h.load(undefined, {
    fetchImpl: function (url) { return Promise.reject(new Error("offline: " + url)); },
  });
  bootVersion(ctx).textContent = "vdev";
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(bootVersion(ctx).textContent, "vdev");
  });
});

// --- update badge (boot topbar #cw-update, update-pill) ---
//
// Same-origin only: version from GET /api/v1/meta, release state from one
// POST /api/v1/admin/update-check. With the Settings switch off the badge
// reads "Check for updates" and a click runs one check; with the switch on
// the badge shows update <tag> / latest and a click follows the link.

function checkCtx(updateImpl, seed) {
  var opts = {
    fetchImpl: function (url) {
      if (url === "/api/v1/admin/update-check") return updateImpl(url);
      if (String(url).indexOf("/api/v1/meta") === 0) {
        return Promise.resolve(metaRes({ version: "v9.8.7" }));
      }
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
  };
  if (seed) opts.localStorageSeed = seed;
  return h.load(undefined, opts);
}

function updateCheckCalls(ctx) {
  return ctx.fetchCalls.filter(function (c) {
    return String(c.url) === "/api/v1/admin/update-check";
  });
}

function urlsOf(ctx) {
  return ctx.fetchCalls.map(function (c) { return String(c.url); });
}

function badgeEl(ctx) {
  return ctx.document.getElementById("cw-update");
}

function clickBadge(ctx) {
  var badge = badgeEl(ctx);
  var clicks = badge.listeners.filter(function (l) { return l.type === "click"; });
  assert.equal(clicks.length, 1, "exactly one badge click listener wired");
  clicks[0].fn.call(badge, {});
  return badge;
}

function autoSwitch(ctx) {
  return ctx.document.getElementById("auto-update-switch");
}

function clickSwitch(ctx) {
  var sw = autoSwitch(ctx);
  var clicks = sw.listeners.filter(function (l) { return l.type === "click"; });
  assert.equal(clicks.length, 1, "exactly one switch click listener wired");
  clicks[0].fn.call(sw, {});
  return sw;
}

function autoStored(ctx) {
  try {
    return ctx.context.localStorage.getItem("cw.autoUpdateCheck");
  } catch (e) { return null; }
}

function liveAutoIntervals(ctx) {
  return ctx.intervals.filter(function (e) { return e.ms === AUTO_MS && !e.cleared; });
}

function okUpdate(update) {
  return function () {
    return Promise.resolve({
      ok: true,
      status: 200,
      json: function () {
        return Promise.resolve({ current: "v1.0.0", latest: "v1.2.0", update: update });
      },
    });
  };
}

var AUTO_MS = 6 * 60 * 60 * 1000;

test("badge: source pins same-origin POST, no third-party host", function () {
  var src = fs.readFileSync(path.join(__dirname, "..", "pb_public", "js", "boot.js"), "utf8");
  assert.ok(src.indexOf("/api/v1/admin/update-check") !== -1, "badge checks the same-origin endpoint");
  assert.equal(src.indexOf("api.github.com"), -1, "no third-party host in boot.js");
});

test("badge: switch off shows the Check for updates trigger with zero checks", function () {
  var ctx = checkCtx(okUpdate(true));
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    var badge = badgeEl(ctx);
    assert.equal(badge.textContent, "Check for updates");
    assert.equal(badge.hidden, false, "trigger is visible while the switch is off");
    assert.equal(updateCheckCalls(ctx).length, 0, "no check on load while off");
    assert.equal(liveAutoIntervals(ctx).length, 0, "no auto timer while off");
    assert.ok(urlsOf(ctx).every(function (u) { return u.indexOf("api.github.com") === -1; }));
  });
});

test("badge: off-mode click fires exactly one bodiless POST; update:true paints the tag", function () {
  var ctx = checkCtx(okUpdate(true));
  ctx.dispatch("DOMContentLoaded");
  var badge = clickBadge(ctx);
  return flushed().then(function () {
    var calls = updateCheckCalls(ctx);
    assert.equal(calls.length, 1, "exactly one update-check POST, got " + JSON.stringify(urlsOf(ctx)));
    assert.equal(calls[0].opts && calls[0].opts.method, "POST");
    assert.ok(!calls[0].opts || calls[0].opts.body === undefined, "POST sends no body");
    assert.equal(badge.textContent, "update v1.2.0");
    assert.equal(badge.title, "New version available: v1.2.0 (current v1.0.0) — open releases");
    assert.equal(badge.hidden, false);
  });
});

test("badge: off-mode click with update:false paints latest", function () {
  var ctx = checkCtx(okUpdate(false));
  ctx.dispatch("DOMContentLoaded");
  var badge = clickBadge(ctx);
  return flushed().then(function () {
    assert.equal(updateCheckCalls(ctx).length, 1);
    assert.equal(badge.textContent, "latest");
    assert.equal(badge.title, "ConfigWire is up to date (current v1.0.0) — open releases");
  });
});

test("badge: 403 hides the badge when checks are disabled", function () {
  var ctx = checkCtx(function () {
    return Promise.resolve({ ok: false, status: 403, json: function () { return Promise.resolve({}); } });
  });
  ctx.dispatch("DOMContentLoaded");
  var badge = clickBadge(ctx);
  return flushed().then(function () {
    assert.equal(updateCheckCalls(ctx).length, 1);
    assert.equal(badge.hidden, true, "disabled env keeps the badge hidden");
  });
});

test("badge: updates:false in /api/v1/meta keeps the badge hidden", function () {
  var ctx = h.load(undefined, {
    fetchImpl: function (url) {
      if (url === "/api/v1/admin/update-check") return okUpdate(true)(url);
      if (String(url).indexOf("/api/v1/meta") === 0) {
        return Promise.resolve(metaRes({ version: "v9.8.7", updates: false }));
      }
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
  });
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(badgeEl(ctx).hidden, true, "updates:false hides the badge");
    assert.equal(updateCheckCalls(ctx).length, 0, "disabled env fires no check");
  });
});

test("badge: 401 and failure keep the off-mode trigger", function () {
  var ctx = checkCtx(function () {
    return Promise.resolve({ ok: false, status: 401, json: function () { return Promise.resolve({}); } });
  });
  ctx.dispatch("DOMContentLoaded");
  var badge = clickBadge(ctx);
  return flushed().then(function () {
    assert.equal(updateCheckCalls(ctx).length, 1);
    assert.equal(badge.textContent, "Check for updates", "logged-out keeps the trigger");
  });
});

test("badge: on-mode click follows the link and fires no check", function () {
  var ctx = checkCtx(okUpdate(true), { "cw.autoUpdateCheck": "1" });
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(updateCheckCalls(ctx).length, 1, "seeded opt-in checks once on load");
    assert.equal(badgeEl(ctx).textContent, "update v1.2.0");
    ctx.fetchCalls.length = 0;
    clickBadge(ctx);
    return flushed();
  }).then(function () {
    assert.equal(updateCheckCalls(ctx).length, 0, "on-mode badge click never checks");
  });
});

test("badge: seeded on arms the 6h timer and paints latest when current", function () {
  var ctx = checkCtx(okUpdate(false), { "cw.autoUpdateCheck": "1" });
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(liveAutoIntervals(ctx).length, 1, "seeded opt-in arms the timer on load");
    assert.equal(updateCheckCalls(ctx).length, 1, "seeded opt-in checks once on load");
    assert.equal(badgeEl(ctx).textContent, "latest");
  });
});

test("badge: timer tick skips hidden tabs and fires when visible", function () {
  var ctx = checkCtx(okUpdate(false), { "cw.autoUpdateCheck": "1" });
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    var timers = liveAutoIntervals(ctx);
    assert.equal(timers.length, 1, "one auto timer armed");
    ctx.fetchCalls.length = 0;
    ctx.document.hidden = true;
    timers[0].fn();
    return flushed();
  }).then(function () {
    assert.equal(updateCheckCalls(ctx).length, 0, "hidden tab tick fires no check");
    ctx.document.hidden = false;
    var timers = liveAutoIntervals(ctx);
    timers[0].fn();
    return flushed();
  }).then(function () {
    assert.equal(updateCheckCalls(ctx).length, 1, "visible tick fires one check");
  });
});

test("switch: enabling persists, arms the timer, and checks immediately", function () {
  var ctx = checkCtx(okUpdate(true));
  ctx.dispatch("DOMContentLoaded");
  assert.equal(autoStored(ctx), null, "default off persists nothing");
  clickSwitch(ctx);
  return flushed().then(function () {
    assert.equal(autoStored(ctx), "1", "toggle persists opt-in");
    assert.equal(liveAutoIntervals(ctx).length, 1, "exactly one 6h auto timer armed");
    assert.equal(updateCheckCalls(ctx).length, 1, "enabling runs one immediate check");
    assert.equal(badgeEl(ctx).textContent, "update v1.2.0");
  });
});

test("switch: disabling clears the timer and restores the trigger", function () {
  var ctx = checkCtx(okUpdate(false), { "cw.autoUpdateCheck": "1" });
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(liveAutoIntervals(ctx).length, 1, "timer armed while on");
    clickSwitch(ctx);
    assert.equal(autoStored(ctx), null, "toggle clears the opt-in");
    assert.equal(liveAutoIntervals(ctx).length, 0, "disabling clears the timer");
    assert.ok(ctx.intervals.some(function (e) { return e.ms === AUTO_MS && e.cleared; }),
      "the 6h timer entry is marked cleared");
    assert.equal(badgeEl(ctx).textContent, "Check for updates", "disabling restores the trigger");
  });
});

test("switch: on-mode failure keeps the badge hidden without throwing", function () {
  var ctx = h.load(undefined, {
    fetchImpl: function (url) {
      if (url === "/api/v1/admin/update-check") {
        return Promise.reject(new Error("bad gateway"));
      }
      if (String(url).indexOf("/api/v1/meta") === 0) {
        return Promise.resolve(metaRes({ version: "v9.8.7" }));
      }
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
    localStorageSeed: { "cw.autoUpdateCheck": "1" },
  });
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(updateCheckCalls(ctx).length, 1, "auto checks once on load even when failing");
    assert.equal(badgeEl(ctx).hidden, true, "on-mode failure keeps the badge hidden");
  });
});

test("badge: dev builds keep the badge hidden", function () {
  var ctx = h.load(undefined, {
    fetchImpl: function (url) {
      if (url === "/api/v1/admin/update-check") return okUpdate(true)(url);
      if (String(url).indexOf("/api/v1/meta") === 0) {
        return Promise.resolve(metaRes({ version: "vdev" }));
      }
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
  });
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(badgeEl(ctx).hidden, true, "dev current hides the badge");
  });
});

function updatesCard(ctx) {
  return ctx.document.getElementById("console-updates-card");
}

test("updates card: hidden unless the server enables update checks", function () {
  var ctx = checkCtx(okUpdate(true));
  var card = updatesCard(ctx);
  card.hidden = true;
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(card.hidden, true, "card stays hidden without the updates flag");
  });
});

test("updates card: server flag unhides the Console updates card", function () {
  var ctx = h.load(undefined, {
    fetchImpl: function (url) {
      if (url === "/api/v1/admin/update-check") return okUpdate(false)(url);
      if (String(url).indexOf("/api/v1/meta") === 0) {
        return Promise.resolve(metaRes({ version: "v9.8.7", updates: true }));
      }
      return Promise.reject(new Error("harness fetch denied: " + url));
    },
  });
  var card = updatesCard(ctx);
  card.hidden = true;
  ctx.dispatch("DOMContentLoaded");
  return flushed().then(function () {
    assert.equal(card.hidden, false, "updates:true reveals the card");
  });
});

