package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestConfigSourceToggleCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  theme: hop\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := runCLI(t, "--config", path, "config", "disable", "git"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SourceEnabled("repos") {
		t.Fatal("repos should be disabled")
	}

	stdout, _, err := runCLI(t, "--config", path, "config", "sources")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "repos") || !strings.Contains(stdout, "off") {
		t.Fatalf("sources output:\n%s", stdout)
	}
	if !strings.Contains(stdout, "pods") || !strings.Contains(stdout, "on") {
		t.Fatalf("sources output:\n%s", stdout)
	}

	if _, _, err := runCLI(t, "--config", path, "config", "enable", "repos"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SourceEnabled("repos") {
		t.Fatal("repos should be enabled")
	}

	if _, _, err := runCLI(t, "--config", path, "config", "disable", "nope"); err == nil {
		t.Fatal("expected unknown source error")
	}
}
