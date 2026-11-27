(function () {
  "use strict";

  const select = document.getElementById("hop-context-select");
  if (!select) return;

  const current = select.value;
  select.addEventListener("change", function () {
    const name = select.value;
    select.disabled = true;
    select.setAttribute("aria-busy", "true");
    fetch("/__hop/context", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ name: name })
    }).then(function (response) {
      if (!response.ok) throw new Error("context switch failed");
      window.location.reload();
    }).catch(function (error) {
      select.value = current;
      select.disabled = false;
      select.removeAttribute("aria-busy");
      if (window.hop && typeof window.hop.toast === "function") {
        window.hop.toast("Could not switch hop context", true);
      } else {
        console.error(error);
      }
    });
  });
})();
