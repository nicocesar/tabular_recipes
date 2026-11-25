// Filing takes a few seconds; say so and take the button away, so a
// second click can't file the card twice (the server dedupes too).
(function () {
  var form = document.querySelector("form.newcard");
  if (!form) return;
  form.addEventListener("submit", function () {
    var btn = form.querySelector("button[type=submit]");
    // Disable after the submit is under way: a disabled button at
    // submit time would drop out of the form data.
    setTimeout(function () {
      btn.disabled = true;
      btn.textContent = "Grampa is typing…";
    }, 0);
  });
  // Back/forward cache restores the disabled button; re-enable it.
  window.addEventListener("pageshow", function () {
    var btn = form.querySelector("button[type=submit]");
    btn.disabled = false;
    btn.textContent = "Type it up";
  });
})();
