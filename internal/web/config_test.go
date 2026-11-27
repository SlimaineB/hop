package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPageAndAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	initial := `ui:
  bind: 127.0.0.1
apps:
  - name: api
    port: 8080
servers:
  - name: web01
    host: 10.0.0.10
shortcuts:
  - name: grafana
    url: https://grafana.example.com
`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(&fakeBackend{}, Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	page := httptest.NewRecorder()
	server.Handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/config", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("config page status = %d, body=%s", page.Code, page.Body.String())
	}
	for _, needle := range []string{"Config", "config-yaml", "config.js", "config.css", path} {
		if !strings.Contains(page.Body.String(), needle) {
			t.Fatalf("config page missing %q", needle)
		}
	}

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("config API status = %d, body=%s", response.Code, response.Body.String())
	}
	var view configView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if !view.Exists || view.ReadOnly || view.Path != path || view.Revision == "" {
		t.Fatalf("view = %+v", view)
	}
	if view.Summary.Apps != 1 || view.Summary.Servers != 1 || view.Summary.Shortcuts != 1 {
		t.Fatalf("summary = %+v", view.Summary)
	}
	if view.YAML != initial {
		t.Fatalf("yaml = %q, want %q", view.YAML, initial)
	}

	next := `ui:
  bind: 127.0.0.1
apps:
  - name: api
    port: 8080
  - name: worker
    port: 9090
`
	saved := postConfig(t, server, view.Revision, next, "")
	if saved.Code != http.StatusOK {
		t.Fatalf("save status = %d, body=%s", saved.Code, saved.Body.String())
	}
	var savedView configView
	if err := json.NewDecoder(saved.Body).Decode(&savedView); err != nil {
		t.Fatal(err)
	}
	if savedView.Summary.Apps != 2 || savedView.Revision == view.Revision {
		t.Fatalf("saved view = %+v", savedView)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != next {
		t.Fatalf("file = %q, want %q", string(onDisk), next)
	}

	invalid := postConfig(t, server, savedView.Revision, "apps:\n  - name: [\n", "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, body=%s", invalid.Code, invalid.Body.String())
	}
	onDisk, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != next {
		t.Fatalf("invalid save changed file: %q", string(onDisk))
	}

	stale := postConfig(t, server, view.Revision, next, "")
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status = %d, body=%s", stale.Code, stale.Body.String())
	}

	crossOrigin := postConfig(t, server, savedView.Revision, next, "http://evil.example")
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, body=%s", crossOrigin.Code, crossOrigin.Body.String())
	}
}

func TestConfigCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	server, err := New(&fakeBackend{}, Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var view configView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Exists || view.ReadOnly || !strings.Contains(view.YAML, "hop configuration") {
		t.Fatalf("view = %+v", view)
	}
	saved := postConfig(t, server, view.Revision, "ui:\n  bind: 127.0.0.1\n", "")
	if saved.Code != http.StatusOK {
		t.Fatalf("save status = %d, body=%s", saved.Code, saved.Body.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config was not created: %v", err)
	}
}

func TestConfigReadOnlyWithoutPath(t *testing.T) {
	server, err := New(&fakeBackend{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var view configView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if !view.ReadOnly {
		t.Fatalf("view = %+v", view)
	}
	saved := postConfig(t, server, view.Revision, "ui:\n  bind: 127.0.0.1\n", "")
	if saved.Code != http.StatusConflict {
		t.Fatalf("save status = %d, body=%s", saved.Code, saved.Body.String())
	}
}

func postConfig(t *testing.T, server *Server, revision, yaml, origin string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(configSaveRequest{Revision: revision, YAML: yaml})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	return response
}
