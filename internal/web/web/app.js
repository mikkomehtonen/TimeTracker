(function () {
  "use strict";

  function pad(n) {
    return String(n).padStart(2, "0");
  }

  function localWall(iso) {
    var d = new Date(iso);
    if (isNaN(d)) {
      return null;
    }
    return (
      d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate()) +
      "T" + pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds())
    );
  }

  // Rewrite each entry edit field to browser-local wall clock and record the
  // matching offset in the hidden input paired via data-offset. The offset is
  // derived from the same instant being rendered, so it is DST-correct per
  // field: editing an entry from the other side of a transition keeps every
  // stored instant intact.
  function applyTimezone() {
    document
      .querySelectorAll('input[type="datetime-local"][data-utc]')
      .forEach(function (input) {
        var iso = input.getAttribute("data-utc");
        var d = new Date(iso);
        if (isNaN(d)) {
          return;
        }
        var target = document.getElementById(input.getAttribute("data-offset"));
        if (!target) {
          // Without the paired offset field, rewriting only the wall value
          // would silently desynchronize the pair; leave the field alone.
          return;
        }
        input.value = localWall(iso);
        target.value = String(-d.getTimezoneOffset());
      });
  }

  function localizeTimes() {
    document.querySelectorAll("time.localizable").forEach(function (el) {
      var d = new Date(el.getAttribute("datetime"));
      if (!isNaN(d)) {
        el.textContent = d.toLocaleString(undefined, {
          dateStyle: "medium",
          timeStyle: "short"
        });
      }
    });
  }

  function tickElapsed() {
    var el = document.getElementById("elapsed");
    if (!el) {
      return;
    }
    var start = new Date(el.getAttribute("data-start"));
    if (isNaN(start)) {
      return;
    }
    function fmt(ms) {
      var total = Math.max(0, Math.floor(ms / 1000));
      var h = Math.floor(total / 3600);
      var m = Math.floor((total % 3600) / 60);
      var s = total % 60;
      return h > 0 ? h + ":" + pad(m) + ":" + pad(s) : m + ":" + pad(s);
    }
    function update() {
      el.textContent = fmt(Date.now() - start.getTime());
    }
    update();
    setInterval(update, 1000);
  }

  applyTimezone();
  localizeTimes();
  tickElapsed();
})();
