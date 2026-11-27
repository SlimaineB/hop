package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestPrepareHopContextDiscoversAndSelects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOP_CONFIG", "")
	t.Setenv("HOP_CONTEXT", "")

	hopDir := filepath.Join(home, ".config", "hop")
	if err := os.MkdirAll(filepath.Join(hopDir, "contexts"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(hopDir, "config.yaml")
	if err := os.WriteFile(base, []byte("ui:\n  theme: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(hopDir, "contexts", "work.yaml")
	if err := os.WriteFile(work, []byte("ui:\n  theme: light\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := &App{}
	cmd := &cobra.Command{Use: "hop"}
	if err := a.prepareHopContext(cmd); err != nil {
		t.Fatal(err)
	}
	if !a.multiContexts() {
		t.Fatal("expected multiple contexts")
	}
	if a.contextName != "default" || a.configPath != base {
		t.Fatalf("active context = %q %q", a.contextName, a.configPath)
	}

	a = &App{hopContext: "work"}
	if err := a.prepareHopContext(cmd); err != nil {
		t.Fatal(err)
	}
	if a.contextName != "work" || a.configPath != work {
		t.Fatalf("active context = %q %q", a.contextName, a.configPath)
	}

	a = &App{hopContext: "missing"}
	if err := a.prepareHopContext(cmd); err == nil {
		t.Fatal("expected unknown context error")
	}
}

func TestCommandUsesHopContext(t *testing.T) {
	for _, name := range []string{"version", "demo", "completion", "help"} {
		if commandUsesHopContext(&cobra.Command{Use: name}) {
			t.Fatalf("command %q should not use hop contexts", name)
		}
	}
	if !commandUsesHopContext(&cobra.Command{Use: "web"}) {
		t.Fatal("web should use hop contexts")
	}
}
