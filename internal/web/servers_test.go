package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SlimaineB/teu/internal/config"
	"github.com/SlimaineB/teu/internal/servers"
)

func TestAPIServersAndRedirect(t *testing.T) {
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh_config")
	if err := os.WriteFile(sshPath, []byte("Host sup-uat-vm-01\n  HostName 10.1.2.3\n  User deploy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.SSHConfig = sshPath
	cfg.Servers = []config.ServerConfig{{Name: "app01", Host: "10.0.0.5", Tags: []string{"app", "prod"}}}
	cfg.Normalize()
	server, err := New(nil, Options{Servers: servers.New(cfg)})
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

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err = client.Get(httpServer.URL + "/servers?q=uat")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect status = %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); !strings.Contains(location, "vm") {
		t.Fatalf("redirect location = %q", location)
	}
}
