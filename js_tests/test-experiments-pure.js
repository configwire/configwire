/* test-experiments-pure.js — pure helper tests for experiments.js.
 * Internals (round2, weightRawHasTooManyDecimals, valuesTextFor, expNameById)
 * are IIFE-closed in experiments.js:2-748, so they are extracted verbatim from
 * source and evaluated in a stub scope; exported surface (bpsToPercent,
 * percentToBps, validateVariants, expCountFor, deleteExperiment-as-expNameById
 * probe) is asserted through the harness CW object (core+experiments only —
 * drafts.js never loaded; CW.drafts stubbed per test).
 * Only node:test + node:assert/strict (+ node:vm/fs/path for loading).
 */
"use strict";

var test = require("node:test");
var assert = require("node:assert/strict");
var fs = require("node:fs");
var path = require("node:path");
var vm = require("node:vm");
var harness = require("./harness.js");

var JS_DIR = path.join(__dirname, "..", "pb_public", "js");
var ESRC = fs.readFileSync(path.join(JS_DIR, "experiments.js"), "utf8");

// Same balanced-brace extractor as test-rules.js (experiments.js is ES5 here:
// no template literals in the extracted fns; regexes contain no quotes).
function balancedEnd(src, openIdx) {
  var depth = 0;
  var i = openIdx;
  var q = null;
  while (i < src.length) {
    var ch = src[i];
    if (q) {
      if (ch === "\\") { i += 2; continue; }
      if (ch === q) q = null;
      i++;
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") { q = ch; i++; continue; }
    if (ch === "/" && src[i + 1] === "/") {
      var nl = src.indexOf("\n", i);
      i = nl === -1 ? src.length : nl + 1;
      continue;
    }
    if (ch === "/" && src[i + 1] === "*") {
      var end = src.indexOf("*/", i + 2);
      i = end === -1 ? src.length : end + 2;
      continue;
    }
    if (ch === "{") depth++;
    else if (ch === "}") {
      depth--;
      if (depth === 0) return i;
    }
    i++;
  }
  throw new Error("unbalanced braces from " + openIdx);
}

function extractFn(src, name) {
  var idx = src.indexOf("function " + name + "(");
  assert.notEqual(idx, -1, name + " defined in experiments.js");
  return src.slice(idx, balancedEnd(src, src.indexOf("{", idx)) + 1);
}

// expNameById (experiments.js:25-35) reads via CW.drafts/CW.state — stub both.
var expStub = {
  drafts: null,
  state: { experiments: [], experimentsLoaded: false },
};
var ectx = { CW: expStub };
vm.createContext(ectx);
vm.runInContext(
  ["round2", "weightRawHasTooManyDecimals", "valuesTextFor", "expNameById"]
    .map(function (n) { return extractFn(ESRC, n); })
    .join("\n") +
    "\nglobalThis.E = { round2: round2, " +
    "weightRawHasTooManyDecimals: weightRawHasTooManyDecimals, " +
    "valuesTextFor: valuesTextFor, expNameById: expNameById };",
  ectx,
  { filename: "experiments-pure-extract" }
);
var E = ectx.E;

var h = harness.load(["core.js", "experiments.js"]);
var CW = h.CW;

test("round2: banker's-free two-decimal rounding (experiments.js:274-276)", function () {
  assert.equal(E.round2(33.336), 33.34);
  assert.equal(E.round2(1.234), 1.23);
  assert.equal(E.round2(60), 60);
  assert.equal(E.round2(2.5), 2.5);
  assert.equal(E.round2(-1.234), -1.23);
});

test("weightRawHasTooManyDecimals: strict string + e/E fallback (experiments.js:300-310)", function () {
  assert.equal(E.weightRawHasTooManyDecimals(""), false);
  assert.equal(E.weightRawHasTooManyDecimals("50"), false);
  assert.equal(E.weightRawHasTooManyDecimals("33.33"), false);
  assert.equal(E.weightRawHasTooManyDecimals("33.330"), true);
  assert.equal(E.weightRawHasTooManyDecimals("33.336"), true);
  assert.equal(E.weightRawHasTooManyDecimals("1E2"), false);
  assert.equal(E.weightRawHasTooManyDecimals("1.5e-1"), false);
  assert.equal(E.weightRawHasTooManyDecimals("1.234E0"), true);
});

test("bpsToPercent/percentToBps round-trip incl. 10000<->100 (experiments.js:314-320)", function () {
  assert.equal(CW.bpsToPercent(10000), 100);
  assert.equal(CW.bpsToPercent(6000), 60);
  assert.equal(CW.bpsToPercent(1), 0.01);
  assert.equal(CW.percentToBps(100), 10000);
  assert.equal(CW.percentToBps(60), 6000);
  assert.equal(CW.percentToBps(0.01), 1);
  assert.equal(CW.percentToBps(CW.bpsToPercent(1234)), 1234);
  assert.equal(CW.bpsToPercent(CW.percentToBps(33.33)), 33.33);
});

test("validateVariants: full matrix incl. exact messages (experiments.js:324-355)", function () {
  // Results are node:vm-realm objects: pin exact shape via JSON strings.
  function check(input, expected) {
    assert.equal(JSON.stringify(CW.validateVariants(input)), expected);
  }
  check(
    [
      { name: "a", weightBps: 6000 },
      { name: "b", weightBps: 4000 },
    ],
    '{"ok":true,"sum":10000,"error":""}'
  );
  check([], '{"ok":false,"sum":0,"error":"experiment has no variants"}');
  check("x", '{"ok":false,"sum":0,"error":"variants must be an array"}');
  check(
    [{ name: "  ", weightBps: 10000 }],
    '{"ok":false,"sum":0,"error":"variant 1 has an empty name"}'
  );
  check(
    [
      { name: "a", weightBps: 5000 },
      { name: "a", weightBps: 5000 },
    ],
    '{"ok":false,"sum":5000,"error":"duplicate variant name: a"}'
  );
  check(
    [
      { name: "a", weightBps: -1 },
      { name: "b", weightBps: 10001 },
    ],
    '{"ok":false,"sum":0,"error":"variant \\"a\\" has negative weightBps"}'
  );
  check(
    [
      { name: "a", weightBps: 1.5 },
      { name: "b", weightBps: 9998 },
    ],
    '{"ok":false,"sum":0,"error":"variant \\"a\\" weightBps must be an integer"}'
  );
  check(
    [
      { name: "a", weightBps: 6000 },
      { name: "b", weightBps: 3999 },
    ],
    '{"ok":false,"sum":9999,"error":"weights sum 9999/10000 — must total 10000"}'
  );
  check(
    [{ name: "a" }],
    '{"ok":false,"sum":0,"error":"variant \\"a\\" weightBps must be an integer"}'
  );
});

test("expCountFor: state path null-until-loaded + drafts-stub path (experiments.js:37-47)", function () {
  CW.drafts = undefined;
  CW.state.experiments = [];
  CW.state.experimentsLoaded = false;
  assert.equal(CW.expCountFor("f1"), null);
  CW.state.experiments = [
    { id: "e1", flag: "f1" },
    { id: "e2", flag: "f1", _draftDeleted: true },
    { id: "e3", flag: "f2" },
  ];
  CW.state.experimentsLoaded = true;
  assert.equal(CW.expCountFor("f1"), 1);
  assert.equal(CW.expCountFor("f2"), 1);
  assert.equal(CW.expCountFor("nope"), 0);
  CW.drafts = {
    mergedExperiments: function () {
      return [{ flag: "f9" }, { flag: "f9" }];
    },
  };
  CW.state.experimentsLoaded = true;
  assert.equal(CW.expCountFor("f9"), 2);
  CW.drafts = undefined;
});

test("expNameById: hit/miss via extract + deleteExperiment label probe (experiments.js:25-35,245-251)", async function () {
  expStub.drafts = null;
  expStub.state.experiments = [{ id: "e1", name: "Exp One" }];
  assert.equal(E.expNameById("e1"), "Exp One");
  assert.equal(E.expNameById("missing"), "missing");
  var staged = [];
  CW.drafts = {
    mergedExperiments: function () {
      return [{ id: "e1", name: "Exp One" }];
    },
    draftStage: function (kind, entry) { staged.push({ kind: kind, entry: entry }); },
    refreshDraftChrome: function () {},
  };
  var res = await CW.deleteExperiment("e1");
  assert.equal(JSON.stringify(res), '{"status":200,"data":{}}');
  assert.equal(staged[0].kind, "experiment");
  assert.equal(staged[0].entry.label, "Exp One");
  await CW.deleteExperiment("ghost");
  assert.equal(staged[1].entry.label, "ghost");
  CW.drafts = undefined;
});

test("valuesTextFor: nullish/string/JSON branches (experiments.js:372-379)", function () {
  assert.equal(E.valuesTextFor(undefined), "");
  assert.equal(E.valuesTextFor(null), "");
  assert.equal(E.valuesTextFor("raw"), "raw");
  assert.equal(E.valuesTextFor({ launch_flag: true }), '{"launch_flag":true}');
  assert.equal(E.valuesTextFor(5), "5");
  assert.equal(E.valuesTextFor(true), "true");
});
