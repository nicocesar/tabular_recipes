// Filing a card or asking for a translation takes a few seconds; say so
// and take the button away, so a second click can't start it twice (the
// server dedupes too). Any form whose button has data-busy gets this.
(function () {
  var buttons = document.querySelectorAll("form button[data-busy]");
  Array.prototype.forEach.call(buttons, function (btn) {
    btn.form.addEventListener("submit", function () {
      // Disable after the submit is under way: a disabled button at
      // submit time would drop out of the form data.
      setTimeout(function () {
        btn.disabled = true;
        var lbl = btn.querySelector(".lbl") || btn; // keep the icon
        btn.dataset.idle = lbl.textContent;
        lbl.textContent = btn.dataset.busy;
      }, 0);
    });
  });
  // Back/forward cache restores disabled buttons; re-enable them.
  window.addEventListener("pageshow", function () {
    Array.prototype.forEach.call(buttons, function (btn) {
      btn.disabled = false;
      if (btn.dataset.idle) (btn.querySelector(".lbl") || btn).textContent = btn.dataset.idle;
    });
  });
})();
