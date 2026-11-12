/*
 * Handheld helpers, ES5 only so it also runs on IE11-era terminals.
 * - Enter in a field with data-next moves to that field instead of
 *   submitting, so a scanned bin code lands in the bin box and the cursor
 *   jumps to the product box.
 * - F1…F4 press the element marked data-key; digits 1…9 open data-digit
 *   items when the cursor is not in a text field.
 * - An error banner beeps (and vibrates where supported).
 */
(function () {
  "use strict";

  function q(sel) {
    return document.querySelector(sel);
  }

  function isField(el) {
    if (!el || !el.tagName) return false;
    var t = el.tagName.toLowerCase();
    return t === "input" || t === "textarea" || t === "select";
  }

  function submitForm(id) {
    var f = document.getElementById(id);
    if (!f) return;
    var msg = f.getAttribute("data-confirm");
    if (msg && !window.confirm(msg)) return;
    f.submit();
  }

  document.addEventListener("keydown", function (e) {
    var code = e.keyCode || e.which;

    if (code >= 112 && code <= 115) {
      var el = q('[data-key="F' + (code - 111) + '"]');
      if (el) {
        e.preventDefault();
        el.click();
      }
      return;
    }

    if (code === 13) {
      var t = e.target;
      var next = t && t.getAttribute ? t.getAttribute("data-next") : null;
      if (next) {
        var n = document.getElementById(next);
        // Only jump if the next field is empty; otherwise submit as usual.
        if (n && !n.value) {
          e.preventDefault();
          n.focus();
          if (n.select) n.select();
        }
      }
      return;
    }

    if (code >= 49 && code <= 57 && !isField(e.target)) {
      var item = q('[data-digit="' + (code - 48) + '"]');
      if (item) {
        e.preventDefault();
        item.click();
      }
    }
  });

  document.addEventListener("click", function (e) {
    var t = e.target;
    while (t && t !== document) {
      if (t.getAttribute && t.getAttribute("data-submit")) {
        e.preventDefault();
        submitForm(t.getAttribute("data-submit"));
        return;
      }
      t = t.parentNode;
    }
  });

  // Ask before submitting forms that carry data-confirm.
  document.addEventListener("submit", function (e) {
    var f = e.target;
    var msg = f.getAttribute && f.getAttribute("data-confirm");
    if (msg && !f.getAttribute("data-confirmed")) {
      if (!window.confirm(msg)) {
        e.preventDefault();
        return;
      }
    }
    // Block double submits from a bouncing trigger.
    if (f.getAttribute("data-sent")) {
      e.preventDefault();
      return;
    }
    f.setAttribute("data-sent", "1");
  });

  function beep() {
    try {
      if (navigator.vibrate) navigator.vibrate([120, 60, 120]);
      var AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return;
      var ctx = new AC();
      var osc = ctx.createOscillator();
      var gain = ctx.createGain();
      osc.type = "square";
      osc.frequency.value = 880;
      gain.gain.value = 0.15;
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start();
      osc.stop(ctx.currentTime + 0.25);
    } catch (err) {
      /* no audio on this device */
    }
  }

  function ready() {
    if (q('[data-result="error"]')) beep();
    // Older browsers ignore the autofocus attribute.
    var af = q("[autofocus]");
    if (af && document.activeElement !== af) af.focus();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", ready);
  } else {
    ready();
  }
})();
