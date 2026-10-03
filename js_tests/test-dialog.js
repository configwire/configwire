"use strict";
// test-dialog.js — dialog.js flows (Wave 3, todo 9).
// Loads core.js + dialog.js only via js_tests/harness.js (never boot.js/scope.js).
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-dialog.js
var test = require("node:test");
var assert = require("node:assert/strict");
var h = require("./harness.js");

function fresh(overrides) {
  return h.load(["core.js", "dialog.js"], overrides);
}

// dialog.js:10-16 element ids.
var IDS = {
  dlg: "cw-dialog",
  title: "cw-dialog-title",
  message: "cw-dialog-message",
  input: "cw-dialog-input",
  error: "cw-dialog-error",
  ok: "cw-dialog-ok",
  cancel: "cw-dialog-cancel",
};

function el(ctx, id) {
  // getElementById force-creates the stub on first touch (harness.js:159-162);
  // the raw elementsById map is undefined until then.
  return ctx.document.getElementById(id);
}

// Fire stub listeners of a type on a harness element stub (dialog.js:216-249
// wires ok/cancel click, dlg cancel+click, input keydown+input).
function fire(stub, type, ev) {
  var found = 0;
  (stub.listeners || []).forEach(function (l) {
    if (l.type === type) { found++; l.fn.call(stub, ev || {}); }
  });
  return found;
}

function clickCount(stub, type) {
  return (stub.listeners || []).filter(function (l) { return l.type === type; }).length;
}

function flush() {
  return new Promise(function (r) { setImmediate(r); });
}

// --- validate: required branch (dialog.js:82-94, error text dialog.js:85) ---

test("prompt required empty blocks OK with exact error text, promise stays pending", async function () {
  var ctx = fresh();
  var settled = false;
  var settledValue = "unset";
  var p = ctx.CW.promptDialog("Enter name:", "", { required: true });
  p.then(function (v) { settled = true; settledValue = v; });
  assert.equal(el(ctx, IDS.input).value, "");
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  await flush();
  assert.equal(settled, false, "must stay pending while validation fails");
  assert.equal(el(ctx, IDS.error).textContent, "This field is required.");
  assert.equal(el(ctx, IDS.error).hidden, false);
  // Fix the input, OK again: resolves with the typed value.
  el(ctx, IDS.input).value = "Ada";
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(await p, "Ada");
  assert.equal(settledValue, "Ada");
});

// --- validate: minLength branch (dialog.js:88-91) ---

test("prompt minLength pins exact error text then resolves once valid", async function () {
  var ctx = fresh();
  var settled = false;
  var p = ctx.CW.promptDialog("Pick a password:", "abc", { required: true, minLength: 8 });
  p.then(function () { settled = true; });
  assert.equal(el(ctx, IDS.input).value, "abc");
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  await flush();
  assert.equal(settled, false);
  assert.equal(el(ctx, IDS.error).textContent, "Enter at least 8 characters.");
  el(ctx, IDS.input).value = "abcdefgh";
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(await p, "abcdefgh");
});

// --- validate: mustMatch branch (OK disabled until exact match, no hint) ---

test("prompt mustMatch keeps OK disabled until the exact value is typed, with no hint", async function () {
  var ctx = fresh();
  var settled = false;
  var p = ctx.CW.promptDialog("Type the project name to confirm:", "", { mustMatch: "demo" });
  p.then(function () { settled = true; });
  assert.equal(el(ctx, IDS.ok).disabled, true, "empty input starts disabled");
  el(ctx, IDS.input).value = "Demo";
  assert.equal(fire(el(ctx, IDS.input), "input"), 1);
  assert.equal(el(ctx, IDS.ok).disabled, true, "case-differing value keeps OK disabled");
  assert.equal(el(ctx, IDS.error).textContent, "", "no hint shown");
  await flush();
  assert.equal(settled, false);
  el(ctx, IDS.input).value = "demo";
  assert.equal(fire(el(ctx, IDS.input), "input"), 1);
  assert.equal(el(ctx, IDS.ok).disabled, false, "exact match enables OK");
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(await p, "demo");
});

test("prompt mustMatch Enter on mismatch stays pending with no hint", async function () {
  var ctx = fresh();
  var settled = false;
  var p = ctx.CW.promptDialog("Confirm:", "", { mustMatch: "demo" });
  p.then(function () { settled = true; });
  el(ctx, IDS.input).value = "nope";
  assert.equal(fire(el(ctx, IDS.input), "keydown", {
    key: "Enter",
    preventDefault: function () {},
  }), 1);
  await flush();
  assert.equal(settled, false);
  assert.equal(el(ctx, IDS.error).textContent, "", "no hint shown");
});

test("OK disabled state does not leak into the next dialog", async function () {
  var ctx = fresh();
  var p1 = ctx.CW.promptDialog("Confirm:", "", { mustMatch: "demo" });
  assert.equal(el(ctx, IDS.ok).disabled, true);
  assert.equal(fire(el(ctx, IDS.cancel), "click"), 1);
  assert.strictEqual(await p1, null);
  var p2 = ctx.CW.confirmDialog("Proceed?");
  assert.equal(el(ctx, IDS.ok).disabled, false);
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.strictEqual(await p2, true);
});

test("prompt without opts resolves empty string (no required gate)", async function () {
  var ctx = fresh();
  var p = ctx.CW.promptDialog("Optional note:");
  assert.equal(el(ctx, IDS.input).value, "");
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(await p, "");
});

// --- ensureMarkup once-only wiring (dialog.js:210-213) ---

test("wireOnce attaches exactly one click listener per control across dialogs", function () {
  var ctx = fresh();
  ctx.CW.confirmDialog("first?");
  ctx.CW.confirmDialog("second?"); // supersedes pending (dialog.js:122-131)
  ctx.CW.alertDialog("third?");
  assert.equal(clickCount(el(ctx, IDS.ok), "click"), 1, "ok wired once");
  assert.equal(clickCount(el(ctx, IDS.cancel), "click"), 1, "cancel wired once");
  assert.equal(clickCount(el(ctx, IDS.dlg), "cancel"), 1, "dlg cancel wired once");
  assert.equal(clickCount(el(ctx, IDS.dlg), "click"), 1, "dlg backdrop wired once");
  assert.equal(clickCount(el(ctx, IDS.input), "keydown"), 1, "input keydown wired once");
  assert.equal(clickCount(el(ctx, IDS.input), "input"), 1, "input live-clear wired once");
});

// --- openDialog: showModal path + ok focus (dialog.js:173-187) ---

test("confirm uses showModal and focuses OK", async function () {
  var ctx = fresh();
  var dlg = el(ctx, IDS.dlg);
  var shown = 0;
  dlg.showModal = function () { shown++; dlg.open = true; };
  var focused = 0;
  el(ctx, IDS.ok).focus = function () { focused++; };
  var p = ctx.CW.confirmDialog("Are you sure?");
  assert.equal(shown, 1, "showModal called once");
  assert.equal(focused, 1, "ok focused once");
  assert.equal(el(ctx, IDS.title).textContent, "Confirm"); // default title dialog.js:133
  assert.equal(el(ctx, IDS.ok).textContent, "Confirm"); // default okText dialog.js:134
  assert.equal(el(ctx, IDS.message).textContent, "Are you sure?");
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(await p, true);
});

// --- openDialog: no-showModal fallback (dialog.js:175-176) ---

test("confirm falls back to open attribute when showModal is missing", async function () {
  var ctx = fresh();
  var dlg = ctx.document.getElementById(IDS.dlg); // force-create stub pre-open
  dlg.showModal = undefined;
  var attrs = [];
  dlg.setAttribute = function (k, v) { attrs.push([k, v]); };
  var p = ctx.CW.confirmDialog("Fallback?");
  assert.deepEqual(JSON.parse(JSON.stringify(attrs.filter(function (a) { return a[0] === "open"; }))), [["open", ""]]);
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(await p, true);
});

// --- openDialog: prompt focuses + selects input (dialog.js:178-184) ---

test("prompt focuses and selects the input, seeding the default value", async function () {
  var ctx = fresh();
  var focused = 0;
  var selected = 0;
  el(ctx, IDS.input).focus = function () { focused++; };
  el(ctx, IDS.input).select = function () { selected++; };
  var p = ctx.CW.promptDialog("Name:", "mydefault");
  assert.equal(el(ctx, IDS.input).value, "mydefault"); // dialog.js:166
  assert.equal(focused, 1);
  assert.equal(selected, 1);
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(await p, "mydefault");
});

// --- finish: onCancel path (dialog.js:205-208 -> confirm false) ---

test("confirm cancel click resolves exact false", async function () {
  var ctx = fresh();
  var p = ctx.CW.confirmDialog("Delete?");
  assert.equal(fire(el(ctx, IDS.cancel), "click"), 1);
  assert.equal(await p, false);
});

// --- finish: backdrop click cancels only when target is the dialog (dialog.js:228-230) ---

test("backdrop click (target===dlg) cancels; inner click does not", async function () {
  var ctx = fresh();
  var settled = false;
  var p = ctx.CW.confirmDialog("Backdrop?");
  p.then(function () { settled = true; });
  assert.equal(fire(el(ctx, IDS.dlg), "click", { target: {} }), 1);
  await flush();
  assert.equal(settled, false, "click inside dialog must not dismiss");
  assert.equal(fire(el(ctx, IDS.dlg), "click", { target: el(ctx, IDS.dlg) }), 1);
  assert.equal(await p, false);
});

// --- finish: Escape via native cancel event (dialog.js:223-226) ---

test("dlg cancel event prevents default and resolves false", async function () {
  var ctx = fresh();
  var prevented = 0;
  var p = ctx.CW.confirmDialog("Esc?");
  assert.equal(fire(el(ctx, IDS.dlg), "cancel", { preventDefault: function () { prevented++; } }), 1);
  assert.equal(prevented, 1);
  assert.equal(await p, false);
});

// --- finish: closes an open dialog (dialog.js:101-103) ---

test("finish closes the dialog when dlg.open is set", async function () {
  var ctx = fresh();
  var closed = 0;
  var dlg = el(ctx, IDS.dlg);
  var shown = 0;
  dlg.showModal = function () { shown++; dlg.open = true; };
  dlg.close = function () { closed++; dlg.open = false; };
  var p = ctx.CW.confirmDialog("Close me?");
  assert.equal(shown, 1);
  // dlg.open was set true by the showModal stub above (harness stubs leave
  // .open undefined, so dialog.js:102 only closes when the test sets it).
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(closed, 1, "close called on finish");
  assert.equal(await p, true);
});

// --- Enter submits prompt; Escape in input cancels (dialog.js:233-243) ---

test("Enter in prompt input submits; Escape in input cancels to null", async function () {
  var ctx = fresh();
  var p = ctx.CW.promptDialog("Name:", "typed!");
  var prevented = 0;
  assert.equal(fire(el(ctx, IDS.input), "keydown", {
    key: "Enter",
    preventDefault: function () { prevented++; },
  }), 1);
  assert.equal(prevented, 1);
  assert.equal(await p, "typed!");

  var ctx2 = fresh();
  var p2 = ctx2.CW.promptDialog("Name:", "typed!");
  var stopped = 0;
  assert.equal(fire(el(ctx2, IDS.input), "keydown", {
    key: "Escape",
    preventDefault: function () {},
    stopPropagation: function () { stopped++; },
  }), 1);
  assert.equal(stopped, 1);
  assert.equal(await p2, null); // prompt cancel resolves null (dialog.js:109)
});

// --- confirm/prompt/alert exact promise values ---

test("confirmDialog resolves true on OK", async function () {
  var ctx = fresh();
  var p = ctx.CW.confirmDialog("Proceed?");
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.strictEqual(await p, true);
});

test("promptDialog cancel resolves exact null", async function () {
  var ctx = fresh();
  var p = ctx.CW.promptDialog("Name:", "x");
  assert.equal(fire(el(ctx, IDS.cancel), "click"), 1);
  assert.strictEqual(await p, null);
});

test("alertDialog hides cancel and resolves undefined on OK", async function () {
  var ctx = fresh();
  var p = ctx.CW.alertDialog("Heads up");
  assert.equal(el(ctx, IDS.cancel).hidden, true, "alert has no cancel (dialog.js:155-156)");
  assert.equal(el(ctx, IDS.ok).textContent, "OK"); // alert default (dialog.js:134)
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.strictEqual(await p, undefined);
});

test("promptDialog(message, opts) overload treats object second arg as opts", async function () {
  var ctx = fresh();
  var settled = false;
  var p = ctx.CW.promptDialog("Code:", { required: true }); // dialog.js:261-263
  p.then(function () { settled = true; });
  assert.equal(el(ctx, IDS.input).value, "");
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  await flush();
  assert.equal(settled, false);
  assert.equal(el(ctx, IDS.error).textContent, "This field is required.");
  el(ctx, IDS.input).value = "ok-now";
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.equal(await p, "ok-now");
});

// --- superseded pending dialog resolves dismissed-first (dialog.js:121-131) ---

test("opening a second dialog dismisses the pending confirm as false", async function () {
  var ctx = fresh();
  var first = ctx.CW.confirmDialog("first?");
  var second = ctx.CW.alertDialog("second!");
  assert.strictEqual(await first, false);
  assert.equal(fire(el(ctx, IDS.ok), "click"), 1);
  assert.strictEqual(await second, undefined);
});
