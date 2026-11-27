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

  function initTabs() {
    document.querySelectorAll("[data-tabs]").forEach(function (root) {
      const tabs = Array.prototype.slice.call(root.querySelectorAll('[role="tab"]'));
      if (!tabs.length) return;
      const tablist = root.querySelector('[role="tablist"]');

      function panelFor(tab) {
        const id = tab.getAttribute("aria-controls");
        return id ? document.getElementById(id) : null;
      }

      function select(tab, focus, updateHash) {
        tabs.forEach(function (candidate) {
          const selected = candidate === tab;
          candidate.setAttribute("aria-selected", selected ? "true" : "false");
          candidate.tabIndex = selected ? 0 : -1;
          const panel = panelFor(candidate);
          if (panel) panel.hidden = !selected;
        });
        const activePanel = panelFor(tab);
        root.setAttribute("data-active-tab", activePanel ? activePanel.id.replace(/^panel-/, "") : "");
        if (focus) tab.focus();
        if (updateHash) {
          const slug = activePanel ? activePanel.id.replace(/^panel-/, "") : "";
          if (slug) {
            try {
              history.replaceState(null, "", "#" + slug);
            } catch (_) {
              window.location.hash = slug;
            }
          }
        }
      }

      tabs.forEach(function (tab) {
        tab.addEventListener("click", function () {
          select(tab, false, true);
        });
      });

      if (tablist) {
        tablist.addEventListener("keydown", function (event) {
          const current = tabs.indexOf(document.activeElement);
          if (current < 0) return;
          let next = current;
          if (event.key === "ArrowRight") next = (current + 1) % tabs.length;
          else if (event.key === "ArrowLeft") next = (current - 1 + tabs.length) % tabs.length;
          else if (event.key === "Home") next = 0;
          else if (event.key === "End") next = tabs.length - 1;
          else return;
          event.preventDefault();
          select(tabs[next], true, true);
        });
      }

      let initial = tabs.find(function (tab) {
        return tab.getAttribute("aria-selected") === "true";
      }) || tabs[0];
      const hash = window.location.hash.replace(/^#/, "");
      if (hash) {
        tabs.forEach(function (tab) {
          const panel = panelFor(tab);
          if (panel && panel.id.replace(/^panel-/, "") === hash) initial = tab;
        });
      }
      select(initial, false, false);
    });
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

  function applyTheme(theme) {
    const next = theme === "light" ? "light" : "dark";
    document.documentElement.setAttribute("data-theme", next);
    const button = document.getElementById("theme-toggle");
    if (button) {
      const label = next === "dark" ? "Switch to light theme" : "Switch to dark theme";
      button.setAttribute("aria-label", label);
      button.setAttribute("title", label);
    }
    try { localStorage.setItem("hop-theme", next); } catch (_) {}
  }

  const themeButton = document.getElementById("theme-toggle");
  if (themeButton) {
    applyTheme(document.documentElement.getAttribute("data-theme"));
    themeButton.addEventListener("click", function () {
      applyTheme(document.documentElement.getAttribute("data-theme") === "light" ? "dark" : "light");
    });
  }

  const path = window.location.pathname;
  document.querySelectorAll("[data-nav]").forEach(function (link) {
    const href = link.getAttribute("data-nav");
    let active = false;
    if (href === "/") active = path === "/" || path === "/cockpit";
    else if (href === "/dashboard") active = path === "/dashboard";
    else if (href === "/servers") active = path === "/servers" || path.startsWith("/server");
    else if (href === "/kubernetes") active = path === "/kubernetes" || path.startsWith("/pod");
    else active = path === href;
    link.classList.toggle("active", active);
    if (active) link.setAttribute("aria-current", "page");
  });

  initTabs();


  const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform || "");
  document.querySelectorAll(".side-kbd, .topbar-search kbd").forEach(function (element) {
    element.textContent = isMac ? "⌘K" : "Ctrl K";
  });

  const sidebarToggle = document.getElementById("sidebar-toggle");
  const sidebarOverlay = document.getElementById("sidebar-overlay");
  const sidebarMedia = window.matchMedia ? window.matchMedia("(max-width: 860px)") : null;

  function sidebarIsMobile() {
    return sidebarMedia ? sidebarMedia.matches : window.innerWidth <= 860;
  }

  function syncSidebar() {
    const collapsed = document.body.classList.contains("sidebar-collapsed");
    const open = document.body.classList.contains("sidebar-open");
    if (sidebarToggle) {
      const expanded = sidebarIsMobile() ? open : !collapsed;
      sidebarToggle.setAttribute("aria-expanded", expanded ? "true" : "false");
      const label = sidebarIsMobile()
        ? (open ? "Close navigation" : "Open navigation")
        : (collapsed ? "Expand navigation" : "Collapse navigation");
      sidebarToggle.setAttribute("aria-label", label);
      sidebarToggle.setAttribute("title", label);
    }
    if (sidebarOverlay) {
      sidebarOverlay.hidden = !(sidebarIsMobile() && open);
    }
  }

  function setSidebarCollapsed(collapsed) {
    document.body.classList.toggle("sidebar-collapsed", collapsed);
    try { localStorage.setItem("hop-sidebar-collapsed", collapsed ? "1" : "0"); } catch (_) {}
    syncSidebar();
  }

  if (sidebarToggle) {
    try {
      if (localStorage.getItem("hop-sidebar-collapsed") === "1") {
        document.body.classList.add("sidebar-collapsed");
      }
    } catch (_) {}
    sidebarToggle.addEventListener("click", function () {
      if (sidebarIsMobile()) {
        document.body.classList.toggle("sidebar-open");
      } else {
        setSidebarCollapsed(!document.body.classList.contains("sidebar-collapsed"));
      }
      syncSidebar();
    });
  }
  if (sidebarOverlay) {
    sidebarOverlay.addEventListener("click", function () {
      document.body.classList.remove("sidebar-open");
      syncSidebar();
    });
  }
  document.querySelectorAll(".side-link").forEach(function (link) {
    link.addEventListener("click", function () {
      if (sidebarIsMobile()) {
        document.body.classList.remove("sidebar-open");
        syncSidebar();
      }
    });
  });
  if (sidebarMedia) {
    const onSidebarBreakpoint = function () {
      document.body.classList.remove("sidebar-open");
      syncSidebar();
    };
    if (sidebarMedia.addEventListener) sidebarMedia.addEventListener("change", onSidebarBreakpoint);
    else if (sidebarMedia.addListener) sidebarMedia.addListener(onSidebarBreakpoint);
  }
  syncSidebar();

  const palette = document.getElementById("palette");
  const paletteInput = document.getElementById("palette-input");
  const paletteResults = document.getElementById("palette-results");
  let paletteTimer = null;
  let paletteController = null;
  let paletteItems = [];
  let paletteIndex = 0;

  function paletteKindPath(kind) {
    switch (kind) {
      case "pods": return "/kubernetes";
      case "servers": return "/servers";
      case "containers": return "/containers";
      case "repos": return "/git";
      case "packages": return "/packages";
      case "tickets": return "/tickets";
      default: return "/search";
    }
  }

  function paletteTarget(section, item) {
    const payload = item.payload || {};
    if (section.kind === "pods") {
      const pod = payload.pod || {};
      if (pod.context && pod.namespace && pod.name) {
        return "/pod?context=" + encodeURIComponent(pod.context) +
          "&namespace=" + encodeURIComponent(pod.namespace) +
          "&name=" + encodeURIComponent(pod.name);
      }
    }
    if (section.kind === "servers") {
      const server = payload.server || {};
      if (server.name) return "/server?name=" + encodeURIComponent(server.name);
    }
    const href = safeHref(item.url);
    if (href) return href;
    const path = paletteKindPath(section.kind);
    const query = item.title || "";
    return path + (query ? "?q=" + encodeURIComponent(query) : "");
  }

  function paletteSubtitle(section, item) {
    const parts = [];
    if (item.subtitle) parts.push(item.subtitle);
    if (item.description) parts.push(item.description);
    if (!parts.length) parts.push(section.title || section.kind);
    return parts.join(" · ");
  }

  function renderPalette() {
    if (!paletteResults) return;
    if (!paletteItems.length) {
      const query = paletteInput ? paletteInput.value.trim() : "";
      paletteResults.innerHTML = '<div class="palette-empty">' +
        (query ? "No match across enabled sources." : "Type to search across every enabled source.") +
        "</div>";
      return;
    }
    let html = "";
    paletteItems.forEach(function (entry, index) {
      const item = entry.item;
      const icon = String(entry.section.kind || "?").slice(0, 2);
      html += '<button class="palette-item' + (index === paletteIndex ? " active" : "") +
        '" type="button" data-palette-index="' + index + '">' +
        '<span class="palette-item-icon">' + escapeHTML(icon) + "</span>" +
        '<span class="palette-item-copy">' +
        '<span class="palette-item-title">' + escapeHTML(item.title || item.id || "—") + "</span>" +
        '<span class="palette-item-sub">' + escapeHTML(paletteSubtitle(entry.section, item)) + "</span>" +
        "</span>" +
        '<span class="palette-kind">' + escapeHTML(entry.section.title || entry.section.kind || "") + "</span>" +
        "</button>";
    });
    paletteResults.innerHTML = html;
  }

  function paletteMove(delta) {
    if (!paletteItems.length) return;
    paletteIndex = (paletteIndex + delta + paletteItems.length) % paletteItems.length;
    renderPalette();
    const active = paletteResults && paletteResults.querySelector(".palette-item.active");
    if (active && active.scrollIntoView) active.scrollIntoView({ block: "nearest" });
  }

  function openPalette() {
    if (!palette) return;
    document.body.classList.remove("sidebar-open");
    syncSidebar();
    palette.hidden = false;
    document.body.classList.add("palette-open");
    if (paletteInput) {
      paletteInput.value = "";
      paletteInput.focus();
    }
    paletteItems = [];
    paletteIndex = 0;
    renderPalette();
  }

  function closePalette() {
    if (!palette) return;
    palette.hidden = true;
    document.body.classList.remove("palette-open");
    if (paletteController) paletteController.abort();
    if (paletteTimer) {
      clearTimeout(paletteTimer);
      paletteTimer = null;
    }
  }

  function selectPalette(index) {
    const entry = paletteItems[index];
    if (!entry) return;
    const target = paletteTarget(entry.section, entry.item);
    closePalette();
    if (/^https?:\/\//i.test(target)) {
      window.open(target, "_blank", "noopener");
    } else {
      window.location.assign(target);
    }
  }

  async function searchPalette(raw) {
    const query = String(raw || "").trim();
    if (!query) {
      paletteItems = [];
      paletteIndex = 0;
      renderPalette();
      return;
    }
    if (paletteController) paletteController.abort();
    const controller = new AbortController();
    paletteController = controller;
    const params = new URLSearchParams();
    params.set("q", query);
    params.set("limit", "8");
    params.set("tickets", "1");
    try {
      const response = await fetch("/api/unified?" + params.toString(), {
        signal: controller.signal,
        headers: { Accept: "application/json" }
      });
      const data = await response.json().catch(function () { return {}; });
      if (!response.ok) throw new Error(data.error || "HTTP " + response.status);
      if (controller !== paletteController) return;
      paletteItems = [];
      (data.sections || []).forEach(function (section) {
        (section.items || []).forEach(function (item) {
          paletteItems.push({ section: section, item: item });
        });
      });
      paletteItems = paletteItems.slice(0, 12);
      paletteIndex = 0;
      renderPalette();
    } catch (error) {
      if (error.name === "AbortError") return;
      if (!paletteResults) return;
      paletteItems = [];
      paletteResults.innerHTML = '<div class="palette-empty">' + escapeHTML(error.message || "Search failed") + "</div>";
    }
  }

  document.querySelectorAll("[data-palette-open]").forEach(function (button) {
    button.addEventListener("click", openPalette);
  });
  document.querySelectorAll("[data-palette-close]").forEach(function (element) {
    element.addEventListener("click", closePalette);
  });
  if (paletteInput) {
    paletteInput.addEventListener("input", function () {
      if (paletteTimer) clearTimeout(paletteTimer);
      paletteTimer = setTimeout(function () {
        searchPalette(paletteInput.value);
      }, 180);
    });
    paletteInput.addEventListener("keydown", function (event) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        paletteMove(1);
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        paletteMove(-1);
        return;
      }
      if (event.key === "Enter") {
        event.preventDefault();
        selectPalette(paletteIndex);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        closePalette();
      }
    });
  }
  if (paletteResults) {
    paletteResults.addEventListener("click", function (event) {
      const button = event.target.closest("[data-palette-index]");
      if (!button) return;
      selectPalette(Number(button.getAttribute("data-palette-index")));
    });
  }
  document.addEventListener("keydown", function (event) {
    if ((event.metaKey || event.ctrlKey) && String(event.key).toLowerCase() === "k") {
      event.preventDefault();
      if (palette && palette.hidden) openPalette();
      else closePalette();
      return;
    }
    if (event.key === "Escape") {
      if (palette && !palette.hidden) {
        closePalette();
        return;
      }
      if (document.body.classList.contains("sidebar-open")) {
        document.body.classList.remove("sidebar-open");
        syncSidebar();
      }
    }
  });

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
