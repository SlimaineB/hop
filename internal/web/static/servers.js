(function () {
  "use strict";

  const h = window.hop.escapeHTML;
  const helpers = window.hop;
  const el = function (id) { return document.getElementById(id); };

  const state = {
    servers: [],
    filter: "",
    live: true,
    timer: null,
    loading: false
  };

  function fmtBytes(value) {
    const bytes = Number(value || 0);
    if (bytes <= 0) return "—";
    const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
    let index = 0;
    let size = bytes;
    while (size >= 1024 && index < units.length - 1) {
      size /= 1024;
      index++;
    }
    return size.toFixed(size >= 100 ? 0 : size >= 10 ? 1 : 2) + " " + units[index];
  }

  function pct(used, total) {
    if (!total) return 0;
    return Math.max(0, Math.min(100, Math.round((used / total) * 100)));
  }

  function diskPct(server) {
    const fromText = Number(String(server.disk_percent || "").replace("%", ""));
    if (Number.isFinite(fromText) && fromText > 0) return Math.max(0, Math.min(100, fromText));
    return pct(server.disk_used, server.disk_total);
  }

  function loadFirst(server) {
    return String(server.load || "").trim().split(/\s+/).filter(Boolean)[0] || "";
  }

  function loadPct(server) {
    const value = Number(loadFirst(server));
    const cpus = Number(server.cpus || 0);
    if (!Number.isFinite(value) || cpus <= 0) return 0;
    return Math.max(0, Math.min(100, Math.round((value / cpus) * 100)));
  }

  function tone(value) {
    if (value >= 90) return "err";
    if (value >= 75) return "warn";
    return "ok";
  }

  function statusLabel(status) {
    return { ok: "Online", warn: "Warning", down: "Down", unknown: "Unknown" }[status] || "Unknown";
  }

  function setKPI(name, value, sub) {
    const target = document.querySelector('[data-srv="' + name + '"]');
    if (target) target.textContent = value;
    if (sub != null) {
      const subtitle = document.querySelector('[data-srv="' + name + '_sub"]');
      if (subtitle) subtitle.textContent = sub;
    }
  }

  function checks(server) {
    function check(name, value, configured) {
      if (!server.checked) return '<span class="check">' + name + "</span>";
      const cls = value ? "ok" : configured === false ? "" : "err";
      return '<span class="check ' + cls + '">' + name + "</span>";
    }
    let out = "";
    if (server.checked && server.network_checked) {
      const ping = server.ping_ok ? (server.ping_ms || 1) + "ms" : (server.network_reachable ? "tcp" : "net down");
      const title = server.ping_ok
        ? "ICMP " + (server.ping_ms || 1) + "ms"
        : (server.network_error || (server.network_reachable ? "ICMP blocked, TCP reachable" : "network unreachable"));
      out += '<span class="check ' + (server.network_reachable ? "ok" : "err") + '" title="' + h(title) + '">' + h(ping) + "</span>";
      (server.ports || []).slice(0, 4).forEach(function (port) {
        const state = port.open ? "open" : (port.state || "closed");
        const cls = port.open ? "ok" : port.state === "filtered" ? "warn" : "err";
        out += '<span class="check ' + cls + '" title="port ' + port.port + " " + h(state) + '">' + port.port + "</span>";
      });
    }
    return out +
      check("metrics", server.metrics_ok, Boolean(server.metrics_url)) +
      check("health", server.health_ok, Boolean(server.health_url)) +
      check("ssh", server.machine_ok, server.ssh_checks !== false);
  }

  function renderKPIs() {
    const servers = state.servers;
    const ok = servers.filter(function (s) { return s.status === "ok"; }).length;
    const warn = servers.filter(function (s) { return s.status === "warn"; }).length;
    const down = servers.filter(function (s) { return s.status === "down"; }).length;
    const online = ok + warn;
    const unknown = servers.filter(function (s) { return s.status !== "ok" && s.status !== "warn" && s.status !== "down"; }).length;
    setKPI("total", helpers.fmtNum(servers.length), unknown ? unknown + " not checked yet" : "inventory");
    setKPI("ok", helpers.fmtNum(online), servers.length ? Math.round((online / servers.length) * 100) + "% reachable" : "—");
    setKPI("warn", helpers.fmtNum(warn), warn ? "degraded checks" : "no warnings");
    setKPI("down", helpers.fmtNum(down), down ? "unreachable" : "all reachable");
    document.querySelectorAll("#server-kpis .kpi-warn").forEach(function (node, index) {
      node.classList.toggle("hot", index === 1 ? down > 0 : warn > 0);
    });
  }

  function matches(server) {
    if (!state.filter) return true;
    const haystack = [server.name, server.env, server.host, server.user, server.source, (server.tags || []).join(" ")].join(" ").toLowerCase();
    return haystack.indexOf(state.filter) !== -1;
  }

  function renderServers() {
    const grid = el("servers-grid");
    const filtered = state.servers.filter(matches);
    el("server-count").textContent = helpers.fmtNum(filtered.length) + (state.filter ? " of " + helpers.fmtNum(state.servers.length) : "");
    if (!state.servers.length) {
      grid.innerHTML = '<div class="dash-empty">No servers configured. Add entries under <code>servers:</code> or <code>~/.ssh/config</code>.</div>';
      return;
    }
    if (!filtered.length) {
      grid.innerHTML = '<div class="dash-empty">No server matches this filter.</div>';
      return;
    }
    const head = '<div class="server-table-head" role="row">' +
      "<span>Server</span><span>Status</span><span>Env</span><span>Load</span><span>Memory</span><span>Disk</span><span>Checks</span><span></span>" +
      "</div>";
    grid.innerHTML = head + filtered.map(function (server) {
      const status = server.status || "unknown";
      const mem = pct(server.memory_used, server.memory_total);
      const disk = diskPct(server);
      const load = loadFirst(server);
      const loadTone = loadPct(server);
      const host = server.user ? server.user + "@" + (server.host || server.name) : (server.host || "");
      const flags = [];
      if (server.failed_units && server.failed_units.length) {
        flags.push(server.failed_units.length + " failed unit" + (server.failed_units.length > 1 ? "s" : ""));
      }
      if (server.error) flags.push("check error");
      if (!flags.length) flags.push(server.source || "inventory");
      const sub = flags.join(" · ") + (host ? " · " + host : "");
      const loadSub = (server.cpus ? server.cpus + " CPUs" : "no data") + (server.uptime ? " · " + server.uptime : "");
      return '<a class="server-row ' + status + '" href="/server?name=' + encodeURIComponent(server.name) + '" role="row" title="' + h([server.os, server.uptime].filter(Boolean).join(" · ")) + '">' +
        '<span class="server-row-name"><i class="server-status ' + status + '"></i><span><b>' + h(server.name) + "</b><small>" + h(sub) + "</small></span></span>" +
        '<span><span class="status-pill ' + status + '">' + statusLabel(status) + "</span></span>" +
        '<span class="server-env"><span class="badge">' + h(server.env || "—") + "</span></span>" +
        '<span class="server-metric server-load"><b>' + h(load || "—") + "</b><small>" + h(loadSub) + "</small>" + (server.cpus && load ? '<i class="' + tone(loadTone) + '" style="width:' + Math.max(2, loadTone) + '%"></i>' : "") + "</span>" +
        '<span class="server-metric"><b>' + (server.memory_total ? mem + "%" : "—") + "</b><small>" + (server.memory_total ? fmtBytes(server.memory_used) + " / " + fmtBytes(server.memory_total) : "no data") + "</small>" + (server.memory_total ? '<i class="' + tone(mem) + '" style="width:' + Math.max(2, mem) + '%"></i>' : "") + "</span>" +
        '<span class="server-metric"><b>' + (server.disk_total || server.disk_percent ? disk + "%" : "—") + "</b><small>" + (server.disk_total ? fmtBytes(server.disk_used) + " / " + fmtBytes(server.disk_total) : "no data") + "</small>" + (server.disk_total || server.disk_percent ? '<i class="' + tone(disk) + '" style="width:' + Math.max(2, disk) + '%"></i>' : "") + "</span>" +
        '<span class="server-checks">' + checks(server) + "</span>" +
        '<span class="server-open">Open</span></a>';
    }).join("");
  }

  function render(data) {
    state.servers = data.servers || [];
    renderKPIs();
    renderServers();
    el("server-updated").textContent = "updated " + new Date(data.checked_at || Date.now()).toLocaleTimeString();
    const errors = state.servers.filter(function (server) { return server.error; });
    const box = el("server-errors");
    if (errors.length) {
      box.hidden = false;
      box.innerHTML = "<details><summary>" + errors.length + " server check error" + (errors.length > 1 ? "s" : "") + "</summary><ul>" +
        errors.slice(0, 8).map(function (server) {
          return "<li><b>" + h(server.name) + "</b> — " + h(server.error) + "</li>";
        }).join("") + "</ul></details>";
    } else {
      box.hidden = true;
      box.innerHTML = "";
    }
  }

  async function load(force) {
    if (state.loading) return;
    state.loading = true;
    try {
      const response = await fetch("/api/dashboard/servers" + (force ? "?refresh=1" : ""), { headers: { Accept: "application/json" } });
      const data = await response.json().catch(function () { return {}; });
      if (!response.ok) throw new Error(data.error || "HTTP " + response.status);
      render(data);
    } catch (error) {
      el("server-updated").textContent = "error: " + (error.message || "checks unavailable");
      helpers.toast("Server refresh failed: " + (error.message || "error"), true);
    } finally {
      state.loading = false;
    }
  }

  function stop() {
    if (state.timer) clearTimeout(state.timer);
    state.timer = null;
  }

  function schedule() {
    stop();
    if (!state.live) return;
    state.timer = setTimeout(function () {
      load(false);
      schedule();
    }, 30000);
  }

  function setLive(value) {
    state.live = value;
    const button = el("server-live");
    button.classList.toggle("on", value);
    button.setAttribute("aria-pressed", value ? "true" : "false");
    if (value) schedule(); else stop();
  }

  el("server-filter").addEventListener("submit", function (event) {
    event.preventDefault();
    state.filter = el("server-filter-input").value.trim().toLowerCase();
    renderServers();
  });

  el("server-filter-input").addEventListener("input", function (event) {
    state.filter = event.target.value.trim().toLowerCase();
    renderServers();
  });

  el("server-refresh").addEventListener("click", function () { load(true); });
  el("server-live").addEventListener("click", function () { setLive(!state.live); });

  document.addEventListener("keydown", function (event) {
    if (event.key === "/" && !/^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName)) {
      event.preventDefault();
      el("server-filter-input").focus();
    }
  });

  const initialFilter = new URLSearchParams(window.location.search).get("q") || "";
  if (initialFilter) {
    state.filter = initialFilter.toLowerCase();
    el("server-filter-input").value = initialFilter;
  }

  load(false);
  schedule();
})();
