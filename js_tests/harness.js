/* ConfigWire js_tests harness — node:vm loader with stub prelude.
 * Wave 1 foundation for configwire js tests (todos 2-12 build on this API).
 * Plain CommonJS, Node >= 20, no dependencies: only node:vm / node:fs /
 * node:path builtins (a file loader without fs is impossible; no third-party
 * packages, no package.json needed).
 */
"use strict";

var vm = require("node:vm");
var fs = require("node:fs");
var path = require("node:path");

// Script order frozen exactly as configwire/pb_public/index.html:694-711.
// core MUST load first: keys.js:5 and boot.js:5 do `var CW = window.CW`
// with no `|| {}` fallback, so they throw if window.CW does not exist yet
// (core.js:7 creates it).
var FULL_ORDER = Object.freeze([
  "core.js", // index.html:694
  "dialog.js", // index.html:695
  "auth.js", // index.html:696
  "router.js", // index.html:697
  "account.js", // index.html:698
  "scope.js", // index.html:699
  "drafts.js", // index.html:700
  "flags.js", // index.html:701
  "rules.js", // index.html:702
  "experiments.js", // index.html:703
  "keys.js", // index.html:704
  "releases.js", // index.html:705
  "transfer.js", // index.html:706
  "stats.js", // index.html:707
  "json-editor.js", // index.html:708
  "limits.js", // index.html:709 (boot.js:636-637 wires its save handlers)
  "boot.js", // index.html:710
  "update.js", // index.html:711
]);

var JS_DIR = path.join(__dirname, "..", "pb_public", "js");

// Minimal element stub: records listeners, never touches a real DOM.
function makeElement(tag, record) {
  var listeners = [];
  var el = {
    tagName: String(tag || "div").toUpperCase(),
    listeners: listeners,
    children: [],
    textContent: "",
    innerHTML: "",
    value: "",
    hidden: false,
    disabled: false,
    style: {},
    dataset: {},
    classList: {
      add: function () {},
      remove: function () {},
      toggle: function () {},
      contains: function () { return false; },
    },
    setAttribute: function () {},
    getAttribute: function () { return null; },
    removeAttribute: function () {},
    addEventListener: function (type, fn) {
      listeners.push({ type: type, fn: fn });
      record.push({ target: el, type: type });
    },
    removeEventListener: function (type, fn) {
      for (var i = listeners.length - 1; i >= 0; i--) {
        if (listeners[i].type === type && listeners[i].fn === fn) listeners.splice(i, 1);
      }
    },
    appendChild: function (child) { el.children.push(child); return child; },
    removeChild: function (child) {
      var i = el.children.indexOf(child);
      if (i >= 0) el.children.splice(i, 1);
      return child;
    },
    closest: function () { return null; },
    querySelector: function () { return null; },
    querySelectorAll: function () { return []; },
    select: function () {},
    click: function () {},
    focus: function () {},
    showModal: function () {},
    close: function () {},
  };
  return el;
}

// Fire captured listeners of a given type on a stub (test-driven DOMContentLoaded).
function fireListeners(stub, type, event) {
  var listeners = stub.listeners || [];
  for (var i = 0; i < listeners.length; i++) {
    if (listeners[i].type === type) listeners[i].fn.call(stub, event || {});
  }
}

// load(files, overrides): build the stub prelude, install it in a fresh
// node:vm context BEFORE compiling the first source file, then run each
// file in order. Returns a handle with everything tests need to assert on.
//
// files: array of "core.js" basenames (default FULL_ORDER) or absolute paths.
// overrides (all optional):
//   fetch: replace the deny-by-default stub entirely. Pass undefined/null to
//     REMOVE it, in which case load() throws before any file executes
//     (proves prelude-first ordering).
//   fetchImpl(url, opts): called by the recording stub instead of throwing,
//     so a test can allowlist specific URLs. Return value becomes the fetch
//     result (may be a Promise).
//   readyState: document.readyState, default "loading" (defers
//     dialog.js:273-279, boot.js:882-886, update.js:96-100 to a captured
//     DOMContentLoaded listener; "complete" runs them at load instead).
//   getRandomValues(fn): injectable deterministic crypto hook, default fills
//     bytes with a 0xAB/i-xor pattern. subtle: injectable window.crypto.subtle
//     stub (keys.js:46-48 needs digest at CALL time, never at load).
//   localStorageSeed: {key: value} object preloaded into the in-memory store.
function load(files, overrides) {
  var names = files === undefined ? FULL_ORDER.slice() : files.slice();
  var opts = overrides || {};
  var has = function (k) { return Object.prototype.hasOwnProperty.call(opts, k); };

  var fetchCalls = [];
  var intervals = [];
  var timeouts = [];
  var listenerLog = [];
  var nextTimerId = 1;

  // In-memory localStorage (core.js:108-118, boot.js:15 touch it).
  var store = {};
  if (opts.localStorageSeed && typeof opts.localStorageSeed === "object") {
    Object.keys(opts.localStorageSeed).forEach(function (k) {
      store[k] = String(opts.localStorageSeed[k]);
    });
  }
  var localStorageStub = {
    getItem: function (k) {
      return Object.prototype.hasOwnProperty.call(store, k) ? store[k] : null;
    },
    setItem: function (k, v) { store[k] = String(v); },
    removeItem: function (k) { delete store[k]; },
    clear: function () { Object.keys(store).forEach(function (k) { delete store[k]; }); },
    key: function (i) { return Object.keys(store)[i] || null; },
    get length() { return Object.keys(store).length; },
  };

  // Document stub. boot.js:705 needs document.body with addEventListener;
  // core.js:77 needs document.addEventListener at load; dialog.js:273-279,
  // boot.js:882-886, update.js:96-100 branch on document.readyState.
  var elementsById = {};
  var documentStub = {
    readyState: has("readyState") ? opts.readyState : "loading",
    hidden: false,
    listeners: [],
    body: null,
    documentElement: null,
    addEventListener: function (type, fn) {
      documentStub.listeners.push({ type: type, fn: fn });
      listenerLog.push({ target: "document", type: type });
    },
    removeEventListener: function () {},
    getElementById: function (id) {
      if (!elementsById[id]) elementsById[id] = makeElement("div#" + id, listenerLog);
      return elementsById[id];
    },
    createElement: function (tag) { return makeElement(tag, listenerLog); },
    querySelector: function () { return null; },
    querySelectorAll: function () { return []; },
    execCommand: function () { return false; },
  };
  documentStub.body = makeElement("body", listenerLog);
  documentStub.documentElement = makeElement("html", listenerLog);

  // Deterministic crypto hooks (keys.js:34-35,46-48 call at call-time only).
  var counter = { n: 0 };
  var getRandomValues = has("getRandomValues") && opts.getRandomValues
    ? opts.getRandomValues
    : function (arr) {
        for (var i = 0; i < arr.length; i++) {
          arr[i] = (0xab + counter.n + i) & 0xff;
        }
        counter.n += arr.length;
        return arr;
      };
  var subtleStub = has("subtle") && opts.subtle
    ? opts.subtle
    : {
        digest: function () {
          return Promise.resolve(new Uint8Array(32).buffer);
        },
      };

  // Sandbox: window === the vm global so `window.CW` and bare `CW`-via-
  // window assignments behave exactly like the browser (core.js:7,
  // dialog.js:8 merge; keys.js:5 / boot.js:5 assume it exists).
  var sandbox = {
    console: console,
    TextEncoder: TextEncoder,
    TextDecoder: TextDecoder,
    Date: Date,
    Promise: Promise,
    JSON: JSON,
    Math: Math,
    Object: Object,
    Array: Array,
    String: String,
    Number: Number,
    Boolean: Boolean,
    RegExp: RegExp,
    Error: Error,
    Uint8Array: Uint8Array,
    ArrayBuffer: ArrayBuffer,
    Map: Map,
    Set: Set,
    encodeURIComponent: encodeURIComponent,
    decodeURIComponent: decodeURIComponent,
    document: documentStub,
    localStorage: localStorageStub,
    navigator: {},
    location: { hash: "", href: "http://127.0.0.1:8090/", reload: function () {} },
  };

  // Deny-by-default recording fetch: records URL+opts, throws on any call
  // unless overrides.fetchImpl allows it (update.js:24 polls at load when
  // readyState is "complete"; boot.js:850,862 fetch at load) — hence the
  // throw must exist BEFORE the first file runs.
  var fetchStub = function (url, fetchOpts) {
    fetchCalls.push({ url: url, opts: fetchOpts });
    if (opts.fetchImpl) return opts.fetchImpl(url, fetchOpts);
    throw new Error("harness fetch denied: " + url);
  };
  if (has("fetch")) {
    if (opts.fetch == null) delete sandbox.fetch; // armed for the prelude-first proof
    else sandbox.fetch = opts.fetch;
  } else {
    sandbox.fetch = fetchStub;
  }

  // Capturing timers: record, never schedule real ones, so the process can
  // always exit with no --test-force-exit (update.js:78 arms a 5-min
  // setInterval at load; core.js:72 uses setTimeout inside toast()).
  sandbox.setInterval = function (fn, ms) {
    var id = nextTimerId++;
    intervals.push({ id: id, fn: fn, ms: ms, cleared: false });
    return id;
  };
  sandbox.clearInterval = function (id) {
    intervals.forEach(function (t) { if (t.id === id) t.cleared = true; });
  };
  sandbox.setTimeout = function (fn) {
    var id = nextTimerId++;
    timeouts.push({ id: id, fn: fn, cleared: false });
    return id;
  };
  sandbox.clearTimeout = function (id) {
    timeouts.forEach(function (t) { if (t.id === id) t.cleared = true; });
  };

  // window.addEventListener for boot.js:53 hashchange + update.js:84 online.
  sandbox.listeners = [];
  sandbox.addEventListener = function (type, fn) {
    sandbox.listeners.push({ type: type, fn: fn });
    listenerLog.push({ target: "window", type: type });
  };
  sandbox.removeEventListener = function () {};

  sandbox.crypto = { getRandomValues: getRandomValues, subtle: subtleStub };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.self = sandbox;

  vm.createContext(sandbox);

  // PRELUDE-FIRST GATE: the stubs above must be live before any source file
  // compiles. A load call with the fetch stub removed throws here, before
  // the first file executes (the temp probe asserts exactly this).
  if (typeof sandbox.fetch !== "function") {
    throw new Error("harness prelude missing: fetch stub must be installed before the first file loads");
  }
  if (typeof sandbox.document !== "object" || typeof sandbox.localStorage !== "object") {
    throw new Error("harness prelude missing: document/localStorage stubs must be installed before the first file loads");
  }

  names.forEach(function (name) {
    var file = path.isAbsolute(name) ? name : path.join(JS_DIR, name);
    var src = fs.readFileSync(file, "utf8");
    vm.runInContext(src, sandbox, { filename: file });
  });

  return {
    context: sandbox,
    CW: sandbox.CW,
    cwAdmin: sandbox.cwAdmin,
    fetchCalls: fetchCalls,
    intervals: intervals,
    timeouts: timeouts,
    listeners: listenerLog,
    elementsById: elementsById,
    document: documentStub,
    localStorage: localStorageStub,
    store: store,
    // Fire captured listeners of a type on document then window (e.g. "DOMContentLoaded").
    dispatch: function (type, event) {
      fireListeners(documentStub, type, event);
      fireListeners(documentStub.body, type, event);
      fireListeners({ listeners: sandbox.listeners }, type, event);
    },
  };
}

exports.load = load;
exports.FULL_ORDER = FULL_ORDER;
