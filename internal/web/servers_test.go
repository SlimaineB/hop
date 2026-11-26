package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/plugins"
	"github.com/SlimaineB/hop/internal/servers"
)

func TestAPIServersAndPage(t *testing.T) {
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh_config")
	if err := os.WriteFile(sshPath, []byte("Host sup-uat-vm-01\n  HostName 10.1.2.3\n  User deploy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.SSHConfig = sshPath
	cfg.Servers = []config.ServerConfig{{Name: "app01", Host: "10.0.0.5", Tags: []string{"app", "prod"}}}
	cfg.Normalize()
	serverSvc := servers.New(cfg)
	registry := plugins.Build(&config.Config{}, plugins.Deps{Servers: serverSvc})
	server, err := New(nil, Options{Servers: serverSvc, Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, err := http.Get(httpServer.URL + "/api/servers?q=uat%20vm")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload struct {
		Servers []servers.Hit `json:"servers"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Servers) != 1 || payload.Servers[0].Server.Name != "sup-uat-vm-01" {
		t.Fatalf("servers = %+v", payload.Servers)
	}

	response, err = http.Get(httpServer.URL + "/api/unified?q=uat%20vm")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var combined plugin.Response
	if err := json.NewDecoder(response.Body).Decode(&combined); err != nil {
		t.Fatal(err)
	}
	var serverSection *plugin.Result
	for i := range combined.Sections {
		if combined.Sections[i].ID == "servers" {
			serverSection = &combined.Sections[i]
		}
	}
	if serverSection == nil || len(serverSection.Items) != 1 || serverSection.Items[0].Title != "sup-uat-vm-01" {
		t.Fatalf("unified servers = %+v", combined.Sections)
	}
	if pulse := sectionByID(combined.Sections, "pods"); pulse != nil {
		t.Fatalf("vm selector should not search pods: %+v", pulse)
	}

	response, err = http.Get(httpServer.URL + "/servers")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("servers page status = %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"Servers", "All servers", "servers.js"} {
		if !strings.Contains(string(body), needle) {
			t.Fatalf("servers page missing %q", needle)
		}
	}
}
