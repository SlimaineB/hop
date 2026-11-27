package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/diag"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

func TestPodTroubleshootKeyEntersDiagMode(t *testing.T) {
	model := New(Options{})
	model.hits = []app.HitView{{Pod: kube.Pod{Context: "prod", Namespace: "apps", Name: "api-1"}}}
	model.cursor = 0

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	next := updated.(Model)
	if !next.diagMode {
		t.Fatal("diag mode not enabled")
	}
	view := next.View()
	if !strings.Contains(view, "pod diagnostics") || !strings.Contains(view, "prod/apps/api-1") {
		t.Fatalf("diag view missing context: %q", view)
	}
}

func TestServerTroubleshootKeyEntersDiagMode(t *testing.T) {
	model := testServerModel(t)
	model.hits = []servers.Hit{{Server: servers.Server{Name: "web01"}}}
	model.cursor = 0

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	next := updated.(ServerModel)
	if !next.diagMode {
		t.Fatal("diag mode not enabled")
	}
	if cmd == nil {
		t.Fatal("expected a diagnostics command")
	}
	view := next.View()
	if !strings.Contains(view, "VM diagnostics") || !strings.Contains(view, "web01") {
		t.Fatalf("diag view missing server: %q", view)
	}
}

func TestDiagnosticsModelSortsBySeverity(t *testing.T) {
	bad := diag.NewReport("app", "bad-app")
	bad.Add(diag.Finding{ID: "probe-x", Severity: diag.SeverityCritical, Title: "Probe failed"})
	bad.Finish()
	good := diag.NewReport("app", "good-app")
	good.Finish()

	model := NewDiagnostics(DiagnosticsOptions{Apps: []config.AppConfig{{Name: "bad-app"}, {Name: "good-app"}}})
	model.width = 150
	model.height = 40
	model.reports = []*diag.Report{good, bad}
	sortDiagReports(model.reports)
	model.cursor = 0
	view := model.View()
	if !strings.Contains(view, "APPLICATIONS") || !strings.Contains(view, "CRITICAL") {
		t.Fatalf("view missing summary: %q", view)
	}
	if strings.Index(view, "bad-app") > strings.Index(view, "good-app") {
		t.Fatalf("critical app should be first: %q", view)
	}
}
