(function () {
  "use strict";

  const DEV_ENV = /(^|[-_/ ])(dev|development|local|sandbox|test|testing|qa|uat|review|preview|branch)([-_/ ]|$)/i;
  const OPS_ENV = /(^|[-_/ ])(prod|production|prd|live|ops|operate|preprod|pre-prod|stage|staging|perf|load)([-_/ ]|$)/i;
  const WORLD_W = 1400;
  const WORLD_H = 960;
  const LIVE_MS = 30000;
  const GOLDEN = 2.399963229728653;

  const ROUTES = [
    { match: /^\/dashboard$|^\/$/, view: "overview", label: "Overview" },
    { match: /^\/kubernetes$|^\/pod$|^\/pods$/, view: "kubernetes", label: "Kubernetes" },
    { match: /^\/git$/, view: "repos", label: "Repositories" },
    { match: /^\/packages$/, view: "packages", label: "Packages" },
    { match: /^\/tickets$/, view: "tickets", label: "Tickets" },
    { match: /^\/servers$|^\/server$/, view: "servers", label: "Servers" },
    { match: /^\/containers$/, view: "containers", label: "Containers" },
    { match: /^\/diagnostics$/, view: "diagnostics", label: "Diagnostics" },
    { match: /^\/config$/, view: "config", label: "Config" },
    { match: /^\/search$/, view: "search", label: "Search" }
  ];

  const state = {
    overview: null,
    apps: [],
    selected: null,
    filter: "",
    side: "all",
    live: true,
    labels: false,
    diagnostics: new Map(),
    diagAt: null,
    loading: false,
    scanning: false,
    error: "",
    fetchedAt: null,
    interacted: false,
    zoom: 1,
    panX: 0,
    panY: 0,
    view: "orbit",
    viewLabel: "Orbit",
    frameHref: ""
  };

  const nodes = new Map();
  let liveTimer = null;
  let ageTimer = null;
  let toastTimer = null;

  const el = function (id) { return document.getElementById(id); };
  const field = el("app-field");
  const world = el("world");
  const stage = el("stage");
  const cockpit = el("cockpit");
  const tooltip = el("tooltip");
  const empty = el("empty");
  const orbitView = el("orbit-view");
  const frameView = el("frame-view");
  const frame = el("frame");
  const frameLoading = el("frame-loading");

  function clamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function hash(text) {
    let value = 2166136261;
    for (let i = 0; i < text.length; i++) {
      value ^= text.charCodeAt(i);
      value = Math.imul(value, 16777619);
    }
    return value >>> 0;
  }

  function fmtNum(value) {
    return new Intl.NumberFormat().format(Number(value || 0));
  }

  function fmtCPU(value) {
    const millis = Number(value || 0);
    if (millis >= 1000) return (millis / 1000).toFixed(millis >= 10000 ? 0 : 1) + " cores";
    return Math.round(millis) + "m";
  }

  function fmtBytes(value) {
    const bytes = Number(value || 0);
    if (bytes <= 0) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
    let index = 0;
    let size = bytes;
    while (size >= 1024 && index < units.length - 1) {
      size /= 1024;
      index++;
    }
    return size.toFixed(size >= 100 ? 0 : size >= 10 ? 1 : 2) + " " + units[index];
  }

  function initials(text) {
    const clean = String(text || "").replace(/[^a-z0-9]+/gi, " ").trim();
    if (!clean) return "--";
    const parts = clean.split(/\s+/);
    if (parts.length > 1) return (parts[0].charAt(0) + parts[1].charAt(0)).toUpperCase();
    return clean.slice(0, 2).toUpperCase();
  }

  function displayName(value) {
    const text = String(value || "").trim();
    return text || "unknown";
  }

  function sideFor(bucket, key) {
    const envs = bucket.envs || {};
    let dev = 0;
    let ops = 0;
    Object.keys(envs).forEach(function (env) {
      if (DEV_ENV.test(env)) dev += Number(envs[env] || 0);
      if (OPS_ENV.test(env)) ops += Number(envs[env] || 0);
    });
    if (dev && ops) return "bridge";
    if (dev) return "dev";
    if (ops) return "ops";
    const text = key + " " + (bucket.title || "");
    if (DEV_ENV.test(text)) return "dev";
    if (OPS_ENV.test(text)) return "ops";
    return hash(key) % 2 === 0 ? "dev" : "ops";
  }

  function hueFor(key, side) {
    const value = hash(key);
    if (side === "dev") return 166 + (value % 44);
    if (side === "ops") return 304 + (value % 50);
    return 36 + (value % 30);
  }

  function statusFor(bucket) {
    const failed = Number(bucket.failed || 0);
    const notReady = Number(bucket.not_ready || 0);
    const restarts = Number(bucket.restarts || 0);
    if (failed > 0 || (notReady > 0 && Number(bucket.ready || 0) === 0)) return "critical";
    if (notReady > 0 || restarts >= 10) return "warning";
    if (Number(bucket.pods || 0) > 0) return "ok";
    return "unknown";
  }

  function diagFor(key) {
    return state.diagnostics.get(String(key || "").toLowerCase()) || null;
  }

  function effectiveStatus(app) {
    const diag = diagFor(app.key);
    if (diag) {
      if (diag.verdict === "critical") return "critical";
      if (diag.verdict === "warning" && app.status !== "critical") return "warning";
      if (diag.verdict === "info" && app.status === "unknown") return "ok";
    }
    return app.status;
  }

  function sideLabel(side) {
    if (side === "dev") return "develop";
    if (side === "ops") return "operate";
    return "mixed";
  }

  function buildApps(overview) {
    const buckets = (overview.groups && overview.groups.app) || [];
    return buckets.map(function (bucket) {
      const key = displayName(bucket.key || bucket.title);
      const envs = bucket.envs || {};
      const envList = Object.keys(envs).sort(function (left, right) {
        return envs[right] - envs[left] || left.localeCompare(right);
      });
      const side = sideFor(bucket, key);
      const app = {
        key: key,
        title: displayName(bucket.title || key),
        side: side,
        status: statusFor(bucket),
        hue: hueFor(key, side),
        envs: envList,
        envCounts: envs,
        pods: Number(bucket.pods || 0),
        ready: Number(bucket.ready || 0),
        notReady: Number(bucket.not_ready || 0),
        failed: Number(bucket.failed || 0),
        running: Number(bucket.running || 0),
        pending: Number(bucket.pending || 0),
        restarts: Number(bucket.restarts || 0),
        contexts: Number(bucket.contexts || 0),
        namespaces: Number(bucket.namespaces || 0),
        cpu: Number(bucket.cpu_millicores || 0),
        memory: Number(bucket.memory_bytes || 0),
        x: 0,
        y: 0,
        el: null
      };
      const bits = [];
      if (app.failed) bits.push(app.failed + " failed");
      if (app.notReady) bits.push(app.notReady + " not ready");
      if (app.restarts >= 10) bits.push(app.restarts + " restarts");
      if (!bits.length) bits.push(app.ready + "/" + app.pods + " ready");
      bits.push(app.contexts + " ctx");
      bits.push(app.namespaces + " ns");
      app.summary = bits.join(" · ");
      return app;
    }).sort(function (left, right) {
      return left.key.localeCompare(right.key);
    });
  }

  function layoutApps(apps) {
    const count = apps.length;
    if (!count) return;
    const spacing = Math.min(56, 332 / Math.sqrt(Math.max(1, count)));
    apps.forEach(function (app, index) {
      const angle = index * GOLDEN;
      const radius = 126 + spacing * Math.sqrt(index + 0.5);
      app.x = 700 + Math.cos(angle) * radius;
      app.y = 480 + Math.sin(angle) * radius;
    });
  }

  function nodeSize(app) {
    const base = 38 + Math.min(18, Math.sqrt(Math.max(1, app.pods)) * 7);
    const penalty = state.apps.length > 80 ? 0.9 : 1;
    return Math.round(base * penalty);
  }

  function matchesFilter(app) {
    const needle = state.filter.trim().toLowerCase();
    if (state.side !== "all" && app.side !== state.side) return false;
    if (!needle) return true;
    if (app.key.toLowerCase().indexOf(needle) >= 0) return true;
    if (app.title.toLowerCase().indexOf(needle) >= 0) return true;
    return app.envs.some(function (env) { return env.toLowerCase().indexOf(needle) >= 0; });
  }

  function createNode(app) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "app-node";
    button.setAttribute("aria-label", app.title);

    const sigil = document.createElement("span");
    sigil.className = "app-sigil";
    sigil.textContent = initials(app.title);
    button.appendChild(sigil);

    const label = document.createElement("span");
    label.className = "app-label";
    label.textContent = app.title;
    button.appendChild(label);

    button.addEventListener("pointerenter", function (event) { showTooltip(app, event); });
    button.addEventListener("pointermove", function (event) { moveTooltip(event); });
    button.addEventListener("pointerleave", hideTooltip);
    button.addEventListener("focus", function () { showTooltipAtNode(app); });
    button.addEventListener("blur", hideTooltip);
    button.addEventListener("click", function (event) {
      event.stopPropagation();
      selectApp(app.key);
    });

    return button;
  }

  function applyNode(button, app) {
    const status = effectiveStatus(app);
    const diag = diagFor(app.key);
    button.dataset.key = app.key;
    button.dataset.side = app.side;
    button.style.left = app.x.toFixed(1) + "px";
    button.style.top = app.y.toFixed(1) + "px";
    button.style.setProperty("--node-size", nodeSize(app) + "px");
    button.style.setProperty("--node-scale", (0.9 + Math.min(0.18, app.pods * 0.018)).toFixed(3));
    button.style.setProperty("--node-hue", String(app.hue));
    button.style.setProperty("--node-delay", Math.min(900, hash(app.key) % 700) + "ms");
    button.setAttribute("aria-label", app.title + " · " + app.summary);
    button.title = app.title + "\n" + app.summary;
    button.classList.toggle("is-critical", status === "critical");
    button.classList.toggle("is-warning", status === "warning");
    button.classList.toggle("is-ok", status === "ok");
    button.classList.toggle("is-scanned", Boolean(diag));
    button.classList.toggle("is-selected", state.selected === app.key);
    button.classList.toggle("is-dim", !matchesFilter(app));
    const label = button.querySelector(".app-label");
    if (label) label.textContent = app.title;
    app.el = button;
  }

  function renderApps() {
    const seen = new Set();
    state.apps.forEach(function (app) {
      seen.add(app.key);
      let button = nodes.get(app.key);
      if (!button) {
        button = createNode(app);
        applyNode(button, app);
        nodes.set(app.key, button);
        field.appendChild(button);
      } else {
        applyNode(button, app);
      }
    });
    nodes.forEach(function (button, key) {
      if (seen.has(key)) return;
      button.remove();
      nodes.delete(key);
    });
    empty.hidden = state.apps.length > 0;
    applyFilter();
    if (state.selected && !state.apps.some(function (app) { return app.key === state.selected; })) {
      closeInspector();
    } else if (state.selected) {
      const app = state.apps.find(function (item) { return item.key === state.selected; });
      if (app) renderInspector(app);
    }
  }

  function applyFilter() {
    state.apps.forEach(function (app) {
      if (!app.el) return;
      app.el.classList.toggle("is-dim", !matchesFilter(app));
    });
    if (state.selected) {
      const app = state.apps.find(function (item) { return item.key === state.selected; });
      if (app) renderInspector(app);
    }
  }

  function showTooltip(app, event) {
    const diag = diagFor(app.key);
    tooltip.textContent = "";
    const name = document.createElement("b");
    name.textContent = app.title;
    const meta = document.createElement("span");
    meta.textContent = sideLabel(app.side) + " · " + effectiveStatus(app) + " · " + (diag ? diag.summary : app.summary);
    tooltip.appendChild(name);
    tooltip.appendChild(meta);
    tooltip.hidden = false;
    moveTooltip(event);
  }

  function moveTooltip(event) {
    if (tooltip.hidden) return;
    tooltip.style.left = event.clientX + "px";
    tooltip.style.top = (event.clientY - 4) + "px";
  }

  function showTooltipAtNode(app) {
    if (!app.el) return;
    const rect = app.el.getBoundingClientRect();
    const diag = diagFor(app.key);
    tooltip.textContent = "";
    const name = document.createElement("b");
    name.textContent = app.title;
    const meta = document.createElement("span");
    meta.textContent = sideLabel(app.side) + " · " + effectiveStatus(app) + " · " + (diag ? diag.summary : app.summary);
    tooltip.appendChild(name);
    tooltip.appendChild(meta);
    tooltip.hidden = false;
    tooltip.style.left = (rect.left + rect.width / 2) + "px";
    tooltip.style.top = (rect.top - 4) + "px";
  }

  function hideTooltip() {
    tooltip.hidden = true;
  }

  function selectApp(key) {
    state.selected = key;
    const app = state.apps.find(function (item) { return item.key === key; });
    if (!app) return;
    state.apps.forEach(function (item) {
      if (item.el) item.el.classList.toggle("is-selected", item.key === key);
    });
    renderInspector(app);
    if (app.el) app.el.focus({ preventScroll: true });
    markInteracted();
  }

  function renderInspector(app) {
    const box = el("inspector");
    const diag = diagFor(app.key);
    box.hidden = false;
    box.style.setProperty("--inspect-hue", String(app.hue));
    el("inspect-sigil").textContent = initials(app.title);
    el("inspect-name").textContent = app.title;
    el("inspect-side").textContent = sideLabel(app.side) + " application";
    el("inspect-summary").textContent = diag ? diag.summary + " · " + app.summary : app.summary;
    el("inspect-pods").textContent = fmtNum(app.pods);
    el("inspect-ready").textContent = fmtNum(app.ready);
    el("inspect-restarts").textContent = fmtNum(app.restarts);
    el("inspect-cpu").textContent = fmtCPU(app.cpu);
    el("inspect-memory").textContent = fmtBytes(app.memory);
    el("inspect-namespaces").textContent = fmtNum(app.namespaces);
    const envBox = el("inspect-envs");
    envBox.textContent = "";
    if (!app.envs.length) {
      const chip = document.createElement("span");
      chip.className = "cx-env-chip";
      chip.textContent = "env unmapped";
      envBox.appendChild(chip);
    } else {
      app.envs.forEach(function (env) {
        const chip = document.createElement("span");
        chip.className = "cx-env-chip";
        if (DEV_ENV.test(env)) chip.classList.add("is-dev");
        if (OPS_ENV.test(env)) chip.classList.add("is-ops");
        chip.textContent = env + " × " + fmtNum(app.envCounts[env]);
        envBox.appendChild(chip);
      });
    }
    const target = new URL("/kubernetes", window.location.origin);
    target.searchParams.set("embed", "1");
    target.searchParams.set("q", app.key);
    el("inspect-link").href = target.pathname + target.search;
  }

  function closeInspector() {
    state.selected = null;
    el("inspector").hidden = true;
    state.apps.forEach(function (app) {
      if (app.el) app.el.classList.remove("is-selected");
    });
  }

  function setSummary() {
    const overview = state.overview;
    const totals = (overview && overview.totals) || {};
    const apps = state.apps;
    const dev = apps.filter(function (app) { return app.side === "dev"; }).length;
    const ops = apps.filter(function (app) { return app.side === "ops"; }).length;
    const bridge = apps.filter(function (app) { return app.side === "bridge"; }).length;
    const pods = Number(totals.pods || 0);
    const ready = Number(totals.ready || 0);
    const attention = Number(totals.failed || 0) + Number(totals.not_ready || 0);
    el("stat-pods").textContent = fmtNum(pods);
    el("stat-pods-sub").textContent = Number(totals.contexts || 0) + " ctx · " + Number(totals.namespaces || 0) + " ns";
    el("stat-ready").textContent = pods ? Math.round((ready / pods) * 100) + "%" : "—";
    el("stat-ready-sub").textContent = fmtNum(ready) + " ready";
    el("stat-attention").textContent = fmtNum(attention);
    el("stat-attention-sub").textContent = fmtNum(overview ? overview.issue_total || 0 : 0) + " triage";
    el("stat-apps").textContent = fmtNum(apps.length);
    el("stat-apps-sub").textContent = dev + " dev · " + ops + " ops" + (bridge ? " · " + bridge + " mixed" : "");
    el("stat-servers").textContent = fmtNum(totals.servers || 0);
    el("stat-servers-sub").textContent = fmtNum(totals.servers_down || 0) + " down · " + fmtNum(totals.servers_warn || 0) + " warn";
    el("dev-meta").textContent = dev + (bridge ? " + " + bridge : "") + " dev";
    el("ops-meta").textContent = ops + (bridge ? " + " + bridge : "") + " ops";
    renderPulse();
  }

  function renderPulse() {
    const pulse = el("live-btn");
    const status = el("cx-status");
    if (!pulse || !status) return;
    pulse.classList.toggle("is-live", state.live && !state.error);
    pulse.classList.toggle("is-error", Boolean(state.error));
    pulse.setAttribute("aria-pressed", String(state.live));
    if (state.error) {
      status.textContent = "offline";
      return;
    }
    if (state.loading) {
      status.textContent = "syncing";
      return;
    }
    if (!state.fetchedAt) {
      status.textContent = state.live ? "live" : "paused";
      return;
    }
    const seconds = Math.max(0, Math.round((Date.now() - state.fetchedAt.getTime()) / 1000));
    if (!state.live) {
      status.textContent = "paused · " + seconds + "s ago";
      return;
    }
    status.textContent = "live · " + seconds + "s ago";
  }

  function loadOverview(force) {
    if (state.loading) return;
    state.loading = true;
    state.error = "";
    renderPulse();
    const url = "/api/dashboard" + (force ? "?refresh=1" : "");
    fetch(url, { headers: { Accept: "application/json" } })
      .then(function (response) {
        if (!response.ok) {
          return response.json().catch(function () { return {}; }).then(function (body) {
            throw new Error(body.error || "dashboard request failed (" + response.status + ")");
          });
        }
        return response.json();
      })
      .then(function (overview) {
        state.overview = overview;
        state.fetchedAt = new Date(overview.fetched_at || Date.now());
        state.apps = buildApps(overview);
        layoutApps(state.apps);
        renderApps();
        setSummary();
        if (state.view === "orbit") {
          if (state.interacted) applyTransform();
          else fitWorld();
        }
        const errors = overview.errors || [];
        if (errors.length) {
          toast(errors.length + " source warning" + (errors.length > 1 ? "s" : "") + " · " + errors[0], true);
        }
      })
      .catch(function (error) {
        state.error = error.message || "dashboard unavailable";
        renderPulse();
        toast(state.error, true);
      })
      .finally(function () {
        state.loading = false;
        renderPulse();
      });
  }

  function applyTransform() {
    world.style.transform = "translate(-50%, -50%) translate(" + state.panX.toFixed(1) + "px, " + state.panY.toFixed(1) + "px) scale(" + state.zoom.toFixed(4) + ")";
  }

  function fitWorld() {
    const width = stage.clientWidth || Math.max(240, window.innerWidth - 230);
    const height = stage.clientHeight || Math.max(200, window.innerHeight - 220);
    const safeW = Math.max(180, width - (width < 620 ? 30 : 80));
    const safeH = Math.max(160, height - (height < 480 ? 70 : 110));
    state.zoom = clamp(Math.min(safeW / WORLD_W, safeH / WORLD_H), 0.28, 1.08);
    state.panX = 0;
    state.panY = 0;
    state.interacted = false;
    applyTransform();
  }

  function zoomBy(factor, clientX, clientY) {
    const rect = stage.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    const cx = (clientX == null ? rect.left + rect.width / 2 : clientX) - rect.left - rect.width / 2;
    const cy = (clientY == null ? rect.top + rect.height / 2 : clientY) - rect.top - rect.height / 2;
    const next = clamp(state.zoom * factor, 0.24, 2.4);
    const ratio = next / state.zoom;
    state.panX = cx - (cx - state.panX) * ratio;
    state.panY = cy - (cy - state.panY) * ratio;
    state.zoom = next;
    state.interacted = true;
    applyTransform();
  }

  function markInteracted() {
    if (state.interacted) return;
    state.interacted = true;
    cockpit.classList.add("has-interacted");
  }

  function toast(message, isError) {
    const box = el("toast");
    box.textContent = message;
    box.classList.toggle("is-error", Boolean(isError));
    box.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { box.hidden = true; }, 4200);
  }

  function scanApps() {
    if (state.scanning) return;
    state.scanning = true;
    const button = el("scan-btn");
    button.classList.add("is-busy");
    button.disabled = true;
    toast("scanning configured applications…");
    const controller = new AbortController();
    const timer = setTimeout(function () { controller.abort(); }, 60000);
    fetch("/api/diagnostics/apps", { headers: { Accept: "application/json" }, signal: controller.signal })
      .then(function (response) {
        if (!response.ok) throw new Error("diagnostics request failed (" + response.status + ")");
        return response.json();
      })
      .then(function (body) {
        const reports = body.apps || [];
        state.diagnostics = new Map();
        reports.forEach(function (report) {
          state.diagnostics.set(String(report.target || "").toLowerCase(), report);
        });
        state.diagAt = new Date();
        renderApps();
        if (reports.length) {
          const critical = reports.filter(function (report) { return report.verdict === "critical"; }).length;
          const warning = reports.filter(function (report) { return report.verdict === "warning"; }).length;
          toast(reports.length + " apps scanned · " + critical + " critical · " + warning + " warning", critical > 0);
        } else {
          toast("No apps configured for diagnostics.");
        }
      })
      .catch(function (error) {
        toast(error.name === "AbortError" ? "App scan timed out." : error.message, true);
      })
      .finally(function () {
        clearTimeout(timer);
        state.scanning = false;
        button.classList.remove("is-busy");
        button.disabled = false;
      });
  }

  function setLive(value) {
    state.live = value;
    if (liveTimer) {
      clearInterval(liveTimer);
      liveTimer = null;
    }
    if (value) {
      liveTimer = setInterval(function () { loadOverview(false); }, LIVE_MS);
    }
    renderPulse();
  }

  function setLabels(value) {
    state.labels = value;
    cockpit.classList.toggle("show-labels", value);
    el("labels-btn").setAttribute("aria-pressed", String(value));
    try { localStorage.setItem("hop-cockpit-labels", value ? "1" : "0"); } catch (_) {}
  }

  function themeName() {
    return document.documentElement.getAttribute("data-theme") === "light" ? "light" : "dark";
  }

  function syncFrameTheme() {
    try {
      frame.contentDocument.documentElement.setAttribute("data-theme", themeName());
    } catch (_) {}
  }

  function toggleTheme() {
    const next = themeName() === "light" ? "dark" : "light";
    document.documentElement.setAttribute("data-theme", next);
    try { localStorage.setItem("hop-theme", next); } catch (_) {}
    syncFrameTheme();
  }

  function openHelp() {
    el("help").hidden = false;
  }

  function closeHelp() {
    el("help").hidden = true;
  }

  function setSide(side) {
    state.side = side;
    document.querySelectorAll("#side-switch button").forEach(function (button) {
      button.classList.toggle("on", button.dataset.side === side);
    });
    applyFilter();
  }

  function setTitle(label) {
    state.viewLabel = label || "Orbit";
    el("view-title").textContent = state.viewLabel;
    document.title = state.viewLabel + " · cockpit · hop";
  }

  function setActive(view) {
    document.querySelectorAll(".cx-rail-item[data-view]").forEach(function (item) {
      item.classList.toggle("is-active", item.dataset.view === view);
    });
  }

  function withoutEmbed(raw) {
    const url = new URL(raw, window.location.origin);
    url.searchParams.delete("embed");
    return url.pathname + url.search + url.hash;
  }

  function withEmbed(raw) {
    const url = new URL(raw, window.location.origin);
    url.searchParams.set("embed", "1");
    return url.pathname + url.search + url.hash;
  }

  function setOpenExternal(href) {
    el("open-external").href = href || "/";
  }

  function viewFromPath(pathname) {
    for (let i = 0; i < ROUTES.length; i++) {
      if (ROUTES[i].match.test(pathname)) return ROUTES[i];
    }
    return null;
  }

  function frameLoaded() {
    frameLoading.hidden = true;
    let href = "";
    let pathname = "";
    let search = "";
    let hash = "";
    try {
      const location = frame.contentWindow.location;
      href = location.href;
      pathname = location.pathname;
      search = location.search;
      hash = location.hash;
    } catch (_) {
      return;
    }
    if (!href || href === "about:blank") return;
    const params = new URLSearchParams(search);
    if (params.get("embed") !== "1") {
      frameLoading.hidden = false;
      frame.src = withEmbed(href);
      return;
    }
    syncFrameTheme();
    const route = viewFromPath(pathname);
    if (route) {
      state.view = route.view;
      setActive(route.view);
      setTitle(route.label);
    } else {
      state.view = "frame";
      setActive("");
      setTitle(document.title.replace(/ · cockpit · hop$/, "") || "Workspace");
    }
    state.frameHref = pathname + search + hash;
    setOpenExternal(withoutEmbed(href));
  }

  function showView(view, href, label) {
    hideTooltip();
    if (view === "orbit") {
      state.view = "orbit";
      orbitView.hidden = false;
      frameView.hidden = true;
      setActive("orbit");
      setTitle("Orbit");
      setOpenExternal("/");
      requestAnimationFrame(function () {
        if (state.interacted) applyTransform();
        else fitWorld();
      });
      return;
    }

    state.view = view;
    orbitView.hidden = true;
    frameView.hidden = false;
    setActive(view);
    setTitle(label || "Workspace");

    if (href) {
      const target = new URL(withEmbed(href), window.location.origin);
      const next = target.pathname + target.search + target.hash;
      state.frameHref = next;
      setOpenExternal(withoutEmbed(target.href));
      if (frame.src !== target.href) {
        frameLoading.hidden = false;
        frame.src = target.href;
      } else {
        frameLoading.hidden = true;
      }
    }
  }

  function refreshCurrent() {
    const button = el("refresh-btn");
    button.classList.add("is-busy");
    if (state.view === "orbit") {
      loadOverview(true);
    } else {
      try {
        frame.contentWindow.location.reload();
      } catch (_) {
        frame.src = frame.src;
      }
    }
    setTimeout(function () { button.classList.remove("is-busy"); }, 900);
  }

  function globalSearch(query) {
    const target = new URL("/search", window.location.origin);
    target.searchParams.set("embed", "1");
    if (query) target.searchParams.set("q", query);
    showView("search", target.pathname + target.search, "Search");
  }

  function bindRails() {
    document.querySelectorAll("[data-dock]").forEach(function (rail) {
      const items = Array.prototype.slice.call(rail.querySelectorAll(".cx-rail-item"));
      if (!items.length) return;
      const isLeft = rail.classList.contains("cx-rail-left");
      rail.addEventListener("pointermove", function (event) {
        items.forEach(function (item) {
          const rect = item.getBoundingClientRect();
          const center = rect.top + rect.height / 2;
          const distance = Math.abs(event.clientY - center);
          const reach = Math.max(80, rect.height * 2.6);
          const influence = Math.max(0, 1 - distance / reach);
          const scale = 1 + influence * 0.16;
          const shift = influence * (isLeft ? 4 : -4);
          item.style.setProperty("--rail-scale", scale.toFixed(3));
          item.style.setProperty("--rail-shift", shift.toFixed(1) + "px");
        });
      });
      rail.addEventListener("pointerleave", function () {
        items.forEach(function (item) {
          item.style.removeProperty("--rail-scale");
          item.style.removeProperty("--rail-shift");
        });
      });
    });
  }

  function bindStage() {
    let dragging = false;
    let startX = 0;
    let startY = 0;
    let originX = 0;
    let originY = 0;

    stage.addEventListener("pointerdown", function (event) {
      if (event.button !== 0) return;
      if (event.target.closest(".app-node") || event.target.closest(".cx-inspector") || event.target.closest(".cx-empty")) return;
      dragging = true;
      startX = event.clientX;
      startY = event.clientY;
      originX = state.panX;
      originY = state.panY;
      stage.classList.add("is-dragging");
      stage.setPointerCapture(event.pointerId);
      markInteracted();
    });

    stage.addEventListener("pointermove", function (event) {
      if (!dragging) return;
      state.panX = originX + event.clientX - startX;
      state.panY = originY + event.clientY - startY;
      applyTransform();
    });

    function stopDrag(event) {
      if (!dragging) return;
      dragging = false;
      stage.classList.remove("is-dragging");
      if (event.pointerId != null && stage.hasPointerCapture(event.pointerId)) {
        stage.releasePointerCapture(event.pointerId);
      }
    }

    stage.addEventListener("pointerup", stopDrag);
    stage.addEventListener("pointercancel", stopDrag);

    stage.addEventListener("wheel", function (event) {
      event.preventDefault();
      zoomBy(event.deltaY < 0 ? 1.12 : 0.89, event.clientX, event.clientY);
      markInteracted();
    }, { passive: false });

    stage.addEventListener("click", function (event) {
      if (event.target.closest(".app-node")) return;
      closeInspector();
      hideTooltip();
    });
  }

  function bindControls() {
    el("global-form").addEventListener("submit", function (event) {
      event.preventDefault();
      globalSearch(el("global-input").value.trim());
    });

    el("filter-form").addEventListener("submit", function (event) { event.preventDefault(); });
    el("filter-input").addEventListener("input", function (event) {
      state.filter = event.target.value;
      applyFilter();
    });

    document.querySelectorAll("#side-switch button").forEach(function (button) {
      button.addEventListener("click", function () { setSide(button.dataset.side); });
    });

    document.querySelectorAll(".cx-rail-item[data-view]").forEach(function (item) {
      item.addEventListener("click", function (event) {
        event.preventDefault();
        showView(item.dataset.view, item.dataset.href, item.dataset.label);
      });
    });

    el("scan-btn").addEventListener("click", scanApps);
    el("refresh-btn").addEventListener("click", refreshCurrent);
    el("live-btn").addEventListener("click", function () { setLive(!state.live); });
    el("theme-btn").addEventListener("click", toggleTheme);
    el("labels-btn").addEventListener("click", function () { setLabels(!state.labels); });
    el("help-btn").addEventListener("click", openHelp);
    document.querySelectorAll("[data-help-close]").forEach(function (node) {
      node.addEventListener("click", closeHelp);
    });
    el("inspector-close").addEventListener("click", closeInspector);
    el("inspect-link").addEventListener("click", function (event) {
      event.preventDefault();
      showView("kubernetes", event.currentTarget.getAttribute("href"), "Kubernetes");
    });

    el("zoom-in").addEventListener("click", function () { zoomBy(1.18, null, null); markInteracted(); });
    el("zoom-out").addEventListener("click", function () { zoomBy(0.85, null, null); markInteracted(); });
    el("zoom-fit").addEventListener("click", function () { fitWorld(); });
    el("frame").addEventListener("load", frameLoaded);
    document.querySelector(".cx-brand").addEventListener("click", function (event) {
      event.preventDefault();
      showView("orbit");
    });
  }

  function bindKeys() {
    document.addEventListener("keydown", function (event) {
      const target = event.target;
      const typing = target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable);
      if (event.key === "Escape") {
        if (!el("help").hidden) {
          closeHelp();
          return;
        }
        if (typing) {
          target.value = "";
          if (target === el("filter-input")) {
            state.filter = "";
            applyFilter();
          }
          target.blur();
          return;
        }
        if (state.selected) {
          closeInspector();
          return;
        }
        return;
      }
      if (typing) return;
      if (event.key === "/") {
        event.preventDefault();
        el("global-input").focus();
        return;
      }
      if (event.key === "f") {
        if (state.view !== "orbit") return;
        event.preventDefault();
        el("filter-input").focus();
        return;
      }
      if (event.key === "r") {
        event.preventDefault();
        refreshCurrent();
        return;
      }
      if (event.key === "g") {
        if (state.view !== "orbit") return;
        event.preventDefault();
        fitWorld();
        return;
      }
      if (event.key === "+" || event.key === "=") {
        if (state.view !== "orbit") return;
        event.preventDefault();
        zoomBy(1.18, null, null);
        return;
      }
      if (event.key === "-") {
        if (state.view !== "orbit") return;
        event.preventDefault();
        zoomBy(0.85, null, null);
        return;
      }
      if (event.key === "l") {
        event.preventDefault();
        setLabels(!state.labels);
        return;
      }
      if (event.key === "?") {
        event.preventDefault();
        openHelp();
      }
    });
  }

  function init() {
    let labels = false;
    try { labels = localStorage.getItem("hop-cockpit-labels") === "1"; } catch (_) {}
    setLabels(labels);
    bindRails();
    bindStage();
    bindControls();
    bindKeys();
    setLive(true);
    loadOverview(false);
    fitWorld();
    ageTimer = setInterval(renderPulse, 5000);
    window.addEventListener("resize", function () {
      if (state.view === "orbit" && !state.interacted) fitWorld();
    });
    window.addEventListener("beforeunload", function () {
      clearInterval(liveTimer);
      clearInterval(ageTimer);
    });
  }

  init();
})();
