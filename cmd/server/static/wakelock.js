// Keep screen on: the one kitchen feature that needs JS (rule 2).
// Without the Wake Lock API the button stays hidden and nothing changes.
(function () {
  var btn = document.getElementById("wake");
  if (!btn || !("wakeLock" in navigator)) return;
  btn.hidden = false;
  var lock = null, wanted = false;

  function set(on) {
    btn.setAttribute("aria-pressed", on ? "true" : "false");
    btn.textContent = on ? btn.dataset.on : btn.dataset.off;
  }
  async function acquire() {
    try {
      lock = await navigator.wakeLock.request("screen");
      lock.addEventListener("release", function () { lock = null; if (!wanted) set(false); });
      set(true);
    } catch (e) { wanted = false; set(false); }
  }
  btn.addEventListener("click", function () {
    wanted = !wanted;
    if (wanted) acquire();
    else if (lock) { lock.release(); lock = null; set(false); }
  });
  // Browsers drop the lock when the tab is hidden; take it back on return.
  document.addEventListener("visibilitychange", function () {
    if (wanted && document.visibilityState === "visible" && !lock) acquire();
  });
})();
