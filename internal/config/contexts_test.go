package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadContextsDefaultConfigOnly(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("ui:\n  theme: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := LoadContexts(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(set.Contexts))
	}
	if set.DefaultName != "default" {
		t.Fatalf("default = %q, want default", set.DefaultName)
	}
	ctx, ok := set.Default()
	if !ok || ctx.Path != configPath {
		t.Fatalf("default context = %+v, ok=%v", ctx, ok)
	}
}

func TestLoadContextsDiscoversNamedFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("ui:\n  theme: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contextsDir := filepath.Join(dir, "contexts")
	if err := os.MkdirAll(contextsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"work.yaml", "lab.yml", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(contextsDir, name), []byte("ui:\n  theme: dark\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	set, err := LoadContexts(configPath)
	if err != nil {
		t.Fatal(err)
	}
	names := set.Names()
	if len(names) != 3 || names[0] != "default" || names[1] != "lab" || names[2] != "work" {
		t.Fatalf("names = %v", names)
	}
	if set.DefaultName != "default" {
		t.Fatalf("default = %q", set.DefaultName)
	}
}

func TestLoadContextsUsesPersistedDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("ui:\n  theme: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contextsDir := filepath.Join(dir, "contexts")
	if err := os.MkdirAll(contextsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextsDir, "work.yaml"), []byte("ui:\n  theme: light\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "active-context"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := LoadContexts(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if set.DefaultName != "work" {
		t.Fatalf("default = %q, want work", set.DefaultName)
	}
	ctx, ok := set.Default()
	if !ok || ctx.Name != "work" || ctx.Path != filepath.Join(contextsDir, "work.yaml") {
		t.Fatalf("default context = %+v, ok=%v", ctx, ok)
	}
}

func TestLoadContextsWithoutBaseConfigUsesFirstContext(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	contextsDir := filepath.Join(dir, "contexts")
	if err := os.MkdirAll(contextsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextsDir, "work.yaml"), []byte("ui:\n  theme: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := LoadContexts(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if set.DefaultName != "work" {
		t.Fatalf("default = %q, want work", set.DefaultName)
	}
}

func TestLoadContextsRejectsDuplicateDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	contextsDir := filepath.Join(dir, "contexts")
	if err := os.MkdirAll(contextsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextsDir, "default.yaml"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadContexts(configPath); err == nil {
		t.Fatal("expected duplicate default error")
	}
}

func TestLoadContextsRejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	contextsDir := filepath.Join(dir, "contexts")
	if err := os.MkdirAll(contextsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextsDir, "bad name.yaml"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadContexts(filepath.Join(dir, "config.yaml")); err == nil {
		t.Fatal("expected invalid name error")
	}
}

func TestContextSetSetDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	contextsDir := filepath.Join(dir, "contexts")
	if err := os.MkdirAll(contextsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextsDir, "work.yaml"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := LoadContexts(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.SetDefault("work"); err != nil {
		t.Fatal(err)
	}
	if set.DefaultName != "work" {
		t.Fatalf("default = %q", set.DefaultName)
	}
	data, err := os.ReadFile(set.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "work\n" {
		t.Fatalf("state = %q", data)
	}
	if err := set.SetDefault("missing"); err == nil {
		t.Fatal("expected unknown context error")
	}
}
