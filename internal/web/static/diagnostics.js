(function () {
  "use strict";

  const h = window.hop.escapeHTML;
  const helpers = window.hop;
  const state = { live: true, timer: null, loading: false };

  const el = function (id) { return document.getElementById(id); };

  function tone(severity) {
    if (severity === "critical") return "err";
    if (severity === "warning") return "warn";
    return "ok";
  }

  function duration(ns) {
    const value = Number(ns || 0) / 1e6;
    if (value >= 1000) return (value / 1000).toFixed(2) + "s";
    return Math.round(value) + "ms";
  }

  function formatTime(value) {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  }

  function probeRow(probe) {
    const cls = probe.ok ? "ok" : tone(probe.severity);
    const label = probe.ok ? "ok" : (probe.severity || "failed");
    const detail = probe.error || probe.summary || "";
    return '<li class="diag-probe ' + cls + '">' +
      '<span class="diag-probe-dot" aria-hidden="true"></span>' +
      '<span class="diag-probe-name"><b>' + h(probe.title || probe.id || "probe") + '</b><small>' + h(probe.type || "") + '</small></span>' +
      '<span class="diag-probe-state">' + h(label) + '</span>' +
      '<span class="diag-probe-detail">' + h(detail) + '</span>' +
      '<time>' + h(duration(probe.duration_ns)) + '</time>' +
      '</li>';
  }

  function findingBlock(finding) {
    const evidence = (finding.evidence || []).map(function (item) {
      return '<li>' + h(item) + '</li>';
    }).join("");
    return '<article class="diag-finding sev-' + h(finding.severity) + '">' +
      '<div class="diag-finding-head"><span class="diag-sev">' + h(finding.severity) + '</span><h3>' + h(finding.title) + '</h3></div>' +
      (finding.detail ? '<p>' + h(finding.detail) + '</p>' : "") +
      (evidence ? '<ul class="diag-evidence">' + evidence + '</ul>' : "") +
      (finding.remedy ? '<p class="diag-remedy"><b>Next:</b> ' + h(finding.remedy) + '</p>' : "") +
      '</article>';
  }

  function appCard(report) {
    const probeList = (report.probes || []).map(probeRow).join("");
    const findings = (report.findings || []).map(findingBlock).join("");
    const verdict = report.verdict || "info";
    return '<article class="diag-app sev-' + h(verdict) + '">' +
      '<header class="diag-app-head">' +
        '<div><span class="section-kicker">' + h(report.env || "app") + '</span><h2>' + h(report.target) + '</h2><p class="muted">' + h(report.summary || "") + '</p></div>' +
        '<div class="diag-app-meta"><span class="pill ' + tone(verdict) + '">' + h(verdict) + '</span><span class="muted small">' + h(duration(report.duration_ns)) + '</span></div>' +
      '</header>' +
      (probeList ? '<ul class="diag-probes">' + probeList + '</ul>' : "") +
      (findings ? '<details class="diag-app-findings"' + (verdict === "critical" ? " open" : "") + '><summary>' + (report.findings || []).length + ' finding' + ((report.findings || []).length === 1 ? "" : "s") + '</summary><div class="diag-list">' + findings + '</div></details>' : '<p class="muted small">No finding on this app.</p>') +
      '</article>';
  }

  function render(data) {
    const apps = data.apps || [];
    const target = el("diag-apps");
    if (target) {
      target.innerHTML = apps.length
        ? apps.map(appCard).join("")
        : '<div class="dash-panel diag-empty"><h2>No application report</h2><p class="muted">Add an <code>apps:</code> section to your hop config.</p></div>';
    }
    const updated = el("diag-updated");
    if (updated) {
      const critical = apps.filter(function (app) { return app.verdict === "critical"; }).length;
      const warning = apps.filter(function (app) { return app.verdict === "warning"; }).length;
      updated.textContent = apps.length + " apps · " + critical + " critical · " + warning + " warning · " + duration(data.duration) + " · " + formatTime(data.checked_at);
    }
    const errors = el("diag-errors");
    if (errors) {
      errors.hidden = true;
      errors.innerHTML = "";
    }
  }

  async function run() {
    if (state.loading) return;
    const target = el("diag-apps");
    if (!target) return;
    state.loading = true;
    const button = el("diag-refresh");
    if (button) button.disabled = true;
    try {
      const response = await fetch("/api/diagnostics/apps", { headers: { Accept: "application/json" } });
      if (!response.ok) throw new Error("diagnostics request failed (" + response.status + ")");
      render(await response.json());
    } catch (error) {
      const errors = el("diag-errors");
      if (errors) {
        errors.hidden = false;
        errors.innerHTML = "<details open><summary>Probe error</summary><ul><li>" + h(error.message || "unknown error") + "</li></ul></details>";
      }
    } finally {
      state.loading = false;
      if (button) button.disabled = false;
    }
  }

  function setLive(value) {
    state.live = value;
    const button = el("diag-live");
    if (button) {
      button.classList.toggle("on", value);
      button.setAttribute("aria-pressed", value ? "true" : "false");
    }
    if (state.timer) {
      clearInterval(state.timer);
      state.timer = null;
    }
    if (value) state.timer = setInterval(run, 15000);
  }

  const refresh = el("diag-refresh");
  if (refresh) refresh.addEventListener("click", run);
  const live = el("diag-live");
  if (live) live.addEventListener("click", function () { setLive(!state.live); });
  setLive(true);
  run();
})();
