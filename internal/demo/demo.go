package demo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/dashboard"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/plugins"
	"github.com/SlimaineB/hop/internal/search"
	"github.com/SlimaineB/hop/internal/servers"
)

type Runtime struct {
	Config    *config.Config
	App       *app.Service
	Backend   Backend
	Servers   *servers.Service
	Registry  *plugin.Registry
	Dashboard *dashboard.Service
}

type Backend struct {
	*app.Service
}

func New() (*Runtime, error) {
	cfg := Config()
	service, err := app.NewWithProvider(cfg, NewProvider())
	if err != nil {
		return nil, err
	}
	if _, err := service.Warm(context.Background(), app.WarmRequest{ForceRefresh: true}); err != nil {
		_ = service.Close()
		return nil, err
	}
	serverService := servers.New(cfg)
	registry := plugins.Build(cfg, plugins.Deps{
		Pods:      service,
		Servers:   serverService,
		Shortcuts: cfg.Shortcuts,
	})
	for _, source := range demoSources() {
		_ = registry.Register(source)
	}
	return &Runtime{
		Config:    cfg,
		App:       service,
		Backend:   Backend{Service: service},
		Servers:   serverService,
		Registry:  registry,
		Dashboard: dashboard.New(service, serverService, cfg),
	}, nil
}

func (r *Runtime) Close() error {
	if r == nil || r.App == nil {
		return nil
	}
	return r.App.Close()
}

func (b Backend) PodLogs(_ context.Context, ref kube.PodRef, _ *search.TimeRange) (string, error) {
	return Logs(ref), nil
}

func Config() *config.Config {
	cfg := config.Default()
	cfg.Kube.Kubeconfigs = nil
	cfg.Kube.CacheTTL = 0
	cfg.Kube.RequestTimeout = config.Duration(2 * time.Second)
	cfg.SSHConfig = filepath.Join(os.TempDir(), "hop-demo-no-ssh-config")
	cfg.Search.MinScore = 0.1
	cfg.Workspaces = []config.Workspace{
		{Name: "acme", Context: "demo-prod", Env: "prod", Display: "acme/prod", OrgID: "acme"},
		{Name: "acme", Context: "demo-staging", Env: "staging", Display: "acme/staging", OrgID: "acme"},
	}
	cfg.Links = config.Links{
		Grafana: config.Grafana{
			URL:          "https://grafana.example.com",
			DashboardURL: "https://grafana.example.com/d/k8s/pods?var-namespace={namespace}&var-pod={pod}",
			PodURL:       "https://grafana.example.com/d/k8s/pods?from={from_ms}&to={to_ms}&var-namespace={namespace}&var-pod={pod}",
			LogsURL:      "https://grafana.example.com/explore?from={from_ms}&to={to_ms}&var-namespace={namespace}&var-pod={pod}",
		},
		Loki: config.Loki{
			URL:        "https://grafana.example.com",
			ExploreURL: "https://grafana.example.com/explore?from={from_ms}&to={to_ms}&var-namespace={namespace}&var-pod={pod}",
			Query:      `{namespace="{namespace}", pod="{pod}"}`,
		},
		Mimir: config.Mimir{
			URL:        "https://grafana.example.com",
			ExploreURL: "https://grafana.example.com/explore?from={from_ms}&to={to_ms}&var-namespace={namespace}&var-pod={pod}",
			Query:      `sum(rate(container_cpu_usage_seconds_total{namespace="{namespace}", pod="{pod}"}[5m]))`,
		},
	}
	off := false
	cfg.Servers = []config.ServerConfig{
		{Name: "prod-api-01", Host: "10.20.0.11", User: "deploy", Env: "prod", Tags: []string{"api", "web", "prod"}, SSHChecks: &off, NetworkChecks: &off},
		{Name: "prod-db-01", Host: "10.20.0.12", User: "dba", Env: "prod", Tags: []string{"db", "postgres", "prod"}, SSHChecks: &off, NetworkChecks: &off},
		{Name: "prod-worker-01", Host: "10.20.0.13", User: "deploy", Env: "prod", Tags: []string{"worker", "queue", "prod"}, SSHChecks: &off, NetworkChecks: &off},
		{Name: "staging-api-01", Host: "10.30.0.11", User: "deploy", Env: "staging", Tags: []string{"api", "web", "staging"}, SSHChecks: &off, NetworkChecks: &off},
		{Name: "staging-db-01", Host: "10.30.0.12", User: "dba", Env: "staging", Tags: []string{"db", "postgres", "staging"}, SSHChecks: &off, NetworkChecks: &off},
		{Name: "bastion-01", Host: "10.10.0.5", User: "ops", Env: "shared", Tags: []string{"bastion", "ssh", "shared"}, SSHChecks: &off, NetworkChecks: &off},
	}
	cfg.Apps = []config.AppConfig{
		{
			Name:        "checkout-api",
			Description: "Customer checkout API",
			Env:         "prod",
			Host:        "127.0.0.1",
			Probes: []config.AppProbeConfig{
				{ID: "process", Title: "Process probe", Type: "command", Command: "echo checkout-api ready", Severity: "critical"},
				{ID: "config", Title: "Config probe", Type: "command", Command: "printf 'INFO config loaded\nINFO listening on :8080\n'", Severity: "warning"},
			},
		},
		{
			Name:        "ledger-worker",
			Description: "Async ledger worker",
			Env:         "prod",
			Host:        "127.0.0.1",
			Probes: []config.AppProbeConfig{
				{ID: "queue", Title: "Queue probe", Type: "command", Command: "echo ledger queue healthy", Severity: "critical"},
				{ID: "retry-budget", Title: "Retry budget", Type: "command", Command: "echo retries=2/10", Severity: "warning"},
			},
		},
		{
			Name:        "notifications",
			Description: "Notification dispatcher",
			Env:         "staging",
			Host:        "127.0.0.1",
			Probes: []config.AppProbeConfig{
				{ID: "smtp", Title: "SMTP probe", Type: "command", Command: "echo smtp queue connected", Severity: "critical"},
			},
		},
	}
	cfg.Shortcuts = []config.ShortcutConfig{
		{Name: "grafana", URL: "https://grafana.example.com", Keywords: []string{"metrics", "dashboards", "observability"}, Description: "Metrics and dashboards"},
		{Name: "gitlab", URL: "https://gitlab.example.com/acme", Keywords: []string{"git", "repos", "merge requests"}, Description: "Source code and merge requests"},
		{Name: "jira", URL: "https://jira.example.com", Keywords: []string{"tickets", "issues", "sprint"}, Description: "Issue tracker"},
		{Name: "runbooks", URL: "https://wiki.example.com/runbooks", Keywords: []string{"docs", "wiki", "on-call"}, Description: "On-call runbooks"},
		{Name: "status", URL: "https://status.example.com", Keywords: []string{"incidents", "status", "uptime"}, Description: "Service status"},
	}
	cfg.Sources = config.Sources{}
	cfg.Normalize()
	return cfg
}

func Logs(ref kube.PodRef) string {
	now := time.Now()
	lines := []string{
		fmt.Sprintf("%s INFO  %s starting request processing namespace=%s pod=%s", now.Add(-28*time.Second).Format(time.RFC3339), ref.Name, ref.Namespace, ref.Name),
		fmt.Sprintf("%s INFO  cache warmup complete entries=1842 duration=42ms", now.Add(-24*time.Second).Format(time.RFC3339)),
		fmt.Sprintf("%s INFO  GET /healthz status=200 latency=1.8ms", now.Add(-19*time.Second).Format(time.RFC3339)),
		fmt.Sprintf("%s WARN  downstream payments-api timeout after 900ms retry=1/2", now.Add(-13*time.Second).Format(time.RFC3339)),
		fmt.Sprintf("%s INFO  GET /v1/checkout status=200 latency=118ms trace=7f3c9a2e", now.Add(-8*time.Second).Format(time.RFC3339)),
		fmt.Sprintf("%s INFO  GET /v1/checkout status=200 latency=96ms trace=8b1d4c7f", now.Add(-4*time.Second).Format(time.RFC3339)),
		fmt.Sprintf("%s INFO  metrics flushed samples=212 queue_depth=0", now.Add(-1*time.Second).Format(time.RFC3339)),
	}
	return strings.Join(lines, "\n") + "\n"
}
