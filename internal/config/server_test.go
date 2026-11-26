package config

import (
	"os"
	"path/filepath"
	"testing"
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
