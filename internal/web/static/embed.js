(function () {
  "use strict";

  const params = new URLSearchParams(window.location.search);
  if (params.get("embed") !== "1") return;

  document.documentElement.classList.add("embed-mode");
  document.documentElement.setAttribute("data-embed", "1");
  window.hopEmbed = true;

  function isEmbedded(href) {
    try {
      const url = new URL(href, window.location.href);
      return url.origin === window.location.origin && url.pathname === window.location.pathname && url.searchParams.get("embed") === "1";
    } catch (_) {
      return false;
    }
  }

  function beforeEachNavigation(url) {
    if (url.origin !== window.location.origin) return false;
    if (url.protocol !== "http:" && url.protocol !== "https:") return false;
    if (url.pathname.indexOf("/static/") === 0) return false;
    if (url.pathname.indexOf("/api/") === 0) return false;
    if (url.pathname === "/healthz" || url.pathname === "/favicon.ico" || url.pathname === "/favicon.svg") return false;
    url.searchParams.set("embed", "1");
    return true;
  }

  document.addEventListener("click", function (event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const link = event.target && event.target.closest ? event.target.closest("a[href]") : null;
    if (!link || link.target === "_blank" || link.hasAttribute("download")) return;
    let url;
    try {
      url = new URL(link.getAttribute("href"), window.location.href);
    } catch (_) {
      return;
    }
    if (!beforeEachNavigation(url)) return;
    event.preventDefault();
    window.location.href = url.pathname + url.search + url.hash;
  }, true);

  document.addEventListener("submit", function (event) {
    const form = event.target;
    if (!form || !form.getAttribute) return;
    const method = String(form.getAttribute("method") || "get").toLowerCase();
    if (method !== "get") return;
    if (form.querySelector('input[name="embed"]')) return;
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = "embed";
    input.value = "1";
    form.appendChild(input);
  }, true);

  window.addEventListener("pageshow", function () {
    if (!isEmbedded(window.location.href)) {
      const url = new URL(window.location.href);
      if (beforeEachNavigation(url)) {
        window.location.replace(url.pathname + url.search + url.hash);
      }
    }
  });
})();
