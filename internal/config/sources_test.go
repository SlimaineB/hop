package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalSource(t *testing.T) {
	cases := map[string]string{
		"pod": "pods", "K8S": "pods", "vm": "servers", "git": "repos",
		"pkg": "packages", "issue": "tickets", "link": "shortcuts",
		"ctr": "containers", "docker": "containers", "podman": "containers",
		"custom": "custom", "": "",
	}
	for input, want := range cases {
		if got := CanonicalSource(input); got != want {
			t.Fatalf("CanonicalSource(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestConfigSourceToggles(t *testing.T) {
	cfg := &Config{}
	if !cfg.SourceEnabled("git") {
		t.Fatal("source should start enabled")
	}
	if !cfg.SetSourceEnabled("git", false) {
		t.Fatal("expected change")
	}
	if cfg.SourceEnabled("repos") {
		t.Fatal("repos should be disabled")
	}
	if cfg.SetSourceEnabled("git", false) {
		t.Fatal("repeated disable should not report change")
	}
	if !cfg.SetSourceEnabled("repos", true) {
		t.Fatal("expected enable change")
	}
	if !cfg.SourceEnabled("git") {
		t.Fatal("repos should be enabled")
	}
	cfg.Sources.Disabled = []string{"vm", "pod", "vm"}
	cfg.Normalize()
	if len(cfg.Sources.Disabled) != 2 || cfg.Sources.Disabled[0] != "pods" || cfg.Sources.Disabled[1] != "servers" {
		t.Fatalf("disabled = %v", cfg.Sources.Disabled)
	}
}

func TestUpdateSourceEnabledPreservesComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "# hop config\nui:\n  theme: hop # keep this\nsearch:\n  limit: 20\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateSourceEnabled(path, "git", false); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, "# hop config") || !strings.Contains(text, "# keep this") {
		t.Fatalf("comments lost:\n%s", text)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SourceEnabled("repos") {
		t.Fatal("repos should be disabled")
	}
	if _, err := UpdateSourceEnabled(path, "git", true); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SourceEnabled("repos") {
		t.Fatal("repos should be enabled again")
	}
	updated, _ = os.ReadFile(path)
	if strings.Contains(string(updated), "disabled") {
		t.Fatalf("disabled list should be empty:\n%s", updated)
	}
}
