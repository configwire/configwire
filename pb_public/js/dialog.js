/* ConfigWire admin shell — dialog.js (themed alert/confirm/prompt).
 * Replaces native window.alert/confirm/prompt with a single reusable
 * <dialog id="cw-dialog"> element. Promise-based API on window.CW.
 */
(function () {
  "use strict";

  var CW = window.CW = window.CW || {};

  var DIALOG_ID = "cw-dialog";
  var TITLE_ID = "cw-dialog-title";
  var MESSAGE_ID = "cw-dialog-message";
  var INPUT_ID = "cw-dialog-input";
  var ERROR_ID = "cw-dialog-error";
  var OK_ID = "cw-dialog-ok";
  var CANCEL_ID = "cw-dialog-cancel";

  var current = null; // {resolve, reject, mode, opts, lastFocus}
  var wired = false;

  function $(id) {
    if (CW.$) return CW.$(id);
    return document.getElementById(id);
  }

  function esc(s) {
    if (CW.esc) return CW.esc(s);
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;")
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  function ensureMarkup() {
    var dlg = document.getElementById(DIALOG_ID);
    if (dlg) return dlg;
    dlg = document.createElement("dialog");
    dlg.setAttribute("id", DIALOG_ID);
    dlg.setAttribute("aria-labelledby", TITLE_ID);
    dlg.setAttribute("role", "alertdialog");
    // Static text only — dynamic message/title set via textContent (XSS-safe).
    dlg.innerHTML =
      '<h3 id="' + TITLE_ID + '"></h3>' +
      '<p id="' + MESSAGE_ID + '"></p>' +
      '<input id="' + INPUT_ID + '" type="text" hidden>' +
      '<p id="' + ERROR_ID + '" class="json-hint" role="status" hidden></p>' +
      '<div class="dialog-actions">' +
      '<span class="dialog-spacer"></span>' +
      '<button type="button" id="' + CANCEL_ID + '" class="btn ghost">Cancel</button>' +
      '<button type="button" id="' + OK_ID + '" class="btn primary">OK</button>' +
      "</div>";
    document.body.appendChild(dlg);
    wireOnce(dlg);
    return dlg;
  }

  function els() {
    ensureMarkup();
    return {
      dlg: document.getElementById(DIALOG_ID),
      title: document.getElementById(TITLE_ID),
      message: document.getElementById(MESSAGE_ID),
      input: document.getElementById(INPUT_ID),
      error: document.getElementById(ERROR_ID),
      ok: document.getElementById(OK_ID),
      cancel: document.getElementById(CANCEL_ID)
    };
  }

  function setError(refs, msg) {
    if (!refs.error) return;
    if (!msg) {
      refs.error.textContent = "";
      refs.error.hidden = true;
      refs.error.classList.remove("err");
      return;
    }
    refs.error.textContent = msg;
    refs.error.hidden = false;
    refs.error.classList.add("err");
  }

  function validate(refs, opts) {
    var val = refs.input ? refs.input.value : "";
    if (opts.required && val.trim() === "") {
      setError(refs, "This field is required.");
      return false;
    }
    if (opts.minLength && val.length < opts.minLength) {
      setError(refs, "Enter at least " + opts.minLength + " characters.");
      return false;
    }
    setError(refs, null);
    return true;
  }

  function finish(outcome) {
    // outcome: {ok:boolean, value:string|null}
    var refs = els();
    var st = current;
    current = null;
    try {
      if (refs.dlg && refs.dlg.open) refs.dlg.close();
    } catch (e) { /* already closed */ }
    if (!st) return;
    if (st.lastFocus && st.lastFocus.focus) {
      try { st.lastFocus.focus(); } catch (e) { /* ignore */ }
    }
    if (st.mode === "prompt") {
      if (!outcome.ok) st.resolve(null);
      else st.resolve(outcome.value);
    } else if (st.mode === "confirm") {
      st.resolve(!!outcome.ok);
    } else {
      st.resolve();
    }
  }

  function openDialog(mode, message, defaultValue, opts) {
    opts = opts || {};
    var refs = els();
    // Cancel any pending dialog: resolve it as dismissed first.
    if (current) {
      var pending = current;
      current = null;
      try {
        if (refs.dlg && refs.dlg.open) refs.dlg.close();
      } catch (e) { /* ignore */ }
      if (pending.mode === "prompt") pending.resolve(null);
      else if (pending.mode === "confirm") pending.resolve(false);
      else pending.resolve();
    }
    return new Promise(function (resolve) {
      var title = opts.title || (mode === "prompt" ? "Input" : mode === "confirm" ? "Confirm" : "Notice");
      var okText = opts.okText || (mode === "alert" ? "OK" : mode === "confirm" ? "Confirm" : "Save");
      var cancelText = opts.cancelText || "Cancel";

      current = {
        resolve: resolve,
        mode: mode,
        opts: opts,
        lastFocus: document.activeElement
      };

      // XSS-safe: textContent only, never innerHTML with message.
      refs.title.textContent = String(title);
      refs.message.textContent = String(message == null ? "" : message);
      refs.message.setAttribute("id", MESSAGE_ID);
      refs.dlg.setAttribute("aria-labelledby", TITLE_ID);
      refs.dlg.setAttribute("aria-describedby", MESSAGE_ID);
      refs.dlg.setAttribute("role", "alertdialog");

      refs.ok.textContent = String(okText);
      refs.ok.className = opts.danger ? "btn danger" : "btn primary";

      var needsCancel = mode !== "alert";
      refs.cancel.hidden = !needsCancel;
      refs.cancel.textContent = String(cancelText);

      setError(refs, null);

      if (mode === "prompt") {
        refs.input.hidden = false;
        refs.input.type = opts.inputType || "text";
        if (opts.placeholder != null) refs.input.setAttribute("placeholder", String(opts.placeholder));
        else refs.input.removeAttribute("placeholder");
        refs.input.value = defaultValue == null ? "" : String(defaultValue);
      } else {
        refs.input.hidden = true;
        refs.input.value = "";
        refs.input.removeAttribute("placeholder");
      }

      wireOnce(refs.dlg);

      if (typeof refs.dlg.showModal === "function") refs.dlg.showModal();
      else refs.dlg.setAttribute("open", "");

      if (mode === "prompt") {
        try {
          refs.input.focus();
          refs.input.select();
        } catch (e) {
          try { refs.input.focus(); } catch (e2) { /* ignore */ }
        }
      } else {
        try { refs.ok.focus(); } catch (e) { /* ignore */ }
      }
    });
  }

  function onOk() {
    if (!current) return;
    var refs = els();
    if (current.mode === "prompt") {
      if (!validate(refs, current.opts || {})) {
        try { refs.input.focus(); } catch (e) { /* ignore */ }
        return;
      }
      finish({ ok: true, value: refs.input.value });
    } else {
      finish({ ok: true });
    }
  }

  function onCancel() {
    if (!current) return;
    finish({ ok: false });
  }

  function wireOnce(dlg) {
    if (wired) return;
    wired = true;
    var refs = els();
    dlg = refs.dlg;

    if (refs.ok && refs.ok.addEventListener) {
      refs.ok.addEventListener("click", onOk);
    }
    if (refs.cancel && refs.cancel.addEventListener) {
      refs.cancel.addEventListener("click", onCancel);
    }
    // Escape cancels (native cancel event).
    dlg.addEventListener("cancel", function (ev) {
      ev.preventDefault();
      onCancel();
    });
    // Backdrop click cancels: click target === dialog element.
    dlg.addEventListener("click", function (ev) {
      if (ev.target === dlg) onCancel();
    });
    // Enter submits prompt; live-clear validation error while typing.
    var input = refs.input;
    if (input && input.addEventListener) {
      input.addEventListener("keydown", function (ev) {
        if (ev.key === "Enter") {
          ev.preventDefault();
          onOk();
        } else if (ev.key === "Escape") {
          ev.preventDefault();
          ev.stopPropagation();
          onCancel();
        }
      });
      input.addEventListener("input", function () {
        if (!current) return;
        var r = els();
        if (!r.error.hidden) validate(r, current.opts || {});
      });
    }
    // Ensure lazily-built JS path also wires input even if static markup
    // existed first: re-fetch after ensureMarkup is idempotent.
    void dlg;
  }

  function confirmDialog(message, opts) {
    return openDialog("confirm", message, null, opts);
  }

  function promptDialog(message, defaultValue, opts) {
    // Support promptDialog(message, opts) overload.
    if (defaultValue != null && typeof defaultValue === "object") {
      opts = defaultValue;
      defaultValue = "";
    }
    return openDialog("prompt", message, defaultValue == null ? "" : defaultValue, opts);
  }

  function alertDialog(message, opts) {
    return openDialog("alert", message, null, opts);
  }

  // Wire static markup on load (lazy path covers missing markup too).
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () {
      ensureMarkup();
    });
  } else {
    ensureMarkup();
  }

  CW.confirmDialog = confirmDialog;
  CW.promptDialog = promptDialog;
  CW.alertDialog = alertDialog;
  CW.confirm = confirmDialog;
  CW.prompt = promptDialog;
  CW.alert = alertDialog;
})();
