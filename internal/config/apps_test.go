package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestNormalizeAppsAndProbes(t *testing.T) {
	raw := `
apps:
  - name: Checkout
    tags: [api, prod]
    port: 8080
    log_path: /tmp/checkout.log
    probes:
      - id: queue
        type: command
        command: "systemctl is-active checkout"
      - type: tcp
        port: 5432
diagnostics:
  checks:
    - id: app-probe
      kinds: [applications]
      when:
        field: app.failed_probe_count
        op: eq
        value: 0
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Normalize()
	if len(cfg.Apps) != 1 {
		t.Fatalf("apps = %d, want 1", len(cfg.Apps))
	}
	app := cfg.Apps[0]
	if app.Host != "127.0.0.1" || app.Env != "prod" {
		t.Fatalf("app defaults = host %q env %q", app.Host, app.Env)
	}
	if app.LogPattern == "" || app.LogSince.Get() != 15*time.Minute {
		t.Fatalf("log defaults = pattern %q since %s", app.LogPattern, app.LogSince.Get())
	}
	if len(app.Probes) != 2 {
		t.Fatalf("probes = %d, want 2: %+v", len(app.Probes), app.Probes)
	}
	if app.Probes[0].ID != "queue" || app.Probes[0].Severity != "warning" {
		t.Fatalf("explicit probe = %+v", app.Probes[0])
	}
	if app.Probes[1].ID != "tcp" || app.Probes[1].Port != 5432 {
		t.Fatalf("inferred tcp probe = %+v", app.Probes[1])
	}
	if len(cfg.Diagnostics.Checks) != 1 || len(cfg.Diagnostics.Checks[0].Kinds) != 1 || cfg.Diagnostics.Checks[0].Kinds[0] != "app" {
		t.Fatalf("diagnostic kinds = %+v", cfg.Diagnostics.Checks)
	}
}
