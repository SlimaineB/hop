(function () {
  "use strict";

  function isTyping() {
    const element = document.activeElement;
    if (!element) return false;
    const tag = element.tagName;
    return tag === "INPUT" || tag === "TEXTAREA" || element.isContentEditable;
  }

  function initTopology() {
    const holder = document.getElementById("topology-data");
    const graphEl = document.getElementById("topology-graph");
    const detailEl = document.getElementById("topology-detail");
    if (!holder || !graphEl || !detailEl) return;

    let graph;
    try {
      graph = JSON.parse(holder.textContent || "{}");
    } catch (_) {
      return;
    }
    const nodes = graph.nodes || [];
    const edges = graph.edges || [];
    if (nodes.length === 0) return;

    const escape = window.hop.escapeHTML;
    const byId = new Map();
    nodes.forEach(function (node) { byId.set(node.id, node); });

    const NODE_W = 188;
    const NODE_H = 76;
    const GAP_X = 78;
    const GAP_Y = 20;
    const MARGIN = 22;

    const incoming = new Map();
    const outgoing = new Map();
    nodes.forEach(function (node) {
      incoming.set(node.id, 0);
      outgoing.set(node.id, []);
    });
    edges.forEach(function (edge) {
      if (!byId.has(edge.from) || !byId.has(edge.to)) return;
      outgoing.get(edge.from).push(edge.to);
      incoming.set(edge.to, (incoming.get(edge.to) || 0) + 1);
    });

    const rank = new Map();
    const queue = [];
    nodes.forEach(function (node) {
      rank.set(node.id, 0);
      if ((incoming.get(node.id) || 0) === 0) queue.push(node.id);
    });
    const indegree = new Map(incoming);
    while (queue.length > 0) {
      const id = queue.shift();
      const nextRank = (rank.get(id) || 0) + 1;
      (outgoing.get(id) || []).forEach(function (child) {
        rank.set(child, Math.max(rank.get(child) || 0, nextRank));
        indegree.set(child, (indegree.get(child) || 0) - 1);
        if (indegree.get(child) === 0) queue.push(child);
      });
    }
    const fallback = {
      Ingress: 0, Deployment: 0, ReplicaSet: 1, StatefulSet: 0, DaemonSet: 0,
      Job: 0, CronJob: 0, PersistentVolumeClaim: 0,
      Service: 2, EndpointSlice: 3, Pod: 4, Node: 5
    };
    nodes.forEach(function (node) {
      if (!rank.has(node.id) || rank.get(node.id) === 0 && incoming.get(node.id) === 0 && (outgoing.get(node.id) || []).length === 0) {
        rank.set(node.id, fallback[node.kind] || 0);
      }
    });

    const groups = new Map();
    nodes.forEach(function (node) {
      const value = rank.get(node.id) || 0;
      if (!groups.has(value)) groups.set(value, []);
      groups.get(value).push(node);
    });
    const ranks = Array.from(groups.keys()).sort(function (a, b) { return a - b; });
    let maxRows = 0;
    ranks.forEach(function (value) {
      maxRows = Math.max(maxRows, groups.get(value).length);
    });
    const width = MARGIN * 2 + ranks.length * NODE_W + Math.max(0, ranks.length - 1) * GAP_X;
    const height = MARGIN * 2 + maxRows * NODE_H + Math.max(0, maxRows - 1) * GAP_Y;

    const positions = new Map();
    ranks.forEach(function (value, column) {
      const list = groups.get(value);
      const groupHeight = list.length * NODE_H + Math.max(0, list.length - 1) * GAP_Y;
      const y0 = MARGIN + Math.max(0, (height - MARGIN * 2 - groupHeight) / 2);
      list.forEach(function (node, row) {
        positions.set(node.id, {
          x: MARGIN + column * (NODE_W + GAP_X),
          y: y0 + row * (NODE_H + GAP_Y)
        });
      });
    });

    graphEl.innerHTML = "";
    graphEl.style.width = width + "px";
    graphEl.style.height = height + "px";

    const svgNS = "http://www.w3.org/2000/svg";
    const svg = document.createElementNS(svgNS, "svg");
    svg.setAttribute("class", "topology-edges");
    svg.setAttribute("width", String(width));
    svg.setAttribute("height", String(height));
    svg.setAttribute("viewBox", "0 0 " + width + " " + height);
    const defs = document.createElementNS(svgNS, "defs");
    defs.innerHTML = '<marker id="topology-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z"></path></marker>';
    svg.appendChild(defs);
    edges.forEach(function (edge) {
      const from = positions.get(edge.from);
      const to = positions.get(edge.to);
      if (!from || !to) return;
      const x1 = from.x + NODE_W;
      const y1 = from.y + NODE_H / 2;
      const x2 = to.x;
      const y2 = to.y + NODE_H / 2;
      const dx = Math.max(28, (x2 - x1) * 0.52);
      const path = document.createElementNS(svgNS, "path");
      path.setAttribute("d", "M " + x1 + " " + y1 + " C " + (x1 + dx) + " " + y1 + ", " + (x2 - dx) + " " + y2 + ", " + x2 + " " + y2);
      path.setAttribute("class", "topology-edge");
      path.setAttribute("marker-end", "url(#topology-arrow)");
      const title = document.createElementNS(svgNS, "title");
      title.textContent = edge.label || "";
      path.appendChild(title);
      svg.appendChild(path);
    });
    graphEl.appendChild(svg);

    function kindClass(kind) {
      const value = String(kind || "").toLowerCase();
      if (value === "endpointslice") return "endpoint";
      if (value === "persistentvolumeclaim") return "pvc";
      if (value === "service" || value === "ingress" || value === "pod" || value === "node") return value;
      return "controller";
    }

    function showDetail(node) {
      if (!node) return;
      graphEl.querySelectorAll(".topology-node").forEach(function (element) {
        element.classList.toggle("selected", element.getAttribute("data-id") === node.id);
      });
      const facts = (node.facts || []).map(function (fact) {
        return "<div><dt>" + escape(fact.key) + "</dt><dd>" + escape(fact.value || "—") + "</dd></div>";
      }).join("");
      detailEl.innerHTML =
        '<div class="topology-detail-head"><span class="topology-kind kind-' + kindClass(node.kind) + '">' + escape(node.kind) + '</span></div>' +
        "<h3>" + escape(node.name) + "</h3>" +
        (node.subtitle ? '<p class="muted">' + escape(node.subtitle) + "</p>" : "") +
        (node.status ? '<span class="pill">' + escape(node.status) + "</span>" : "") +
        (facts ? '<dl class="topology-facts">' + facts + "</dl>" : "") +
        (node.command ? '<div class="topology-command"><code>' + escape(node.command) + "</code>" +
          '<button class="icon-btn" type="button" data-copy="' + escape(node.command) + '" title="Copy kubectl command" aria-label="Copy kubectl command">' +
          '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="11" height="11" rx="2"></rect><path d="M5 15V5a2 2 0 0 1 2-2h10"></path></svg>' +
          "</button></div>" : "");
    }

    nodes.forEach(function (node) {
      const position = positions.get(node.id);
      if (!position) return;
      const button = document.createElement("button");
      button.type = "button";
      button.className = "topology-node kind-" + kindClass(node.kind);
      button.setAttribute("data-id", node.id);
      button.style.left = position.x + "px";
      button.style.top = position.y + "px";
      button.style.width = NODE_W + "px";
      button.style.height = NODE_H + "px";
      button.innerHTML =
        '<span class="topology-kind">' + escape(node.kind) + "</span>" +
        '<span class="topology-name">' + escape(node.name) + "</span>" +
        (node.status ? '<span class="topology-status">' + escape(node.status) + "</span>" : "");
      button.addEventListener("click", function () { showDetail(node); });
      graphEl.appendChild(button);
    });

    const podNode = nodes.find(function (node) { return node.kind === "Pod"; });
    showDetail(podNode || nodes[0]);
  }

  function initLiveMetrics() {
    const root = document.querySelector("[data-live-metrics]");
    if (!root) return;
    const body = root.querySelector("[data-live-body]");
    const source = root.querySelector("[data-live-source]");
    const note = root.querySelector("[data-live-note]");
    const refreshButton = root.querySelector("[data-live-refresh]");
    if (!body) return;

    const params = new URLSearchParams();
    params.set("context", root.dataset.context || "");
    params.set("namespace", root.dataset.namespace || "");
    params.set("name", root.dataset.name || "");
    const endpoint = "/api/pod/stats?" + params.toString();
    const esc = window.hop.escapeHTML;
    let previous = null;
    let loading = false;

    function num(value) { return Number(value || 0); }

    function fmtBytes(value) {
      const bytes = num(value);
      if (bytes <= 0) return "0 B";
      const units = ["B", "KiB", "MiB", "GiB", "TiB"];
      let size = bytes;
      let unit = 0;
      while (size >= 1024 && unit < units.length - 1) {
        size /= 1024;
        unit++;
      }
      return (size >= 100 ? size.toFixed(0) : size >= 10 ? size.toFixed(1) : size.toFixed(2)) + " " + units[unit];
    }

    function fmtCPU(value) {
      const millis = num(value);
      if (millis >= 1000) return (millis / 1000).toFixed(2) + "c";
      return Math.round(millis) + "m";
    }

    function fmtRate(value) {
      return fmtBytes(value) + "/s";
    }

    function percent(used, total) {
      if (!total) return 0;
      return Math.max(0, Math.min(100, (used / total) * 100));
    }

    function tone(value) {
      if (value >= 90) return "err";
      if (value >= 75) return "warn";
      return "ok";
    }

    function bar(value) {
      const pct = Math.max(0, Math.min(100, value));
      return '<span class="live-bar"><i class="' + tone(pct) + '" style="width:' + pct.toFixed(1) + '%"></i></span>';
    }

    function metric(label, value, sub, pct) {
      return '<div class="live-metric">' +
        '<span class="live-label">' + esc(label) + '</span>' +
        '<span class="live-value">' + esc(value) + '</span>' +
        bar(pct) +
        '<span class="live-sub">' + esc(sub || "") + '</span>' +
        '</div>';
    }

    function readyPill(node) {
      if (!node) return "";
      if (!node.ready_known) return '<span class="pill muted">state unknown</span>';
      return node.ready ? '<span class="pill ok">Ready</span>' : '<span class="pill err">NotReady</span>';
    }

    function render(stats) {
      stats = stats || {};
      const fetched = stats.fetched_at ? new Date(stats.fetched_at).getTime() : Date.now();
      const at = Number.isFinite(fetched) ? fetched : Date.now();
      const elapsed = previous ? Math.max(0.1, (at - previous.at) / 1000) : 0;

      const network = stats.network || {};
      const rx = num(network.rx_bytes);
      const tx = num(network.tx_bytes);
      const rxRate = previous ? Math.max(0, rx - previous.rx) / elapsed : 0;
      const txRate = previous ? Math.max(0, tx - previous.tx) / elapsed : 0;

      const node = stats.node || null;
      const nodeNetwork = node && node.network ? node.network : {};
      const nodeRx = num(nodeNetwork.rx_bytes);
      const nodeTx = num(nodeNetwork.tx_bytes);
      const nodeRxRate = previous && previous.nodeRx !== null ? Math.max(0, nodeRx - previous.nodeRx) / elapsed : 0;
      const nodeTxRate = previous && previous.nodeTx !== null ? Math.max(0, nodeTx - previous.nodeTx) / elapsed : 0;

      const cpuLimit = num(root.dataset.cpuLimit) || (node ? num(node.cpu_capacity_millicores) : 0);
      const memLimit = num(root.dataset.memoryLimit) || (node ? num(node.memory_capacity_bytes) : 0);
      const cpuPct = percent(num(stats.cpu_millicores), cpuLimit);
      const memPct = percent(num(stats.memory_bytes), memLimit);
      const disk = stats.disk || {};
      const diskPct = percent(num(disk.used_bytes), num(disk.capacity_bytes));

      let html = '<div class="live-grid">';
      html += metric("CPU", fmtCPU(stats.cpu_millicores), cpuLimit ? "of " + fmtCPU(cpuLimit) + " limit" : "current usage", cpuPct);
      html += metric("MEMORY", fmtBytes(stats.memory_bytes), memLimit ? "of " + fmtBytes(memLimit) + " limit" : "current usage", memPct);
      html += metric("DISK", fmtBytes(disk.used_bytes), disk.capacity_bytes ? "of " + fmtBytes(disk.capacity_bytes) + " ephemeral" : "ephemeral storage", diskPct);
      html += '<div class="live-metric live-network">' +
        '<span class="live-label">NETWORK</span>' +
        '<span class="live-value">↓ ' + esc(fmtRate(rxRate)) + '</span>' +
        '<span class="live-sub">↑ ' + esc(fmtRate(txRate)) + '</span>' +
        '<span class="live-sub muted">total ↓ ' + esc(fmtBytes(rx)) + ' · ↑ ' + esc(fmtBytes(tx)) + '</span>' +
        '</div>';
      html += "</div>";

      const requestParts = [];
      if (num(root.dataset.cpuRequest) || num(root.dataset.memoryRequest)) {
        requestParts.push("requests " + fmtCPU(root.dataset.cpuRequest) + " cpu · " + fmtBytes(root.dataset.memoryRequest) + " ram");
      }
      if (num(stats.working_set_bytes)) {
        requestParts.push("working set " + fmtBytes(stats.working_set_bytes));
      }
      if (stats.window) {
        requestParts.push("window " + stats.window);
      }
      if (requestParts.length) {
        html += '<div class="live-detail">' + esc(requestParts.join("  ·  ")) + '</div>';
      }

      if (Array.isArray(stats.containers) && stats.containers.length) {
        html += '<div class="live-section"><div class="live-section-head"><span class="live-section-title">Containers</span></div>';
        html += '<div class="live-rows">';
        stats.containers.forEach(function (container) {
          const rootfs = container.rootfs || {};
          const logs = container.logs || {};
          const diskParts = [];
          if (num(rootfs.used_bytes)) diskParts.push("rootfs " + fmtBytes(rootfs.used_bytes));
          if (num(logs.used_bytes)) diskParts.push("logs " + fmtBytes(logs.used_bytes));
          html += '<div class="live-row">' +
            '<span class="live-row-name">' + esc(container.name || "—") + '</span>' +
            '<span>' + esc(fmtCPU(container.cpu_millicores)) + ' cpu</span>' +
            '<span>' + esc(fmtBytes(container.memory_bytes)) + ' ram</span>' +
            '<span class="muted">' + esc(diskParts.join(" · ") || "—") + '</span>' +
            '</div>';
        });
        html += "</div></div>";
      }

      if (node) {
        html += '<div class="live-section"><div class="live-section-head"><span class="live-section-title">Node</span>' +
          '<span class="live-node-name">' + esc(node.name || "—") + " " + readyPill(node) + '</span></div>';
        html += '<div class="live-grid live-grid-node">';
        html += metric("CPU", fmtCPU(node.cpu_millicores), node.cpu_capacity_millicores ? "of " + fmtCPU(node.cpu_capacity_millicores) + " allocatable" : "node usage", percent(num(node.cpu_millicores), num(node.cpu_capacity_millicores)));
        html += metric("MEMORY", fmtBytes(node.memory_bytes), node.memory_capacity_bytes ? "of " + fmtBytes(node.memory_capacity_bytes) + " allocatable" : "node usage", percent(num(node.memory_bytes), num(node.memory_capacity_bytes)));
        if (num(node.disk && node.disk.capacity_bytes)) {
          html += metric("DISK", fmtBytes(node.disk.used_bytes), "of " + fmtBytes(node.disk.capacity_bytes) + " node fs", percent(num(node.disk.used_bytes), num(node.disk.capacity_bytes)));
        }
        html += '<div class="live-metric live-network">' +
          '<span class="live-label">NETWORK</span>' +
          '<span class="live-value">↓ ' + esc(fmtRate(nodeRxRate)) + '</span>' +
          '<span class="live-sub">↑ ' + esc(fmtRate(nodeTxRate)) + '</span>' +
          '<span class="live-sub muted">total ↓ ' + esc(fmtBytes(nodeRx)) + ' · ↑ ' + esc(fmtBytes(nodeTx)) + '</span>' +
          '</div>';
        html += "</div>";
        const nodeParts = [];
        if (num(node.memory_available_bytes)) nodeParts.push("memory available " + fmtBytes(node.memory_available_bytes));
        if (num(node.working_set_bytes)) nodeParts.push("working set " + fmtBytes(node.working_set_bytes));
        if (num(node.disk && node.disk.available_bytes)) nodeParts.push("disk available " + fmtBytes(node.disk.available_bytes));
        if (num(node.disk && node.disk.inodes)) nodeParts.push("inodes " + window.hop.fmtNum(node.disk.inodes_used) + "/" + window.hop.fmtNum(node.disk.inodes));
        if (nodeParts.length) html += '<div class="live-detail">' + esc(nodeParts.join("  ·  ")) + '</div>';
        html += "</div>";
      }

      if (stats.source || stats.fetched_at) {
        const age = stats.fetched_at ? " · " + window.hop.fmtAge(stats.fetched_at) + " ago" : "";
        source.className = "pill ok";
        source.textContent = (stats.source || "metrics") + age;
      }

      const warnings = Array.isArray(stats.warnings) ? stats.warnings.filter(Boolean) : [];
      note.textContent = warnings.length ? "▲ " + warnings.join("  ·  ") : "";
      note.hidden = warnings.length === 0;

      body.innerHTML = html;
      previous = { at: at, rx: rx, tx: tx, nodeRx: node ? nodeRx : null, nodeTx: node ? nodeTx : null };
    }

    async function refresh() {
      if (loading) return;
      loading = true;
      if (refreshButton) refreshButton.classList.add("spinning");
      try {
        const response = await fetch(endpoint, { cache: "no-store", headers: { Accept: "application/json" } });
        if (!response.ok) {
          let message = "HTTP " + response.status;
          try {
            const payload = await response.json();
            if (payload && payload.error) message = payload.error;
          } catch (_) {}
          throw new Error(message);
        }
        render(await response.json());
      } catch (error) {
        source.className = "pill err";
        source.textContent = "unavailable";
        body.innerHTML = '<div class="error-panel small">Unable to load live metrics: ' + esc(error.message || "unknown error") + '</div>';
        note.hidden = true;
        previous = null;
      } finally {
        loading = false;
        if (refreshButton) refreshButton.classList.remove("spinning");
      }
    }

    if (refreshButton) {
      refreshButton.addEventListener("click", refresh);
    }
    document.addEventListener("visibilitychange", function () {
      if (!document.hidden) refresh();
    });
    refresh();
    setInterval(function () {
      if (!document.hidden) refresh();
    }, 10000);
  }

  function initPodLogs() {
    const root = document.querySelector("[data-pod-logs]");
    if (!root) return;
    const body = root.querySelector("[data-logs-body]");
    const meta = root.querySelector("[data-logs-meta]");
    const note = root.querySelector("[data-logs-note]");
    const refreshButton = root.querySelector("[data-logs-refresh]");
    if (!body) return;

    const params = new URLSearchParams();
    params.set("context", root.dataset.context || "");
    params.set("namespace", root.dataset.namespace || "");
    params.set("name", root.dataset.name || "");
    if (root.dataset.query) params.set("q", root.dataset.query);
    const endpoint = "/api/pod/logs?" + params.toString();
    const esc = window.hop.escapeHTML;
    let loaded = false;
    let loading = false;

    function lineTone(line) {
      const value = line.toLowerCase();
      if (/error|panic|fatal|exception|failed/.test(value)) return "err";
      if (/warn/.test(value)) return "warn";
      if (/info|ready|started/.test(value)) return "info";
      return "";
    }

    function render(payload) {
      const text = String(payload.logs || "")
        .replace(/\x1b\[[0-9;]*[a-zA-Z]/g, "")
        .replace(/\s+$/, "");
      body.innerHTML = "";
      if (!text) {
        body.innerHTML = '<p class="muted small">No logs returned.</p>';
      } else {
        text.split(/\r?\n/).forEach(function (line) {
          const row = document.createElement("div");
          row.className = "log-line" + (lineTone(line) ? " " + lineTone(line) : "");
          row.textContent = line || " ";
          body.appendChild(row);
        });
      }
      if (meta) {
        const range = payload.range ? payload.range + " · " : "";
        meta.className = "pill ok";
        meta.textContent = range + (payload.at ? window.hop.fmtAge(payload.at) + " ago" : "loaded");
      }
      if (note) note.hidden = true;
      body.scrollTop = body.scrollHeight;
      loaded = true;
    }

    async function load(force) {
      if (loading || (loaded && !force)) return;
      loading = true;
      if (refreshButton) refreshButton.classList.add("spinning");
      if (meta) {
        meta.className = "pill muted";
        meta.textContent = "loading";
      }
      try {
        const response = await fetch(endpoint, { cache: "no-store", headers: { Accept: "application/json" } });
        if (!response.ok) {
          let message = "HTTP " + response.status;
          try {
            const payload = await response.json();
            if (payload && payload.error) message = payload.error;
          } catch (_) {}
          throw new Error(message);
        }
        render(await response.json());
      } catch (error) {
        if (meta) {
          meta.className = "pill err";
          meta.textContent = "unavailable";
        }
        body.innerHTML = '<div class="error-panel small">Unable to load logs: ' + esc(error.message || "unknown error") + "</div>";
        if (note) note.hidden = true;
        loaded = false;
      } finally {
        loading = false;
        if (refreshButton) refreshButton.classList.remove("spinning");
      }
    }

    if (refreshButton) refreshButton.addEventListener("click", function () { load(true); });
    const tabsRoot = document.querySelector(".col-main[data-tabs]");
    function maybeLoad() {
      if (tabsRoot && tabsRoot.getAttribute("data-active-tab") === "logs") load(false);
    }
    if (tabsRoot && window.MutationObserver) {
      new MutationObserver(maybeLoad).observe(tabsRoot, { attributes: true, attributeFilter: ["data-active-tab"] });
    }
    maybeLoad();
  }

  document.addEventListener("keydown", function (event) {
    if (event.metaKey || event.ctrlKey || event.altKey || isTyping()) return;
    if (event.key === "g") {
      const link = document.querySelector('.pod-quick a[href]');
      if (link) {
        event.preventDefault();
        link.click();
      }
      return;
    }
    if (event.key === "l") {
      const links = document.querySelectorAll('.pod-quick a[href]');
      const loki = Array.prototype.find.call(links, function (link) {
        return /loki/i.test(link.textContent || "");
      });
      if (loki) {
        event.preventDefault();
        loki.click();
      }
      return;
    }
    if (event.key === "k") {
      const button = document.querySelector('.pod-quick [data-copy]');
      if (button) {
        event.preventDefault();
        button.click();
      }
    }
  });

  initTopology();
  initLiveMetrics();
  initPodLogs();
})();
