/* test-json-editor.js — pure + stub-DOM tests for json-editor.js (146 lines).
 * Loads core.js + json-editor.js only (never boot.js — avoids wiring).
 * node:test + node:assert/strict only. Run from configwire/:
 *   node --test js_tests/test-json-editor.js
 */
"use strict";

var test = require("node:test").test;
var assert = require("node:assert/strict");
var h = require("./harness.js");

function fresh() {
  return h.load(["core.js", "json-editor.js"]);
}

// CW return values are vm-realm objects (different prototypes), so
// deepStrictEqual rejects them; normalize through JSON first.
function dj(o) {
  return JSON.parse(JSON.stringify(o));
}

// Replace a harness stub's no-op classList with a call recorder.
function trackClassList(el) {
  var calls = [];
  el.classList = {
    add: function (a) { calls.push(["add", a]); },
    remove: function (a, b) { calls.push(["remove", a, b]); },
    toggle: function () {},
    contains: function () { return false; },
  };
  return calls;
}

test("parseJSONInput: empty string maps to null (json-editor.js:9)", function () {
  var ctx = fresh();
  assert.deepEqual(dj(ctx.CW.parseJSONInput("", "defaultValue")), { ok: true, value: null });
});

test("parseJSONInput: valid JSON parses", function () {
  var ctx = fresh();
  assert.deepEqual(dj(ctx.CW.parseJSONInput('{"a":1,"b":[1,2]}', "defaultValue")),
    { ok: true, value: { a: 1, b: [1, 2] } });
  assert.deepEqual(dj(ctx.CW.parseJSONInput("[1,2]", "v")), { ok: true, value: [1, 2] });
  assert.deepEqual(dj(ctx.CW.parseJSONInput("42", "v")), { ok: true, value: 42 });
});

test("parseJSONInput: invalid JSON reports label error", function () {
  var ctx = fresh();
  assert.deepEqual(dj(ctx.CW.parseJSONInput("{bad", "defaultValue")),
    { ok: false, error: "defaultValue is not valid JSON" });
  assert.deepEqual(dj(ctx.CW.parseJSONInput("[1,", "condition")),
    { ok: false, error: "condition is not valid JSON" });
});

test("parseJSONInput: whitespace-only is NOT null (only raw==='' short-circuits, json-editor.js:9)", function () {
  var ctx = fresh();
  // JSON.parse('  ') throws, so this takes the error branch per source.
  var out = ctx.CW.parseJSONInput("  ", "defaultValue");
  assert.equal(out.ok, false);
  assert.equal(out.error, "defaultValue is not valid JSON");
});

test("jsonDetail: blank/whitespace trims to null (json-editor.js:16)", function () {
  var ctx = fresh();
  assert.deepEqual(dj(ctx.CW.jsonDetail("")), { ok: true, value: null });
  assert.deepEqual(dj(ctx.CW.jsonDetail("   \n\t ")), { ok: true, value: null });
});

test("jsonDetail: valid JSON with surrounding whitespace parses", function () {
  var ctx = fresh();
  assert.deepEqual(dj(ctx.CW.jsonDetail('  {"x":1}  ')), { ok: true, value: { x: 1 } });
  assert.deepEqual(dj(ctx.CW.jsonDetail("null")), { ok: true, value: null });
});

test("jsonDetail: invalid JSON returns ok:false with engine message", function () {
  var ctx = fresh();
  var out = ctx.CW.jsonDetail("{bad");
  assert.equal(out.ok, false);
  assert.equal(typeof out.error, "string");
  assert.ok(out.error.length > 0);
});

test("get/setJsonEditorTarget: default flag-default (json-editor.js:53)", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.getJsonEditorTarget(), "flag-default");
  ctx.CW.setJsonEditorTarget("flag-rules-condition");
  assert.equal(ctx.CW.getJsonEditorTarget(), "flag-rules-condition");
  ctx.CW.setJsonEditorTarget("flag-default");
  assert.equal(ctx.CW.getJsonEditorTarget(), "flag-default");
});

test("setJsonHint: null hint element is a silent no-op (json-editor.js:25)", function () {
  var ctx = fresh();
  assert.equal(ctx.CW.setJsonHint(null, null, true, "x"), undefined);
  assert.equal(ctx.CW.setJsonHint(undefined, null, false, "x"), undefined);
});

test("setJsonHint: ok branch sets text + json-hint ok + valid class", function () {
  var ctx = fresh();
  var hint = { textContent: "", className: "" };
  var input = { classList: null };
  var calls = trackClassList(input);
  ctx.CW.setJsonHint(hint, input, true, "Valid JSON");
  assert.equal(hint.textContent, "Valid JSON");
  assert.equal(hint.className, "json-hint ok");
  assert.deepEqual(calls, [["remove", "valid", "invalid"], ["add", "valid"]]);
});

test("setJsonHint: err branch sets text + json-hint err + invalid class", function () {
  var ctx = fresh();
  var hint = { textContent: "", className: "" };
  var input = { classList: null };
  var calls = trackClassList(input);
  ctx.CW.setJsonHint(hint, input, false, "Invalid JSON: boom");
  assert.equal(hint.textContent, "Invalid JSON: boom");
  assert.equal(hint.className, "json-hint err");
  assert.deepEqual(calls, [["remove", "valid", "invalid"], ["add", "invalid"]]);
});

test("setJsonHint: empty message clears classes (json-editor.js:27-30)", function () {
  var ctx = fresh();
  var hint = { textContent: "stale", className: "json-hint err" };
  var input = { classList: null };
  var calls = trackClassList(input);
  ctx.CW.setJsonHint(hint, input, true, "");
  assert.equal(hint.textContent, "");
  assert.equal(hint.className, "json-hint");
  assert.deepEqual(calls, [["remove", "valid", "invalid"]]);
});

test("setJsonHint: null input element still sets hint (json-editor.js:28)", function () {
  var ctx = fresh();
  var hint = { textContent: "", className: "" };
  ctx.CW.setJsonHint(hint, null, true, "Valid JSON");
  assert.equal(hint.textContent, "Valid JSON");
  assert.equal(hint.className, "json-hint ok");
});

test("updateJsonHint: valid value shows Valid JSON and returns true", function () {
  var ctx = fresh();
  var input = ctx.CW.$("flag-default");
  var hint = ctx.CW.$("flag-default-hint");
  var calls = trackClassList(input);
  input.value = '{"a":1}';
  assert.equal(ctx.CW.updateJsonHint("flag-default"), true);
  assert.equal(hint.textContent, "Valid JSON");
  assert.equal(hint.className, "json-hint ok");
  assert.deepEqual(calls, [["remove", "valid", "invalid"], ["add", "valid"]]);
});

test("updateJsonHint: invalid value shows Invalid JSON + message and returns false", function () {
  var ctx = fresh();
  var input = ctx.CW.$("flag-default");
  var hint = ctx.CW.$("flag-default-hint");
  input.value = "{bad";
  assert.equal(ctx.CW.updateJsonHint("flag-default"), false);
  assert.ok(hint.textContent.indexOf("Invalid JSON: ") === 0);
  assert.equal(hint.className, "json-hint err");
});

test("updateJsonHint: blank value clears hint and returns true", function () {
  var ctx = fresh();
  var input = ctx.CW.$("flag-default");
  var hint = ctx.CW.$("flag-default-hint");
  hint.textContent = "stale";
  input.value = "  ";
  assert.equal(ctx.CW.updateJsonHint("flag-default"), true);
  assert.equal(hint.textContent, "");
  assert.equal(hint.className, "json-hint");
});

test("updateAllJsonHints: wires the three known inputs (json-editor.js:47-51)", function () {
  var ctx = fresh();
  ctx.CW.$("flag-default").value = '{"a":1}';
  ctx.CW.$("flag-rules-condition").value = "{bad";
  ctx.CW.$("flag-rules-value").value = "";
  assert.equal(ctx.CW.updateAllJsonHints(), undefined);
  assert.equal(ctx.CW.$("flag-default-hint").textContent, "Valid JSON");
  assert.ok(ctx.CW.$("flag-rules-condition-hint").textContent.indexOf("Invalid JSON: ") === 0);
  assert.equal(ctx.CW.$("flag-rules-value-hint").textContent, "");
});

test("openJsonEditorFor: pretty-prints valid JSON + sets title (json-editor.js:77-89)", function () {
  var ctx = fresh();
  ctx.CW.$("flag-default").value = '{"a":1}';
  ctx.CW.openJsonEditorFor("flag-default");
  assert.equal(ctx.CW.getJsonEditorTarget(), "flag-default");
  assert.equal(ctx.CW.$("json-editor-title").textContent, "Edit defaultValue (JSON)");
  assert.equal(ctx.CW.$("json-editor-text").value, '{\n  "a": 1\n}');
  assert.equal(ctx.CW.$("json-editor-status").textContent, "Valid JSON");
});

test("openJsonEditorFor: invalid JSON passes through raw + unknown id uses raw label", function () {
  var ctx = fresh();
  ctx.CW.$("custom-x").value = "not-json";
  ctx.CW.openJsonEditorFor("custom-x");
  assert.equal(ctx.CW.getJsonEditorTarget(), "custom-x");
  assert.equal(ctx.CW.$("json-editor-title").textContent, "Edit custom-x (JSON)");
  assert.equal(ctx.CW.$("json-editor-text").value, "not-json");
});

test("openJsonEditor: string id path delegates to openJsonEditorFor", function () {
  var ctx = fresh();
  ctx.CW.$("flag-rules-value").value = "[1,2]";
  ctx.CW.openJsonEditor("flag-rules-value");
  assert.equal(ctx.CW.getJsonEditorTarget(), "flag-rules-value");
  assert.equal(ctx.CW.$("json-editor-title").textContent, "Edit value (JSON)");
  assert.equal(ctx.CW.$("json-editor-text").value, "[\n  1,\n  2\n]");
});

test("openJsonEditor: event data-target path (json-editor.js:106-113)", function () {
  var ctx = fresh();
  ctx.CW.$("flag-default").value = '{"k":true}';
  var ev = { target: { closest: function () { return { getAttribute: function () { return "flag-default"; } }; } } };
  ctx.CW.openJsonEditor(ev);
  assert.equal(ctx.CW.getJsonEditorTarget(), "flag-default");
  assert.equal(ctx.CW.$("json-editor-text").value, '{\n  "k": true\n}');
});

test("openJsonEditor: fallback opens current target", function () {
  var ctx = fresh();
  ctx.CW.setJsonEditorTarget("flag-rules-condition");
  ctx.CW.$("flag-rules-condition").value = '{"op":"eq"}';
  ctx.CW.openJsonEditor({});
  assert.equal(ctx.CW.getJsonEditorTarget(), "flag-rules-condition");
  assert.equal(ctx.CW.$("json-editor-title").textContent, "Edit condition (JSON)");
});

function armDialog(ctx) {
  var dlg = ctx.CW.$("json-editor-dialog");
  dlg.open = true;
  var closed = { n: 0 };
  dlg.close = function () { closed.n++; dlg.open = false; };
  return { dlg: dlg, closed: closed };
}

test("closeJsonEditor save: valid JSON copies textarea into target + closes", function () {
  var ctx = fresh();
  ctx.CW.setJsonEditorTarget("flag-default");
  ctx.CW.$("flag-default").value = "old";
  ctx.CW.$("json-editor-text").value = '{"b":2}';
  var d = armDialog(ctx);
  ctx.CW.closeJsonEditor(true);
  assert.equal(ctx.CW.$("flag-default").value, '{"b":2}');
  assert.equal(ctx.CW.$("flag-default-hint").textContent, "Valid JSON");
  assert.equal(d.closed.n, 1);
  assert.equal(d.dlg.open, false);
});

test("closeJsonEditor save: invalid JSON keeps editor open, target untouched (json-editor.js:121)", function () {
  var ctx = fresh();
  ctx.CW.setJsonEditorTarget("flag-default");
  ctx.CW.$("flag-default").value = "old";
  ctx.CW.$("json-editor-text").value = "{bad";
  var d = armDialog(ctx);
  ctx.CW.closeJsonEditor(true);
  assert.equal(ctx.CW.$("flag-default").value, "old");
  assert.ok(ctx.CW.$("json-editor-status").textContent.indexOf("Invalid JSON: ") === 0);
  assert.equal(d.closed.n, 0);
  assert.equal(d.dlg.open, true);
});

test("closeJsonEditor cancel: closes without touching target", function () {
  var ctx = fresh();
  ctx.CW.setJsonEditorTarget("flag-default");
  ctx.CW.$("flag-default").value = "old";
  ctx.CW.$("json-editor-text").value = "{bad";
  var d = armDialog(ctx);
  ctx.CW.closeJsonEditor(false);
  assert.equal(ctx.CW.$("flag-default").value, "old");
  assert.equal(d.closed.n, 1);
});

test("closeJsonEditor save on flag-rules-condition calls syncRuleBuilderFromCondition (json-editor.js:126-128)", function () {
  var ctx = fresh();
  var synced = { n: 0 };
  ctx.CW.syncRuleBuilderFromCondition = function () { synced.n++; };
  ctx.CW.setJsonEditorTarget("flag-rules-condition");
  ctx.CW.$("flag-rules-condition").value = "{}";
  ctx.CW.$("json-editor-text").value = '{"op":"eq"}';
  var d = armDialog(ctx);
  ctx.CW.closeJsonEditor(true);
  assert.equal(synced.n, 1);
  assert.equal(ctx.CW.$("flag-rules-condition").value, '{"op":"eq"}');
  assert.equal(d.closed.n, 1);
});
