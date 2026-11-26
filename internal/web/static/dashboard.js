(function () {
  "use strict";

  const h = window.hop.escapeHTML;
  const helpers = window.hop;

  const state = {
    overview: null,
    group: "env",
    live: true,
    timer: null,
    loading: false,
    history: new Map()
  };

  const el = function (id) { return document.getElementById(id); };

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

  function pct(used, total) {
    if (!total) return 0;
    return Math.max(0, Math.min(100, Math.round((used / total) * 100)));
  }

  function tone(value) {
    if (value >= 90) return "err";
    if (value >= 70) return "warn";
    return "ok";
  }

  function sparkline(history) {
    const points = history.slice(-32);
    if (points.length < 2) {
      return '<svg class="sparkline" viewBox="0 0 120 30" preserveAspectRatio="none" aria-hidden="true"><path d="M0 24 L120 24" class="spark-empty"/></svg>';
    }
    const max = Math.max.apply(null, points.concat([1]));
    const step = 120 / (points.length - 1);
    const path = points.map(function (value, index) {
      const x = (index * step).toFixed(1);
      const y = (27 - Math.min(1, value / max) * 24).toFixed(1);
      return (index === 0 ? "M" : "L") + x + " " + y;
    }).join(" ");
    return '<svg class="sparkline" viewBox="0 0 120 30" preserveAspectRatio="none" aria-hidden="true">' +
      '<path d="' + path + ' L120 30 L0 30 Z" class="spark-area"/><path d="' + path + '" class="spark-line"/></svg>';
  }

  function remember(key, bucket) {
    const series = state.history.get(key) || [];
    series.push(bucket.cpu_millicores || 0);
    while (series.length > 40) series.shift();
    state.history.set(key, series);
    return series;
  }

  function setKPI(name, value, sub) {
    const target = document.querySelector('[data-kpi="' + name + '"]');
    if (target) target.textContent = value;
    if (sub != null) {
      const subtitle = document.querySelector('[data-kpi="' + name + '_sub"]');
      if (subtitle) subtitle.textContent = sub;
    }
  }

  function setBar(name, value) {
    const bar = document.querySelector('[data-kpi-bar="' + name + '"]');
    if (!bar) return;
    bar.style.width = Math.max(0, Math.min(100, value || 0)) + "%";
    bar.className = tone(value);
  }

  function renderKPIs(data) {
    const totals = data.totals || {};
    const pods = Number(totals.pods || 0);
    const ready = Number(totals.ready || 0);
    const completed = Number(totals.completed || 0);
    const healthy = ready + completed;
    const attention = Number(data.issue_total != null ? data.issue_total : (data.issues || []).length);
    const cpuPct = pct(totals.cpu_millicores, totals.cpu_capacity_millicores);
    const memPct = pct(totals.memory_bytes, totals.memory_capacity_bytes);
    setKPI("pods", helpers.fmtNum(pods), totals.contexts + " ctx · " + totals.namespaces + " ns");
    setKPI("ready", pods ? Math.round((healthy / pods) * 100) + "%" : "—", helpers.fmtNum(ready) + " ready" + (completed ? " · " + helpers.fmtNum(completed) + " completed" : ""));
    setKPI("bad", helpers.fmtNum(attention), attention ? "requires triage" : "all clear");
    setKPI("restarts", helpers.fmtNum(totals.restarts || 0));
    setKPI("cpu", totals.metrics_available ? fmtCPU(totals.cpu_millicores) : "n/a",
      totals.cpu_capacity_millicores ? cpuPct + "% of " + fmtCPU(totals.cpu_capacity_millicores) : "no metrics-server data");
    setKPI("memory", totals.metrics_available ? fmtBytes(totals.memory_bytes) : "n/a",
      totals.memory_capacity_bytes ? memPct + "% of " + fmtBytes(totals.memory_capacity_bytes) : "no metrics-server data");
    setKPI("apps", helpers.fmtNum(totals.apps || 0), "across the fleet");
    setKPI("nodes", helpers.fmtNum(totals.nodes || 0), "in fleet");
    setBar("cpu", cpuPct);
    setBar("memory", memPct);
    document.querySelectorAll(".kpi-warn").forEach(function (node) {
      node.classList.toggle("hot", attention > 0);
    });
    const errors = data.errors || [];
    const errorBox = el("dash-errors");
    if (errors.length) {
      errorBox.hidden = false;
      errorBox.innerHTML = "<details><summary>" + errors.length + " source warning" +
        (errors.length > 1 ? "s" : "") + "</summary><ul>" + errors.map(function (item) {
          return "<li>" + h(item) + "</li>";
        }).join("") + "</ul></details>";
    } else {
      errorBox.hidden = true;
      errorBox.innerHTML = "";
    }
  }

  function groupLabel(group) {
    return { env: "environments", app: "applications", cluster: "clusters", namespace: "namespaces" }[group] || "groups";
  }

  function searchFor(group, key) {
    if (group === "namespace") return "/search?q=" + encodeURIComponent("ns:" + key);
    if (group === "cluster") return "/search?q=" + encodeURIComponent("ctx:" + key);
    if (group === "env") return "/search?q=" + encodeURIComponent("env:" + key);
    return "/search?q=" + encodeURIComponent(key);
  }

  function renderBuckets(data, record) {
    const grid = el("group-grid");
    const buckets = (data.groups && data.groups[state.group]) || [];
    el("group-kicker").textContent = groupLabel(state.group);
    el("group-count").textContent = helpers.fmtNum(buckets.length);
    if (!buckets.length) {
      grid.innerHTML = '<div class="dash-empty">No pods found in this grouping.</div>';
      return;
    }
    let maxCPU = 1;
    let maxMem = 1;
    buckets.forEach(function (bucket) {
      maxCPU = Math.max(maxCPU, bucket.cpu_millicores || 0);
      maxMem = Math.max(maxMem, bucket.memory_bytes || 0);
    });
    const head = '<div class="fleet-table-head" role="row">' +
      '<span>Group</span><span>Pods</span><span>Health</span><span>CPU</span><span>Memory</span><span>Restarts</span><span>Trend</span>' +
      "</div>";
    grid.innerHTML = head + buckets.map(function (bucket) {
      const pods = Number(bucket.pods || 0);
      const ready = Number(bucket.ready || 0);
      const completed = Number(bucket.completed || 0);
      const healthy = ready + completed;
      const bad = Number(bucket.not_ready || 0) + Number(bucket.failed || 0) + Number(bucket.pending || 0);
      const health = pods ? Math.round((healthy / pods) * 100) : 0;
      const historyKey = state.group + ":" + bucket.key;
      const series = record ? remember(historyKey, bucket) : (state.history.get(historyKey) || []);
      const meta = state.group === "app"
        ? bucket.namespaces + " ns · " + bucket.contexts + " ctx"
        : bucket.apps + " apps · " + bucket.namespaces + " ns";
      const cpuWidth = pct(bucket.cpu_millicores, maxCPU);
      const memWidth = pct(bucket.memory_bytes, maxMem);
      const readyWidth = pods ? (ready / pods) * 100 : 0;
      const doneWidth = pods ? (completed / pods) * 100 : 0;
      const badWidth = pods ? Math.min(100 - readyWidth - doneWidth, (bad / pods) * 100) : 0;
      const dot = bad ? "bad" : health === 100 ? "ok" : "warn";
      return '<a class="fleet-row" href="' + h(searchFor(state.group, bucket.key)) + '" role="row">' +
        '<span class="fleet-name"><i class="fleet-dot ' + dot + '"></i><span><b>' + h(bucket.title) + "</b><small>" + h(meta) + "</small></span></span>" +
        '<span class="fleet-pods"><b>' + helpers.fmtNum(pods) + "</b><small>" + helpers.fmtNum(ready) + " ready" + (completed ? " · " + helpers.fmtNum(completed) + " done" : "") + "</small></span>" +
        '<span class="fleet-health"><span class="fleet-health-bar"><i class="ok" style="width:' + readyWidth + '%"></i><i class="done" style="width:' + doneWidth + '%"></i><i class="err" style="width:' + badWidth + '%"></i></span><b>' + health + "%</b></span>" +
        '<span class="fleet-metric"><b>' + fmtCPU(bucket.cpu_millicores) + '</b><i class="' + tone(cpuWidth) + '" style="width:' + Math.max(2, cpuWidth) + '%"></i></span>' +
        '<span class="fleet-metric"><b>' + fmtBytes(bucket.memory_bytes) + '</b><i class="' + tone(memWidth) + '" style="width:' + Math.max(2, memWidth) + '%"></i></span>' +
        '<span class="fleet-restarts"><b>' + helpers.fmtNum(bucket.restarts || 0) + "</b><small>" + (bucket.restarts ? "restarts" : "clean") + "</small></span>" +
        '<span class="fleet-trend">' + sparkline(series) + "</span></a>";
    }).join("");
  }

  function renderIssues(data) {
    const list = el("issues-list");
    const issues = data.issues || [];
    el("issue-count").textContent = helpers.fmtNum(data.issue_total != null ? data.issue_total : issues.length);
    if (!issues.length) {
      list.innerHTML = '<div class="dash-empty">Nothing needs attention. Nice.</div>';
      return;
    }
    const shown = issues.slice(0, 3);
    list.innerHTML = shown.map(function (issue) {
      const status = issue.reason || issue.phase || "Unknown";
      const href = "/pod?context=" + encodeURIComponent(issue.context) +
        "&namespace=" + encodeURIComponent(issue.namespace) +
        "&name=" + encodeURIComponent(issue.name);
      return '<a class="issue" href="' + h(href) + '">' +
        '<span class="issue-dot ' + (issue.ready ? "warn" : "err") + '"></span>' +
        '<div class="issue-main"><span class="issue-name">' + h(issue.name) + "</span>" +
        '<span class="issue-sub">' + h(issue.app || "—") + " · " + h(issue.namespace) + "</span></div>" +
        '<div class="issue-meta">' + (issue.env && issue.env !== "—" ? '<span class="badge">' + h(issue.env) + "</span>" : "") +
        '<span class="badge mono">' + h(status) + "</span>" +
        (issue.restarts ? '<span class="badge warn">' + helpers.fmtNum(issue.restarts) + " restarts</span>" : "") +
        '<span class="muted">' + h(issue.age || "") + "</span></div></a>";
    }).join("") + (Number(data.issue_total || issues.length) > shown.length
      ? '<div class="panel-note">Top ' + shown.length + " of " + helpers.fmtNum(data.issue_total || issues.length) + " by severity</div>"
      : "");
  }

  function renderNodes(data) {
    const list = el("nodes-list");
    const nodes = data.nodes || [];
    el("node-count").textContent = helpers.fmtNum(nodes.length);
    if (!nodes.length) {
      list.innerHTML = '<div class="dash-empty">No nodes reported.</div>';
      return;
    }
    const shown = nodes.slice(0, 4);
    list.innerHTML = shown.map(function (node) {
      const cpu = pct(node.cpu_millicores, node.cpu_capacity_millicores);
      const mem = pct(node.memory_bytes, node.memory_capacity_bytes);
      return '<div class="node-row">' +
        '<span class="node-dot ' + (node.ready ? "ok" : (node.cpu_capacity_millicores || node.memory_capacity_bytes ? "err" : "")) + '"></span>' +
        '<div class="node-name"><b>' + h(node.name) + "</b><span>" + h(node.context) + " · " + h(node.pods) + " pods</span></div>" +
        '<div class="node-bars">' +
        '<div class="meter small"><span>CPU</span><div class="meter-track"><i class="' + tone(cpu) + '" style="width:' + cpu + '%"></i></div><b>' + (node.cpu_capacity_millicores ? cpu + "%" : "—") + "</b></div>" +
        '<div class="meter small"><span>MEM</span><div class="meter-track"><i class="' + tone(mem) + '" style="width:' + mem + '%"></i></div><b>' + (node.memory_capacity_bytes ? mem + "%" : "—") + "</b></div>" +
        "</div></div>";
    }).join("") + (nodes.length > shown.length
      ? '<div class="panel-note">Top ' + shown.length + " of " + helpers.fmtNum(nodes.length) + " nodes</div>"
      : "");
  }


  function render(data) {
    state.overview = data;
    renderKPIs(data);
    renderBuckets(data, true);
    renderIssues(data);
    renderNodes(data);
    const when = data.fetched_at ? new Date(data.fetched_at) : new Date();
    el("dash-updated").textContent = "updated " + when.toLocaleTimeString() +
      " · " + helpers.fmtDuration((data.duration || 0) / 1e6);
  }

  async function load(force) {
    if (state.loading) return;
    state.loading = true;
    try {
      const response = await fetch("/api/dashboard" + (force ? "?refresh=1" : ""), { headers: { Accept: "application/json" } });
      const data = await response.json().catch(function () { return {}; });
      if (!response.ok) throw new Error(data.error || "HTTP " + response.status);
      render(data);
    } catch (error) {
      el("dash-updated").textContent = "error: " + (error.message || "dashboard unavailable");
      helpers.toast("Dashboard refresh failed: " + (error.message || "error"), true);
    } finally {
      state.loading = false;
    }
  }


  function schedule() {
    stop();
    if (!state.live) return;
    state.timer = setTimeout(function () {
      load(false);
      schedule();
    }, 10000);
  }

  function stop() {
    if (state.timer) clearTimeout(state.timer);
    state.timer = null;
  }

  function setLive(value) {
    state.live = value;
    const button = el("dash-live");
    button.classList.toggle("on", value);
    button.setAttribute("aria-pressed", value ? "true" : "false");
    if (value) schedule(); else stop();
  }

  document.getElementById("group-tabs").addEventListener("click", function (event) {
    const button = event.target.closest("[data-group]");
    if (!button) return;
    state.group = button.getAttribute("data-group");
    document.querySelectorAll("#group-tabs [data-group]").forEach(function (node) {
      const active = node === button;
      node.classList.toggle("on", active);
      node.setAttribute("aria-selected", active ? "true" : "false");
    });
    if (state.overview) renderBuckets(state.overview, false);
  });

  el("dash-refresh").addEventListener("click", function () { load(true); });
  el("dash-live").addEventListener("click", function () { setLive(!state.live); });

  el("dash-search").addEventListener("submit", function (event) {
    event.preventDefault();
    const query = el("dash-search-input").value.trim();
    window.location.href = "/search" + (query ? "?q=" + encodeURIComponent(query) : "");
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "/" && !/^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName)) {
      event.preventDefault();
      el("dash-search-input").focus();
    }
  });

  load(false);
  schedule();
})();
