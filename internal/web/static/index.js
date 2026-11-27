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
  const liveBtn = document.getElementById("live-btn");
  const resultsBody = document.getElementById("results-body");
  const resultCount = document.getElementById("result-count");
  const metaBar = document.getElementById("meta-bar");
  const contextPills = document.getElementById("context-pills");
  const statsEl = document.getElementById("stats");
  const source = (tagInput.getAttribute("data-sources") || "").trim();
  const emptyHint = (resultsBody && resultsBody.getAttribute("data-empty-hint")) || "";

  const FILTER_PREFIXES = new Set([
    "pod", "name", "ns", "namespace", "ctx", "context", "cluster", "env", "environment",
    "owner", "workload", "deploy", "node", "label", "labels", "lbl", "container",
    "image", "phase", "status", "server", "servers", "srv", "tag", "tags", "host",
    "hostname", "user", "ssh", "repo", "repos", "repository", "branch", "branches"
  ]);
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
    searching: false,
    live: false,
    liveTimer: null
  };

  function isFilter(tag) {
    const index = tag.indexOf(":");
    if (index <= 0) return false;
    return FILTER_PREFIXES.has(tag.slice(0, index).toLowerCase());
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
        const colon = tag.indexOf(":");
        const key = tag.slice(0, colon);
        const value = tag.slice(colon + 1);
        element.innerHTML = "<b>" + h(key) + "</b>:" + h(value);
      } else {
        element.textContent = tag;
      }
      if (timeTag) element.title = "Logs and metrics range";
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
    if (event.key === "Escape") input.value = "";
  });

  input.addEventListener("paste", function (event) {
    const text = (event.clipboardData || window.clipboardData).getData("text");
    if (text && /\s/.test(text.trim())) {
      event.preventDefault();
      addTag(text);
    }
  });

  tagInput.addEventListener("click", function (event) {
    if (event.target === tagInput || event.target === tagsEl) focusInput();
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
  liveBtn.addEventListener("click", function () {
    toggleLive();
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

  function showError(error) {
    resultCount.textContent = "Error";
    resultsBody.innerHTML =
      '<div class="error-panel"><strong>Search failed</strong>' +
      "<p>" + h(error.message || String(error)) + "</p>" +
      '<p class="muted">Check your hop config, kubeconfigs, SSH config, and Git roots.</p></div>';
  }

  function toggleLive() {
    state.live = !state.live;
    liveBtn.setAttribute("aria-pressed", state.live ? "true" : "false");
    liveBtn.classList.toggle("on", state.live);
    if (state.live) {
      helpers.toast("Live mode on · refresh every 5s");
      tickLive();
    } else {
      stopLive();
      helpers.toast("Live mode off");
    }
  }

  function stopLive() {
    if (state.liveTimer) {
      clearTimeout(state.liveTimer);
      state.liveTimer = null;
    }
  }

  function tickLive() {
    if (!state.live) return;
    runSearch({ live: true, quiet: true, silent: true }).finally(function () {
      if (!state.live) return;
      stopLive();
      state.liveTimer = setTimeout(tickLive, 5000);
    });
  }

  async function runSearch(options) {
    const opts = options || {};
    if (!opts.silent) commitInput();
    const query = queryString();
    if (state.controller) state.controller.abort();
    const controller = new AbortController();
    state.controller = controller;
    if (!opts.quiet) {
      setBusy(true);
      showSkeleton();
    }

    const params = new URLSearchParams();
    if (query) params.set("q", query);
    if (source) params.set("sources", source);
    params.set("limit", "100");
    if (opts.live) {
      params.set("live", "1");
    } else {
      params.set("tickets", "1");
    }
    if (opts.refresh) params.set("refresh", "true");

    try {
      const response = await fetch("/api/unified?" + params.toString(), {
        signal: controller.signal,
        headers: { Accept: "application/json" }
      });
      const data = await response.json().catch(function () { return {}; });
      if (!response.ok) throw new Error(data.error || "HTTP " + response.status);
      state.lastResult = data;
      renderResponse(data, query);
      updateURL(query);
    } catch (error) {
      if (error.name === "AbortError") return;
      if (opts.quiet) {
        helpers.toast("Live refresh failed: " + (error.message || "error"), true);
      } else {
        showError(error);
      }
    } finally {
      if (state.controller === controller && !opts.quiet) setBusy(false);
    }
  }

  function updateURL(query) {
    try {
      const url = new URL(window.location.href);
      if (query) url.searchParams.set("q", query);
      else url.searchParams.delete("q");
      window.history.replaceState(null, "", url);
      document.title = query ? query + " · hop" : "hop";
    } catch (_) {
      // ignore
    }
  }

  function sectionById(data, id) {
    const sections = (data && data.sections) || [];
    for (let i = 0; i < sections.length; i++) {
      if (sections[i].id === id) return sections[i];
    }
    return null;
  }

  function renderResponse(data, query) {
    const pods = sectionById(data, "pods");
    const podMeta = (pods && pods.meta) || {};
    const stats = podMeta.stats || {};
    const meta = podMeta.meta || {};
    if (statsEl) statsEl.hidden = !pods;
    renderStats(stats);
    renderContexts(meta.contexts || []);
    renderMeta(data, podMeta, meta);
    renderSections(data, query);

    const total = Number(data.total || 0);
    const count = ((data.sections || []).length);
    resultCount.textContent = helpers.fmtNum(total) + " result" + (total === 1 ? "" : "s") +
      " · " + helpers.fmtNum(count) + " section" + (count === 1 ? "" : "s");
  }

  function renderStats(stats) {
    document.querySelectorAll("[data-stat]").forEach(function (element) {
      const key = element.getAttribute("data-stat");
      element.textContent = helpers.fmtNum(stats[key] || 0);
    });
  }

  function renderMeta(data, podMeta, meta) {
    const parts = [];
    if (podMeta.total) {
      parts.push("<span><b>" + helpers.fmtNum(podMeta.total) + "</b> pods</span>");
    }
    if (meta.fetched_at) {
      parts.push("<span>pods <b>" + helpers.fmtAge(meta.fetched_at) + "</b></span>");
    }
    if (meta.from_cache) {
      parts.push("<span>cache <b>" + helpers.fmtDuration((meta.cache_age || 0) / 1e6) + "</b></span>");
    }
    parts.push("<span>search <b>" + helpers.fmtDuration((data.duration || 0) / 1e6) + "</b></span>");
    const timeRange = data.query && data.query.time_range;
    if (timeRange) {
      parts.push('<span class="range-meta">◷ <b>' + h(formatRange(timeRange)) + "</b></span>");
    }
    const contextErrors = meta.errors || [];
    if (contextErrors.length > 0) {
      let items = "";
      contextErrors.forEach(function (item) {
        items += "<li><b>" + h(item.context) + "</b> · " + h(item.error) + "</li>";
      });
      parts.push(
        '<details class="meta-errors"><summary>' + contextErrors.length +
        " context error" + (contextErrors.length > 1 ? "s" : "") + "</summary><ul>" + items + "</ul></details>"
      );
    }
    const errors = data.errors || [];
    if (errors.length > 0) {
      let items = "";
      errors.forEach(function (item) {
        items += "<li>" + h(item) + "</li>";
      });
      parts.push(
        '<details class="meta-errors"><summary>' + errors.length +
        " source error" + (errors.length > 1 ? "s" : "") + "</summary><ul>" + items + "</ul></details>"
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

  function renderSections(data, query) {
    const sections = data.sections || [];
    if (!sections.length) {
      renderEmpty(query);
      return;
    }
    let html = "";
    sections.forEach(function (section) {
      if (!section.items || (!section.items.length && !(section.errors || []).length)) return;
      const body = renderSection(section, query);
      if (!body) return;
      const count = section.total != null ? section.total : section.items.length;
      html += '<section class="result-section" data-kind="' + h(section.kind) + '">' +
        '<div class="section-head">' +
        '<div class="section-title"><span class="section-kicker">' + h(section.kind) + "</span>" +
        "<h2>" + h(section.title) + "</h2></div>" +
        '<span class="section-count">' + helpers.fmtNum(count) + "</span></div>" +
        renderSectionErrors(section) + body +
        "</section>";
    });
    if (!html) {
      renderEmpty(query);
      return;
    }
    resultsBody.innerHTML = html;
  }

  function renderSectionErrors(section) {
    const errors = section.errors || [];
    if (!errors.length) return "";
    let items = "";
    errors.forEach(function (item) {
      items += "<li>" + h(item) + "</li>";
    });
    return '<details class="section-errors"><summary>' + errors.length +
      " error" + (errors.length > 1 ? "s" : "") + "</summary><ul>" + items + "</ul></details>";
  }

  function renderSection(section, query) {
    switch (section.kind) {
      case "pods":
        return podTable(section.items, query);
      case "servers":
        return serverTable(section.items, query);
      case "repos":
        return repoGrid(section.items);
      case "packages":
        return packageGrid(section.items);
      case "tickets":
        return ticketGrid(section.items);
      case "shortcuts":
        return shortcutGrid(section.items);
      default:
        return genericGrid(section.items);
    }
  }

  function actionLink(url, label) {
    const href = helpers.safeHref(url);
    if (!href) return "";
    return '<a class="action-link" href="' + h(href) + '" target="_blank" rel="noopener">' +
      h(label) + "</a>";
  }

  function copyButton(text, label) {
    if (!text) return "";
    return '<button class="icon-btn" type="button" data-copy="' + h(text) +
      '" title="' + h(label || "Copy") + '" aria-label="' + h(label || "Copy") + '">' +
      '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2">' +
      '<rect x="9" y="9" width="11" height="11" rx="2"></rect>' +
      '<path d="M5 15V5a2 2 0 0 1 2-2h10"></path></svg></button>';
  }

  function podStatusClass(pod) {
    if (pod.phase === "Running" && pod.ready) return "ok";
    if (pod.phase === "Failed") return "err";
    if (pod.phase === "Succeeded") return "muted";
    return "warn";
  }

  function kubectlCommand(pod) {
    return "kubectl --context " + helpers.shellQuote(pod.context) +
      " -n " + helpers.shellQuote(pod.namespace) +
      " get pod " + helpers.shellQuote(pod.name) + " -o yaml";
  }

  function podDetailURL(pod, query) {
    return "/pod?context=" + encodeURIComponent(pod.context) +
      "&namespace=" + encodeURIComponent(pod.namespace) +
      "&name=" + encodeURIComponent(pod.name) +
      "&q=" + encodeURIComponent(query || "");
  }

  function podTable(items, query) {
    if (!items.length) return "";
    let html = '<div class="table-wrap"><table class="table"><thead><tr>' +
      "<th>Score</th><th>Pod</th><th>Context</th>" +
      '<th class="hide-sm">Namespace</th><th>Status</th>' +
      '<th class="hide-sm">Node</th><th class="hide-sm">Restarts</th><th class="hide-sm">Age</th><th></th>' +
      "</tr></thead><tbody>";
    items.forEach(function (item) {
      const payload = item.payload || {};
      const pod = payload.pod || {};
      const percent = Math.max(0, Math.min(100, item.percent || Math.round((payload.score || 0) * 100)));
      const reasons = (payload.reasons || []).slice(0, 2).join(" · ");
      const owner = pod.owner_name ? (pod.owner_kind ? pod.owner_kind + "/" : "") + pod.owner_name : (pod.workload || "");
      const env = (pod.env || "").toLowerCase();
      const statusClass = podStatusClass(pod);
      const statusText = pod.reason || pod.phase || "Unknown";
      const restarts = pod.restarts > 0
        ? '<span class="pill warn">' + pod.restarts + "</span>"
        : '<span class="muted">0</span>';
      const links = payload.links || {};
      const actions = '<div class="row-actions">' +
        actionLink(links.grafana_pod, "Grafana") +
        actionLink(links.loki_explore, "Loki") +
        actionLink(links.mimir_explore, "Mimir") +
        copyButton(kubectlCommand(pod), "Copy kubectl command") +
        "</div>";
      html += "<tr>" +
        '<td><div class="score' + (percent < 50 ? " suggestion" : "") + '">' +
        '<span class="score-num">' + percent + "%</span>" +
        '<span class="score-bar"><i style="width:' + percent + '%"></i></span></div></td>' +
        '<td class="cell-pod">' +
        '<a class="pod-name" href="' + h(podDetailURL(pod, query)) + '">' + h(pod.name) + "</a>" +
        (owner ? '<div class="pod-owner">' + h(owner) + "</div>" : "") +
        (reasons ? '<div class="pod-match">' + h(reasons) + "</div>" : "") +
        "</td>" +
        '<td><span class="badge env env-' + h(env) + '">' + h(pod.env || "env?") + "</span>" +
        '<div class="pod-owner">' + h(pod.display || pod.context) + "</div></td>" +
        '<td class="hide-sm"><span class="badge mono">' + h(pod.namespace) + "</span></td>" +
        '<td><span class="status status-' + statusClass + '"><span class="dot"></span>' +
        h(statusText) + "</span></td>" +
        '<td class="hide-sm">' + (pod.node ? '<span class="badge mono">' + h(pod.node) + "</span>" : '<span class="muted">—</span>') + "</td>" +
        '<td class="hide-sm">' + restarts + "</td>" +
        '<td class="hide-sm muted">' + helpers.fmtAge(pod.start_time) + "</td>" +
        "<td>" + actions + "</td></tr>";
    });
    html += "</tbody></table></div>";
    return html;
  }

  function sshCommand(server) {
    if (server.ssh) return "ssh " + helpers.shellQuote(server.ssh);
    let command = "ssh";
    if (server.port && server.port !== 22) command += " -p " + server.port;
    const target = (server.user ? server.user + "@" : "") + (server.host || server.name);
    return command + " " + helpers.shellQuote(target);
  }

  function serverDetailURL(server, query) {
    return "/server?name=" + encodeURIComponent(server.name) +
      "&q=" + encodeURIComponent(query || "");
  }

  function serverTable(items, query) {
    if (!items.length) return "";
    let html = '<div class="table-wrap"><table class="table"><thead><tr>' +
      "<th>Score</th><th>Server</th><th>Env</th><th>Host</th>" +
      '<th class="hide-sm">Tags</th><th class="hide-sm">Source</th><th></th>' +
      "</tr></thead><tbody>";
    items.forEach(function (item) {
      const payload = item.payload || {};
      const server = payload.server || {};
      const percent = Math.max(0, Math.min(100, item.percent || payload.percent || Math.round((payload.score || 0) * 100)));
      const reasons = (payload.reasons || []).slice(0, 2).join(" · ");
      const tags = (server.tags || []).map(function (tag) {
        return '<span class="badge">' + h(tag) + "</span>";
      }).join(" ");
      const env = (server.env || "").toLowerCase();
      const detail = serverDetailURL(server, query);
      html += "<tr>" +
        '<td><div class="score' + (percent < 50 ? " suggestion" : "") + '">' +
        '<span class="score-num">' + percent + "%</span>" +
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
        copyButton(sshCommand(server), "Copy SSH command") +
        "</div></td></tr>";
    });
    html += "</tbody></table></div>";
    return html;
  }

  function repoGrid(items) {
    let html = '<div class="card-grid">';
    items.forEach(function (item) {
      const payload = item.payload || {};
      const repo = payload.repo || {};
      const links = repo.links || {};
      const web = helpers.safeHref(links.repo || repo.web_url);
      const title = web
        ? '<a class="card-title" href="' + h(web) + '" target="_blank" rel="noopener">' + h(repo.name) + "</a>"
        : '<span class="card-title">' + h(repo.name) + "</span>";
      const changes = [];
      if (repo.dirty) changes.push('<span class="badge warn">' + helpers.fmtNum(repo.changed) + " changed</span>");
      if (repo.ahead) changes.push('<span class="badge">↑' + helpers.fmtNum(repo.ahead) + "</span>");
      if (repo.behind) changes.push('<span class="badge">↓' + helpers.fmtNum(repo.behind) + "</span>");
      const actions = '<div class="card-actions">' +
        actionLink(links.branch, "Branch") +
        actionLink(links.commit, "Commit") +
        actionLink(links.merge_requests, "MRs") +
        actionLink(links.new_merge_request, "New MR") +
        copyButton(repo.path, "Copy repository path") +
        "</div>";
      html += '<article class="card">' +
        '<div class="card-head">' + title +
        (repo.provider ? '<span class="badge">' + h(repo.provider) + "</span>" : "") + "</div>" +
        '<div class="card-sub">' +
        (repo.branch ? '<span class="badge mono">' + h(repo.branch) + "</span>" : "") +
        changes.join(" ") + "</div>" +
        (repo.subject ? '<p class="card-desc">' + h(repo.subject) + "</p>" : "") +
        '<div class="card-meta">' +
        (repo.author ? "<span>" + h(repo.author) + "</span>" : "") +
        (repo.committed_at ? "<span>" + helpers.fmtAge(repo.committed_at) + " ago</span>" : "") +
        '<span class="mono">' + h(repo.commit_short || "") + "</span></div>" +
        actions + "</article>";
    });
    return html + "</div>";
  }

  function packageGrid(items) {
    let html = '<div class="card-grid">';
    items.forEach(function (item) {
      const payload = item.payload || {};
      const image = payload.image || {};
      const web = helpers.safeHref(image.web_url);
      const tags = (image.tags || []).slice(0, 8).map(function (tag) {
        return '<span class="badge mono">' + h(tag) + "</span>";
      }).join(" ");
      const title = web
        ? '<a class="card-title" href="' + h(web) + '" target="_blank" rel="noopener">' + h(imageName(image.repository)) + "</a>"
        : '<span class="card-title">' + h(imageName(image.repository)) + "</span>";
      html += '<article class="card">' +
        '<div class="card-head">' + title +
        (image.provider ? '<span class="badge">' + h(image.provider) + "</span>" : "") + "</div>" +
        '<div class="card-sub"><span class="mono">' + h(image.registry + "/" + image.repository) + "</span></div>" +
        (tags ? '<div class="tag-list">' + tags + "</div>" : "") +
        '<div class="card-actions">' + copyButton(image.pull, "Copy docker pull") + "</div>" +
        "</article>";
    });
    return html + "</div>";
  }

  function ticketGrid(items) {
    let html = '<div class="card-grid">';
    items.forEach(function (item) {
      const ticket = item.payload || {};
      const web = helpers.safeHref(ticket.url);
      const title = web
        ? '<a class="card-title" href="' + h(web) + '" target="_blank" rel="noopener">' + h(ticket.title || ticket.key) + "</a>"
        : '<span class="card-title">' + h(ticket.title || ticket.key) + "</span>";
      html += '<article class="card">' +
        '<div class="card-head">' + title +
        '<span class="badge mono">' + h(ticket.key) + "</span>" + "</div>" +
        '<div class="card-sub">' +
        (ticket.provider ? '<span class="badge">' + h(ticket.provider) + "</span>" : "") +
        (ticket.status ? '<span class="badge">' + h(ticket.status) + "</span>" : "") +
        (ticket.updated ? '<span class="muted">' + h(ticket.updated) + "</span>" : "") + "</div>" +
        '<div class="card-actions">' + copyButton(ticket.key, "Copy ticket key") + "</div>" +
        "</article>";
    });
    return html + "</div>";
  }

  function shortcutGrid(items) {
    let html = '<div class="card-grid">';
    items.forEach(function (item) {
      const hit = item.payload || {};
      const shortcut = hit.shortcut || {};
      const web = helpers.safeHref(shortcut.url);
      const title = web
        ? '<a class="card-title" href="' + h(web) + '" target="_blank" rel="noopener">' + h(shortcut.name) + "</a>"
        : '<span class="card-title">' + h(shortcut.name) + "</span>";
      const keywords = (shortcut.keywords || []).map(function (keyword) {
        return '<span class="badge">' + h(keyword) + "</span>";
      }).join(" ");
      html += '<article class="card">' +
        '<div class="card-head">' + title + '<span class="badge">link</span></div>' +
        (shortcut.description ? '<p class="card-desc">' + h(shortcut.description) + "</p>" : "") +
        (keywords ? '<div class="tag-list">' + keywords + "</div>" : "") +
        '<div class="card-meta"><span class="mono">' + h(shortcut.url) + "</span></div>" +
        "</article>";
    });
    return html + "</div>";
  }

  function genericGrid(items) {
    let html = '<div class="card-grid">';
    items.forEach(function (item) {
      const web = helpers.safeHref(item.url);
      const title = web
        ? '<a class="card-title" href="' + h(web) + '" target="_blank" rel="noopener">' + h(item.title) + "</a>"
        : '<span class="card-title">' + h(item.title) + "</span>";
      let meta = "";
      if (item.meta) {
        Object.keys(item.meta).sort().forEach(function (key) {
          if (!item.meta[key]) return;
          meta += '<span><b>' + h(key) + "</b> " + h(item.meta[key]) + "</span>";
        });
      }
      html += '<article class="card">' +
        '<div class="card-head">' + title +
        (item.badge ? '<span class="badge">' + h(item.badge) + "</span>" : "") + "</div>" +
        (item.subtitle ? '<div class="card-sub">' + h(item.subtitle) + "</div>" : "") +
        (item.description ? '<p class="card-desc">' + h(item.description) + "</p>" : "") +
        (meta ? '<div class="card-meta">' + meta + "</div>" : "") +
        '<div class="card-actions">' + copyButton(item.copy, "Copy") + "</div>" +
        "</article>";
    });
    return html + "</div>";
  }

  function imageName(repository) {
    const value = String(repository || "");
    const slash = value.lastIndexOf("/");
    return slash >= 0 ? value.slice(slash + 1) : value;
  }

  function renderEmpty(query) {
    const message = emptyHint || (query
      ? "No source matches these tags. Try fewer constraints or a partial name."
      : "No sources returned data. Check your kubeconfigs, SSH config, Git roots, and registries.");
    const tips = query
      ? '<span class="badge mono">remove a filter</span><span class="badge mono">git api</span>' +
        '<span class="badge mono">vm supervision</span><span class="badge mono">pkg ghcr</span>'
      : '<span class="badge mono">check your config</span><span class="badge mono">hop config git add</span>';
    resultsBody.innerHTML =
      '<div class="empty"><div class="empty-mark">?</div>' +
      "<h3>Nothing found</h3>" +
      "<p>" + h(message) + "</p>" +
      '<div class="tips">' + tips + "</div></div>";
  }

  function boot() {
    const initial = input.value.trim();
    input.value = "";
    if (initial) {
      addTag(initial);
    } else {
      renderTags();
    }
    runSearch();
    setTimeout(focusInput, 80);
  }

  boot();
})();
