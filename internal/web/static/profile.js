// Progressive enhancement for the profile check-in filters. The form works
// without this file; the time presets stay hidden until their dates can be
// calculated in the browser.
(function () {
  var form = document.querySelector("[data-profile-filters]");
  if (!form) return;
  var from = form.querySelector('input[name="from"]');
  var to = form.querySelector('input[name="to"]');
  var presets = form.querySelector("[data-profile-presets]");
  if (!from || !to || !presets) return;

  function iso(date) {
    return (
      date.getFullYear() +
      "-" +
      String(date.getMonth() + 1).padStart(2, "0") +
      "-" +
      String(date.getDate()).padStart(2, "0")
    );
  }

  presets.hidden = false;
  presets.addEventListener("click", function (event) {
    var button = event.target.closest("[data-profile-range]");
    if (!button) return;

    var now = new Date();
    var range = button.getAttribute("data-profile-range");
    if (range === "all") {
      from.value = "";
      to.value = "";
    } else if (range === "year") {
      from.value = now.getFullYear() + "-01-01";
      to.value = iso(now);
    } else {
      var days = parseInt(range, 10);
      if (!days || days < 1) return;
      var start = new Date(
        now.getFullYear(),
        now.getMonth(),
        now.getDate() - (days - 1)
      );
      from.value = iso(start);
      to.value = iso(now);
    }

    if (form.requestSubmit) form.requestSubmit();
    else form.submit();
  });
})();
