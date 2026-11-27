package diag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

func TestLookupFactDottedKey(t *testing.T) {
	facts := map[string]any{
		"pod": map[string]any{
			"labels": map[string]any{"app.kubernetes.io/name": "checkout"},
		},
	}
	value, ok := lookupFact(facts, "pod.labels.app.kubernetes.io/name")
	if !ok || value != "checkout" {
		t.Fatalf("lookup = %v, %v", value, ok)
	}
}

func TestEngineCustomDeclarativeCheck(t *testing.T) {
	cfg := config.Config{
		Diagnostics: config.Diagnostics{Checks: []config.DiagnosticCheckConfig{{
			ID:       "restart-budget",
			Title:    "Restart budget exceeded",
			Kinds:    []string{"pod"},
			Severity: "critical",
			When: &config.DiagnosticCondition{
				Field: "pod.restarts",
				Op:    "gt",
				Value: 3,
			},
			Facts:  []string{"pod.restarts"},
			Remedy: "Inspect the crash loop.",
		}}},
	}
	cfg.Normalize()
	engine, err := NewEngine(cfg.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	detail := kube.PodDetail{Pod: kube.Pod{
		Context:   "prod",
		Namespace: "payments",
		Name:      "api",
		Phase:     "Running",
		Ready:     true,
		Restarts:  7,
	}}
	report := engine.DiagnosePod(context.Background(), detail, nil)
	assertFinding(t, report, "custom-restart-budget")
}

func TestEngineCustomCommandCheck(t *testing.T) {
	cfg := config.Config{
		Diagnostics: config.Diagnostics{Checks: []config.DiagnosticCheckConfig{{
			ID:       "disk-guard",
			Title:    "Disk guard",
			Kinds:    []string{"vm"},
			Severity: "warning",
			Command:  "df -h /",
			Expect:   config.DiagnosticExpect{StdoutContains: "enough"},
		}}},
	}
	cfg.Normalize()
	engine, err := NewEngine(cfg.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	server := servers.Server{Name: "vm-1", SSHChecks: true}
	runner := fakeRunner{result: servers.CommandResult{ExitCode: 1, Stderr: "no space left"}}
	report := engine.DiagnoseServer(context.Background(), server, servers.Check{Server: "vm-1"}, runner)
	assertFinding(t, report, "custom-disk-guard")
}

func TestEngineDisabledCustomCheck(t *testing.T) {
	cfg := config.Config{
		Diagnostics: config.Diagnostics{
			Disabled: []string{"custom-restart-budget"},
			Checks: []config.DiagnosticCheckConfig{{
				ID:    "restart-budget",
				Title: "Restart budget exceeded",
				Kinds: []string{"pod"},
				When:  &config.DiagnosticCondition{Field: "pod.restarts", Op: "gt", Value: 3},
			}},
		},
	}
	cfg.Normalize()
	engine, err := NewEngine(cfg.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	detail := kube.PodDetail{Pod: kube.Pod{Context: "prod", Namespace: "payments", Name: "api", Phase: "Running", Ready: true, Restarts: 7}}
	report := engine.DiagnosePod(context.Background(), detail, nil)
	for _, finding := range report.Findings {
		if finding.ID == "custom-restart-budget" {
			t.Fatalf("disabled finding still present: %+v", report.Findings)
		}
	}
}

func TestAppDiagnosticsRunsProbesInParallel(t *testing.T) {
	cfg := config.Config{Apps: []config.AppConfig{{
		Name: "checkout",
		Env:  "prod",
		Probes: []config.AppProbeConfig{
			{ID: "ok", Title: "OK probe", Type: "command", Command: "echo ready", Severity: "warning"},
			{ID: "bad", Title: "Bad probe", Type: "command", Command: "echo broken >&2; exit 2", Severity: "critical", Expect: config.DiagnosticExpect{ExitCode: intPtr(0)}},
		},
	}}}
	cfg.Normalize()
	engine, err := NewEngine(cfg.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	report := engine.DiagnoseApp(context.Background(), cfg.Apps[0])
	if len(report.Probes) != 2 {
		t.Fatalf("probes = %d, want 2", len(report.Probes))
	}
	if report.Verdict != SeverityCritical {
		t.Fatalf("verdict = %s, want critical: %+v", report.Verdict, report.Findings)
	}
	assertFinding(t, report, "probe-bad")
}

func TestAppLogProbe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	line := time.Now().Format(time.RFC3339) + " ERROR database unavailable\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Apps: []config.AppConfig{{
		Name:    "checkout",
		LogPath: path,
	}}}
	cfg.Normalize()
	engine, err := NewEngine(cfg.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	report := engine.DiagnoseApp(context.Background(), cfg.Apps[0])
	assertFinding(t, report, "probe-logs")
}

func TestAppCustomDeclarativeCheck(t *testing.T) {
	cfg := config.Config{
		Apps: []config.AppConfig{{
			Name: "checkout",
			Probes: []config.AppProbeConfig{{
				ID: "ok", Title: "OK probe", Type: "command", Command: "echo ready",
			}},
		}},
		Diagnostics: config.Diagnostics{Checks: []config.DiagnosticCheckConfig{{
			ID:       "probe-inventory",
			Title:    "Probe inventory",
			Kinds:    []string{"app"},
			Severity: "info",
			When:     &config.DiagnosticCondition{Field: "app.probe_count", Op: "gte", Value: 1},
			Facts:    []string{"app.probe_count"},
		}}},
	}
	cfg.Normalize()
	engine, err := NewEngine(cfg.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	report := engine.DiagnoseApp(context.Background(), cfg.Apps[0])
	assertFinding(t, report, "custom-probe-inventory")
}

type fakeRunner struct {
	result servers.CommandResult
}

func (f fakeRunner) RunCommand(context.Context, servers.Server, string) (servers.CommandResult, error) {
	return f.result, nil
}

func intPtr(value int) *int { return &value }
