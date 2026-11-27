package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpsertAndRemoveServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  bind: 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := ServerConfig{
		Name:       "web01",
		Host:       "10.0.0.10",
		User:       "deploy",
		Port:       2222,
		Env:        "prod",
		Tags:       []string{"api", "prod"},
		MetricsURL: "http://10.0.0.10:9100/metrics",
		CheckPorts: []int{443, 5432},
	}
	if _, err := UpsertServer(path, entry); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(cfg.Servers))
	}
	got := cfg.Servers[0]
	if got.Name != entry.Name || got.Host != entry.Host || got.User != entry.User || got.Port != entry.Port {
		t.Fatalf("server = %+v, want %+v", got, entry)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "api" || got.Tags[1] != "prod" {
		t.Fatalf("tags = %v", got.Tags)
	}
	if len(got.CheckPorts) != 2 || got.CheckPorts[0] != 443 || got.CheckPorts[1] != 5432 {
		t.Fatalf("check ports = %v", got.CheckPorts)
	}

	entry.Tags = []string{"web", "prod"}
	if _, err := UpsertServer(path, entry); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers after update = %d, want 1", len(cfg.Servers))
	}
	if len(cfg.Servers[0].Tags) != 2 || cfg.Servers[0].Tags[0] != "web" {
		t.Fatalf("updated tags = %v", cfg.Servers[0].Tags)
	}

	if _, err := RemoveServer(path, "web01"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("servers after remove = %d, want 0", len(cfg.Servers))
	}
}

func TestNormalizeServerCheckPorts(t *testing.T) {
	cfg := Default()
	cfg.Servers = []ServerConfig{{Name: "web01", Host: "10.0.0.10", CheckPorts: []int{443, 22, 443, 70000, 0}}}
	cfg.Normalize()
	ports := cfg.Servers[0].CheckPorts
	if len(ports) != 2 || ports[0] != 443 || ports[1] != 22 {
		t.Fatalf("check ports = %v", ports)
	}
}

func TestUpsertServerCreatesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	if _, err := UpsertServer(path, ServerConfig{Name: "db01", SSH: "db01"}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "db01" {
		t.Fatalf("config = %+v", cfg.Servers)
	}
}

func TestUpsertAndRemoveShortcut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	entry := ShortcutConfig{
		Name:        "grafana",
		URL:         "https://grafana.example.com",
		Keywords:    []string{"grafana", "metrics"},
		Description: "Metrics dashboards",
	}
	if _, err := UpsertShortcut(path, entry); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Shortcuts) != 1 {
		t.Fatalf("shortcuts = %d, want 1", len(cfg.Shortcuts))
	}
	if cfg.Shortcuts[0].Name != "grafana" || len(cfg.Shortcuts[0].Keywords) != 2 {
		t.Fatalf("shortcut = %+v", cfg.Shortcuts[0])
	}
	entry.URL = "https://grafana.example.com/d/overview"
	if _, err := UpsertShortcut(path, entry); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Shortcuts) != 1 || cfg.Shortcuts[0].URL != entry.URL {
		t.Fatalf("updated shortcut = %+v", cfg.Shortcuts)
	}
	if _, err := RemoveShortcut(path, "grafana"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Shortcuts) != 0 {
		t.Fatalf("shortcuts after remove = %d, want 0", len(cfg.Shortcuts))
	}
}

func TestNormalizeProviders(t *testing.T) {
	cfg := Default()
	cfg.Providers = map[string]ProviderConfig{
		" Jira-Prod ": {
			Type:        " JIRA ",
			BaseURL:     "https://jira.example.com/",
			TokenEnv:    " JIRA_TOKEN ",
			AuthScheme:  " BASIC ",
			Keywords:    []string{"jira, tickets", "bug"},
			Defaults:    map[string]string{" Project ": " OPS "},
			UsernameEnv: " JIRA_USER ",
		},
	}
	cfg.Issues.DefaultProvider = " JIRA-PROD "
	cfg.Normalize()
	provider, ok := cfg.Providers["jira-prod"]
	if !ok {
		t.Fatalf("providers = %+v", cfg.Providers)
	}
	if provider.Type != "jira" || provider.BaseURL != "https://jira.example.com" || provider.AuthScheme != "basic" {
		t.Fatalf("provider = %+v", provider)
	}
	if provider.Defaults["project"] != "OPS" || provider.TokenEnv != "JIRA_TOKEN" {
		t.Fatalf("provider = %+v", provider)
	}
	if len(provider.Keywords) != 3 || cfg.Issues.DefaultProvider != "jira-prod" {
		t.Fatalf("provider = %+v, default = %s", provider, cfg.Issues.DefaultProvider)
	}
}

func TestKubePodCapsDefaultsAndDisable(t *testing.T) {
	cfg := Default()
	cfg.Normalize()
	if cfg.Kube.MaxPods != 10000 || cfg.Kube.MaxPodsPerContext != 1000 {
		t.Fatalf("defaults = %d/%d, want 10000/1000", cfg.Kube.MaxPods, cfg.Kube.MaxPodsPerContext)
	}
	cfg.Kube.MaxPods = 0
	cfg.Kube.MaxPodsPerContext = -1
	cfg.Normalize()
	if cfg.Kube.MaxPods != 0 || cfg.Kube.MaxPodsPerContext != 0 {
		t.Fatalf("disabled caps = %d/%d, want 0/0", cfg.Kube.MaxPods, cfg.Kube.MaxPodsPerContext)
	}
}

func TestNormalizeContainers(t *testing.T) {
	cfg := Default()
	cfg.Containers.Engines = []ContainerEngineConfig{
		{Name: " Docker ", Type: "", Command: ""},
		{Name: "", Type: "PODMAN", Command: ""},
		{Name: "docker", Type: "docker"},
		{Name: "custom", Type: "", Command: ""},
	}
	cfg.Normalize()
	if cfg.Containers.Timeout.Get() != 10*time.Second || cfg.Containers.CacheTTL.Get() != 15*time.Second {
		t.Fatalf("timeouts = %s/%s", cfg.Containers.Timeout, cfg.Containers.CacheTTL)
	}
	if len(cfg.Containers.Engines) != 2 {
		t.Fatalf("engines = %+v", cfg.Containers.Engines)
	}
	docker := cfg.Containers.Engines[0]
	if docker.Name != "docker" || docker.Type != "docker" || docker.Command != "docker" {
		t.Fatalf("docker engine = %+v", docker)
	}
	podman := cfg.Containers.Engines[1]
	if podman.Name != "podman" || podman.Type != "podman" || podman.Command != "podman" {
		t.Fatalf("podman engine = %+v", podman)
	}
}
