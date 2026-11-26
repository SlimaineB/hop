(function () {
  "use strict";

  let toastTimer = null;

  function toast(message, isError) {
    const element = document.getElementById("toast");
    if (!element) return;
    element.textContent = message;
    element.classList.toggle("error", Boolean(isError));
    element.hidden = false;
    requestAnimationFrame(function () {
      element.classList.add("show");
    });
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(function () {
      element.classList.remove("show");
      setTimeout(function () {
        element.hidden = true;
      }, 180);
    }, 2200);
  }

  async function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      try {
        await navigator.clipboard.writeText(text);
        return true;
      } catch (_) {
        // fall through to the legacy path
      }
    }
    try {
      const area = document.createElement("textarea");
      area.value = text;
      area.setAttribute("readonly", "");
      area.style.position = "fixed";
      area.style.opacity = "0";
      document.body.appendChild(area);
      area.select();
      const ok = document.execCommand("copy");
      document.body.removeChild(area);
      return ok;
    } catch (_) {
      return false;
    }
  }

  document.addEventListener("click", function (event) {
    const button = event.target.closest("[data-copy]");
    if (!button) return;
    event.preventDefault();
    const text = button.getAttribute("data-copy") || "";
    copyText(text).then(function (ok) {
      toast(ok ? "Copied to clipboard" : "Copy failed", !ok);
    });
  });

  function escapeHTML(value) {
    return String(value == null ? "" : value).replace(/[&<>"']/g, function (character) {
      switch (character) {
        case "&": return "&amp;";
        case "<": return "&lt;";
        case ">": return "&gt;";
        case '"': return "&quot;";
        case "'": return "&#39;";
        default: return character;
      }
    });
  }

  function safeHref(value) {
    if (!value) return "";
    try {
      const url = new URL(value, window.location.origin);
      if (url.protocol === "http:" || url.protocol === "https:") {
        return url.href;
      }
    } catch (_) {
      return "";
    }
    return "";
  }

  function fmtNum(value) {
    return Number(value || 0).toLocaleString("en-US");
  }

  function fmtAge(iso) {
    if (!iso) return "—";
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) return "—";
    let seconds = Math.max(0, Math.floor((Date.now() - date.getTime()) / 1000));
    if (seconds < 60) return seconds + "s";
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return minutes + "m";
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return hours + "h";
    const days = Math.floor(hours / 24);
    if (days < 30) return days + "d";
    const months = Math.floor(days / 30);
    if (months < 12) return months + "mo";
    return Math.floor(months / 12) + "y";
  }

  function fmtDuration(ms) {
    const value = Number(ms || 0);
    if (value < 1) return "<1ms";
    if (value < 1000) return Math.round(value) + "ms";
    if (value < 60000) return (value / 1000).toFixed(2) + "s";
    return (value / 60000).toFixed(1) + "min";
  }

  function shellQuote(value) {
    const text = String(value == null ? "" : value);
    if (text === "") return "''";
    if (/^[A-Za-z0-9_./:@%+=-]+$/.test(text)) return text;
    return "'" + text.replace(/'/g, "'\\''") + "'";
  }

  window.hop = {
    toast: toast,
    copyText: copyText,
    escapeHTML: escapeHTML,
    safeHref: safeHref,
    fmtNum: fmtNum,
    fmtAge: fmtAge,
    fmtDuration: fmtDuration,
    shellQuote: shellQuote
  };
})();
