package servers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func testService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh_config")
	sshConfig := `Host sup-uat-vm-01
  HostName 10.1.2.3
  User deploy
  Port 2222
  ProxyJump bastion

Host web01
  User root

Host *.internal
  User fallback
`
	if err := os.WriteFile(sshPath, []byte(sshConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SSHConfig: sshPath,
		Servers: []config.ServerConfig{
			{Name: "app01", Host: "10.0.0.5", User: "ubuntu", Env: "prod", Tags: []string{"app", "prod"}},
		},
	}
	cfg.Normalize()
	return New(cfg)
}

func TestListMergesSSHAndConfig(t *testing.T) {
	service := testService(t)
	list, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Server{}
	for _, server := range list {
		byName[server.Name] = server
	}
	sup, ok := byName["sup-uat-vm-01"]
	if !ok {
		t.Fatalf("missing ssh host: %+v", list)
	}
	if sup.Host != "10.1.2.3" || sup.User != "deploy" || sup.Port != 2222 || sup.ProxyJump != "bastion" {
		t.Fatalf("ssh server = %+v", sup)
	}
	if !contains(sup.Tags, "uat") || !contains(sup.Tags, "vm") {
		t.Fatalf("ssh tags = %v", sup.Tags)
	}
	app, ok := byName["app01"]
	if !ok || app.Source != "config" || app.Env != "prod" {
		t.Fatalf("config server = %+v", app)
	}
}

func TestSearchTagsAndVMSelector(t *testing.T) {
	service := testService(t)
	hits, err := service.Search("supervision uat vm", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("unexpected hits for missing tag: %+v", hits)
	}
	hits, err = service.Search("uat vm", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Server.Name != "sup-uat-vm-01" {
		t.Fatalf("hits = %+v", hits)
	}
	hits, err = service.Search("tag:prod", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Server.Name != "app01" {
		t.Fatalf("tag hits = %+v", hits)
	}
	hits, err = service.Search("vm", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 3 {
		t.Fatalf("vm selector should list all servers, got %d", len(hits))
	}
}

func TestSSHCommandAndMachineParsing(t *testing.T) {
	service := testService(t)
	command, err := service.SSHCommand("sup-uat-vm-01")
	if err != nil {
		t.Fatal(err)
	}
	if command == "" {
		t.Fatal("empty ssh command")
	}
	info := parseMachineOutput(`@@HOSTNAME
vm01
@@OS
Ubuntu 24.04
@@KERNEL
6.8.0
@@UPTIME
up 3 days
@@LOAD
0.10 0.20 0.30
@@CPU
4
@@MEM
1000 400 500
@@DISK
2000 1000 900 50%
@@FAILED
`)
	if info.Hostname != "vm01" || info.CPUs != 4 || info.MemoryTotal != 1000 || info.DiskPercent != "50" {
		t.Fatalf("machine = %+v", info)
	}
}

func TestHTTPCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	check := checkHTTP(context.Background(), server.Client(), server.URL)
	if !check.OK || check.StatusCode != 200 {
		t.Fatalf("check = %+v", check)
	}
	missing := checkHTTP(context.Background(), server.Client(), "")
	if missing.Error != "not configured" {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestSearchLocalUsesSnapshotUntilRefresh(t *testing.T) {
	service := testService(t)
	all, err := service.Search("", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("expected servers in the snapshot")
	}

	file, err := os.OpenFile(service.cfg.SSHConfig, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\nHost newvm\n  HostName 10.9.9.9\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	local, err := service.SearchLocal("newvm", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != 0 {
		t.Fatalf("snapshot should not see the new host yet: %+v", local)
	}

	service.Refresh()
	local, err = service.SearchLocal("newvm", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != 1 || local[0].Server.Name != "newvm" {
		t.Fatalf("after refresh hits = %+v", local)
	}
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
