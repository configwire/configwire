/* test-rules.js — pure helper tests for rules.js (loaded via harness as core+rules).
 * Internals (trunc, fmtOperand, sentenceFor, normalizeCondition, ruleBaseField,
 * toFiniteNumber, builderValueFor, condValueToText) are IIFE-closed in
 * rules.js:2-343, so they are extracted verbatim from source and evaluated in a
 * stub scope; exported surface (conditionHTML, valueHTML, CONDITION_OPS,
 * loadRules, deleteRule) is asserted through the harness CW object.
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
function srcOf(name) {
  return fs.readFileSync(path.join(JS_DIR, name), "utf8");
}

// Balanced-brace scanner skipping strings/comments (rules.js is ES5: no
// template literals; regexes below contain no quotes/braces-in-class).
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
  assert.notEqual(idx, -1, name + " defined in rules.js");
  return src.slice(idx, balancedEnd(src, src.indexOf("{", idx)) + 1);
}

// Build the pure-helper scope from verbatim source: consts (rules.js:7-8) +
// CONDITION_OPS (rules.js:125-132) + the IIFE-internal helpers.
var RSRC = srcOf("rules.js");
var TRUNC_DECL = RSRC.match(/var TRUNC_COND = \d+;\n  var TRUNC_VALUE = \d+;/)[0];
var OPS_IDX = RSRC.indexOf("var CONDITION_OPS = ");
var OPS_DECL = RSRC.slice(OPS_IDX, balancedEnd(RSRC, RSRC.indexOf("{", OPS_IDX)) + 1) + ";";
var RULE_FNS = [
  "trunc",
  "fmtOperand",
  "sentenceFor",
  "normalizeCondition",
  "ruleBaseField",
  "toFiniteNumber",
  "builderValueFor",
  "condValueToText",
].map(function (n) { return extractFn(RSRC, n); });

// builderValueFor (rules.js:198-225) reads inputs via CW.$ — map-backed stub.
var fakeEls = {};
var fakeCW = {
  $: function (id) {
    return Object.prototype.hasOwnProperty.call(fakeEls, id) ? fakeEls[id] : null;
  },
};
function el(value) {
  return { value: value };
}
var rctx = { CW: fakeCW };
vm.createContext(rctx);
vm.runInContext(
  TRUNC_DECL + "\n" + OPS_DECL + "\n" + RULE_FNS.join("\n") +
    "\nglobalThis.R = { trunc: trunc, fmtOperand: fmtOperand, sentenceFor: sentenceFor, " +
    "normalizeCondition: normalizeCondition, ruleBaseField: ruleBaseField, " +
    "toFiniteNumber: toFiniteNumber, builderValueFor: builderValueFor, " +
    "condValueToText: condValueToText, CONDITION_OPS: CONDITION_OPS };",
  rctx,
  { filename: "rules-pure-extract" }
);
var R = rctx.R;

// Exported surface via harness (core+rules only — drafts.js never loaded).
var h = harness.load(["core.js", "rules.js"]);
var CW = h.CW;
var staged = [];
CW.drafts = {
  restoreDrafts: function () {},
  draftStage: function (kind, entry) { staged.push({ kind: kind, entry: entry }); },
  refreshDraftChrome: function () {},
  hasDrafts: function () { return false; },
};

test("trunc: empty/short/boundary/long ASCII (rules.js:10-14)", function () {
  assert.equal(R.trunc("", 48), "");
  assert.equal(R.trunc("abc", 48), "abc");
  assert.equal(R.trunc("a".repeat(48), 48), "a".repeat(48));
  var out = R.trunc("a".repeat(50), 48);
  assert.equal(out, "a".repeat(47) + "…");
  assert.equal(out.length, 48);
});

test("fmtOperand: string/number/bool/null/undefined/array/object (rules.js:16-27)", function () {
  assert.equal(R.fmtOperand("hello"), "hello");
  assert.equal(R.fmtOperand(42), "42");
  assert.equal(R.fmtOperand(true), "true");
  assert.equal(R.fmtOperand(false), "false");
  assert.equal(R.fmtOperand(null), "null");
  assert.equal(R.fmtOperand(undefined), "null");
  assert.equal(R.fmtOperand(["a", "b"]), "a, b");
  assert.equal(R.fmtOperand([1, 2]), "1, 2");
  assert.equal(R.fmtOperand({ a: 1 }), '{"a":1}');
  assert.equal(R.fmtOperand("x".repeat(60)), "x".repeat(47) + "…");
});

test("sentenceFor: between bounds + seed suffix + unknown-op fallback (rules.js:29-42)", function () {
  assert.equal(
    R.sentenceFor({ field: "percentile", op: "between", value: [10, 20] }),
    "percentile between 10 and 20"
  );
  assert.equal(
    R.sentenceFor({ field: "country", op: "==", value: "US", seed: "s1" }),
    "country == US · seed s1"
  );
  assert.equal(
    R.sentenceFor({ field: "country", op: "frobnicate", value: "x" }),
    "country frobnicate x"
  );
  assert.equal(R.sentenceFor(null), null);
  assert.equal(R.sentenceFor([]), null);
  assert.equal(R.sentenceFor({}), null);
  assert.equal(R.sentenceFor({ field: "", op: "==", value: "x" }), null);
  assert.equal(R.sentenceFor({ field: "a", value: "x" }), null);
});

test("normalizeCondition: JSON string/object passthrough/garbage fallback (rules.js:44-50)", function () {
  // Cross-realm results (node:vm literals) pin via exact JSON, not deepEqual.
  assert.equal(JSON.stringify(R.normalizeCondition('{"a":1}')), '{"a":1}');
  var obj = { field: "country", op: "==", value: "US" };
  assert.equal(R.normalizeCondition(obj), obj);
  assert.equal(R.normalizeCondition("{oops"), null);
  assert.equal(R.normalizeCondition(42), 42);
});

test("conditionHTML: single/array sentences + fallback + CW.esc escaping (rules.js:60-77)", function () {
  assert.equal(
    CW.conditionHTML({ field: "country", op: "==", value: "US" }),
    '<span class="rule-cond">country == US</span>'
  );
  assert.equal(
    CW.conditionHTML([
      { field: "country", op: "==", value: "US" },
      { field: "platform", op: "!=", value: "ios" },
    ]),
    '<span class="rule-cond">country == US and platform != ios</span>'
  );
  assert.equal(CW.conditionHTML("{oops"), "<code>{oops</code>");
  assert.equal(
    CW.conditionHTML({ field: "f", op: "==", value: "<b>&" }),
    '<span class="rule-cond">f == &lt;b&gt;&amp;</span>'
  );
});

test("valueHTML: bool branches + esc/trunc of JSON (rules.js:79-88)", function () {
  assert.equal(CW.valueHTML(true), '<span class="rule-value is-true">true</span>');
  assert.equal(CW.valueHTML(false), '<span class="rule-value is-false">false</span>');
  assert.equal(CW.valueHTML(7), '<span class="rule-value">7</span>');
  assert.equal(CW.valueHTML(null), '<span class="rule-value">null</span>');
  assert.equal(
    CW.valueHTML("a<b"),
    '<span class="rule-value">&quot;a&lt;b&quot;</span>'
  );
});

test("CONDITION_OPS allowlist + ruleBaseField mapping (rules.js:125-139)", function () {
  assert.equal(JSON.stringify(CW.CONDITION_OPS.platform), '["==","!=","contains","regex"]');
  assert.equal(JSON.stringify(CW.CONDITION_OPS.percentile), '["<=","between"]');
  assert.equal(JSON.stringify(R.CONDITION_OPS), JSON.stringify(CW.CONDITION_OPS));
  assert.equal(R.ruleBaseField("platform"), "platform");
  assert.equal(R.ruleBaseField("percentile"), "percentile");
  assert.equal(R.ruleBaseField("custom."), "custom.");
  assert.equal(R.ruleBaseField("custom.plan"), "custom.");
  assert.equal(R.ruleBaseField("nope"), null);
  assert.equal(R.ruleBaseField(""), null);
  assert.equal(R.ruleBaseField(null), null);
});

test("toFiniteNumber: numeric strings vs fallback (rules.js:191-194)", function () {
  assert.equal(R.toFiniteNumber(42, 0), 42);
  assert.equal(R.toFiniteNumber("3.5", 0), 3.5);
  assert.equal(R.toFiniteNumber("abc", 7), 7);
  assert.equal(R.toFiniteNumber("", 9), 9);
  assert.equal(R.toFiniteNumber("   ", 3), 3);
  assert.equal(R.toFiniteNumber(null, 5), 5);
  assert.equal(R.toFiniteNumber(undefined, 5), 5);
});

test("builderValueFor: percentile single/between + custom coercion (rules.js:198-225)", function () {
  fakeEls["flag-rules-cond-value"] = el("123");
  assert.equal(R.builderValueFor("percentile", "<="), 123);
  fakeEls["flag-rules-cond-value"] = el("  ");
  assert.equal(R.builderValueFor("percentile", "<="), 5000);
  fakeEls["flag-rules-cond-value"] = el("abc");
  assert.equal(R.builderValueFor("percentile", "<="), 5000);
  fakeEls["flag-rules-cond-lo"] = el("10");
  fakeEls["flag-rules-cond-hi"] = el("20");
  assert.equal(JSON.stringify(R.builderValueFor("percentile", "between")), "[10,20]");
  fakeEls["flag-rules-cond-value"] = el('{"a":1}');
  assert.equal(JSON.stringify(R.builderValueFor("custom.", "==")), '{"a":1}');
  fakeEls["flag-rules-cond-value"] = el("42");
  assert.equal(R.builderValueFor("custom.", "=="), 42);
  fakeEls["flag-rules-cond-value"] = el("not json{");
  assert.equal(R.builderValueFor("custom.", "=="), "not json{");
  fakeEls["flag-rules-cond-value"] = el("ios");
  assert.equal(R.builderValueFor("platform", "=="), "ios");
});

test("condValueToText: nullish/primitive/JSON branches (rules.js:227-233)", function () {
  assert.equal(R.condValueToText(undefined), "");
  assert.equal(R.condValueToText(null), "");
  assert.equal(R.condValueToText("x"), "x");
  assert.equal(R.condValueToText(5), "5");
  assert.equal(R.condValueToText(false), "false");
  assert.equal(R.condValueToText({ a: 1 }), '{"a":1}');
  assert.equal(R.condValueToText([1, 2]), "[1,2]");
});

test("loadRules sorts by priority + deleteRule stages draft (rules.js:90-111)", async function () {
  var origApiAll = CW.apiAll;
  CW.apiAll = function () {
    return Promise.resolve([
      { id: "b", priority: 2 },
      { id: "a", priority: 1 },
    ]);
  };
  try {
    await CW.loadRules();
  } finally {
    CW.apiAll = origApiAll;
  }
  assert.equal(CW.state.rulesLoaded, true);
  assert.equal(
    JSON.stringify(CW.state.rules.map(function (r) { return r.id; })),
    '["a","b"]'
  );
  staged.length = 0;
  var res = await CW.deleteRule("r1");
  assert.equal(JSON.stringify(res), '{"status":200,"data":{}}');
  assert.equal(
    JSON.stringify(staged),
    '[{"kind":"rule","entry":{"op":"delete","body":{},"baseId":"r1","label":"rule deleted"}}]'
  );
});
