(function () {
  "use strict";

  const el = function (id) { return document.getElementById(id); };
  const editor = el("config-yaml");
  const saveButton = el("config-save");
  const reloadButton = el("config-reload");
  const status = el("config-status");
  const message = el("config-message");
  const pathNode = el("config-path");
  const metaNode = el("config-meta");
  const warningNode = el("config-warning");
  const summaryNode = el("config-summary");
  const lineCount = el("config-line-count");
  const revisionNode = el("config-revision");
  const fileState = el("config-file-state");
  const fields = [
    ["apps", "Apps"],
    ["servers", "Servers"],
    ["shortcuts", "Shortcuts"],
    ["providers", "Providers"],
    ["kubeconfigs", "Kubeconfigs"],
    ["workspaces", "Workspaces"],
    ["git_roots", "Git roots"],
    ["registries", "Registries"],
    ["plugins", "Plugins"]
  ];
  const state = {
    revision: "",
    original: "",
    readOnly: false,
    dirty: false,
    busy: false,
    loaded: false
  };

  function setStatus(text, tone) {
    status.textContent = text || "";
    status.className = "config-status" + (tone ? " " + tone : "");
  }

  function setMessage(text, tone) {
    message.textContent = text || "";
    message.className = "config-message" + (tone ? " " + tone : "");
  }

  function setBusy(value) {
    state.busy = value;
    reloadButton.disabled = value;
    saveButton.disabled = value || state.readOnly || !state.dirty;
    editor.readOnly = value || state.readOnly;
  }

  function updateDirty() {
    state.dirty = editor.value !== state.original;
    saveButton.disabled = state.busy || state.readOnly || !state.dirty;
    updateLineCount();
    if (!state.loaded) return;
    if (state.dirty) {
      setStatus("modified", "warn");
    } else {
      setStatus(state.readOnly ? "read only" : "synced", state.readOnly ? "warn" : "ok");
    }
  }

  function updateLineCount() {
    const count = editor.value ? editor.value.split("\n").length : 0;
    lineCount.textContent = count + (count === 1 ? " line" : " lines");
  }

  function fmtTime(value) {
    if (!value) return "";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "";
    return date.toLocaleString();
  }

  function renderSummary(summary) {
    summaryNode.textContent = "";
    fields.forEach(function (entry) {
      const item = document.createElement("span");
      item.className = "config-summary-item";
      const value = document.createElement("b");
      value.textContent = String(Number((summary || {})[entry[0]] || 0));
      const label = document.createElement("em");
      label.textContent = entry[1];
      item.appendChild(value);
      item.appendChild(label);
      summaryNode.appendChild(item);
    });
  }

  function applyView(view) {
    state.revision = view.revision || "";
    state.original = view.yaml || "";
    state.readOnly = Boolean(view.read_only);
    state.loaded = true;
    editor.value = state.original;
    editor.disabled = false;
    pathNode.textContent = view.path || "demo / read-only";
    renderSummary(view.summary);
    revisionNode.textContent = state.revision ? "rev " + state.revision.slice(0, 10) : "";

    const parts = [];
    if (view.read_only) {
      parts.push("read only");
    } else if (view.exists) {
      parts.push("loaded from disk");
    } else {
      parts.push("new file · save to create");
    }
    if (view.updated_at) parts.push("updated " + fmtTime(view.updated_at));
    if (view.error) parts.push("parse warning");
    metaNode.textContent = parts.join(" · ");

    const warning = view.error || "";
    warningNode.hidden = !warning;
    warningNode.textContent = warning;

    fileState.textContent = view.path ? view.path.split("/").pop() : "Demo configuration";
    setStatus(view.error ? "parse warning" : view.read_only ? "read only" : "synced", view.error || view.read_only ? "warn" : "ok");
    setMessage(view.read_only ? "This instance has no editable config file." : "Loaded. Edit YAML, then save.", view.read_only ? "err" : "");
    state.dirty = false;
    setBusy(false);
    updateDirty();
  }

  function load() {
    if (state.busy) return;
    setBusy(true);
    setStatus("loading", "");
    setMessage("Reading config…", "");
    fetch("/api/config", { headers: { Accept: "application/json" } })
      .then(function (response) {
        return response.json().catch(function () { return {}; }).then(function (body) {
          if (!response.ok) throw new Error(body.error || "config request failed (" + response.status + ")");
          return body;
        });
      })
      .then(applyView)
      .catch(function (error) {
        state.loaded = true;
        editor.disabled = true;
        saveButton.disabled = true;
        setStatus("error", "err");
        setMessage(error.message, "err");
        warningNode.hidden = false;
        warningNode.textContent = error.message;
      })
      .finally(function () {
        setBusy(false);
      });
  }

  function save() {
    if (state.busy || state.readOnly || !state.dirty) return;
    setBusy(true);
    setStatus("validating", "");
    setMessage("Saving…", "");
    fetch("/api/config", {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json"
      },
      body: JSON.stringify({ revision: state.revision, yaml: editor.value })
    })
      .then(function (response) {
        return response.json().catch(function () { return {}; }).then(function (body) {
          if (!response.ok) {
            const error = new Error(body.error || "save failed (" + response.status + ")");
            error.status = response.status;
            throw error;
          }
          return body;
        });
      })
      .then(function (view) {
        state.revision = view.revision || state.revision;
        editor.value = view.yaml || editor.value;
        state.original = editor.value;
        state.dirty = false;
        revisionNode.textContent = state.revision ? "rev " + state.revision.slice(0, 10) : "";
        metaNode.textContent = "saved " + fmtTime(view.updated_at || new Date().toISOString());
        renderSummary(view.summary);
        warningNode.hidden = true;
        setMessage("Config saved to disk. Restart hop to apply changes that are read at startup.", "ok");
        setBusy(false);
        updateDirty();
        setStatus("saved", "ok");
      })
      .catch(function (error) {
        setStatus(error.status === 409 ? "stale" : "error", "err");
        setMessage(error.message, "err");
        setBusy(false);
      });
  }

  function reload() {
    if (state.busy) return;
    if (state.dirty && !window.confirm("Discard unsaved changes and reload from disk?")) return;
    load();
  }

  editor.addEventListener("input", updateDirty);
  editor.addEventListener("keydown", function (event) {
    if (event.key === "Tab") {
      event.preventDefault();
      const start = editor.selectionStart;
      const end = editor.selectionEnd;
      editor.setRangeText("  ", start, end, "end");
      updateDirty();
    }
  });
  saveButton.addEventListener("click", save);
  reloadButton.addEventListener("click", reload);
  document.addEventListener("keydown", function (event) {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "s") {
      event.preventDefault();
      save();
    }
  });
  window.addEventListener("beforeunload", function (event) {
    if (!state.dirty) return;
    event.preventDefault();
    event.returnValue = "";
  });

  load();
})();
