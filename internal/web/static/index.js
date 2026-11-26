(function () {
  "use strict";

  const tagInput = document.getElementById("tag-input");
  if (!tagInput) return;

  const helpers = window.hop;
  const h = helpers.escapeHTML;
  const tagsEl = document.getElementById("tags");
  const input = document.getElementById("search-input");
  const searchBtn = document.getElementById("search-btn");
  const refreshBtn = document.getElementById("refresh-btn");
  const resultsBody = document.getElementById("results-body");
  const resultCount = document.getElementById("result-count");
  const metaBar = document.getElementById("meta-bar");
  const contextPills = document.getElementById("context-pills");
  const statsEl = document.getElementById("stats");

  const FILTER_PREFIXES = new Set([
    "pod", "name", "ns", "namespace", "ctx", "context", "cluster", "env", "environment",
    "owner", "workload", "deploy", "node", "label", "labels", "lbl", "container",
    "image", "phase", "status", "server", "servers", "srv", "tag", "tags", "host",
    "hostname", "user", "ssh"
  ]);
  const VM_SELECTORS = new Set(["vm", "vms"]);
  const TIME_CLOCK = /^\d{1,2}(?::\d{2}|h\d{0,2})$/;
  const TIME_RELATIVE = /^-\d+(?:\.\d+)?[smhdw]$/;
  const TIME_ISO = /^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2})?)?$/;
  const TIME_DAY_MONTH = /^\d{1,2}\/\d{1,2}(?:\/\d{2,4})?$/;
  const ENV_COLORS = {
    prod: "#fb7185",
    preprod: "#fbbf24",
    staging: "#fbbf24",
    dev: "#34d399",
    test: "#34d399",
    sandbox: "#a78bfa",
    local: "#67e8f9"
  };

  const state = {
    tags: [],
    lastResult: null,
    controller: null,
    searching: false
  };

  function isFilter(tag) {
    const index = tag.indexOf(":");
    if (index <= 0) return false;
    return FILTER_PREFIXES.has(tag.slice(0, index).toLowerCase());
  }

  function isVMQuery(query) {
    return String(query || "").split(/\s+/).some(function (tag) {
      return VM_SELECTORS.has(tag.toLowerCase().replace(/^['"]|['"]$/g, ""));
    });
  }

  function setMode(mode) {
    const serverMode = mode === "servers";
    if (statsEl) statsEl.hidden = serverMode;
    if (serverMode) {
      contextPills.hidden = true;
      contextPills.innerHTML = "";
    }
  }

  function isTimeTag(tag) {
    const value = String(tag || "").toLowerCase();
    if (/^(from|to|since|until|after|before):/.test(value)) return true;
    return value === "now" || value === "today" || value === "yesterday" ||
      value.indexOf("last:") === 0 ||
      TIME_CLOCK.test(value) || TIME_RELATIVE.test(value) ||
      TIME_ISO.test(value) || TIME_DAY_MONTH.test(value);
  }

  function addTag(raw) {
    const text = String(raw || "").trim();
    if (!text) return;
    text.split(/\s+/).filter(Boolean).forEach(function (part) {
      const exists = state.tags.some(function (tag) {
        return tag.toLowerCase() === part.toLowerCase();
      });
      if (!exists) state.tags.push(part);
    });
    renderTags();
  }

  function removeTag(index) {
    state.tags.splice(index, 1);
    renderTags();
  }

  function renderTags() {
    tagsEl.innerHTML = "";
    state.tags.forEach(function (tag, index) {
      const element = document.createElement("span");
      const timeTag = isTimeTag(tag);
      element.className = "tag" + (isFilter(tag) ? " tag-filter" : "") + (timeTag ? " tag-time" : "");
      if (isFilter(tag)) {
        const indexColon = tag.indexOf(":");
        const key = tag.slice(0, indexColon);
        const value = tag.slice(indexColon + 1);
        element.innerHTML = "<b>" + h(key) + "</b>:" + h(value);
      } else {
        element.textContent = tag;
      }
      if (timeTag) {
        element.title = "Logs and metrics range";
      }
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "tag-remove";
      remove.setAttribute("aria-label", "Remove " + tag);
      remove.textContent = "×";
      remove.addEventListener("click", function (event) {
        event.stopPropagation();
        removeTag(index);
        runSearch();
      });
      element.appendChild(remove);
      tagsEl.appendChild(element);
    });
  }

  function commitInput() {
    if (input.value.trim() !== "") {
      addTag(input.value);
      input.value = "";
    }
  }

  function queryString() {
    return state.tags.join(" ");
  }

  function focusInput() {
    input.focus();
  }

  input.addEventListener("keydown", function (event) {
    if (event.key === " " || event.key === "Tab") {
      if (input.value.trim() !== "") {
        event.preventDefault();
        commitInput();
      } else if (event.key === "Tab") {
        event.preventDefault();
        focusInput();
      }
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      commitInput();
      runSearch();
      return;
    }
    if (event.key === "Backspace" && input.value === "" && state.tags.length > 0) {
      event.preventDefault();
      removeTag(state.tags.length - 1);
      return;
    }
    if (event.key === "Escape") {
      input.value = "";
    }
  });

  input.addEventListener("paste", function (event) {
    const text = (event.clipboardData || window.clipboardData).getData("text");
    if (text && /\s/.test(text.trim())) {
      event.preventDefault();
      addTag(text);
    }
  });

  tagInput.addEventListener("click", function (event) {
    if (event.target === tagInput || event.target === tagsEl) {
      focusInput();
    }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "/" && document.activeElement !== input) {
      const tag = document.activeElement && document.activeElement.tagName;
      if (tag !== "INPUT" && tag !== "TEXTAREA") {
        event.preventDefault();
        focusInput();
      }
    }
  });

  searchBtn.addEventListener("click", function () {
    runSearch();
  });

  refreshBtn.addEventListener("click", function () {
    runSearch({ refresh: true });
  });

  function setBusy(busy) {
    state.searching = busy;
    searchBtn.disabled = busy;
    const label = searchBtn.querySelector("span");
    if (label) label.textContent = busy ? "Searching…" : "Search";
  }

  function showSkeleton() {
    resultCount.textContent = "Searching…";
    metaBar.hidden = true;
    let rows = "";
    for (let i = 0; i < 8; i++) rows += '<div class="skeleton-row"></div>';
    resultsBody.innerHTML = '<div class="skeleton">' + rows + "</div>";
  }

  function showError(error, serverMode) {
    resultCount.textContent = "Error";
    resultsBody.innerHTML =
      '<div class="error-panel"><strong>Search failed</strong>' +
      "<p>" + h(error.message || String(error)) + "</p>" +
      '<p class="muted">' + (serverMode
        ? "Check your hop config and ~/.ssh/config."
        : "Check your hop config, kubeconfigs, and cluster access.") + "</p></div>";
  }

  async function runSearch(options) {
    const opts = options || {};
    commitInput();
    const query = queryString();
    const serverMode = isVMQuery(query);
    setMode(serverMode ? "servers" : "pods");
    if (state.controller) state.controller.abort();
    const controller = new AbortController();
    state.controller = controller;
    setBusy(true);
    showSkeleton();

    const params = new URLSearchParams();
    if (query) params.set("q", query);
    params.set("limit", "100");
    if (opts.refresh) params.set("refresh", "true");

    const endpoint = serverMode ? "/api/servers" : "/api/search";
    try {
      const response = await fetch(endpoint + "?" + params.toString(), {
        signal: controller.signal,
        headers: { Accept: "application/json" }
      });
      const data = await response.json().catch(function () { return {}; });
      if (!response.ok) {
        throw new Error(data.error || "HTTP " + response.status);
      }
      state.lastResult = data;
      if (serverMode) {
        renderServers(data, query);
      } else {
        render(data, query);
      }
      updateURL(query);
    } catch (error) {
      if (error.name === "AbortError") return;
      showError(error, serverMode);
    } finally {
      if (state.controller === controller) {
        setBusy(false);
      }
    }
  }

  function updateURL(query) {
    try {
      const url = new URL(window.location.href);
      if (query) {
        url.searchParams.set("q", query);
      } else {
        url.searchParams.delete("q");
      }
      window.history.replaceState(null, "", url);
      document.title = query ? query + " · hop" : "hop";
    } catch (_) {
      // ignore
    }
  }

  function serverDetailURL(server, query) {
    return "/server?name=" + encodeURIComponent(server.name) +
      "&q=" + encodeURIComponent(query || "");
  }

  function sshCommand(server) {
    if (server.ssh) return "ssh " + helpers.shellQuote(server.ssh);
    let command = "ssh";
    if (server.port && server.port !== 22) command += " -p " + server.port;
    const target = (server.user ? server.user + "@" : "") + (server.host || server.name);
    return command + " " + helpers.shellQuote(target);
  }

  function renderServers(data, query) {
    const hits = data.servers || [];
    resultCount.textContent = helpers.fmtNum(hits.length) + " server" + (hits.length === 1 ? "" : "s") + " · vm mode";
    metaBar.innerHTML =
      '<span><b>' + helpers.fmtNum(hits.length) + "</b> servers</span>" +
      '<span><b>vm</b> selector</span>' +
      '<span>SSH inventory</span>';
    metaBar.hidden = false;
    if (hits.length === 0) {
      renderServerEmpty(query);
      return;
    }
    let html = "";
    const topScore = hits[0].score || 0;
    if (query && topScore < 0.5) {
      html +=
        '<div class="suggestion-banner"><strong>Approximate match.</strong>' +
        "<span>No VM matches strongly. Here are the best suggestions, ranked by score.</span></div>";
    }
    html += '<div class="table-wrap"><table class="table"><thead><tr>' +
      "<th>Score</th><th>Server</th><th>Env</th><th>Host</th>" +
      '<th class="hide-sm">Tags</th><th class="hide-sm">Source</th><th></th>' +
      "</tr></thead><tbody>";
    hits.forEach(function (hit) {
      const server = hit.server || {};
      const percent = Math.max(0, Math.min(100, Math.round((hit.score || 0) * 100)));
      const reasons = (hit.reasons || []).slice(0, 2).join(" · ");
      const tags = (server.tags || []).map(function (tag) {
        return '<span class="badge">' + h(tag) + "</span>";
      }).join(" ");
      const env = (server.env || "").toLowerCase();
      const command = sshCommand(server);
      const detail = serverDetailURL(server, query);
      html += "<tr>" +
        '<td><div class="score' + (percent < 50 ? " suggestion" : "") + '">' +
        '<span class="score-num">' + percent + '%</span>' +
        '<span class="score-bar"><i style="width:' + percent + '%"></i></span></div></td>' +
        '<td class="cell-pod">' +
        '<a class="pod-name" href="' + h(detail) + '">' + h(server.name) + "</a>" +
        '<div class="pod-owner">' + h(server.ssh || server.name) + "</div>" +
        (reasons ? '<div class="pod-match">' + h(reasons) + "</div>" : "") +
        "</td>" +
        "<td>" + (server.env
          ? '<span class="badge env env-' + h(env) + '">' + h(server.env) + "</span>"
          : '<span class="muted">—</span>') + "</td>" +
        "<td><span class=\"badge mono\">" + h(server.host || "—") + "</span>" +
        (server.user ? '<div class="pod-owner">' + h(server.user) + ":" + h(String(server.port || 22)) + "</div>" : "") +
        "</td>" +
        '<td class="hide-sm">' + (tags || '<span class="muted">—</span>') + "</td>" +
        '<td class="hide-sm"><span class="badge mono">' + h(server.source || "—") + "</span></td>" +
        '<td><div class="row-actions">' +
        '<a class="action-link" href="' + h(detail) + '">Open</a>' +
        '<button class="icon-btn" type="button" data-copy="' + h(command) +
        '" title="Copy SSH command" aria-label="Copy SSH command">' +
        '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2">' +
        '<rect x="9" y="9" width="11" height="11" rx="2"></rect>' +
        '<path d="M5 15V5a2 2 0 0 1 2-2h10"></path></svg></button>' +
        "</div></td></tr>";
    });
    html += "</tbody></table></div>";
    resultsBody.innerHTML = html;
  }

  function renderServerEmpty(query) {
    resultsBody.innerHTML =
      '<div class="empty"><div class="empty-mark">?</div>' +
      "<h3>No VMs found</h3>" +
      "<p>" + (query
        ? "No VM matches these tags. Remove a tag or check your SSH inventory."
        : "No SSH hosts were found. Add entries in ~/.ssh/config or run hop config server add.") + "</p>" +
      '<div class="tips"><span class="badge mono">hop config server add NAME user@host tag...</span>' +
      '<span class="badge mono">~/.ssh/config</span></div></div>';
  }

  function render(data, query) {
    renderStats(data.stats || {});
    renderMeta(data);
    renderContexts((data.meta && data.meta.contexts) || []);
    renderResults(data, query);
    resultCount.textContent =
      helpers.fmtNum(data.matched) + " result" + (data.matched > 1 ? "s" : "") +
      " · " + helpers.fmtNum(data.eligible) + " eligible pod" + (data.eligible > 1 ? "s" : "") +
      " · " + helpers.fmtNum(data.total) + " total";
  }

  function renderStats(stats) {
    document.querySelectorAll("[data-stat]").forEach(function (element) {
      const key = element.getAttribute("data-stat");
      element.textContent = helpers.fmtNum(stats[key] || 0);
    });
  }

  function renderMeta(data) {
    const meta = data.meta || {};
    const parts = [];
    parts.push("<span><b>" + helpers.fmtNum(data.total) + "</b> pods</span>");
    parts.push("<span><b>" + helpers.fmtNum((meta.contexts || []).length) + "</b> contexts</span>");
    parts.push("<span>fetch <b>" + helpers.fmtDuration((meta.fetch_duration || 0) / 1e6) + "</b></span>");
    parts.push("<span>ranking <b>" + helpers.fmtDuration((data.search_duration || 0) / 1e6) + "</b></span>");
    if (meta.from_cache) {
      parts.push("<span>cache <b>" + helpers.fmtDuration((meta.cache_age || 0) / 1e6) + "</b></span>");
    }
    if (meta.fetched_at) {
      parts.push("<span>updated <b>" + helpers.fmtAge(meta.fetched_at) + "</b></span>");
    }
    const timeRange = data.query && data.query.time_range;
    if (timeRange) {
      parts.push('<span class="range-meta">◷ <b>' + h(formatRange(timeRange)) + "</b></span>");
    }
    const errors = meta.errors || [];
    if (errors.length > 0) {
      let items = "";
      errors.forEach(function (item) {
        items += "<li><b>" + h(item.context) + "</b> · " + h(item.error) + "</li>";
      });
      parts.push(
        '<details class="meta-errors"><summary>' + errors.length +
        " error" + (errors.length > 1 ? "s" : "") + "</summary><ul>" + items + "</ul></details>"
      );
    }
    metaBar.innerHTML = parts.join("");
    metaBar.hidden = false;
  }

  function formatRange(timeRange) {
    const from = new Date(timeRange.from);
    const to = new Date(timeRange.to);
    if (isNaN(from.getTime()) || isNaN(to.getTime())) return "";
    const options = { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" };
    return from.toLocaleString(undefined, options) + " → " + to.toLocaleString(undefined, options);
  }

  function globMatch(pattern, value) {
    const escaped = String(pattern)
      .replace(/[.+^${}()|[\]\\]/g, "\\$&")
      .replace(/\*/g, ".*")
      .replace(/\?/g, ".");
    try {
      return new RegExp("^" + escaped + "$", "i").test(value);
    } catch (_) {
      return false;
    }
  }

  function activeContextPatterns() {
    const patterns = [];
    state.tags.forEach(function (tag) {
      const match = /^(?:ctx|context):(.+)$/i.exec(tag);
      if (match) patterns.push(match[1]);
    });
    return patterns;
  }

  function renderContexts(contexts) {
    if (!contexts.length) {
      contextPills.hidden = true;
      contextPills.innerHTML = "";
      return;
    }
    const active = activeContextPatterns();
    let html = "";
    contexts.forEach(function (context) {
      const isActive = active.some(function (pattern) {
        return globMatch(pattern, context.name);
      });
      const env = (context.env || "").toLowerCase();
      const color = ENV_COLORS[env] || "#64748b";
      html +=
        '<button type="button" class="context-pill' + (isActive ? " active" : "") +
        '" data-context="' + h(context.name) + '" title="' + h(context.name) +
        (context.cluster ? " · " + h(context.cluster) : "") + '">' +
        '<span class="env-dot" style="background:' + color + '"></span>' +
        "<span>" + h(context.display || context.name) + "</span>" +
        (context.env ? '<span class="muted">' + h(context.env) + "</span>" : "") +
        "</button>";
    });
    contextPills.innerHTML = html;
    contextPills.hidden = false;
    contextPills.querySelectorAll("[data-context]").forEach(function (button) {
      button.addEventListener("click", function () {
        const name = button.getAttribute("data-context");
        const existing = state.tags.findIndex(function (tag) {
          return /^(?:ctx|context):/i.test(tag) && globMatch(tag.replace(/^(?:ctx|context):/i, ""), name);
        });
        if (existing >= 0) {
          state.tags.splice(existing, 1);
          renderTags();
          runSearch();
          return;
        }
        state.tags.push("ctx:" + name);
        renderTags();
        runSearch();
      });
    });
  }

  function podStatusClass(pod) {
    if (pod.phase === "Running" && pod.ready) return "ok";
    if (pod.phase === "Failed") return "err";
    if (pod.phase === "Succeeded") return "muted";
    return "warn";
  }

  function actionLink(url, label) {
    const href = helpers.safeHref(url);
    if (!href) return "";
    return '<a class="action-link" href="' + h(href) + '" target="_blank" rel="noopener">' +
      h(label) + "</a>";
  }

  function kubectlCommand(pod) {
    return "kubectl --context " + helpers.shellQuote(pod.context) +
      " -n " + helpers.shellQuote(pod.namespace) +
      " get pod " + helpers.shellQuote(pod.name) + " -o yaml";
  }

  function renderResults(data, query) {
    const hits = data.hits || [];
    if (hits.length === 0) {
      renderEmpty(query);
      return;
    }
    let html = "";
    const topScore = hits[0].score || 0;
    if (query && topScore < 0.5) {
      html +=
        '<div class="suggestion-banner"><strong>Approximate match.</strong>' +
        "<span>No pod matches strongly. Here are the best suggestions, ranked by score.</span></div>";
    }
    html += '<div class="table-wrap"><table class="table"><thead><tr>' +
      "<th>Score</th><th>Pod</th><th>Context</th>" +
      '<th class="hide-sm">Namespace</th><th>Status</th>' +
      '<th class="hide-sm">Node</th><th class="hide-sm">Restarts</th><th class="hide-sm">Age</th><th></th>' +
      "</tr></thead><tbody>";

    hits.forEach(function (hit) {
      const pod = hit.pod;
      const percent = Math.max(0, Math.min(100, Math.round((hit.score || 0) * 100)));
      const detailURL = "/pod?context=" + encodeURIComponent(pod.context) +
        "&namespace=" + encodeURIComponent(pod.namespace) +
        "&name=" + encodeURIComponent(pod.name) +
        "&q=" + encodeURIComponent(query || "");
      const reasons = (hit.reasons || []).slice(0, 2).join(" · ");
      const owner = pod.owner_name ? (pod.owner_kind ? pod.owner_kind + "/" : "") + pod.owner_name : (pod.workload || "");
      const env = (pod.env || "").toLowerCase();
      const statusClass = podStatusClass(pod);
      const statusText = pod.reason || pod.phase || "Unknown";
      const restarts = pod.restarts > 0
        ? '<span class="pill warn">' + pod.restarts + "</span>"
        : '<span class="muted">0</span>';
      const links = hit.links || {};
      const actions =
        '<div class="row-actions">' +
        actionLink(links.grafana_pod, "Grafana") +
        actionLink(links.loki_explore, "Loki") +
        actionLink(links.mimir_explore, "Mimir") +
        '<button class="icon-btn" type="button" data-copy="' + h(kubectlCommand(pod)) +
        '" title="Copy kubectl command" aria-label="Copy kubectl command">' +
        '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2">' +
        '<rect x="9" y="9" width="11" height="11" rx="2"></rect>' +
        '<path d="M5 15V5a2 2 0 0 1 2-2h10"></path></svg></button>' +
        "</div>";

      html += "<tr>" +
        '<td><div class="score' + (percent < 50 ? " suggestion" : "") + '">' +
        '<span class="score-num">' + percent + '%</span>' +
        '<span class="score-bar"><i style="width:' + percent + '%"></i></span></div></td>' +
        '<td class="cell-pod">' +
        '<a class="pod-name" href="' + h(detailURL) + '">' + h(pod.name) + "</a>" +
        (owner ? '<div class="pod-owner">' + h(owner) + "</div>" : "") +
        (reasons ? '<div class="pod-match">' + h(reasons) + "</div>" : "") +
        "</td>" +
        "<td><span class=\"badge env env-" + h(env) + "\">" + h(pod.env || "env?") + "</span>" +
        '<div class="pod-owner">' + h(pod.display || pod.context) + "</div></td>" +
        '<td class="hide-sm"><span class="badge mono">' + h(pod.namespace) + "</span></td>" +
        '<td><span class="status status-' + statusClass + '"><span class="dot"></span>' +
        h(statusText) + "</span></td>" +
        '<td class="hide-sm">' + (pod.node ? '<span class="badge mono">' + h(pod.node) + "</span>" : '<span class="muted">—</span>') + "</td>" +
        '<td class="hide-sm">' + restarts + "</td>" +
        '<td class="hide-sm muted">' + helpers.fmtAge(pod.start_time) + "</td>" +
        "<td>" + actions + "</td>" +
        "</tr>";
    });

    html += "</tbody></table></div>";
    resultsBody.innerHTML = html;
  }

  function renderEmpty(query) {
    const tips = query
      ? '<span class="badge mono">remove a tag</span><span class="badge mono">ns:dev</span>' +
        '<span class="badge mono">env:prod</span><span class="badge mono">label:app=api</span>'
      : '<span class="badge mono">check your kubeconfigs</span><span class="badge mono">hop cache clear</span>';
    resultsBody.innerHTML =
      '<div class="empty"><div class="empty-mark">?</div>' +
      "<h3>No pods found</h3>" +
      "<p>" + (query
        ? "No pod matches these tags. Try fewer constraints or a partial name."
        : "No pods were listed. Check your Kubernetes contexts and access permissions.") + "</p>" +
      '<div class="tips">' + tips + "</div></div>";
  }

  function boot() {
    const initial = input.value.trim();
    input.value = "";
    if (initial) {
      addTag(initial);
      runSearch();
    } else {
      renderTags();
      runSearch();
    }
    setTimeout(focusInput, 80);
  }

  boot();
})();
