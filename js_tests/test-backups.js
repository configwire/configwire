"use strict";
// test-backups.js — backups.js pure helpers only (no DOM, no network).
// Loads core.js + backups.js via js_tests/harness.js.
// node:test + node:assert/strict only. Run from configwire/: node --test js_tests/test-backups.js
var test = require("node:test");
var assert = require("node:assert/strict");
var harness = require("./harness.js");

var h = harness.load(["core.js", "backups.js"], {});
var C = h.CW;

// --- BACKUP_NAME_PATTERN / isValidBackupName ---

test("BACKUP_NAME_PATTERN is the strict zip rule", function () {
  assert.equal(C.BACKUP_NAME_PATTERN, "^[a-z0-9_-]+\\.zip$");
  var re = new RegExp(C.BACKUP_NAME_PATTERN);
  assert.equal(re.test("backup1.zip"), true);
  assert.equal(re.test("Backup.zip"), false);
});

test("isValidBackupName accept table", function () {
  ["a.zip", "backup1.zip", "my-backup_01.zip", "0-_.zip"].forEach(function (n) {
    assert.equal(C.isValidBackupName(n), true, n);
  });
});

test("isValidBackupName reject table: case, spaces, suffix, traversal", function () {
  ["", null, undefined, "Backup.zip", "A.ZIP", "a b.zip", " a.zip",
    "a.zip ", "a.tar.gz", "a", ".zip", "a.Zip", "a/b.zip",
    "../etc.zip", "..\\x.zip", "a.zip\n", "é.zip",
  ].forEach(function (n) {
    assert.equal(C.isValidBackupName(n), false, String(n));
  });
});

// --- normalizeBackupName ---

test("normalizeBackupName trims surrounding whitespace, stringifies", function () {
  assert.equal(C.normalizeBackupName("  a.zip  "), "a.zip");
  assert.equal(C.normalizeBackupName("a.zip"), "a.zip");
  assert.equal(C.normalizeBackupName(""), "");
  assert.equal(C.normalizeBackupName(null), "");
  assert.equal(C.normalizeBackupName(undefined), "");
});

// --- encodeBackupKey ---

test("encodeBackupKey encodes special chars", function () {
  assert.equal(C.encodeBackupKey("a.zip"), "a.zip");
  assert.equal(C.encodeBackupKey("a b.zip"), "a%20b.zip");
  assert.equal(C.encodeBackupKey("a/b.zip"), "a%2Fb.zip");
  assert.equal(C.encodeBackupKey(""), "");
  assert.equal(C.encodeBackupKey(null), "");
});

// --- backupDownloadPath ---

test("backupDownloadPath shape: /api/backups/{key}?token={tok}", function () {
  assert.equal(C.backupDownloadPath("a.zip", "tok"), "/api/backups/a.zip?token=tok");
  assert.equal(
    C.backupDownloadPath("a b.zip", "t&k"),
    "/api/backups/a%20b.zip?token=t%26k"
  );
  assert.equal(C.backupDownloadPath("a.zip", null), "/api/backups/a.zip?token=");
});

// --- formatBackupSize ---

test("formatBackupSize boundaries and rejects", function () {
  assert.equal(C.formatBackupSize(null), "—");
  assert.equal(C.formatBackupSize(undefined), "—");
  assert.equal(C.formatBackupSize(""), "—");
  assert.equal(C.formatBackupSize(-1), "—");
  assert.equal(C.formatBackupSize("nope"), "—");
  assert.equal(C.formatBackupSize(Infinity), "—");
  assert.equal(C.formatBackupSize(0), "0 B");
  assert.equal(C.formatBackupSize(1023), "1023 B");
  assert.equal(C.formatBackupSize(1024), "1 KB");
  assert.equal(C.formatBackupSize(1536), "1.5 KB");
  assert.equal(C.formatBackupSize(1048576), "1 MB");
  assert.equal(C.formatBackupSize(1073741824), "1 GB");
});

// --- formatBackupDate ---

test("formatBackupDate: empty -> dash, garbage passthrough, valid -> formatted", function () {
  assert.equal(C.formatBackupDate(""), "—");
  assert.equal(C.formatBackupDate(null), "—");
  assert.equal(C.formatBackupDate(undefined), "—");
  assert.equal(C.formatBackupDate("not-a-date"), "not-a-date");
  var out = C.formatBackupDate("2024-01-15T10:30:00.000Z");
  assert.equal(typeof out, "string");
  assert.ok(out.length > 0 && out !== "—");
});

// --- isBackupBusy ---

// --- backupS3TestMessage ---

test("not-enabled failure maps to still-disabled guidance", function () {
  [
    "S3 storage filesystem is not enabled",
    "S3 STORAGE FILESYSTEM IS NOT ENABLED",
    "s3 not enabled",
    "Not Enabled in stored settings",
  ].forEach(function (message) {
    assert.equal(
      C.backupS3TestMessage({ status: 400, data: { message: message } }),
      "S3 is still disabled in the saved settings — flip the switch off and on again, then check.",
      message);
  });
});

test("backupS3TestMessage passes other failures through verbatim", function () {
  [
    { status: 400, data: { message: "invalid endpoint" } },
    { status: 500, data: { message: "boom" } },
    { status: 400, data: {} },
  ].forEach(function (out) {
    assert.equal(C.backupS3TestMessage(out), C.apiError(out, "S3 connection test failed"));
  });
});

// --- parseBackupS3 ---

test("parseBackupS3 missing backups.s3 -> safe defaults", function () {
  [null, undefined, {}, { backups: {} }].forEach(function (d) {
    var out = C.parseBackupS3(d);
    assert.equal(out.enabled, false);
    assert.equal(out.endpoint, "");
    assert.equal(out.bucket, "");
    assert.equal(out.region, "");
    assert.equal(out.accessKey, "");
    assert.equal(out.secret, "");
    assert.equal(out.forcePathStyle, false);
  });
});

test("parseBackupS3 maps fields, stringifies, coerces booleans", function () {
  var out = C.parseBackupS3({ backups: { s3: {
    enabled: 1, endpoint: "https://s3.example.com", bucket: "b",
    region: "us-east-1", accessKey: "AK", secret: "shh", forcePathStyle: 0,
  } } });
  assert.equal(out.enabled, true);
  assert.equal(out.endpoint, "https://s3.example.com");
  assert.equal(out.bucket, "b");
  assert.equal(out.region, "us-east-1");
  assert.equal(out.accessKey, "AK");
  assert.equal(out.secret, "shh");
  assert.equal(out.forcePathStyle, false);
});

test("parseBackupS3 blanked secret \"\" handled as empty", function () {
  var out = C.parseBackupS3({ backups: { s3: { enabled: true, secret: "" } } });
  assert.equal(out.secret, "");
  assert.equal(out.enabled, true);
});

test("renderBackupS3 paints UNCONNECTED badge and prefills form via textContent only", function () {
  var els = h.elementsById;
  C.renderBackupS3({ enabled: true, endpoint: "https://s3.example.com", bucket: "b",
    region: "r", accessKey: "AK", secret: "shh", forcePathStyle: true });
  assert.equal(els["backup-s3-badge"].textContent, "UNCONNECTED");
  assert.equal(els["backup-s3-endpoint"].value, "https://s3.example.com");
  assert.equal(els["backup-s3-bucket"].value, "b");
  assert.equal(els["backup-s3-region"].value, "r");
  assert.equal(els["backup-s3-access-key"].value, "AK");
  assert.equal(els["backup-s3-secret"].value, "");
  assert.equal(els["backup-s3-fields"].hidden, false);
  assert.equal(els["backup-s3-action"].textContent, "Check connection");
  assert.equal(els["backup-s3-force-path-style"].checked, true);
  C.renderBackupS3({ enabled: false, endpoint: "", bucket: "", region: "", accessKey: "", secret: "", forcePathStyle: false });
  assert.equal(els["backup-s3-badge"].textContent, "UNCONNECTED");
  assert.equal(els["backup-s3-endpoint"].value, "");
  assert.equal(els["backup-s3-bucket"].value, "");
  assert.equal(els["backup-s3-region"].value, "");
  assert.equal(els["backup-s3-access-key"].value, "");
  assert.equal(els["backup-s3-secret"].value, "");
  assert.equal(els["backup-s3-fields"].hidden, true);
  assert.equal(els["backup-s3-force-path-style"].checked, false);
});

test("toggleBackupS3Secret flips input type and button label", function () {
  var els = h.elementsById;
  els["backup-s3-secret"].type = "password";
  C.toggleBackupS3Secret();
  assert.equal(els["backup-s3-secret"].type, "text");
  assert.equal(els["backup-s3-secret-toggle"].textContent, "Hide");
  C.toggleBackupS3Secret();
  assert.equal(els["backup-s3-secret"].type, "password");
  assert.equal(els["backup-s3-secret-toggle"].textContent, "Show");
});

// --- buildBackupS3Patch ---

test("buildBackupS3Patch subtree-only {backups:{s3}} shape", function () {
  var out = C.buildBackupS3Patch({
    enabled: true, endpoint: "https://s3.example.com", bucket: "b",
    region: "r", accessKey: "AK", secret: "shh", forcePathStyle: true,
  });
  assert.deepEqual(Object.keys(out), ["backups"]);
  assert.deepEqual(Object.keys(out.backups), ["s3"]);
  var s3 = out.backups.s3;
  assert.equal(s3.enabled, true);
  assert.equal(s3.endpoint, "https://s3.example.com");
  assert.equal(s3.bucket, "b");
  assert.equal(s3.region, "r");
  assert.equal(s3.accessKey, "AK");
  assert.equal(s3.secret, "shh");
  assert.equal(s3.forcePathStyle, true);
});

test("buildBackupS3Patch empty secret OMITTED never sent", function () {
  ["", null, undefined].forEach(function (sec) {
    var out = C.buildBackupS3Patch({ enabled: true, secret: sec });
    assert.equal("secret" in out.backups.s3, false, String(sec));
  });
});

test("buildBackupS3Patch falsy enabled passes through as false", function () {
  [false, 0, "", null, undefined].forEach(function (en) {
    var out = C.buildBackupS3Patch({ enabled: en });
    assert.equal(out.backups.s3.enabled, false, String(en));
  });
  assert.equal(C.buildBackupS3Patch({ enabled: 1 }).backups.s3.enabled, true);
});

test("buildBackupS3Patch no secret leakage into other fields", function () {
  var out = C.buildBackupS3Patch({ secret: "topsecret", enabled: true });
  assert.equal(out.backups.s3.endpoint, "");
  assert.equal(out.backups.s3.bucket, "");
  assert.equal(out.backups.s3.region, "");
  assert.equal(out.backups.s3.accessKey, "");
  assert.ok(out.backups.s3.endpoint.indexOf("topsecret") === -1);
  assert.ok(out.backups.s3.bucket.indexOf("topsecret") === -1);
  assert.ok(out.backups.s3.region.indexOf("topsecret") === -1);
  assert.ok(out.backups.s3.accessKey.indexOf("topsecret") === -1);
  assert.equal(out.backups.s3.secret, "topsecret");
});

// --- normalizeBackupCron ---

test("normalizeBackupCron trims surrounding whitespace, stringifies", function () {
  assert.equal(C.normalizeBackupCron("  0 3 * * *  "), "0 3 * * *");
  assert.equal(C.normalizeBackupCron("0 3 * * *"), "0 3 * * *");
  assert.equal(C.normalizeBackupCron(""), "");
  assert.equal(C.normalizeBackupCron(null), "");
  assert.equal(C.normalizeBackupCron(undefined), "");
});

// --- parseBackupKeep ---

test("parseBackupKeep empty -> 0, digits -> int, garbage -> NaN", function () {
  assert.equal(C.parseBackupKeep(""), 0);
  assert.equal(C.parseBackupKeep(null), 0);
  assert.equal(C.parseBackupKeep(undefined), 0);
  assert.equal(C.parseBackupKeep("10"), 10);
  assert.equal(C.parseBackupKeep("  7 "), 7);
  assert.equal(C.parseBackupKeep(3), 3);
  assert.equal(C.parseBackupKeep("3.5"), 3.5);
  assert.ok(isNaN(C.parseBackupKeep("abc")), "garbage is NaN");
  assert.ok(isNaN(C.parseBackupKeep("10x")), "trailing junk is NaN");
});

// --- isValidBackupKeep ---

test("isValidBackupKeep accept table: integers >= 1", function () {
  [1, 3, 10, 1000].forEach(function (k) {
    assert.equal(C.isValidBackupKeep(k), true, String(k));
  });
});

test("isValidBackupKeep reject table: zero, fractions, negatives, non-numbers", function () {
  [0, -1, 3.5, NaN, Infinity, "", null, undefined, "10", false].forEach(function (k) {
    assert.equal(C.isValidBackupKeep(k), false, String(k));
  });
});

// --- parseBackupSchedule ---

test("parseBackupSchedule missing backups -> disabled defaults", function () {
  [null, undefined, {}, { backups: {} }].forEach(function (d) {
    var out = C.parseBackupSchedule(d);
    assert.equal(out.cron, "");
    assert.equal(out.cronMaxKeep, 0);
  });
});

test("parseBackupSchedule maps cron, trims, coerces keep", function () {
  var out = C.parseBackupSchedule({ backups: { cron: "  0 3 * * * ", cronMaxKeep: 10 } });
  assert.equal(out.cron, "0 3 * * *");
  assert.equal(out.cronMaxKeep, 10);
  assert.equal(C.parseBackupSchedule({ backups: { cron: "", cronMaxKeep: "7" } }).cronMaxKeep, 7);
  assert.equal(C.parseBackupSchedule({ backups: { cronMaxKeep: -2 } }).cronMaxKeep, 0);
  assert.equal(C.parseBackupSchedule({ backups: { cronMaxKeep: "nope" } }).cronMaxKeep, 0);
  assert.equal(C.parseBackupSchedule({ backups: { cronMaxKeep: 2.9 } }).cronMaxKeep, 2);
});

// --- buildBackupSchedulePatch ---

test("buildBackupSchedulePatch subtree-only {backups:{cron,cronMaxKeep}} shape", function () {
  var out = C.buildBackupSchedulePatch({ cron: "0 3 * * *", keep: 10 });
  assert.deepEqual(Object.keys(out), ["backups"]);
  assert.deepEqual(Object.keys(out.backups).sort(), ["cron", "cronMaxKeep"]);
  assert.equal(out.backups.cron, "0 3 * * *");
  assert.equal(out.backups.cronMaxKeep, 10);
});

test("buildBackupSchedulePatch trims cron, floors keep, clamps garbage to 0", function () {
  var out = C.buildBackupSchedulePatch({ cron: "  0 3 * * * ", keep: 10 });
  assert.equal(out.backups.cron, "0 3 * * *");
  assert.equal(C.buildBackupSchedulePatch({ cron: "", keep: 2.9 }).backups.cronMaxKeep, 2);
  ["", null, undefined, "abc", NaN, -5].forEach(function (k) {
    assert.equal(C.buildBackupSchedulePatch({ cron: "", keep: k }).backups.cronMaxKeep, 0, String(k));
  });
  assert.equal(C.buildBackupSchedulePatch({}).backups.cron, "");
});

// --- validateBackupSchedule ---

test("validateBackupSchedule empty cron always sendable", function () {
  ["", null, undefined, "   "].forEach(function (cron) {
    [0, 1, 10, NaN].forEach(function (keep) {
      assert.equal(C.validateBackupSchedule(cron, keep), "", cron + "/" + keep);
    });
  });
});

test("validateBackupSchedule set cron needs keep >= 1", function () {
  assert.equal(C.validateBackupSchedule("0 3 * * *", 10), "");
  assert.equal(C.validateBackupSchedule("0 3 * * *", 1), "");
  [0, NaN, 3.5, -1, "10", null, undefined].forEach(function (keep) {
    assert.ok(C.validateBackupSchedule("0 3 * * *", keep) !== "", String(keep));
  });
});

// --- formatBackupScheduleStatus ---

test("formatBackupScheduleStatus disabled vs scheduled text", function () {
  assert.ok(C.formatBackupScheduleStatus({ cron: "", cronMaxKeep: 0 }).indexOf("disabled") !== -1);
  assert.ok(C.formatBackupScheduleStatus(null).indexOf("disabled") !== -1);
  assert.ok(C.formatBackupScheduleStatus(undefined).indexOf("disabled") !== -1);
  var out = C.formatBackupScheduleStatus({ cron: "0 3 * * *", cronMaxKeep: 10 });
  assert.ok(out.indexOf("0 3 * * *") !== -1);
  assert.ok(out.indexOf("10") !== -1);
});

test("formatBackupScheduleStatus never touches secrets", function () {
  var out = C.formatBackupScheduleStatus({ cron: "0 3 * * *", cronMaxKeep: 10, secret: "shh" });
  assert.ok(out.indexOf("shh") === -1, "secret leaked verbatim");
  assert.ok(out.indexOf("******") === -1, "no masked-secret placeholder either");
});

test("renderBackupSchedule prefills inputs, clears on empty", function () {
  var els = h.elementsById;
  C.renderBackupSchedule({ cron: "0 3 * * *", cronMaxKeep: 10 });
  assert.ok(els["backup-schedule-status"].textContent.indexOf("0 3 * * *") !== -1);
  assert.equal(els["backup-schedule-cron"].value, "0 3 * * *");
  assert.equal(els["backup-schedule-keep"].value, "10");
  C.renderBackupSchedule({ cron: "", cronMaxKeep: 0 });
  assert.ok(els["backup-schedule-status"].textContent.indexOf("disabled") !== -1);
  assert.equal(els["backup-schedule-cron"].value, "");
  assert.equal(els["backup-schedule-keep"].value, "");
});

// --- buildBackupCron ---

test("buildBackupCron daily/weekly/monthly vectors", function () {
  assert.equal(C.buildBackupCron("daily", { hour: 3, minute: 15 }), "15 3 * * *");
  assert.equal(C.buildBackupCron("daily", { hour: 0, minute: 0 }), "0 0 * * *");
  assert.equal(C.buildBackupCron("weekly", { hour: 3, minute: 15, dow: 1 }), "15 3 * * 1");
  assert.equal(C.buildBackupCron("weekly", { hour: 23, minute: 59, dow: 0 }), "59 23 * * 0");
  assert.equal(C.buildBackupCron("monthly", { hour: 3, minute: 15, dom: 5 }), "15 3 5 * *");
  assert.equal(C.buildBackupCron("monthly", { hour: 0, minute: 0, dom: 31 }), "0 0 31 * *");
});

test("buildBackupCron emits no leading zeros", function () {
  assert.equal(C.buildBackupCron("daily", { hour: 3, minute: 5 }), "5 3 * * *");
  assert.equal(C.buildBackupCron("weekly", { hour: 8, minute: 5, dow: 6 }), "5 8 * * 6");
  assert.ok(C.buildBackupCron("daily", { hour: 3, minute: 5 }).indexOf("05") === -1);
});

test("buildBackupCron range rejections never produce a cron", function () {
  ["", null, undefined, "daily2", "hourly"].forEach(function (freq) {
    assert.equal(C.buildBackupCron(freq, { hour: 3, minute: 15, dow: 1, dom: 5 }), "", String(freq));
  });
  assert.equal(C.buildBackupCron("daily", { hour: 24, minute: 0 }), "");
  assert.equal(C.buildBackupCron("daily", { hour: -1, minute: 0 }), "");
  assert.equal(C.buildBackupCron("daily", { hour: 3.5, minute: 0 }), "");
  assert.equal(C.buildBackupCron("daily", { hour: "3", minute: 0 }), "");
  assert.equal(C.buildBackupCron("daily", { hour: 3, minute: 60 }), "");
  assert.equal(C.buildBackupCron("daily", { hour: 3, minute: NaN }), "");
  assert.equal(C.buildBackupCron("weekly", { hour: 3, minute: 15, dow: 7 }), "");
  assert.equal(C.buildBackupCron("weekly", { hour: 3, minute: 15, dow: -1 }), "");
  assert.equal(C.buildBackupCron("weekly", { hour: 3, minute: 15 }), "");
  assert.equal(C.buildBackupCron("monthly", { hour: 3, minute: 15, dom: 0 }), "");
  assert.equal(C.buildBackupCron("monthly", { hour: 3, minute: 15, dom: 32 }), "");
  assert.equal(C.buildBackupCron("monthly", { hour: 3, minute: 15 }), "");
});

// --- range validators ---

test("backup hour/minute/dow/dom range tables", function () {
  [0, 12, 23].forEach(function (h) { assert.equal(C.isValidBackupHour(h), true, String(h)); });
  [-1, 24, 3.5, NaN, "3", null].forEach(function (h) { assert.equal(C.isValidBackupHour(h), false, String(h)); });
  [0, 30, 59].forEach(function (m) { assert.equal(C.isValidBackupMinute(m), true, String(m)); });
  [-1, 60, 1.5, NaN, "30"].forEach(function (m) { assert.equal(C.isValidBackupMinute(m), false, String(m)); });
  [0, 1, 6].forEach(function (d) { assert.equal(C.isValidBackupDow(d), true, String(d)); });
  [-1, 7, 1.5, "1"].forEach(function (d) { assert.equal(C.isValidBackupDow(d), false, String(d)); });
  [1, 15, 31].forEach(function (d) { assert.equal(C.isValidBackupDom(d), true, String(d)); });
  [0, 32, 1.5, "15"].forEach(function (d) { assert.equal(C.isValidBackupDom(d), false, String(d)); });
});

// --- parseBackupCronToControls ---

test("parseBackupCronToControls canonical shapes", function () {
  [
    ["15 3 * * *", "daily", 3, 15, 1, 1],
    ["15 3 * * 1", "weekly", 3, 15, 1, 1],
    ["15 3 * * MON", "weekly", 3, 15, 1, 1],
    ["15 3 * * sat", "weekly", 3, 15, 6, 1],
    ["15 3 5 * *", "monthly", 3, 15, 1, 5],
  ].forEach(function (vec) {
    var out = C.parseBackupCronToControls(vec[0]);
    assert.equal(out.mode, vec[1], vec[0]);
    assert.equal(out.hour, vec[2], vec[0]);
    assert.equal(out.minute, vec[3], vec[0]);
    assert.equal(out.dow, vec[4], vec[0]);
    assert.equal(out.dom, vec[5], vec[0]);
    assert.equal(out.raw, vec[0], vec[0]);
  });
});

test("parseBackupCronToControls custom fallbacks keep raw", function () {
  ["", "   ", "* * * * *", "*/15 * * * *", "0 0 * * 0,6", "0 0 L * *",
    "1 2 3", "1 2 3 4 5 6", "nope", "15 3 * * 7", "15 24 * * *",
  ].forEach(function (cron) {
    var out = C.parseBackupCronToControls(cron);
    assert.equal(out.mode, "custom", cron);
    assert.equal(out.raw, String(cron).replace(/^\s+|\s+$/g, ""), cron);
  });
});

// --- round-trip ---

test("parse(build(x)) round-trips canonical shapes", function () {
  [
    ["daily", { hour: 3, minute: 15 }],
    ["daily", { hour: 0, minute: 0 }],
    ["daily", { hour: 23, minute: 59 }],
    ["weekly", { hour: 3, minute: 15, dow: 1 }],
    ["weekly", { hour: 0, minute: 0, dow: 0 }],
    ["weekly", { hour: 23, minute: 59, dow: 6 }],
    ["monthly", { hour: 3, minute: 15, dom: 5 }],
    ["monthly", { hour: 0, minute: 0, dom: 1 }],
    ["monthly", { hour: 23, minute: 59, dom: 31 }],
  ].forEach(function (vec) {
    var cron = C.buildBackupCron(vec[0], vec[1]);
    assert.ok(cron !== "", vec[0]);
    var back = C.parseBackupCronToControls(cron);
    assert.equal(back.mode, vec[0]);
    assert.equal(C.buildBackupCron(back.mode, back), cron);
  });
});

// --- describeBackupSchedule ---

test("describeBackupSchedule plain words with padded time", function () {
  assert.equal(C.describeBackupSchedule({ mode: "daily", hour: 3, minute: 15 }), "Daily at 03:15");
  assert.equal(C.describeBackupSchedule({ mode: "weekly", hour: 3, minute: 15, dow: 1 }),
    "Weekly on Monday at 03:15");
  assert.equal(C.describeBackupSchedule({ mode: "monthly", hour: 3, minute: 5, dom: 5 }),
    "Monthly on day 5 at 03:05");
  assert.equal(C.describeBackupSchedule({ mode: "custom" }), "Custom schedule");
  assert.equal(C.describeBackupSchedule(null), "Custom schedule");
  assert.equal(C.describeBackupSchedule({ mode: "weekly", hour: 99, minute: 15, dow: 1 }),
    "Custom schedule");
});

// --- composeBackupHelperTime ---

test("composeBackupHelperTime zero-pads valid pairs, blanks the rest", function () {
  assert.equal(C.composeBackupHelperTime(3, 15), "03:15");
  assert.equal(C.composeBackupHelperTime(0, 0), "00:00");
  assert.equal(C.composeBackupHelperTime(23, 59), "23:59");
  assert.equal(C.composeBackupHelperTime(24, 0), "");
  assert.equal(C.composeBackupHelperTime(3, 60), "");
  assert.equal(C.composeBackupHelperTime(3.5, 15), "");
  assert.equal(C.composeBackupHelperTime(NaN, 0), "");
  assert.equal(C.composeBackupHelperTime("3", 15), "");
  assert.equal(C.composeBackupHelperTime(3, null), "");
});

// --- splitBackupHelperTime ---

test("splitBackupHelperTime valid text splits, garbage falls back to 03:00", function () {
  [
    ["03:15", 3, 15],
    ["3:5", 3, 5],
    ["0:00", 0, 0],
    ["23:59", 23, 59],
    ["  03:15  ", 3, 15],
  ].forEach(function (vec) {
    var out = C.splitBackupHelperTime(vec[0]);
    assert.equal(out.hour, vec[1], vec[0]);
    assert.equal(out.minute, vec[2], vec[0]);
  });
  ["", "   ", null, undefined, "abc", "99:99", "12:60", "24:00", "1:2:3", "3",
  ].forEach(function (raw) {
    var out = C.splitBackupHelperTime(raw);
    assert.equal(out.hour, 3, String(raw));
    assert.equal(out.minute, 0, String(raw));
  });
});

// --- open-prefill mapping ---

test("openBackupScheduleHelper prefills hour/minute selects from cron", function () {
  var els = h.elementsById;
  els["backup-schedule-cron"].value = "15 3 * * 1";
  els["backup-schedule-keep"].value = "10";
  C.openBackupScheduleHelper();
  assert.equal(els["backup-schedule-helper-freq"].value, "weekly");
  assert.equal(els["backup-schedule-helper-hour"].value, "3");
  assert.equal(els["backup-schedule-helper-minute"].value, "15");
  assert.equal(els["backup-schedule-helper-dow"].value, "1");
  assert.equal(els["backup-schedule-helper-keep"].value, "10");
  els["backup-schedule-cron"].value = "";
  C.openBackupScheduleHelper();
  assert.equal(els["backup-schedule-helper-freq"].value, "custom");
  assert.equal(els["backup-schedule-helper-hour"].value, "3");
  assert.equal(els["backup-schedule-helper-minute"].value, "0");
});

// --- updateBackupScheduleCronHint ---

test("updateBackupScheduleCronHint maps cron to plain words, blank when empty", function () {
  var els = h.elementsById;
  assert.equal(C.updateBackupScheduleCronHint("10 4 * * *"), "Daily at 04:10");
  assert.equal(els["backup-schedule-cron-hint"].textContent, "Daily at 04:10");
  assert.equal(C.updateBackupScheduleCronHint("15 3 * * 1"), "Weekly on Monday at 03:15");
  assert.equal(C.updateBackupScheduleCronHint("15 3 5 * *"), "Monthly on day 5 at 03:15");
  assert.equal(C.updateBackupScheduleCronHint("*/15 * * * *"), "Custom schedule");
  assert.equal(C.updateBackupScheduleCronHint("0 0 * * 0,6"), "Custom schedule");
  assert.equal(C.updateBackupScheduleCronHint(""), "");
  assert.equal(C.updateBackupScheduleCronHint("   "), "");
  assert.equal(els["backup-schedule-cron-hint"].textContent, "");
});

// --- schedule dirty arming (Save must stay pressable after edits) ---

test("renderBackupSchedule refreshes the cron hint on prefill", function () {
  var els = h.elementsById;
  C.renderBackupSchedule({ cron: "10 4 * * *", cronMaxKeep: 7 });
  assert.equal(els["backup-schedule-cron-hint"].textContent, "Daily at 04:10");
  C.renderBackupSchedule({ cron: "", cronMaxKeep: 0 });
  assert.equal(els["backup-schedule-cron-hint"].textContent, "");
});

function loadScheduleHandle(fetchImpl) {
  return harness.load(["core.js", "releases.js", "backups.js"], { fetchImpl: fetchImpl });
}

function settingsFetchImpl(settings) {
  return function () {
    return Promise.resolve({
      status: 200,
      ok: true,
      json: function () { return Promise.resolve(settings); }
    });
  };
}

test("loading schedule arms dirty tracking so clearing cron enables Save", async function () {
  var h2 = loadScheduleHandle(settingsFetchImpl({ backups: { cron: "0 3 * * *", cronMaxKeep: 10 } }));
  // static markup exists before JS runs: pre-create the stubs
  var form = h2.document.getElementById("backup-schedule-form");
  h2.document.getElementById("backup-schedule-cron");
  h2.document.getElementById("backup-schedule-keep");
  await h2.CW.loadBackupSchedule();
  var types = form.listeners.map(function (l) { return l.type; }).sort();
  assert.deepEqual(types, ["change", "input"]);
  // operator clears the cron field: the browser fires input → dirty
  form.listeners.forEach(function (l) { if (l.type === "input") l.fn.call(form, {}); });
  assert.equal(h2.CW.state.dirtyForms["backup-schedule-form"], true);
});

test("save with empty cron sends the clearing PATCH pair", async function () {
  var seen = [];
  var h2 = loadScheduleHandle(function (url, opts) {
    seen.push({ url: url, opts: opts });
    return Promise.resolve({
      status: 200,
      ok: true,
      json: function () { return Promise.resolve({ backups: { cron: "", cronMaxKeep: 10 } }); }
    });
  });
  var els = h2.elementsById;
  els["backup-schedule-cron"] = h2.document.getElementById("backup-schedule-cron");
  els["backup-schedule-keep"] = h2.document.getElementById("backup-schedule-keep");
  els["backup-schedule-cron"].value = "";
  els["backup-schedule-keep"].value = "10";
  var out = await h2.CW.saveBackupSchedule();
  assert.equal(out.status, 200);
  var patches = seen.filter(function (c) { return c.opts && c.opts.method === "PATCH"; });
  assert.equal(patches.length, 1);
  assert.deepEqual(JSON.parse(patches[0].opts.body), { backups: { cron: "", cronMaxKeep: 10 } });
});

// --- schedule toast path (bottom-right #toast must show + stay) ---

function loadToastHandle(fetchImpl) {
  return harness.load(["core.js", "backups.js"], { fetchImpl: fetchImpl });
}

test("schedule save shows bottom-right toast and arms its hide timer", async function () {
  var h3 = loadToastHandle(function () {
    return Promise.resolve({
      status: 200,
      ok: true,
      json: function () { return Promise.resolve({ backups: { cron: "0 3 * * *", cronMaxKeep: 10 } }); }
    });
  });
  var calls = [];
  var orig = h3.CW.toast;
  h3.CW.toast = function (msg, ok) { calls.push([msg, ok]); return orig(msg, ok); };
  var toastEl = h3.document.getElementById("toast");
  h3.document.getElementById("backup-schedule-cron").value = "0 3 * * *";
  h3.document.getElementById("backup-schedule-keep").value = "10";
  var out = await h3.CW.saveBackupSchedule();
  assert.equal(out.status, 200);
  assert.deepEqual(calls, [["Schedule saved.", true]]);
  assert.equal(toastEl.hidden, false);
  assert.equal(toastEl.textContent, "Schedule saved.");
  // stays visible: exactly one live hide timer, nothing re-hid synchronously
  assert.equal(h3.timeouts.length, 1);
  assert.equal(h3.timeouts[0].cleared, false);
});

test("sibling S3 save toasts identically (parity)", async function () {
  var h3 = loadToastHandle(function () {
    return Promise.resolve({
      status: 200,
      ok: true,
      json: function () { return Promise.resolve({ backups: { s3: { enabled: true } } }); }
    });
  });
  var calls = [];
  var orig = h3.CW.toast;
  h3.CW.toast = function (msg, ok) { calls.push([msg, ok]); return orig(msg, ok); };
  var toastEl = h3.document.getElementById("toast");
  var out = await h3.CW.saveBackupS3();
  assert.equal(out.status, 200);
  assert.deepEqual(calls, [["S3 settings saved.", true]]);
  assert.equal(toastEl.hidden, false);
  assert.equal(toastEl.textContent, "S3 settings saved.");
  assert.equal(h3.timeouts.length, 1);
  assert.equal(h3.timeouts[0].cleared, false);
});

// --- schedule save success: toast only, inline cleared ---

function loadSaveHandle(patchData, patchStatus) {
  return loadToastHandle(function (url, opts) {
    if (opts && opts.method === "PATCH") {
      return Promise.resolve({
        status: patchStatus,
        ok: patchStatus >= 200 && patchStatus < 300,
        json: function () { return Promise.resolve(patchData); }
      });
    }
    return Promise.resolve({
      status: 200,
      ok: true,
      json: function () { return Promise.resolve({ backups: { cron: "", cronMaxKeep: 0 } }); }
    });
  });
}

test("RED: schedule save success leaves inline empty, toasts only", async function () {
  var h4 = loadSaveHandle({ backups: { cron: "0 3 * * *", cronMaxKeep: 10 } }, 200);
  var calls = [];
  var orig = h4.CW.toast;
  h4.CW.toast = function (msg, ok) { calls.push([msg, ok]); return orig(msg, ok); };
  var resultEl = h4.document.getElementById("backup-schedule-result");
  resultEl.textContent = "stale prior text";
  h4.document.getElementById("backup-schedule-cron").value = "0 3 * * *";
  h4.document.getElementById("backup-schedule-keep").value = "10";
  var out = await h4.CW.saveBackupSchedule();
  assert.equal(out.status, 200);
  assert.equal(resultEl.textContent, "");
  assert.deepEqual(calls, [["Schedule saved.", true]]);
});

test("schedule save error still writes inline + toast", async function () {
  var h4 = loadSaveHandle({ message: "bad cron" }, 400);
  var calls = [];
  var orig = h4.CW.toast;
  h4.CW.toast = function (msg, ok) { calls.push([msg, ok]); return orig(msg, ok); };
  var resultEl = h4.document.getElementById("backup-schedule-result");
  h4.document.getElementById("backup-schedule-cron").value = "0 3 * * *";
  h4.document.getElementById("backup-schedule-keep").value = "10";
  var out = await h4.CW.saveBackupSchedule();
  assert.equal(out.status, 400);
  assert.ok(resultEl.textContent.indexOf("Schedule save failed") !== -1);
  assert.equal(calls.length, 1);
  assert.equal(calls[0][0], resultEl.textContent);
});

// --- backupS3Step ---

test("RED: backupS3Step hidden/check/save transitions", function () {
  function step(storedEnabled, fieldsVisible, checkedOk, dirtySinceCheck) {
    return C.backupS3Step({ storedEnabled: storedEnabled, fieldsVisible: fieldsVisible,
      checkedOk: checkedOk, dirtySinceCheck: dirtySinceCheck });
  }
  assert.equal(step(false, false, false, false), "hidden");
  assert.equal(step(true, false, false, false), "hidden");
  assert.equal(step(false, true, false, false), "check");
  assert.equal(step(true, true, false, false), "check");
  assert.equal(step(true, true, true, false), "save");
  assert.equal(step(true, true, true, true), "check");
  assert.equal(C.backupS3Step(null), "hidden");
  assert.equal(C.backupS3Step({}), "hidden");
});

// --- guided S3 switch/check/save flow ---

function loadS3Handle(fetchImpl) {
  return harness.load(["core.js", "backups.js"], { fetchImpl: fetchImpl });
}

test("switch ON reveals fields and fires a fresh silent auto-verify", async function () {
  var posts = [];
  var h5 = loadS3Handle(function (url, opts) {
    if (opts && opts.method === "POST") posts.push({ url: url, opts: opts });
    return Promise.resolve({
      status: 204,
      ok: true,
      json: function () { return Promise.resolve({}); }
    });
  });
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  h5.CW.state.backupS3 = { enabled: false };
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = true;
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  els["backup-s3-result"] = h5.document.getElementById("backup-s3-result");
  var calls = [];
  var orig = h5.CW.toast;
  h5.CW.toast = function (msg, ok) { calls.push([msg, ok]); return orig(msg, ok); };
  var out = await h5.CW.onBackupS3Switch();
  assert.equal(out.status, 204);
  assert.equal(els["backup-s3-fields"].hidden, false);
  assert.equal(posts.length, 1);
  assert.equal(posts[0].url, "/api/settings/test/s3");
  assert.deepEqual(JSON.parse(posts[0].opts.body), { filesystem: "backups" });
  assertBadgeButtonAgree(h5, els, badgeSet);
  assert.equal(els["backup-s3-badge"].textContent, "CONNECTED");
  assert.deepEqual(calls, [], "silent auto-verify success must not toast");
  assert.equal(els["backup-s3-result"].textContent, "", "silent auto-verify writes no inline status");
});

test("switch OFF while stored off is a no-op hide", function () {
  var h5 = loadS3Handle();
  var els = h5.elementsById;
  h5.CW.state.backupS3 = { enabled: false };
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  h5.CW.onBackupS3Switch();
  assert.equal(els["backup-s3-fields"].hidden, true);
  assert.equal(h5.fetchCalls.length, 0);
});

test("switch OFF while stored on PATCHes disable", async function () {
  var seen = [];
  var h5 = loadS3Handle(function (url, opts) {
    seen.push({ url: url, opts: opts });
    return Promise.resolve({
      status: 200,
      ok: true,
      json: function () { return Promise.resolve({ backups: { s3: { enabled: false } } }); }
    });
  });
  var els = h5.elementsById;
  h5.CW.state.backupS3 = { enabled: true };
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var out = await h5.CW.onBackupS3Switch();
  assert.equal(out.status, 200);
  var patches = seen.filter(function (c) { return c.opts && c.opts.method === "PATCH"; });
  assert.equal(patches.length, 1);
  assert.deepEqual(JSON.parse(patches[0].opts.body), { backups: { s3: { enabled: false } } });
  assert.equal(h5.CW.state.backupS3.enabled, false);
  assert.equal(els["backup-s3-fields"].hidden, true);
  assert.equal(els["backup-s3-result"].textContent, "S3 storage disabled.");
});

test("switch OFF failure reverts to Check with error", async function () {
  var h5 = loadS3Handle(function () {
    return Promise.resolve({
      status: 400,
      ok: false,
      json: function () { return Promise.resolve({ message: "cannot disable now" }); }
    });
  });
  var els = h5.elementsById;
  h5.CW.state.backupS3 = { enabled: true };
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var out = await h5.CW.onBackupS3Switch();
  assert.equal(out.status, 400);
  assert.equal(els["backup-s3-fields"].hidden, false);
  assert.equal(els["backup-s3-action"].textContent, "Check connection");
  assert.ok(els["backup-s3-result"].textContent.indexOf("S3 disable failed") !== -1);
  assert.equal(h5.CW.state.backupS3.enabled, true);
});

test("check 204 morphs to Save; field edit reverts to Check", async function () {
  var h5 = loadS3Handle(function (url, opts) {
    if (opts && opts.method === "PATCH") {
      return Promise.resolve({
        status: 200,
        ok: true,
        json: function () { return Promise.resolve({ backups: { s3: { enabled: true } } }); }
      });
    }
    return Promise.resolve({
      status: 204,
      ok: true,
      json: function () { return Promise.resolve({}); }
    });
  });
  var els = h5.elementsById;
  h5.CW.state.backupS3 = { enabled: true };
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var out = await h5.CW.testBackupS3();
  assert.equal(out.status, 204);
  assert.equal(h5.CW.state.backupS3CheckedOk, true);
  assert.equal(els["backup-s3-action"].textContent, "Save S3 Settings");
  assert.equal(els["backup-s3-action"].className, "btn primary");
  h5.CW.revertBackupS3Check();
  assert.equal(h5.CW.state.backupS3CheckedOk, false);
  assert.equal(els["backup-s3-action"].textContent, "Check connection");
  assert.equal(els["backup-s3-action"].className, "btn secondary");
});

test("check failure keeps Check with still-disabled guidance", async function () {
  var h5 = loadS3Handle(function (url, opts) {
    if (opts && opts.method === "PATCH") {
      return Promise.resolve({
        status: 200,
        ok: true,
        json: function () { return Promise.resolve({ backups: { s3: { enabled: true } } }); }
      });
    }
    return Promise.resolve({
      status: 400,
      ok: false,
      json: function () { return Promise.resolve({ message: "S3 storage filesystem is not enabled" }); }
    });
  });
  var els = h5.elementsById;
  h5.CW.state.backupS3 = { enabled: false };
  var out = await h5.CW.testBackupS3();
  assert.equal(out.status, 400);
  assert.equal(h5.CW.state.backupS3CheckedOk, false);
  assert.equal(els["backup-s3-action"].textContent, "Check connection");
  assert.ok(els["backup-s3-result"].textContent.indexOf("still disabled") !== -1);
});

test("check PATCH failure shows save error, never tests", async function () {
  var seen = [];
  var h5 = loadS3Handle(function (url, opts) {
    seen.push({ url: url, opts: opts });
    return Promise.resolve({
      status: 400,
      ok: false,
      json: function () { return Promise.resolve({ message: "bad bucket" }); }
    });
  });
  var els = h5.elementsById;
  h5.CW.state.backupS3 = { enabled: false };
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var out = await h5.CW.testBackupS3();
  assert.equal(out.status, 400);
  assert.deepEqual(seen.map(function (c) { return c.opts && c.opts.method; }), ["PATCH"]);
  assert.equal(h5.CW.state.backupS3CheckedOk, false);
  assert.equal(els["backup-s3-action"].textContent, "Check connection");
  assert.equal(els["backup-s3-action"].disabled, false);
  assert.ok(els["backup-s3-result"].textContent.indexOf("S3 save failed") !== -1);
});

test("check holds busy disabled label through the sequence", async function () {
  var release;
  var gate = new Promise(function (resolve) { release = resolve; });
  var h5 = loadS3Handle(function (url, opts) {
    if (opts && opts.method === "PATCH") return gate;
    return Promise.resolve({
      status: 204,
      ok: true,
      json: function () { return Promise.resolve({}); }
    });
  });
  var els = h5.elementsById;
  h5.CW.state.backupS3 = { enabled: false };
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var pending = h5.CW.testBackupS3();
  assert.equal(els["backup-s3-action"].disabled, true);
  assert.equal(els["backup-s3-action"].textContent, "Checking…");
  release({
    status: 200,
    ok: true,
    json: function () { return Promise.resolve({ backups: { s3: { enabled: true } } }); }
  });
  await pending;
  assert.equal(els["backup-s3-action"].disabled, false);
  assert.equal(els["backup-s3-action"].textContent, "Save S3 Settings");
});

test("action dispatches Save after a pass with enabled:true", async function () {
  var seen = [];
  var h5 = loadS3Handle(function (url, opts) {
    seen.push({ url: url, opts: opts });
    return Promise.resolve({
      status: 200,
      ok: true,
      json: function () { return Promise.resolve({ backups: { s3: { enabled: true } } }); }
    });
  });
  var els = h5.elementsById;
  h5.CW.state.backupS3 = { enabled: false };
  h5.CW.state.backupS3CheckedOk = true;
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  els["backup-s3-endpoint"] = h5.document.getElementById("backup-s3-endpoint");
  els["backup-s3-endpoint"].value = "https://s3.example.com";
  var calls = [];
  var orig = h5.CW.toast;
  h5.CW.toast = function (msg, ok) { calls.push([msg, ok]); return orig(msg, ok); };
  await h5.CW.onBackupS3Action();
  var patches = seen.filter(function (c) { return c.opts && c.opts.method === "PATCH"; });
  assert.equal(patches.length, 1);
  assert.equal(JSON.parse(patches[0].opts.body).backups.s3.enabled, true);
  assert.deepEqual(calls, [["S3 settings saved.", true]]);
});

// --- §19 badge means connection, not config (RED-first) ---

function trackBadgeClasses(el) {
  var set = {};
  String(el.className || "").split(/\s+/).forEach(function (c) { if (c) set[c] = true; });
  el.classList = {
    toggle: function (cls, force) { if (force) set[cls] = true; else delete set[cls]; return !!set[cls]; },
    add: function (cls) { set[cls] = true; },
    remove: function (cls) { delete set[cls]; },
    contains: function (cls) { return !!set[cls]; }
  };
  return set;
}

function s3OkFetch(patchData) {
  return function (url, opts) {
    if (opts && opts.method === "PATCH") {
      return Promise.resolve({ status: 200, ok: true,
        json: function () { return Promise.resolve(patchData); } });
    }
    return Promise.resolve({ status: 204, ok: true,
      json: function () { return Promise.resolve({}); } });
  };
}

// Quiescent agreement: CONNECTED ⟺ Save button; UNCONNECTED ⟺ Check (or hidden fields).
// §20: badge visibility follows fields — hidden step hides the badge, visible steps show it.
function assertBadgeButtonAgree(h5, els, badgeSet) {
  var badge = els["backup-s3-badge"].textContent;
  var btn = els["backup-s3-action"].textContent;
  var fieldsHidden = !!els["backup-s3-fields"].hidden;
  assert.equal(!!els["backup-s3-badge"].hidden, fieldsHidden,
    "badge hidden must follow fields hidden");
  if (badge === "CONNECTED") {
    assert.equal(fieldsHidden, false, "CONNECTED badge must be visible");
    assert.equal(btn, "Save S3 Settings", "CONNECTED must pair with Save");
    assert.ok(badgeSet["is-connected"], "CONNECTED must carry is-connected");
    assert.ok(!badgeSet["is-unconnected"], "CONNECTED must not carry is-unconnected");
    assert.equal(h5.CW.state.backupS3CheckedOk, true);
  } else {
    assert.equal(badge, "UNCONNECTED", "non-connected text must be UNCONNECTED");
    assert.ok(badgeSet["is-unconnected"], "UNCONNECTED must carry is-unconnected");
    assert.ok(!badgeSet["is-connected"], "UNCONNECTED must not carry is-connected");
    assert.equal(h5.CW.state.backupS3CheckedOk, false);
    if (!els["backup-s3-fields"].hidden) assert.equal(btn, "Check connection");
  }
}

test("RED §19: fresh-load badge UNCONNECTED despite stored enabled", function () {
  var h5 = loadS3Handle();
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  h5.CW.renderBackupS3({ enabled: true, endpoint: "https://s3.example.com", bucket: "b",
    region: "r", accessKey: "AK", secret: "shh", forcePathStyle: true });
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("RED §19: post-204 check paints CONNECTED + Save", async function () {
  var h5 = loadS3Handle(s3OkFetch({ backups: { s3: { enabled: true } } }));
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var out = await h5.CW.testBackupS3();
  assert.equal(out.status, 204);
  assertBadgeButtonAgree(h5, els, badgeSet);
  assert.equal(els["backup-s3-badge"].textContent, "CONNECTED");
});

test("RED §19: check failure reverts badge to UNCONNECTED + Check", async function () {
  var h5 = loadS3Handle(function (url, opts) {
    if (opts && opts.method === "PATCH") {
      return Promise.resolve({ status: 200, ok: true,
        json: function () { return Promise.resolve({ backups: { s3: { enabled: true } } }); } });
    }
    return Promise.resolve({ status: 400, ok: false,
      json: function () { return Promise.resolve({ message: "boom" }); } });
  });
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var out = await h5.CW.testBackupS3();
  assert.equal(out.status, 400);
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("RED §19: edit-after-pass reverts badge to UNCONNECTED + Check", async function () {
  var h5 = loadS3Handle(s3OkFetch({ backups: { s3: { enabled: true } } }));
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  await h5.CW.testBackupS3();
  assert.equal(els["backup-s3-badge"].textContent, "CONNECTED");
  h5.CW.revertBackupS3Check();
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("RED §19: switch-off (stored on) reverts badge to UNCONNECTED + hidden", async function () {
  var h5 = loadS3Handle(function () {
    return Promise.resolve({ status: 200, ok: true,
      json: function () { return Promise.resolve({ backups: { s3: { enabled: false } } }); } });
  });
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  h5.CW.state.backupS3 = { enabled: true };
  h5.CW.state.backupS3CheckedOk = true;
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var out = await h5.CW.onBackupS3Switch();
  assert.equal(out.status, 200);
  assert.equal(els["backup-s3-fields"].hidden, true);
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("RED §19: switch-off failure resets flag so badge↔button agree", async function () {
  var h5 = loadS3Handle(function () {
    return Promise.resolve({ status: 400, ok: false,
      json: function () { return Promise.resolve({ message: "cannot disable now" }); } });
  });
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  h5.CW.state.backupS3 = { enabled: true };
  h5.CW.state.backupS3CheckedOk = true;
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var out = await h5.CW.onBackupS3Switch();
  assert.equal(out.status, 400);
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("Check PATCHes form values (enabled:true) before POSTing test", async function () {
  var seen = [];
  var h6 = loadS3Handle(function (url, opts) {
    seen.push({ url: url, opts: opts });
    if (opts && opts.method === "PATCH") {
      return Promise.resolve({
        status: 200,
        ok: true,
        json: function () { return Promise.resolve({ backups: { s3: {
          enabled: true, endpoint: "https://s3.example.com", bucket: "b",
          region: "r", accessKey: "AK", forcePathStyle: false } } }); }
      });
    }
    return Promise.resolve({
      status: 204,
      ok: true,
      json: function () { return Promise.resolve({}); }
    });
  });
  var els = h6.elementsById;
  h6.CW.state.backupS3 = { enabled: false };
  els["backup-s3-fields"] = h6.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  els["backup-s3-endpoint"] = h6.document.getElementById("backup-s3-endpoint");
  els["backup-s3-endpoint"].value = "https://s3.example.com";
  els["backup-s3-bucket"] = h6.document.getElementById("backup-s3-bucket");
  els["backup-s3-bucket"].value = "b";
  els["backup-s3-region"] = h6.document.getElementById("backup-s3-region");
  els["backup-s3-region"].value = "r";
  els["backup-s3-access-key"] = h6.document.getElementById("backup-s3-access-key");
  els["backup-s3-access-key"].value = "AK";
  var out = await h6.CW.testBackupS3();
  assert.equal(out.status, 204);
  assert.equal(seen.length, 2);
  assert.equal(seen[0].opts.method, "PATCH");
  assert.equal(seen[0].url, "/api/settings");
  var sent = JSON.parse(seen[0].opts.body).backups.s3;
  assert.equal(sent.enabled, true);
  assert.equal(sent.endpoint, "https://s3.example.com");
  assert.equal(sent.bucket, "b");
  assert.equal(sent.region, "r");
  assert.equal(sent.accessKey, "AK");
  assert.equal("secret" in sent, false);
  assert.equal(seen[1].opts.method, "POST");
  assert.equal(seen[1].url, "/api/settings/test/s3");
  assert.equal(els["backup-s3-action"].textContent, "Save S3 Settings");
});

// --- §20 badge visibility + post-save CONNECTED (RED-first) ---

function s3Patch200Fetch(patchData) {
  return function (url, opts) {
    return Promise.resolve({ status: 200, ok: true,
      json: function () { return Promise.resolve(patchData); } });
  };
}

test("RED §20: disabled-load hides badge instead of showing UNCONNECTED", function () {
  var h5 = loadS3Handle();
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  h5.CW.renderBackupS3({ enabled: false, endpoint: "", bucket: "", region: "",
    accessKey: "", secret: "", forcePathStyle: false });
  assert.equal(els["backup-s3-fields"].hidden, true);
  assert.equal(els["backup-s3-badge"].hidden, true);
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("RED §20: enabled-load shows badge again", function () {
  var h5 = loadS3Handle();
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  h5.CW.renderBackupS3({ enabled: false });
  assert.equal(els["backup-s3-badge"].hidden, true);
  h5.CW.renderBackupS3({ enabled: true, endpoint: "https://s3.example.com", bucket: "b",
    region: "r", accessKey: "AK", secret: "shh", forcePathStyle: false });
  assert.equal(els["backup-s3-badge"].hidden, false);
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("RED §20: switch-off no-op hides badge; switch-on re-shows it", async function () {
  var h5 = loadS3Handle(function () {
    return Promise.resolve({ status: 400, ok: false,
      json: function () { return Promise.resolve({ message: "S3 storage filesystem is not enabled" }); } });
  });
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  h5.CW.state.backupS3 = { enabled: false };
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  els["backup-s3-fields"].hidden = false;
  await h5.CW.onBackupS3Switch();
  assert.equal(els["backup-s3-fields"].hidden, true);
  assert.equal(els["backup-s3-badge"].hidden, true);
  assertBadgeButtonAgree(h5, els, badgeSet);
  await h5.CW.onBackupS3Switch(); // ON refires a silent auto-verify; stored off so it fails silent
  assert.equal(els["backup-s3-fields"].hidden, false);
  assert.equal(els["backup-s3-badge"].hidden, false);
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("RED §20: save-after-pass keeps CONNECTED + Save", async function () {
  var stored = { backups: { s3: { enabled: true, endpoint: "https://s3.example.com",
    bucket: "b", region: "r", accessKey: "AK", forcePathStyle: false } } };
  var h5 = loadS3Handle(s3OkFetch(stored));
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-fields"].hidden = false;
  var tested = await h5.CW.testBackupS3();
  assert.equal(tested.status, 204);
  assert.equal(els["backup-s3-badge"].textContent, "CONNECTED");
  var out = await h5.CW.saveBackupS3();
  assert.equal(out.status, 200);
  assert.equal(els["backup-s3-badge"].textContent, "CONNECTED");
  assert.equal(els["backup-s3-action"].textContent, "Save S3 Settings");
  assert.equal(els["backup-s3-secret"].value, "");
  assertBadgeButtonAgree(h5, els, badgeSet);
});

test("§20 guard: save-after-edit (Check step) stays UNCONNECTED", async function () {
  var stored = { backups: { s3: { enabled: true, endpoint: "https://s3.example.com",
    bucket: "b", region: "r", accessKey: "AK", forcePathStyle: false } } };
  var h5 = loadS3Handle(s3Patch200Fetch(stored));
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  h5.CW.renderBackupS3({ enabled: true, endpoint: "https://s3.example.com", bucket: "b",
    region: "r", accessKey: "AK", secret: "", forcePathStyle: false });
  assert.equal(h5.CW.state.backupS3CheckedOk, false);
  var out = await h5.CW.saveBackupS3();
  assert.equal(out.status, 200);
  assert.equal(els["backup-s3-badge"].textContent, "UNCONNECTED");
  assert.equal(els["backup-s3-action"].textContent, "Check connection");
  assertBadgeButtonAgree(h5, els, badgeSet);
});

// --- §21 re-verify on load (RED-first) ---

function s3StoredFetch(s3data, postImpl) {
  return function (url, opts) {
    if (opts && opts.method === "POST") return postImpl(url, opts);
    return Promise.resolve({ status: 200, ok: true,
      json: function () { return Promise.resolve(s3data); } });
  };
}

function s3Test204() {
  return Promise.resolve({ status: 204, ok: true,
    json: function () { return Promise.resolve({}); } });
}

function s3Test400(message) {
  return Promise.resolve({ status: 400, ok: false,
    json: function () { return Promise.resolve({ message: message }); } });
}

function spyToast(h5) {
  var calls = [];
  var orig = h5.CW.toast;
  h5.CW.toast = function (msg, ok) { calls.push([msg, ok]); return orig(msg, ok); };
  return calls;
}

var S3_ENABLED_STORED = { backups: { s3: { enabled: true, endpoint: "https://s3.example.com",
  bucket: "b", region: "r", accessKey: "AK", forcePathStyle: false } } };

test("RED §21: load stored-enabled auto-fires test; 204 → CONNECTED+Save, silent", async function () {
  var seen = [];
  var h5 = loadS3Handle(function (url, opts) {
    seen.push({ url: url, opts: opts });
    if (opts && opts.method === "POST") return s3Test204();
    return Promise.resolve({ status: 200, ok: true,
      json: function () { return Promise.resolve(S3_ENABLED_STORED); } });
  });
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  els["backup-s3-result"] = h5.document.getElementById("backup-s3-result");
  var calls = spyToast(h5);
  var s3 = await h5.CW.loadBackupS3();
  assert.equal(s3.enabled, true);
  assert.equal(seen.length, 2);
  assert.ok(!seen[0].opts || !seen[0].opts.method || seen[0].opts.method === "GET");
  assert.equal(seen[0].url, "/api/settings");
  assert.equal(seen[1].opts.method, "POST");
  assert.equal(seen[1].url, "/api/settings/test/s3");
  assert.deepEqual(JSON.parse(seen[1].opts.body), { filesystem: "backups" });
  assert.ok(seen.every(function (c) { return !c.opts || c.opts.method !== "PATCH"; }),
    "auto-verify never PATCHes");
  assertBadgeButtonAgree(h5, els, badgeSet);
  assert.equal(els["backup-s3-badge"].textContent, "CONNECTED");
  assert.deepEqual(calls, [], "silent auto-verify must not toast");
  assert.equal(els["backup-s3-result"].textContent, "", "silent auto-verify writes no inline status");
});

test("RED §21: load stored-disabled skips auto-verify entirely", async function () {
  var seen = [];
  var h5 = loadS3Handle(function (url, opts) {
    seen.push({ url: url, opts: opts });
    return Promise.resolve({ status: 200, ok: true,
      json: function () { return Promise.resolve({ backups: { s3: { enabled: false } } }); } });
  });
  var els = h5.elementsById;
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  var s3 = await h5.CW.loadBackupS3();
  assert.equal(s3.enabled, false);
  assert.equal(seen.length, 1, "disabled load must make zero test fetch");
  assert.equal(els["backup-s3-fields"].hidden, true);
  assert.equal(els["backup-s3-badge"].hidden, true);
});

test("RED §21: stale 204 after mid-flight value edit stays UNCONNECTED", async function () {
  var release;
  var gate = new Promise(function (resolve) { release = resolve; });
  var posts = 0;
  var h5 = loadS3Handle(s3StoredFetch(S3_ENABLED_STORED, function () { posts++; return gate; }));
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  var pending = h5.CW.loadBackupS3();
  await new Promise(function (r) { setTimeout(r, 0); });
  assert.equal(posts, 1, "auto-verify POST must be in flight");
  // Mid-flight user edit WITHOUT the revert path: only the serialized
  // form snapshot can catch this, the generation is untouched.
  els["backup-s3-bucket"] = h5.document.getElementById("backup-s3-bucket");
  els["backup-s3-bucket"].value = "changed-mid-flight";
  release(s3Test204());
  var s3 = await pending;
  assert.equal(s3.enabled, true);
  assert.equal(h5.CW.state.backupS3CheckedOk, false, "stale 204 must not mark checked");
  assertBadgeButtonAgree(h5, els, badgeSet);
  assert.equal(els["backup-s3-badge"].textContent, "UNCONNECTED");
});

test("RED §21: auto-verify failure stays silent: no toast, no inline", async function () {
  var h5 = loadS3Handle(s3StoredFetch(S3_ENABLED_STORED, function () { return s3Test400("boom"); }));
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  els["backup-s3-result"] = h5.document.getElementById("backup-s3-result");
  var calls = spyToast(h5);
  await h5.CW.loadBackupS3();
  assert.equal(h5.CW.state.backupS3CheckedOk, false);
  assertBadgeButtonAgree(h5, els, badgeSet);
  assert.deepEqual(calls, [], "silent failure must not toast");
  assert.equal(els["backup-s3-result"].textContent, "", "silent failure writes no inline error");
});

test("RED §21: auto-verify transport failure resolves silent UNCONNECTED", async function () {
  var h5 = loadS3Handle(s3StoredFetch(S3_ENABLED_STORED, function () {
    return Promise.reject(new Error("boom"));
  }));
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  els["backup-s3-result"] = h5.document.getElementById("backup-s3-result");
  var calls = spyToast(h5);
  var s3 = await h5.CW.loadBackupS3();
  assert.equal(s3.enabled, true);
  assert.equal(h5.CW.state.backupS3CheckedOk, false);
  assertBadgeButtonAgree(h5, els, badgeSet);
  assert.deepEqual(calls, [], "transport failure must not toast");
  assert.equal(els["backup-s3-result"].textContent, "", "transport failure writes no inline error");
});

test("RED §21: disable-then-re-enable refires fresh verify; stale first resolve ignored", async function () {
  var gates = [];
  var posts = [];
  var h5 = loadS3Handle(function (url, opts) {
    if (opts && opts.method === "POST") {
      posts.push({ url: url, opts: opts });
      return new Promise(function (resolve) { gates.push(resolve); });
    }
    if (opts && opts.method === "PATCH") {
      return Promise.resolve({ status: 200, ok: true,
        json: function () { return Promise.resolve({ backups: { s3: { enabled: false } } }); } });
    }
    return Promise.resolve({ status: 200, ok: true,
      json: function () { return Promise.resolve(S3_ENABLED_STORED); } });
  });
  var els = h5.elementsById;
  var badgeSet = trackBadgeClasses(h5.document.getElementById("backup-s3-badge"));
  els["backup-s3-fields"] = h5.document.getElementById("backup-s3-fields");
  els["backup-s3-action"] = h5.document.getElementById("backup-s3-action");
  var pending = h5.CW.loadBackupS3();
  await new Promise(function (r) { setTimeout(r, 0); });
  assert.equal(posts.length, 1, "load-time auto-verify must be in flight");
  var off = await h5.CW.onBackupS3Switch();
  assert.equal(off.status, 200);
  assert.equal(els["backup-s3-fields"].hidden, true);
  var onDone = h5.CW.onBackupS3Switch();
  await new Promise(function (r) { setTimeout(r, 0); });
  assert.equal(posts.length, 2, "re-enable must fire a fresh auto-verify");
  gates[0](s3Test204());
  gates[1](s3Test204());
  await onDone;
  await pending;
  assert.equal(h5.CW.state.backupS3CheckedOk, true);
  assertBadgeButtonAgree(h5, els, badgeSet);
  assert.equal(els["backup-s3-badge"].textContent, "CONNECTED");
});

// --- backup upload validation + upload path ---

test("validateBackupUploadFile no-FormData -> unavailable", function () {
  var msg = C.validateBackupUploadFile({ name: "a.zip" }, false);
  assert.ok(msg.indexOf("unavailable") !== -1, msg);
});

test("validateBackupUploadFile null file -> pick", function () {
  var a = C.validateBackupUploadFile(null, true);
  assert.ok(a.indexOf("Pick") !== -1, a);
  var b = C.validateBackupUploadFile(undefined, true);
  assert.ok(b.indexOf("Pick") !== -1, b);
});

test("validateBackupUploadFile bad names -> name-pattern error", function () {
  assert.equal(C.validateBackupUploadFile({ name: "Backup.zip" }, true), "", "Backup.zip now valid");
  ["a.txt", "../x.zip", ""].forEach(function (n) {
    var msg = C.validateBackupUploadFile({ name: n }, true);
    assert.ok(/must (match|end)/.test(msg), n + " -> " + msg);
  });
});

test("validateBackupUploadFile good names -> empty", function () {
  ["a.zip", "my-backup_01.zip"].forEach(function (n) {
    assert.equal(C.validateBackupUploadFile({ name: n }, true), "", n);
  });
});

test("CW.uploadBackup is wired function + boot.js wires header upload button", function () {
  var fs = require("node:fs");
  var path = require("node:path");
  assert.equal(typeof C.uploadBackup, "function");
  assert.equal(typeof C.triggerBackupUpload, "function");
  var src = fs.readFileSync(path.join(__dirname, "..", "pb_public", "js", "boot.js"), "utf8");
  assert.ok(src.indexOf('CW.on("backup-upload-btn"') !== -1);
  assert.ok(src.indexOf('CW.on("backup-upload-file"') !== -1);
});

function loadUploadHandle(fetchImpl) {
  return harness.load(["core.js", "backups.js"], { fetchImpl: fetchImpl });
}

function ensureUploadFormData(handle) {
  if (typeof handle.context.FormData === "undefined") {
    handle.context.FormData = function () { this.parts = []; };
    handle.context.FormData.prototype.append = function (k, v, n) { this.parts.push([k, v, n]); };
  }
}

test("uploadBackup validation path rejects bad name without fetch", async function () {
  var hu = loadUploadHandle();
  ensureUploadFormData(hu);
  var input = hu.document.getElementById("backup-upload-file");
  input.files = [{ name: "bad.txt" }];
  input.value = "bad.txt";
  var result = hu.document.getElementById("backup-result");
  var out = await hu.CW.uploadBackup({ preventDefault: function () {} });
  assert.equal(out.status, 0);
  assert.ok(/must (match|end)/.test(result.textContent), result.textContent);
  assert.equal(hu.fetchCalls.length, 0);
});

test("uploadBackup success posts multipart, clears input, says uploaded", async function () {
  var hu = loadUploadHandle(function (url, opts) {
    if (url === "/api/backups/upload" && opts && opts.method === "POST") {
      return Promise.resolve({ status: 204, ok: true,
        json: function () { return Promise.resolve({}); } });
    }
    return Promise.resolve({ status: 200, ok: true,
      json: function () { return Promise.resolve([]); } });
  });
  ensureUploadFormData(hu);
  var input = hu.document.getElementById("backup-upload-file");
  input.files = [{ name: "good.zip" }];
  input.value = "good.zip";
  var result = hu.document.getElementById("backup-result");
  var out = await hu.CW.uploadBackup({ preventDefault: function () {} });
  assert.equal(out.status, 204);
  assert.equal(input.value, "");
  assert.equal(result.textContent, "Backup uploaded.");
  var up = null;
  hu.fetchCalls.forEach(function (c) { if (c.url === "/api/backups/upload") up = c; });
  assert.ok(up, "upload fetch captured");
  assert.equal(up.opts.method, "POST");
  var body = up.opts.body;
  var isFD = (hu.context.FormData && body instanceof hu.context.FormData) ||
    (body && typeof body.append === "function");
  assert.ok(isFD, "multipart FormData body");
  var hdrs = up.opts.headers || {};
  assert.ok(Object.keys(hdrs).every(function (k) { return String(k).toLowerCase() !== "content-type"; }),
    "no manual Content-Type");
});

test("triggerBackupUpload clicks hidden file input", function () {
  var hu = loadUploadHandle();
  var clicked = false;
  var input = hu.document.getElementById("backup-upload-file");
  input.click = function () { clicked = true; };
  hu.CW.triggerBackupUpload();
  assert.equal(clicked, true);
});

// --- backupListErrorMessage: S3 failures map to friendly guidance ---

test("backupListErrorMessage maps S3 failures to friendly guidance", function () {
  var friendly = "Backup list unavailable \u2014 the backup store is unreachable (S3 AccessDenied). Check the S3 settings below (endpoint, bucket, keys, bucket policy), Save, then retry.";
  var rawReport = "Failed to retrieve backup items. Raw error: 403 AccessDenied: Access Denied (RAW: <?xml version=\"1.0\" encoding=\"UTF-8\"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>...)";
  [rawReport, "Access Denied", "NoSuchBucket", "SignatureDoesNotMatch"].forEach(function (msg) {
    assert.equal(C.backupListErrorMessage({ message: msg }), friendly, msg);
  });
});

test("backupListErrorMessage passes other messages through verbatim", function () {
  assert.equal(C.backupListErrorMessage({ message: "Load failed." }), "Load failed.");
  assert.equal(C.backupListErrorMessage({ message: "boom" }), "boom");
});

// --- isValidBackupUploadName: widened upload rule ---

test("isValidBackupUploadName widened accept table", function () {
  ["Backup.zip", "My Backup (1).zip", "a.b_c-d+e@f.zip", "good.zip"].forEach(function (n) {
    assert.equal(C.isValidBackupUploadName(n), true, n);
  });
});

test("isValidBackupUploadName reject table: dots, slashes, padding, length", function () {
  var longName = "a";
  var i;
  for (i = 0; i < 200; i++) { longName += "a"; }
  longName += ".zip";
  ["", "a.txt", ".zip", ".hidden.zip", "a/b.zip", "a\\b.zip", "../x.zip",
    " a.zip", "a.zip ", longName].forEach(function (n) {
    assert.equal(C.isValidBackupUploadName(n), false, String(n).slice(0, 20));
  });
});

test("validateBackupUploadFile widened names: space/parens ok, slash hints no /", function () {
  assert.equal(C.validateBackupUploadFile({ name: "My Backup (1).zip" }, true), "");
  var msg = C.validateBackupUploadFile({ name: "a/b.zip" }, true);
  assert.ok(msg.indexOf("no /") !== -1, msg);
});
