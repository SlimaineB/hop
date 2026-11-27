package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func TestMatchShortcuts(t *testing.T) {
	shortcuts := []config.ShortcutConfig{
		{Name: "grafana", URL: "https://grafana.example.com", Keywords: []string{"metrics", "dashboards"}},
		{Name: "jira", URL: "https://jira.example.com", Keywords: []string{"ticket", "bug"}},
		{Name: "swagger", URL: "https://api.example.com/swagger", Keywords: []string{"api", "openapi"}},
	}
	hits := config.MatchShortcuts(shortcuts, []string{"jira"})
	if len(hits) != 1 || hits[0].Shortcut.Name != "jira" {
		t.Fatalf("hits = %+v", hits)
	}
	hits = config.MatchShortcuts(shortcuts, []string{"grafana", "metrics"})
	if len(hits) != 1 || hits[0].Shortcut.Name != "grafana" || hits[0].Score <= 100 {
		t.Fatalf("multi-term hits = %+v", hits)
	}
	hits = config.MatchShortcuts(shortcuts, []string{"ticket"})
	if len(hits) != 1 || hits[0].Shortcut.Name != "jira" {
		t.Fatalf("keyword hits = %+v", hits)
	}
	hits = config.MatchShortcuts(shortcuts, []string{"missing"})
	if len(hits) != 0 {
		t.Fatalf("unexpected hits = %+v", hits)
	}
}

func TestValidShortcutURL(t *testing.T) {
	for _, value := range []string{"https://example.com", "http://localhost:3000/path"} {
		if !config.ValidShortcutURL(value) {
			t.Fatalf("expected valid: %s", value)
		}
	}
	for _, value := range []string{"", "example.com", "ftp://example.com", "javascript:alert(1)"} {
		if config.ValidShortcutURL(value) {
			t.Fatalf("expected invalid: %s", value)
		}
	}
}

func TestLinkCommandListsShortcuts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("shortcuts:\n  - name: grafana\n    url: https://grafana.example.com\n    keywords: [metrics]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{configPath: path}
	cmd := app.newLinkCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "grafana") || !strings.Contains(out.String(), "https://grafana.example.com") {
		t.Fatalf("output = %q", out.String())
	}
}
