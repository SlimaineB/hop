package cli

import (
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func TestMatchShortcuts(t *testing.T) {
	shortcuts := []config.ShortcutConfig{
		{Name: "grafana", URL: "https://grafana.example.com", Keywords: []string{"metrics", "dashboards"}},
		{Name: "jira", URL: "https://jira.example.com", Keywords: []string{"ticket", "bug"}},
		{Name: "swagger", URL: "https://api.example.com/swagger", Keywords: []string{"api", "openapi"}},
	}
	hits := matchShortcuts(shortcuts, []string{"jira"})
	if len(hits) != 1 || hits[0].Shortcut.Name != "jira" {
		t.Fatalf("hits = %+v", hits)
	}
	hits = matchShortcuts(shortcuts, []string{"grafana", "metrics"})
	if len(hits) != 1 || hits[0].Shortcut.Name != "grafana" || hits[0].Score <= 100 {
		t.Fatalf("multi-term hits = %+v", hits)
	}
	hits = matchShortcuts(shortcuts, []string{"ticket"})
	if len(hits) != 1 || hits[0].Shortcut.Name != "jira" {
		t.Fatalf("keyword hits = %+v", hits)
	}
	hits = matchShortcuts(shortcuts, []string{"missing"})
	if len(hits) != 0 {
		t.Fatalf("unexpected hits = %+v", hits)
	}
}

func TestValidShortcutURL(t *testing.T) {
	for _, value := range []string{"https://example.com", "http://localhost:3000/path"} {
		if !validShortcutURL(value) {
			t.Fatalf("expected valid: %s", value)
		}
	}
	for _, value := range []string{"", "example.com", "ftp://example.com", "javascript:alert(1)"} {
		if validShortcutURL(value) {
			t.Fatalf("expected invalid: %s", value)
		}
	}
}
