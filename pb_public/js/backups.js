/* ConfigWire admin shell — backups.js (PocketBase data backups, superuser).
 * Lists, creates, downloads, deletes, and restores /api/backups snapshots.
 * Auth is the BARE superuser token (CW.authHeaders — never "Bearer"), same
 * as every other admin card. Download rides a short-lived superuser file
 * token from POST /api/files/token (never logged).
 */
(function () {
  "use strict";

  var CW = window.CW;

  // Server naming rule; empty string means "server auto-names".
  var BACKUP_NAME_PATTERN = "^[a-z0-9_-]+\\.zip$";
  var BACKUP_NAME_RE = new RegExp(BACKUP_NAME_PATTERN);

  function normalizeBackupName(raw) {
    return String(raw == null ? "" : raw).replace(/^\s+|\s+$/g, "");
  }

  function isValidBackupName(name) {
    return BACKUP_NAME_RE.test(String(name == null ? "" : name));
  }

  function isValidBackupUploadName(name) {
    var s = String(name == null ? "" : name);
    if (s.length < 5 || s.length > 150) return false;
    if (s !== s.replace(/^\s+|\s+$/g, "")) return false;
    if (s.charAt(0) === ".") return false;
    if (s.slice(-4) !== ".zip") return false;
    var base = s.slice(0, -4);
    if (base === "" || base.indexOf("..") !== -1) return false;
    if (/[\/\\\x00-\x1f\x7f]/.test(s)) return false;
    return true;
  }

  function encodeBackupKey(key) {
    return encodeURIComponent(String(key == null ? "" : key));
  }

  function backupDownloadPath(key, fileToken) {
    return "/api/backups/" + encodeBackupKey(key) +
      "?token=" + encodeURIComponent(String(fileToken == null ? "" : fileToken));
  }

  function formatBackupSize(bytes) {
    if (bytes == null || bytes === "") return "—";
    var n = Number(bytes);
    if (!isFinite(n) || n < 0) return "—";
    if (n < 1024) return n + " B";
    var units = ["KB", "MB", "GB", "TB"];
    var v = n / 1024;
    var u = 0;
    while (v >= 1024 && u < units.length - 1) { v /= 1024; u++; }
    var s = v >= 100 ? String(Math.round(v)) : String(Math.round(v * 10) / 10);
    return s + " " + units[u];
  }

  function formatBackupDate(v) {
    var raw = String(v == null ? "" : v);
    if (raw === "") return "—";
    var d = new Date(raw);
    if (isNaN(d.getTime())) return raw;
    try {
      if (typeof d.toLocaleString === "function") {
        var s = d.toLocaleString();
        if (s) return s;
      }
    } catch (e) { /* fall through to ISO */ }
    try { return d.toISOString(); } catch (e2) { return raw; }
  }

  // Single-flight 400: PocketBase answers 400 "another backup/restore
  // operation has already been started" when a create/delete/restore races
  // an in-progress one. Surfaced inline, never a hang.
  function backupFailureMessage(outOrErr) {
    if (outOrErr && outOrErr.message) return String(outOrErr.message);
    if (outOrErr && outOrErr.data) return CW.serverMessage(outOrErr.data);
    return "request failed";
  }

  function isBackupBusy(outOrErr) {
    var msg = backupFailureMessage(outOrErr);
    return /already been started|another backup/i.test(msg);
  }

  function backupListErrorMessage(err) {
    var raw = (err && err.message) || "Load failed.";
    raw = String(raw);
    if (/AccessDenied|Access Denied|Failed to retrieve backup items|<\?xml|<Code>|InvalidAccessKeyId|SignatureDoesNotMatch|NoSuchBucket|TokenRefreshRequired|credentials|bucket policy/i.test(raw)) {
      return "Backup list unavailable — the backup store is unreachable (S3 AccessDenied). Check the S3 settings below (endpoint, bucket, keys, bucket policy), Save, then retry.";
    }
    return raw;
  }

  // The test endpoint probes STORED settings, so a just-filled form
  // must be saved first or the test 400s on stale stored values. A
  // "not enabled" answer after our own save means stored is still
  // disabled — map that one case to re-sync guidance; everything else
  // passes through.
  function backupS3TestMessage(out) {
    if (/not enabled/i.test(backupFailureMessage(out))) {
      return "S3 is still disabled in the saved settings — flip the switch off and on again, then check.";
    }
    return CW.apiError(out, "S3 connection test failed");
  }

  function backupStatus(msg, ok) {
    var el = CW.$("backup-result");
    if (el) el.textContent = msg == null ? "" : String(msg);
    if (msg) CW.toast(msg, ok);
  }

  function backupListEl() {
    return CW.$("backup-list");
  }

  function clearBackupList() {
    var list = backupListEl();
    if (!list) return null;
    while (list.firstChild) list.removeChild(list.firstChild);
    return list;
  }

  function backupRowActionCell(key) {
    var cell = document.createElement("span");
    cell.className = "backup-actions";
    var actions = [
      { kind: "download", label: "Download", cls: "btn secondary" },
      { kind: "restore", label: "Restore", cls: "btn ghost" },
      { kind: "delete", label: "Delete", cls: "btn danger" }
    ];
    actions.forEach(function (a) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = a.cls;
      b.setAttribute("data-backup-" + a.kind, key);
      b.textContent = a.label;
      cell.appendChild(b);
    });
    return cell;
  }

  // textContent only — server strings (key/modified) never touch innerHTML.
  function renderBackups(items) {
    var list = backupListEl();
    if (!list) return;
    if (typeof document === "undefined" || !document.createElement) return;
    var rows = Array.isArray(items) ? items : [];
    clearBackupList();
    if (!rows.length) {
      var empty = document.createElement("li");
      empty.textContent = "No backups yet.";
      list.appendChild(empty);
      return;
    }
    rows.forEach(function (b) {
      var key = (b && b.key) != null ? String(b.key) : "";
      var li = document.createElement("li");
      var name = document.createElement("code");
      name.textContent = key || "(unnamed)";
      li.appendChild(name);
      var meta = document.createElement("span");
      meta.className = "backup-meta muted";
      meta.textContent = formatBackupDate(b && b.modified) + " · " + formatBackupSize(b && b.size);
      li.appendChild(meta);
      li.appendChild(backupRowActionCell(key));
      list.appendChild(li);
    });
  }

  function loadBackups() {
    return CW.api("/api/backups").then(function (data) {
      var items = Array.isArray(data) ? data : (data && data.items) || [];
      CW.state.backups = items;
      renderBackups(items);
      return items;
    }, function (err) {
      var list = backupListEl();
      if (list) {
        if (typeof document !== "undefined" && document.createElement) {
          clearBackupList();
          var li = document.createElement("li");
          li.textContent = backupListErrorMessage(err);
          list.appendChild(li);
        } else {
          list.textContent = backupListErrorMessage(err);
        }
      }
      throw err;
    });
  }

  function createBackup(ev) {
    if (ev) ev.preventDefault();
    var name = normalizeBackupName(CW.$("backup-name") && CW.$("backup-name").value);
    if (name !== "" && !isValidBackupName(name)) {
      var bad = "Name must match " + BACKUP_NAME_PATTERN + " (or leave empty for auto-name).";
      backupStatus(bad);
      return Promise.resolve();
    }
    var body = name === "" ? {} : { name: name };
    return CW.apiMut("POST", "/api/backups", body).then(function (out) {
      if (out.status === 204) {
        var input = CW.$("backup-name");
        if (input) input.value = "";
        backupStatus("Backup created.", true);
        if (CW.markFormClean) CW.markFormClean("backup-create-form");
        loadBackups().catch(function (e) { console.warn('backups: create list refresh', e); });
        return out;
      }
      var fail = isBackupBusy(out)
        ? "Backup already in progress — try again when it finishes."
        : CW.apiError(out, "Backup create failed");
      backupStatus(fail);
      return out;
    });
  }

  function deleteBackup(key) {
    var k = String(key == null ? "" : key);
    if (k === "") return Promise.resolve();
    return CW.confirmDialog("Delete backup " + k + "?", {
      title: "Delete backup", okText: "Delete", danger: true
    }).then(function (ok) {
      if (!ok) return;
      return CW.apiMut("DELETE", "/api/backups/" + encodeBackupKey(k)).then(function (out) {
        var done = out.status === 200 || out.status === 204;
        backupStatus(done
          ? "Backup deleted."
          : (isBackupBusy(out)
            ? "Backup is in progress and cannot be deleted yet."
            : CW.apiError(out, "Delete failed")), done || undefined);
        if (done) loadBackups().catch(function (e) { console.warn('backups: delete list refresh', e); });
        return out;
      });
    });
  }

  function restoreBackup(key) {
    var k = String(key == null ? "" : key);
    if (k === "") return Promise.resolve();
    return CW.confirmDialog(
      "Restore backup " + k + "? This overwrites the current data and restarts the server about 1 second later. The console will briefly go offline.",
      { title: "Restore backup", okText: "Restore", danger: true }
    ).then(function (ok) {
      if (!ok) return;
      return CW.apiMut("POST", "/api/backups/" + encodeBackupKey(k) + "/restore").then(function (out) {
        if (out.status === 204) {
          backupStatus("Restore started — the server restarts in about 1 second.", true);
          return out;
        }
        var fail = isBackupBusy(out)
          ? "Another backup/restore operation is already in progress."
          : CW.apiError(out, "Restore failed");
        backupStatus(fail);
        return out;
      });
    });
  }

  // Download: mint a short-lived superuser file token, then ride it as
  // GET /api/backups/{key}?token=<fileToken> through a programmatic
  // anchor download. The token is never logged.
  function downloadBackup(key) {
    var k = String(key == null ? "" : key);
    if (k === "") return Promise.resolve();
    return CW.apiMut("POST", "/api/files/token", {}).then(function (out) {
      if (out.status !== 200 || !out.data || !out.data.token) {
        backupStatus(CW.apiError(out, "Download failed"));
        return out;
      }
      var href = backupDownloadPath(k, out.data.token);
      try {
        var a = document.createElement("a");
        a.href = href;
        a.download = k;
        if (document.body) document.body.appendChild(a);
        if (a.click) a.click();
        if (a.parentNode) a.parentNode.removeChild(a);
      } catch (e) {
        backupStatus((e && e.message) || "Download failed");
        return out;
      }
      backupStatus("Download started.", true);
      return out;
    });
  }

  function backupUploadStatus(msg, ok) {
    var el = CW.$("backup-result");
    if (el) el.textContent = msg == null ? "" : String(msg);
    if (msg) CW.toast(msg, ok);
  }

  function validateBackupUploadFile(file, hasFile) {
    if (!hasFile) return "File upload unavailable in this browser.";
    if (!file) return "Pick a .zip file first.";
    var name = String(file.name || "");
    if (!isValidBackupUploadName(name)) return "File name must end in .zip (1-150 chars; no / \\ or .. sequences).";
    return "";
  }

  function uploadBackup(ev) {
    if (ev && ev.preventDefault) ev.preventDefault();
    var input = CW.$("backup-upload-file");
    var hasApi = (typeof FormData !== "undefined");
    var f = input && input.files ? input.files[0] : null;
    var bad = validateBackupUploadFile(f, hasApi);
    if (bad !== "") { try { var ri = CW.$("backup-upload-file"); if (ri) ri.value = ""; } catch (e3) { console.warn('backups: upload input reset', e3); } backupUploadStatus(bad); return Promise.resolve({ status: 0, data: null }); }
    var fd;
    try { fd = new FormData(); fd.append("file", f, f.name || "backup.zip"); }
    catch (e) { backupUploadStatus((e && e.message) || "Upload failed"); return Promise.resolve({ status: 0, data: null }); }
    return fetch("/api/backups/upload", { method: "POST", headers: CW.authHeaders(), body: fd }).then(function (res) {
      return res.json().then(function (data) { return { status: res.status, data: data }; }, function () { return { status: res.status, data: { message: "HTTP " + res.status } }; });
    }, function (err) {
      backupUploadStatus((err && err.message) || "Upload failed");
      throw err;
    }).then(function (out) {
      if (out.status === 401 || out.status === 403) {
        if (CW.logout) try { CW.logout(); } catch (e) { console.warn('backups: upload logout', e); }
        backupUploadStatus(CW.apiError(out, "Upload failed"));
        return out;
      }
      var done = (out.status === 200 || out.status === 204);
      if (done) {
        try { if (input) input.value = ""; } catch (e) { console.warn('backups: upload input clear', e); }
        backupUploadStatus("Backup uploaded.", true);
        loadBackups().catch(function (e) { console.warn('backups: upload list refresh', e); });
        return out;
      }
      var fail = isBackupBusy(out) ? "Backup already in progress — try again when it finishes." : CW.apiError(out, "Upload failed");
      backupUploadStatus(fail);
      return out;
    });
  }

  function triggerBackupUpload() { var input = CW.$("backup-upload-file"); if (input && input.click) { try { input.click(); } catch (e) { /* picker unavailable */ } } }

  function onBackupListClick(ev) {
    var t = ev && ev.target ? ev.target : null;
    function attr(name) {
      var el = null;
      if (t) {
        if (t.closest) el = t.closest("[" + name + "]");
        else if (t.getAttribute && t.getAttribute(name) != null) el = t;
      }
      return el && el.getAttribute ? el.getAttribute(name) : null;
    }
    var dl = attr("data-backup-download");
    if (dl !== null) { downloadBackup(dl).catch(CW.toastErr); return; }
    var rs = attr("data-backup-restore");
    if (rs !== null) { restoreBackup(rs).catch(CW.toastErr); return; }
    var del = attr("data-backup-delete");
    if (del !== null) { deleteBackup(del).catch(CW.toastErr); }
  }

  // ---- S3 backup target (PocketBase native settings: backups.s3). ----
  // GET /api/settings returns the full settings object with sensitive
  // S3 secrets blanked server-side ("" / omitted — never the real
  // value), so a stored secret is never echoed back into the form.
  //
  // Save sends PATCH /api/settings with ONLY the backups.s3 subtree.
  // An empty secret input is OMITTED so the stored secret is untouched
  // (the server merges the body onto the stored clone; sending ""
  // would wipe it). Test hits POST /api/settings/test/s3 with
  // {filesystem:"backups"} (forms.TestS3Filesystem, 204 = success) and
  // exercises the STORED settings — save first, then test.

  function parseBackupS3(data) {
    var s = (data && data.backups && data.backups.s3) || {};
    return {
      enabled: !!s.enabled,
      endpoint: s.endpoint != null ? String(s.endpoint) : "",
      bucket: s.bucket != null ? String(s.bucket) : "",
      region: s.region != null ? String(s.region) : "",
      accessKey: s.accessKey != null ? String(s.accessKey) : "",
      secret: s.secret != null ? String(s.secret) : "",
      forcePathStyle: !!s.forcePathStyle
    };
  }

  // Pure PATCH-body builder. values mirrors the form fields; an empty
  // secret (""/null/undefined) is omitted, never sent.
  function buildBackupS3Patch(values) {
    var v = values || {};
    var s3 = {
      enabled: !!v.enabled,
      endpoint: String(v.endpoint == null ? "" : v.endpoint),
      bucket: String(v.bucket == null ? "" : v.bucket),
      region: String(v.region == null ? "" : v.region),
      accessKey: String(v.accessKey == null ? "" : v.accessKey),
      forcePathStyle: !!v.forcePathStyle
    };
    if (v.secret != null && String(v.secret) !== "") {
      s3.secret = String(v.secret);
    }
    return { backups: { s3: s3 } };
  }

  function backupS3Status(msg, ok) {
    var el = CW.$("backup-s3-result");
    if (el) el.textContent = msg == null ? "" : String(msg);
    if (msg) CW.toast(msg, ok);
  }

  // Pure step machine for the guided S3 flow. fieldsVisible mirrors
  // the switch position (both are rendered together, never apart).
  function backupS3Step(st) {
    var s = st || {};
    if (!s.fieldsVisible) return "hidden";
    if (s.checkedOk && !s.dirtySinceCheck) return "save";
    return "check";
  }

  // Badge = last-known connection-test outcome this session, never
  // stored config (the switch position already shows that). CONNECTED
  // only after a 204 check; UNCONNECTED everywhere else.
  function paintBackupS3Badge(connected) {
    var badge = CW.$("backup-s3-badge");
    if (!badge) return;
    badge.textContent = connected ? "CONNECTED" : "UNCONNECTED";
    if (badge.classList && badge.classList.toggle) {
      badge.classList.toggle("is-connected", !!connected);
      badge.classList.toggle("is-unconnected", !connected);
    }
  }

  function backupS3FieldsVisible() {
    var fields = CW.$("backup-s3-fields");
    return !(fields && fields.hidden);
  }

  function setBackupS3Switch(on) {
    var sw = CW.$("backup-s3-switch");
    if (sw && sw.setAttribute) sw.setAttribute("aria-checked", on ? "true" : "false");
  }

  function renderBackupS3Step(step) {
    var fields = CW.$("backup-s3-fields");
    if (fields) fields.hidden = (step === "hidden");
    var badge = CW.$("backup-s3-badge");
    if (badge) badge.hidden = (step === "hidden");
    var btn = CW.$("backup-s3-action");
    if (btn) {
      if (step === "save") {
        btn.textContent = "Save S3 Settings";
        btn.className = "btn primary";
      } else {
        btn.textContent = "Check connection";
        btn.className = "btn secondary";
      }
    }
  }

  function backupS3CurrentStep() {
    return backupS3Step({
      storedEnabled: !!(CW.state.backupS3 && CW.state.backupS3.enabled),
      fieldsVisible: backupS3FieldsVisible(),
      checkedOk: !!CW.state.backupS3CheckedOk,
      dirtySinceCheck: false // edits reset checkedOk immediately, so never stale here
    });
  }

  // Switch flip. OFF with stored enabled PATCHes {enabled:false}
  // immediately; OFF while stored off is a no-op hide; ON reveals and
  // fires a fresh silent auto-verify (§21, same staleness guard).
  function onBackupS3Switch() {
    backupS3VerifyGen++; // any flip invalidates an in-flight verify
    var storedOn = !!(CW.state.backupS3 && CW.state.backupS3.enabled);
    if (backupS3FieldsVisible()) {
      setBackupS3Switch(false);
      if (!storedOn) {
        CW.state.backupS3CheckedOk = false;
        paintBackupS3Badge(false);
        renderBackupS3Step("hidden");
        return Promise.resolve();
      }
      return CW.apiMut("PATCH", "/api/settings", { backups: { s3: { enabled: false } } }).then(function (out) {
        if (out.status === 200) {
          var s3 = parseBackupS3(out.data);
          CW.state.backupS3 = s3;
          renderBackupS3(s3);
          backupS3Status("S3 storage disabled.", true);
          return out;
        }
        CW.state.backupS3CheckedOk = false;
        paintBackupS3Badge(false);
        setBackupS3Switch(true);
        renderBackupS3Step("check");
        backupS3Status(CW.apiError(out, "S3 disable failed"));
        return out;
      });
    }
    setBackupS3Switch(true);
    CW.state.backupS3CheckedOk = false;
    paintBackupS3Badge(false);
    renderBackupS3Step("check");
    return autoVerifyBackupS3();
  }

  // Single action button: Check until a pass morphs it to Save.
  function onBackupS3Action(ev) {
    if (ev && ev.preventDefault) ev.preventDefault();
    if (backupS3CurrentStep() === "save") return saveBackupS3(ev);
    if (backupS3CurrentStep() === "hidden") return Promise.resolve();
    return testBackupS3();
  }

  // Any field edit after a pass reverts to Check (must re-verify).
  // The generation bump also invalidates an in-flight load-time
  // auto-verify (§21): any edit signal wins, even one reverted before
  // the test resolves.
  function revertBackupS3Check() {
    backupS3VerifyGen++;
    if (!CW.state.backupS3CheckedOk) return;
    CW.state.backupS3CheckedOk = false;
    paintBackupS3Badge(false);
    renderBackupS3Step("check");
  }

  function readBackupS3Form() {
    function val(id) {
      var el = CW.$(id);
      return el && el.value != null ? String(el.value) : "";
    }
    function checked(id) {
      var el = CW.$(id);
      return !!(el && el.checked);
    }
    return {
      enabled: backupS3FieldsVisible(),
      endpoint: val("backup-s3-endpoint"),
      bucket: val("backup-s3-bucket"),
      region: val("backup-s3-region"),
      accessKey: val("backup-s3-access-key"),
      secret: val("backup-s3-secret"),
      forcePathStyle: checked("backup-s3-force-path-style")
    };
  }

  // textContent only — server strings never touch innerHTML.
  // Also paints the status badge + switch (optional polish nodes); the
  // form prefill below is the functional core. Always lands on Check.
  function renderBackupS3(s3) {
    var v = s3 || {};
    paintBackupS3Badge(false); // every (re)render is unverified until a 204 check
    setBackupS3Switch(!!v.enabled);
    CW.state.backupS3CheckedOk = false;
    renderBackupS3Step(v.enabled ? "check" : "hidden");
    function setVal(id, val) { var el = CW.$(id); if (el) el.value = val; }
    function setChecked(id, val) { var el = CW.$(id); if (el) el.checked = !!val; }
    setVal("backup-s3-endpoint", v.endpoint || "");
    setVal("backup-s3-bucket", v.bucket || "");
    setVal("backup-s3-region", v.region || "");
    setVal("backup-s3-access-key", v.accessKey || "");
    setVal("backup-s3-secret", ""); // never echo a secret back into the form
    setChecked("backup-s3-force-path-style", v.forcePathStyle);
  }

  // Show/hide toggle for the secret input. Wired in boot.js via CW.on;
  // textContent + type flip only, never touches the stored value.
  function toggleBackupS3Secret(ev) {
    if (ev && ev.preventDefault) ev.preventDefault();
    var input = CW.$("backup-s3-secret");
    var btn = CW.$("backup-s3-secret-toggle");
    if (!input) return;
    var show = input.type !== "text";
    try {
      input.type = show ? "text" : "password";
    } catch (e) {
      if (input.setAttribute) input.setAttribute("type", show ? "text" : "password");
    }
    if (btn) {
      btn.textContent = show ? "Hide" : "Show";
      if (btn.setAttribute) {
        btn.setAttribute("aria-pressed", show ? "true" : "false");
        btn.setAttribute("aria-label", show ? "Hide secret" : "Show secret");
        btn.setAttribute("title", show ? "Hide secret" : "Show secret");
      }
    }
  }

  function loadBackupS3() {
    return CW.api("/api/settings").then(function (data) {
      var s3 = parseBackupS3(data);
      CW.state.backupS3 = s3;
      renderBackupS3(s3);
      // Re-verify on load when stored S3 is enabled: one silent test of
      // the stored settings (never a PATCH). Disabled → no fetch at all.
      if (!s3.enabled) return s3;
      return autoVerifyBackupS3().then(function () { return s3; });
    }, function (err) {
      backupS3Status("S3 settings unavailable: " + ((err && err.message) || "load failed"));
      throw err;
    });
  }

  function saveBackupS3(ev) {
    if (ev) ev.preventDefault();
    var wasChecked = !!CW.state.backupS3CheckedOk;
    var body = buildBackupS3Patch(readBackupS3Form());
    return CW.apiMut("PATCH", "/api/settings", body).then(function (out) {
      if (out.status === 200) {
        var s3 = parseBackupS3(out.data);
        CW.state.backupS3 = s3;
        renderBackupS3(s3); // clears the secret input
        // Save is only reachable with checked values: edits revert to
        // Check first (re-verifying via test), empty secret keeps the
        // tested stored value, and a typed secret was saved+tested by
        // the preceding Check — so a pre-save pass still holds.
        if (wasChecked) {
          CW.state.backupS3CheckedOk = true;
          paintBackupS3Badge(true);
          renderBackupS3Step("save");
        }
        backupS3Status("S3 settings saved.", true);
        if (CW.markFormClean) CW.markFormClean("backup-s3-form");
        return out;
      }
      backupS3Status(CW.apiError(out, "S3 save failed"));
      return out;
    });
  }

  // Shared POST-test outcome for the manual Check and the silent
  // load-time auto-verify (§21): 204 → checked + CONNECTED + Save step,
  // anything else → UNCONNECTED + Check step. silent=true writes no
  // inline status and no toast — a load must never alarm; the badge
  // alone carries the result.
  function applyBackupS3TestOutcome(out, silent) {
    if (out.status === 204) {
      CW.state.backupS3CheckedOk = true;
      paintBackupS3Badge(true);
      renderBackupS3Step("save");
      if (!silent) backupS3Status("S3 connection OK.", true);
    } else {
      CW.state.backupS3CheckedOk = false;
      paintBackupS3Badge(false);
      renderBackupS3Step("check");
      if (!silent) backupS3Status(backupS3TestMessage(out));
    }
    return out;
  }

  // Load-time re-verify (§21). POSTs the exact endpoint/body the manual
  // Check uses, but never PATCHes: stored settings are the source of
  // truth on load. Snapshot = generation + serialized form at test
  // start; the outcome applies ONLY when both still match at resolve —
  // a mid-flight user edit (or switch flip, which bumps the
  // generation) wins and the card stays UNCONNECTED + Check.
  // Transport failure resolves the same silent UNCONNECTED end-state;
  // this promise never rejects, so fire-and-forget callers stay quiet.
  var backupS3VerifyGen = 0;
  function serializeBackupS3Form() {
    try { return JSON.stringify(readBackupS3Form()); } catch (e) { return ""; }
  }
  function autoVerifyBackupS3() {
    var snapGen = backupS3VerifyGen;
    var snapForm = serializeBackupS3Form();
    function fresh() {
      return snapGen === backupS3VerifyGen &&
        serializeBackupS3Form() === snapForm;
    }
    return CW.apiMut("POST", "/api/settings/test/s3", { filesystem: "backups" }).then(function (out) {
      if (!fresh()) return out;
      return applyBackupS3TestOutcome(out, true);
    }, function () {
      if (!fresh()) return { status: 0, data: null };
      return applyBackupS3TestOutcome({ status: 0, data: null }, true);
    });
  }

  function setBackupS3ActionBusy(busy) {
    var btn = CW.$("backup-s3-action");
    if (!btn) return;
    btn.disabled = !!busy;
    if (busy) btn.textContent = "Checking…";
  }

  // Check saves the live form first (fields visible ⟺ switch ON, so
  // enabled is forced true), then tests the stored settings. Fixes the
  // first-setup deadlock where the test probed empty stored values.
  function testBackupS3() {
    setBackupS3ActionBusy(true);
    var form = readBackupS3Form();
    form.enabled = true;
    return CW.apiMut("PATCH", "/api/settings", buildBackupS3Patch(form)).then(function (saveOut) {
      if (saveOut.status !== 200) {
        CW.state.backupS3CheckedOk = false;
        paintBackupS3Badge(false);
        renderBackupS3Step("check");
        setBackupS3ActionBusy(false);
        backupS3Status(CW.apiError(saveOut, "S3 save failed"));
        return saveOut;
      }
      var s3 = parseBackupS3(saveOut.data);
      CW.state.backupS3 = s3;
      renderBackupS3(s3);
      setBackupS3ActionBusy(true); // render restores the Check label; stay busy through the test call
      return CW.apiMut("POST", "/api/settings/test/s3", { filesystem: "backups" }).then(function (out) {
        setBackupS3ActionBusy(false);
        return applyBackupS3TestOutcome(out, false);
      });
    });
  }

  // ---- Schedule builder helper (dedicated dialog; the cron text
  // input stays the source of truth — the dialog only writes into the
  // schedule form, never to the server). Canonical 5-field expressions
  // only; exotic-but-valid crons open in Custom mode as raw text.

  var BACKUP_DOW_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday",
    "Thursday", "Friday", "Saturday"];

  function isValidBackupHour(h) {
    return typeof h === "number" && isFinite(h) &&
      Math.floor(h) === h && h >= 0 && h <= 23;
  }

  function isValidBackupMinute(m) {
    return typeof m === "number" && isFinite(m) &&
      Math.floor(m) === m && m >= 0 && m <= 59;
  }

  function isValidBackupDow(d) {
    return typeof d === "number" && isFinite(d) &&
      Math.floor(d) === d && d >= 0 && d <= 6;
  }

  function isValidBackupDom(d) {
    return typeof d === "number" && isFinite(d) &&
      Math.floor(d) === d && d >= 1 && d <= 31;
  }

  // Pure builder. freq is "daily"|"weekly"|"monthly"; parts holds
  // numbers {hour, minute, dow, dom}. Out-of-range never produces a
  // cron — returns "" instead. Numerics stringify without leading zeros.
  function buildBackupCron(freq, parts) {
    var p = parts || {};
    if (!isValidBackupHour(p.hour) || !isValidBackupMinute(p.minute)) return "";
    if (freq === "daily") return p.minute + " " + p.hour + " * * *";
    if (freq === "weekly") {
      if (!isValidBackupDow(p.dow)) return "";
      return p.minute + " " + p.hour + " * * " + p.dow;
    }
    if (freq === "monthly") {
      if (!isValidBackupDom(p.dom)) return "";
      return p.minute + " " + p.hour + " " + p.dom + " * *";
    }
    return "";
  }

  function parseBackupDowToken(tok) {
    var t = String(tok == null ? "" : tok);
    if (/^\d+$/.test(t)) {
      var n = Number(t);
      return isValidBackupDow(n) ? n : -1;
    }
    var names = { SUN: 0, MON: 1, TUE: 2, WED: 3, THU: 4, FRI: 5, SAT: 6 };
    var key = t.replace(/^\s+|\s+$/g, "").toUpperCase();
    return Object.prototype.hasOwnProperty.call(names, key) ? names[key] : -1;
  }

  function parseBackupDomToken(tok) {
    var t = String(tok == null ? "" : tok);
    if (!/^\d+$/.test(t)) return -1;
    var n = Number(t);
    return isValidBackupDom(n) ? n : -1;
  }

  function parseBackupIntField(tok, valid) {
    var t = String(tok == null ? "" : tok);
    if (!/^\d+$/.test(t)) return -1;
    var n = Number(t);
    return valid(n) ? n : -1;
  }

  // Pure parser: canonical daily/weekly/monthly shapes map back onto
  // builder controls; anything else (lists, steps, ranges, names in
  // minute/hour, wrong arity) opens in Custom mode with the raw text.
  function parseBackupCronToControls(cron) {
    var raw = normalizeBackupCron(cron);
    var custom = { mode: "custom", hour: 0, minute: 0, dow: 1, dom: 1, raw: raw };
    if (raw === "") return custom;
    var f = raw.split(/\s+/);
    if (f.length !== 5) return custom;
    var minute = parseBackupIntField(f[0], isValidBackupMinute);
    var hour = parseBackupIntField(f[1], isValidBackupHour);
    if (minute < 0 || hour < 0) return custom;
    if (f[2] === "*" && f[3] === "*" && f[4] === "*") {
      return { mode: "daily", hour: hour, minute: minute, dow: 1, dom: 1, raw: raw };
    }
    var dow = parseBackupDowToken(f[4]);
    if (f[2] === "*" && f[3] === "*" && dow >= 0) {
      return { mode: "weekly", hour: hour, minute: minute, dow: dow, dom: 1, raw: raw };
    }
    var dom = parseBackupDomToken(f[2]);
    if (dom >= 0 && f[3] === "*" && f[4] === "*") {
      return { mode: "monthly", hour: hour, minute: minute, dow: 1, dom: dom, raw: raw };
    }
    return custom;
  }

  function padBackupTime(n) {
    return (n < 10 ? "0" : "") + n;
  }

  // Plain-words readout for the live preview (display only).
  function describeBackupSchedule(controls) {
    var c = controls || {};
    if (c.mode === "weekly" && isValidBackupDow(c.dow) &&
      isValidBackupHour(c.hour) && isValidBackupMinute(c.minute)) {
      return "Weekly on " + BACKUP_DOW_NAMES[c.dow] + " at " +
        padBackupTime(c.hour) + ":" + padBackupTime(c.minute);
    }
    if (c.mode === "monthly" && isValidBackupDom(c.dom) &&
      isValidBackupHour(c.hour) && isValidBackupMinute(c.minute)) {
      return "Monthly on day " + c.dom + " at " +
        padBackupTime(c.hour) + ":" + padBackupTime(c.minute);
    }
    if (c.mode === "daily" &&
      isValidBackupHour(c.hour) && isValidBackupMinute(c.minute)) {
      return "Daily at " + padBackupTime(c.hour) + ":" + padBackupTime(c.minute);
    }
    return "Custom schedule";
  }

  function backupScheduleHelperEls() {
    return {
      dlg: CW.$("backup-schedule-helper-dialog"),
      freq: CW.$("backup-schedule-helper-freq"),
      hour: CW.$("backup-schedule-helper-hour"),
      minute: CW.$("backup-schedule-helper-minute"),
      timeWrap: CW.$("backup-schedule-helper-time-wrap"),
      dowWrap: CW.$("backup-schedule-helper-dow-wrap"),
      dow: CW.$("backup-schedule-helper-dow"),
      domWrap: CW.$("backup-schedule-helper-dom-wrap"),
      dom: CW.$("backup-schedule-helper-dom"),
      keep: CW.$("backup-schedule-helper-keep"),
      customWrap: CW.$("backup-schedule-helper-custom-wrap"),
      custom: CW.$("backup-schedule-helper-custom"),
      preview: CW.$("backup-schedule-helper-preview"),
      error: CW.$("backup-schedule-helper-error"),
      apply: CW.$("backup-schedule-helper-apply")
    };
  }

  function setBackupScheduleHelperError(els, msg) {
    if (!els.error) return;
    if (!msg) {
      els.error.textContent = "";
      els.error.hidden = true;
      if (els.error.classList) els.error.classList.remove("err");
      return;
    }
    els.error.textContent = msg;
    els.error.hidden = false;
    if (els.error.classList) els.error.classList.add("err");
  }

  // Internal time value stays "HH:MM": composed from the hour/minute
  // selects on read, split back on dialog open. Out-of-range or
  // malformed text falls back to the 03:00 default.
  function composeBackupHelperTime(hour, minute) {
    if (!isValidBackupHour(hour) || !isValidBackupMinute(minute)) return "";
    return padBackupTime(hour) + ":" + padBackupTime(minute);
  }

  function splitBackupHelperTime(raw) {
    var m = /^(\d{1,2}):(\d{1,2})$/.exec(normalizeBackupCron(raw));
    if (m) {
      var hour = Number(m[1]);
      var minute = Number(m[2]);
      if (isValidBackupHour(hour) && isValidBackupMinute(minute)) {
        return { hour: hour, minute: minute };
      }
    }
    return { hour: 3, minute: 0 };
  }

  function readBackupScheduleHelper() {
    var els = backupScheduleHelperEls();
    var freq = els.freq && els.freq.value != null ? String(els.freq.value) : "daily";
    if (freq !== "daily" && freq !== "weekly" && freq !== "monthly" && freq !== "custom") {
      freq = "daily";
    }
    var hour = els.hour && els.hour.value != null && els.hour.value !== "" ? Number(els.hour.value) : NaN;
    var minute = els.minute && els.minute.value != null && els.minute.value !== "" ? Number(els.minute.value) : NaN;
    var dow = els.dow && els.dow.value != null && els.dow.value !== "" ? Number(els.dow.value) : NaN;
    var dom = els.dom && els.dom.value != null && els.dom.value !== "" ? Number(els.dom.value) : NaN;
    return {
      freq: freq,
      hour: hour,
      minute: minute,
      dow: dow,
      dom: dom,
      keep: parseBackupKeep(els.keep && els.keep.value != null ? els.keep.value : ""),
      custom: els.custom && els.custom.value != null ? String(els.custom.value) : ""
    };
  }

  // Live preview + conditional day rows. textContent only.
  function refreshBackupScheduleHelper() {
    var els = backupScheduleHelperEls();
    var st = readBackupScheduleHelper();
    function hide(node, yes) { if (node) node.hidden = !!yes; }
    hide(els.dowWrap, st.freq !== "weekly");
    hide(els.domWrap, st.freq !== "monthly");
    hide(els.timeWrap, st.freq === "custom");
    hide(els.customWrap, st.freq !== "custom");
    var cron = "";
    if (st.freq !== "custom") {
      cron = buildBackupCron(st.freq, st);
    }
    if (els.preview) {
      if (st.freq === "custom") {
        els.preview.textContent = normalizeBackupCron(st.custom) +
          " · Custom schedule — the visual controls don't cover this expression.";
      } else if (cron === "") {
        els.preview.textContent = "Enter a valid time and day.";
      } else {
        els.preview.textContent = cron + " · " +
          describeBackupSchedule({ mode: st.freq, hour: st.hour, minute: st.minute, dow: st.dow, dom: st.dom });
      }
    }
    setBackupScheduleHelperError(els, null);
    if (els.apply) els.apply.disabled = (st.freq !== "custom" && cron === "");
  }

  function closeBackupScheduleHelper() {
    var els = backupScheduleHelperEls();
    try {
      if (els.dlg && els.dlg.open) els.dlg.close();
      else if (els.dlg && els.dlg.removeAttribute) els.dlg.removeAttribute("open");
    } catch (e) { /* already closed */ }
  }

  function openBackupScheduleHelper() {
    var els = backupScheduleHelperEls();
    if (!els.dlg) return;
    var cronEl = CW.$("backup-schedule-cron");
    var keepEl = CW.$("backup-schedule-keep");
    var parsed = parseBackupCronToControls(cronEl && cronEl.value != null ? cronEl.value : "");
    function setVal(node, val) { if (node) node.value = val; }
    if (els.freq) els.freq.value = parsed.mode;
    var t = splitBackupHelperTime(parsed.mode === "custom" ? "" :
      (parsed.hour + ":" + parsed.minute));
    if (els.hour) els.hour.value = String(t.hour);
    if (els.minute) els.minute.value = String(t.minute);
    if (els.dow) els.dow.value = String(parsed.dow);
    if (els.dom) els.dom.value = String(parsed.dom);
    if (els.keep) els.keep.value = (keepEl && keepEl.value != null) ? keepEl.value : "";
    if (els.custom) els.custom.value = parsed.mode === "custom" ? parsed.raw : "";
    refreshBackupScheduleHelper();
    try {
      if (typeof els.dlg.showModal === "function") els.dlg.showModal();
      else if (els.dlg.setAttribute) els.dlg.setAttribute("open", "");
    } catch (e) {
      try { if (els.dlg.setAttribute) els.dlg.setAttribute("open", ""); } catch (e2) { /* ignore */ }
    }
    try {
      var focus = parsed.mode === "custom" && els.custom ? els.custom : (els.freq || els.hour);
      if (focus && focus.focus) focus.focus();
    } catch (e3) { /* ignore */ }
  }

  // Fill-only: writes into the schedule form + marks it dirty for the
  // existing Save flow. Never saves to the server.
  function applyBackupScheduleHelper() {
    var els = backupScheduleHelperEls();
    var st = readBackupScheduleHelper();
    var cron = "";
    if (st.freq === "custom") {
      cron = normalizeBackupCron(st.custom);
    } else {
      cron = buildBackupCron(st.freq, st);
      if (cron === "") {
        setBackupScheduleHelperError(els, "Enter a valid time and day.");
        return;
      }
    }
    var bad = validateBackupSchedule(cron, st.keep);
    if (bad !== "") {
      setBackupScheduleHelperError(els, bad);
      return;
    }
    var cronEl = CW.$("backup-schedule-cron");
    var keepEl = CW.$("backup-schedule-keep");
    if (cronEl) cronEl.value = cron;
    if (keepEl) keepEl.value = isValidBackupKeep(st.keep) ? String(st.keep) : "";
    refreshBackupScheduleCronHint();
    if (CW.markFormDirty) CW.markFormDirty("backup-schedule-form");
    closeBackupScheduleHelper();
  }

  // ---- Auto backup schedule (PocketBase native settings: backups.cron
  // + backups.cronMaxKeep). Empty cron disables; a non-empty cron is
  // parsed server-side (400 on bad expressions) and requires keep >= 1.
  // Save sends PATCH /api/settings with ONLY the backups cron subtree.

  function normalizeBackupCron(raw) {
    return String(raw == null ? "" : raw).replace(/^\s+|\s+$/g, "");
  }

  function parseBackupKeep(raw) {
    var s = String(raw == null ? "" : raw).replace(/^\s+|\s+$/g, "");
    if (s === "") return 0;
    return Number(s);
  }

  function isValidBackupKeep(n) {
    return typeof n === "number" && isFinite(n) &&
      Math.floor(n) === n && n >= 1;
  }

  function parseBackupSchedule(data) {
    var b = (data && data.backups) || {};
    var keep = Number(b.cronMaxKeep);
    if (!isFinite(keep) || keep < 0) keep = 0;
    return {
      cron: normalizeBackupCron(b.cron),
      cronMaxKeep: Math.floor(keep)
    };
  }

  function formatBackupScheduleStatus(sched) {
    var s = sched || {};
    var cron = normalizeBackupCron(s.cron);
    if (cron === "") return "Automatic backups disabled.";
    if (!isValidBackupKeep(s.cronMaxKeep)) return "Runs " + cron + ".";
    return "Runs " + cron + " · keeps last " + s.cronMaxKeep + ".";
  }

  // Pure PATCH-body builder. values mirrors the form fields; keep is
  // coerced to an integer >= 0 (0 with an empty cron disables).
  function buildBackupSchedulePatch(values) {
    var v = values || {};
    var keep = Number(v.keep);
    if (!isFinite(keep) || keep < 0) keep = 0;
    return {
      backups: {
        cron: normalizeBackupCron(v.cron),
        cronMaxKeep: Math.floor(keep)
      }
    };
  }

  // Client gate: a set schedule needs keep >= 1 (the server enforces
  // the same rule with a 400). Returns "" when the pair may be sent.
  function validateBackupSchedule(cron, keep) {
    if (normalizeBackupCron(cron) !== "" && !isValidBackupKeep(keep)) {
      return "Keep must be at least 1 when a schedule is set.";
    }
    return "";
  }

  function backupScheduleStatus(msg, ok) {
    var el = CW.$("backup-schedule-result");
    if (el) el.textContent = msg == null ? "" : String(msg);
    if (msg) CW.toast(msg, ok);
  }

  function readBackupScheduleForm() {
    var cronEl = CW.$("backup-schedule-cron");
    var keepEl = CW.$("backup-schedule-keep");
    return {
      cron: cronEl && cronEl.value != null ? String(cronEl.value) : "",
      keep: parseBackupKeep(keepEl && keepEl.value != null ? keepEl.value : "")
    };
  }

  // textContent only — server strings never touch innerHTML. Prefills
  // the inputs with the stored values (empty = placeholders show).
  function renderBackupSchedule(sched) {
    var v = sched || {};
    var status = CW.$("backup-schedule-status");
    if (status) status.textContent = formatBackupScheduleStatus(v);
    var cronEl = CW.$("backup-schedule-cron");
    if (cronEl) cronEl.value = normalizeBackupCron(v.cron);
    var keepEl = CW.$("backup-schedule-keep");
    if (keepEl) keepEl.value = isValidBackupKeep(v.cronMaxKeep) ? String(v.cronMaxKeep) : "";
    refreshBackupScheduleCronHint();
  }

  // Plain-words hint under the cron field, composed from the existing
  // pure helpers. Empty cron reads blank (the card status line covers
  // the disabled state); custom shapes read "Custom schedule".
  function updateBackupScheduleCronHint(value) {
    var cron = normalizeBackupCron(value);
    var text = cron === "" ? "" :
      describeBackupSchedule(parseBackupCronToControls(cron));
    var hint = CW.$("backup-schedule-cron-hint");
    if (hint) hint.textContent = text;
    return text;
  }

  function refreshBackupScheduleCronHint() {
    var cronEl = CW.$("backup-schedule-cron");
    return updateBackupScheduleCronHint(cronEl && cronEl.value != null ? cronEl.value : "");
  }

  function loadBackupSchedule() {
    if (CW.armDirtyForm) CW.armDirtyForm("backup-schedule-form");
    return CW.api("/api/settings").then(function (data) {
      var sched = parseBackupSchedule(data);
      CW.state.backupSchedule = sched;
      renderBackupSchedule(sched);
      return sched;
    }, function (err) {
      backupScheduleStatus("Schedule unavailable: " + ((err && err.message) || "load failed"));
      throw err;
    });
  }

  function saveBackupSchedule(ev) {
    if (ev) ev.preventDefault();
    var form = readBackupScheduleForm();
    var cron = normalizeBackupCron(form.cron);
    var bad = validateBackupSchedule(cron, form.keep);
    if (bad !== "") {
      backupScheduleStatus(bad);
      return Promise.resolve();
    }
    var body = buildBackupSchedulePatch({ cron: cron, keep: form.keep });
    return CW.apiMut("PATCH", "/api/settings", body).then(function (out) {
      if (out.status === 200) {
        var sched = parseBackupSchedule(out.data);
        CW.state.backupSchedule = sched;
        renderBackupSchedule(sched);
        backupScheduleStatus("");
        CW.toast("Schedule saved.", true);
        if (CW.markFormClean) CW.markFormClean("backup-schedule-form");
        return out;
      }
      backupScheduleStatus(CW.apiError(out, "Schedule save failed"));
      return out;
    });
  }

  CW.loadBackups = loadBackups;
  CW.renderBackups = renderBackups;
  CW.createBackup = createBackup;
  CW.deleteBackup = deleteBackup;
  CW.restoreBackup = restoreBackup;
  CW.downloadBackup = downloadBackup;
  CW.uploadBackup = uploadBackup;
  CW.triggerBackupUpload = triggerBackupUpload;
  CW.backupUploadStatus = backupUploadStatus;
  CW.validateBackupUploadFile = validateBackupUploadFile;
  CW.onBackupListClick = onBackupListClick;
  // Pure helpers exported for js_tests (limits.js export precedent):
  // dependency-free, no DOM/fetch.
  CW.BACKUP_NAME_PATTERN = BACKUP_NAME_PATTERN;
  CW.isValidBackupName = isValidBackupName;
  CW.isValidBackupUploadName = isValidBackupUploadName;
  CW.normalizeBackupName = normalizeBackupName;
  CW.encodeBackupKey = encodeBackupKey;
  CW.backupDownloadPath = backupDownloadPath;
  CW.formatBackupSize = formatBackupSize;
  CW.formatBackupDate = formatBackupDate;
  CW.isBackupBusy = isBackupBusy;
  CW.backupListErrorMessage = backupListErrorMessage;
  CW.backupS3TestMessage = backupS3TestMessage;
  CW.loadBackupS3 = loadBackupS3;
  CW.autoVerifyBackupS3 = autoVerifyBackupS3;
  CW.renderBackupS3 = renderBackupS3;
  CW.saveBackupS3 = saveBackupS3;
  CW.testBackupS3 = testBackupS3;
  CW.parseBackupS3 = parseBackupS3;
  CW.buildBackupS3Patch = buildBackupS3Patch;
  CW.toggleBackupS3Secret = toggleBackupS3Secret;
  CW.backupS3Step = backupS3Step;
  CW.onBackupS3Switch = onBackupS3Switch;
  CW.onBackupS3Action = onBackupS3Action;
  CW.revertBackupS3Check = revertBackupS3Check;
  CW.loadBackupSchedule = loadBackupSchedule;
  CW.renderBackupSchedule = renderBackupSchedule;
  CW.saveBackupSchedule = saveBackupSchedule;
  CW.parseBackupSchedule = parseBackupSchedule;
  CW.normalizeBackupCron = normalizeBackupCron;
  CW.parseBackupKeep = parseBackupKeep;
  CW.isValidBackupKeep = isValidBackupKeep;
  CW.buildBackupSchedulePatch = buildBackupSchedulePatch;
  CW.validateBackupSchedule = validateBackupSchedule;
  CW.formatBackupScheduleStatus = formatBackupScheduleStatus;
  CW.isValidBackupHour = isValidBackupHour;
  CW.isValidBackupMinute = isValidBackupMinute;
  CW.isValidBackupDow = isValidBackupDow;
  CW.isValidBackupDom = isValidBackupDom;
  CW.buildBackupCron = buildBackupCron;
  CW.parseBackupCronToControls = parseBackupCronToControls;
  CW.describeBackupSchedule = describeBackupSchedule;
  CW.composeBackupHelperTime = composeBackupHelperTime;
  CW.splitBackupHelperTime = splitBackupHelperTime;
  CW.openBackupScheduleHelper = openBackupScheduleHelper;
  CW.closeBackupScheduleHelper = closeBackupScheduleHelper;
  CW.applyBackupScheduleHelper = applyBackupScheduleHelper;
  CW.refreshBackupScheduleHelper = refreshBackupScheduleHelper;
  CW.updateBackupScheduleCronHint = updateBackupScheduleCronHint;
  CW.refreshBackupScheduleCronHint = refreshBackupScheduleCronHint;
})();
