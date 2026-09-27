/* ConfigWire js_tests — flags pure helpers (no fetch, render stubs only).
 * Loads core+drafts+flags via harness. Most flag helpers are IIFE-private
 * (flags.js), so assertions go through their exported observable surface:
 * - flagTypeExpectation (flags.js:523-531) via updateFlagDefaultHint's
 *   "Invalid defaultValue: <expectation>" hint text (flags.js:497).
 * - flagDefaultMatchesType (flags.js:534-544) via updateFlagDefaultHint +
 *   syncFlagDefaultForType value preservation/reset (flags.js:551-567).
 * - sortedGroupIds/flagsInGroup/groupNameById (flags.js:34-51),
 *   isUnpub/draftOpOf/isDeleted/selectableGroupIds (flags.js:53-72),
 *   folderRowHTML/folderHTML (flags.js:74-144) via renderFlags box HTML and
 *   the flag-group select options.
 * Drafts are seeded via real CW.drafts.draftStage (drafts.js:121-172).
 */
"use strict";

var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

var h = harness.load(["core.js", "drafts.js", "flags.js"]);
// Render stubs for files outside this suite's scope: json-editor's hint
// writer (flags.js:494 calls CW.updateJsonHint) and stats' flag-option
// renderer (renderFlags calls CW.renderStatsFlagOptions, flags.js:32).
h.CW.updateJsonHint = function () { return "hint-stub"; };
h.CW.renderStatsFlagOptions = function () {};

function reset() {
  h.CW.state.flags = [];
  h.CW.state.groups = {};
  h.CW.state.rules = [];
  h.CW.state.rulesLoaded = false;
  h.CW.state.collapsedGroups = {};
  h.CW.drafts.draftClearAll();
  el("flag-folders").innerHTML = "";
  el("flag-group").innerHTML = "";
  el("flag-default-hint").textContent = "";
  el("flag-default-hint").className = "";
}

function el(id) { return h.CW.$(id); }

// Set the flag dialog stub inputs, mirroring openFlagDialog field ids.
function setDefaultInput(raw, type) {
  el("flag-default").value = raw;
  el("flag-default").placeholder = "";
  el("flag-type").value = type;
  el("flag-default-hint").textContent = "";
  el("flag-default-hint").className = "";
}

test("flagTypeExpectation: exact hint string per type on mismatch", function () {
  reset();
  var cases = [
    ["0", "bool", "Invalid defaultValue: expected bool (true or false)"],
    ["true", "number", "Invalid defaultValue: expected number (e.g. 0)"],
    ["5", "string", 'Invalid defaultValue: expected string (JSON quoted, e.g. "hello")'],
    ["5", "json", "Invalid defaultValue: expected JSON object or array (e.g. {} or [])"],
  ];
  for (var i = 0; i < cases.length; i++) {
    setDefaultInput(cases[i][0], cases[i][1]);
    var ret = h.CW.updateFlagDefaultHint();
    assert.equal(ret, false);
    assert.equal(el("flag-default-hint").textContent, cases[i][2]);
    assert.equal(el("flag-default-hint").className, "json-hint err");
  }
});

test("flagDefaultMatchesType: null always valid, JSON-string coercion per type", function () {
  reset();
  // null (empty/blank/"null") is valid for every type — publish skips nil
  // (flags.js:535). Returns the json-editor hint stub, writes no error.
  setDefaultInput("null", "bool");
  assert.equal(h.CW.updateFlagDefaultHint(), "hint-stub");
  assert.equal(el("flag-default-hint").textContent, "");
  setDefaultInput("   ", "number");
  assert.equal(h.CW.updateFlagDefaultHint(), "hint-stub");

  // JSON-string inputs coerce through JSON.parse before the type check
  // (flags.js:491-494): '"hi"' parses to a string, so it matches string
  // but not number — sync preserves matches, resets mismatches to the
  // canonical default (flags.js:559-566, defaults at flags.js:509-514).
  var keep = [
    ["true", "bool", "true"],
    ['"hi"', "string", '"hi"'],
    ["[1,2]", "json", "[1,2]"],
    ['{"a":1}', "json", '{"a":1}'],
    ["7", "number", "7"],
  ];
  for (var i = 0; i < keep.length; i++) {
    setDefaultInput(keep[i][0], keep[i][1]);
    h.CW.syncFlagDefaultForType();
    assert.equal(el("flag-default").value, keep[i][2]);
  }
  var resetCases = [
    ['"oops"', "bool", "false"],
    ['"hi"', "number", "0"],
    ["5", "string", '""'],
    ['"x"', "json", "{}"],
    ["oops-not-json", "bool", "false"],
  ];
  for (var j = 0; j < resetCases.length; j++) {
    setDefaultInput(resetCases[j][0], resetCases[j][1]);
    h.CW.syncFlagDefaultForType();
    assert.equal(el("flag-default").value, resetCases[j][2]);
  }
});

test("flagDefaultMatchesType: unknown type is lenient, placeholder untouched", function () {
  reset();
  // Default branch returns true (flags.js:543), so the value is preserved
  // and the placeholder guard (flags.js:556) leaves unknown types alone.
  // The unknown branch of flagTypeExpectation (flags.js:529) is therefore
  // unreachable through exports — leniency is the observable contract.
  setDefaultInput('{"a":1}', "weird");
  h.CW.syncFlagDefaultForType();
  assert.equal(el("flag-default").value, '{"a":1}');
  assert.equal(el("flag-default").placeholder, "");
  assert.equal(h.CW.updateFlagDefaultHint(), "hint-stub");
});

test("syncFlagDefaultForType: placeholder per known type", function () {
  reset();
  var cases = [
    ["bool", "false"],
    ["number", "0"],
    ["string", '"hello"'],
    ["json", '{"key": "value"}'],
  ];
  for (var i = 0; i < cases.length; i++) {
    setDefaultInput("", cases[i][0]);
    h.CW.syncFlagDefaultForType();
    assert.equal(el("flag-default").placeholder, cases[i][1]);
  }
});

test("flagKeyById: hit returns key, miss returns empty string", function () {
  reset();
  h.CW.state.flags = [{ id: "f1", key: "launch_flag" }];
  assert.equal(h.CW.flagKeyById("f1"), "launch_flag");
  assert.equal(h.CW.flagKeyById("nope"), "");
});

test("sortedGroupIds: folders render case-insensitive by name", function () {
  reset();
  h.CW.state.groups = { g2: "banana", g1: "Apple", g3: "cherry" };
  h.CW.renderFlags();
  var html = el("flag-folders").innerHTML;
  var iApple = html.indexOf(">Apple<");
  var iBanana = html.indexOf(">banana<");
  var iCherry = html.indexOf(">cherry<");
  assert.ok(iApple >= 0 && iBanana >= 0 && iCherry >= 0);
  assert.ok(iApple < iBanana && iBanana < iCherry);
});

test("folderHTML/folderRowHTML: escaping of names, keys, descriptions", function () {
  reset();
  h.CW.state.groups = { g1: "A<B>&\"Q\"" };
  h.CW.state.flags = [{
    id: "f1", key: "<img>", description: "d&\"x\"",
    type: "bool", defaultValue: false, group: "g1",
  }];
  h.CW.renderFlags();
  var html = el("flag-folders").innerHTML;
  assert.ok(html.indexOf("A&lt;B&gt;&amp;&quot;Q&quot;") >= 0);
  assert.ok(html.indexOf("&lt;img&gt;") >= 0);
  assert.ok(html.indexOf("d&amp;&quot;x&quot;") >= 0);
  assert.ok(html.indexOf("<img>") < 0);
});

test("flagsInGroup: ungrouped flags render a Default folder; empty renders notice", function () {
  reset();
  h.CW.state.groups = { g1: "G" };
  h.CW.state.flags = [{ id: "f1", key: "solo", type: "bool", defaultValue: true }];
  h.CW.renderFlags();
  var html = el("flag-folders").innerHTML;
  assert.ok(html.indexOf(">Default<") >= 0);
  assert.ok(html.indexOf("solo") >= 0);

  reset();
  h.CW.renderFlags();
  assert.ok(el("flag-folders").innerHTML.indexOf("No flags in this group.") >= 0);
});

test("isUnpub/draftOpOf/isDeleted: update badge vs delete tombstone", function () {
  reset();
  h.CW.state.groups = { g1: "G" };
  h.CW.state.flags = [
    { id: "f1", key: "upd", type: "bool", defaultValue: false, group: "g1" },
    { id: "f2", key: "del", type: "bool", defaultValue: false, group: "g1" },
  ];
  h.CW.drafts.draftStage("flag", { op: "update", body: { description: "new" }, baseId: "f1", label: "upd" });
  h.CW.drafts.draftStage("flag", { op: "delete", body: {}, baseId: "f2", label: "del deleted" });
  h.CW.renderFlags();
  var html = el("flag-folders").innerHTML;
  assert.ok(html.indexOf('class="is-unpublished"') >= 0);
  assert.ok(html.indexOf('<span class="badge unpublished">Unpublished</span>') >= 0);
  assert.ok(html.indexOf('class="is-deleted"') >= 0);
  assert.ok(html.indexOf('<span class="badge deleted">Deleted</span>') >= 0);
  // Deleted flag's move select is disabled (flags.js:91,109-110).
  assert.ok(html.indexOf(" disabled>") >= 0);
});

test("flag rows highlight when child rules/experiments have drafts", function () {
  reset();
  h.CW.state.flags = [
    { id: "f1", key: "withrule", type: "bool", defaultValue: false },
    { id: "f2", key: "withexp", type: "bool", defaultValue: false },
    { id: "f3", key: "clean", type: "bool", defaultValue: false },
  ];
  h.CW.state.rules = [{ id: "r1", flag: "f1", priority: 0, condition: {}, value: true }];
  h.CW.state.rulesLoaded = true;
  h.CW.state.experiments = [{ id: "x1", flag: "f2", name: "E" }];
  h.CW.drafts.draftStage("rule", {
    op: "update", baseId: "r1",
    body: { flag: "f1", priority: 0, condition: {}, value: false }, label: "rule",
  });
  h.CW.drafts.draftStage("experiment", { op: "update", baseId: "x1", body: { status: "running" }, label: "E" });
  h.CW.renderFlags();
  var html = el("flag-folders").innerHTML;
  var rows = html.match(/<tr class="is-unpublished">/g) || [];
  assert.equal(rows.length, 2);
  var badges = html.match(/<span class="badge unpublished">Unpublished<\/span>/g) || [];
  assert.equal(badges.length, 2);
  assert.ok(html.indexOf("clean</td>") >= 0);
  assert.ok(html.indexOf('class="folder is-unpublished"') >= 0);
  function menuHasDot(attr, id) {
    var at = html.indexOf(attr + '="' + id + '"');
    if (at < 0) return false;
    var end = html.indexOf("</button>", at);
    if (end < 0) return false;
    return html.slice(at, end).indexOf("dot dirty") >= 0;
  }
  assert.equal(menuHasDot("data-flag-rules", "f1"), true);
  assert.equal(menuHasDot("data-flag-experiments", "f2"), true);
  assert.equal(menuHasDot("data-flag-rules", "f2"), false);
  assert.equal(menuHasDot("data-flag-experiments", "f1"), false);
  assert.equal(menuHasDot("data-flag-rules", "f3"), false);
  assert.equal(menuHasDot("data-flag-experiments", "f3"), false);
});

test("selectableGroupIds: deleted group excluded from the group select, folder marked", function () {
  reset();
  h.CW.state.groups = { g1: "Keep", g2: "Drop" };
  h.CW.drafts.draftStage("group", { op: "delete", body: {}, baseId: "g2", memberIds: [], label: "Drop" });
  h.CW.renderFlags();
  var sel = el("flag-group").innerHTML;
  assert.ok(sel.indexOf(">Keep<") >= 0);
  assert.ok(sel.indexOf(">Drop<") < 0);
  var html = el("flag-folders").innerHTML;
  assert.ok(html.indexOf('class="folder is-deleted"') >= 0);
});

test("ruleCountFor: rules label until loaded, count once loaded", function () {
  reset();
  h.CW.state.flags = [{ id: "f1", key: "f", type: "bool", defaultValue: false }];
  h.CW.renderFlags();
  assert.ok(el("flag-folders").innerHTML.indexOf(">rules<") >= 0);
  h.CW.state.rulesLoaded = true;
  h.CW.state.rules = [{ id: "r1", flag: "f1", priority: 0, condition: {}, value: true }];
  h.CW.renderFlags();
  assert.ok(el("flag-folders").innerHTML.indexOf("rules (1)") >= 0);
});

test("no fetch performed by any pure-flags path", function () {
  assert.equal(h.fetchCalls.length, 0);
});
