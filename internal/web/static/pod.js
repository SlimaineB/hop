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
})();
