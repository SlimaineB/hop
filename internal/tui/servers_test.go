package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/servers"
)

func testServerModel(t *testing.T) ServerModel {
	t.Helper()
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh_config")
	sshConfig := `Host sup-uat-vm-01
  HostName 10.1.2.3
  User deploy

Host web01
  User root
`
	if err := os.WriteFile(sshPath, []byte(sshConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{SSHConfig: sshPath}
	cfg.Normalize()
	return NewServers(ServerOptions{Service: servers.New(cfg)})
}

func TestServerViewModelRenders(t *testing.T) {
	model := testServerModel(t)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := updated.View()
	if !strings.Contains(view, "hop") || !strings.Contains(view, "VMs & servers") {
		t.Fatalf("view missing title: %q", view)
	}
	if !strings.Contains(view, "SERVERS") || !strings.Contains(view, "select a server") {
		t.Fatalf("view missing panes: %q", view)
	}
}

func TestServerViewHelp(t *testing.T) {
	model := testServerModel(t)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	view := updated.View()
	if !strings.Contains(view, "VMs & servers") || !strings.Contains(view, "run metrics, health and SSH checks") {
		t.Fatalf("help view incomplete: %q", view)
	}
}

func TestServerLocalSearchFiltersWhileEditing(t *testing.T) {
	model := testServerModel(t)
	model.editing = true
	model.input.SetValue("uat")

	cmd := model.localSearchCmd()
	if cmd == nil {
		t.Fatal("localSearchCmd returned nil")
	}
	msg, ok := cmd().(serverSearchMsg)
	if !ok {
		t.Fatalf("unexpected message type %T", cmd())
	}
	if !msg.local {
		t.Fatal("expected a local search message")
	}
	if msg.err != nil {
		t.Fatalf("local search error: %v", msg.err)
	}
	if len(msg.hits) != 1 || msg.hits[0].Server.Name != "sup-uat-vm-01" {
		t.Fatalf("local hits = %+v", msg.hits)
	}
}
