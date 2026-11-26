package app

import (
	"strings"
	"testing"
	"time"

	"github.com/SlimaineB/teu/internal/config"
	"github.com/SlimaineB/teu/internal/kube"
	"github.com/SlimaineB/teu/internal/search"
)

func TestLinksForUsesTimeRange(t *testing.T) {
	cfg := config.Default()
	cfg.Links.Grafana.URL = "https://grafana.example.com"
	cfg.Links.Grafana.LogsURL = "{grafana}/logs?from={from_ms}&to={to_ms}&pod={pod}"
	cfg.Links.Loki.URL = "https://loki.example.com"
	cfg.Links.Loki.Query = `{namespace="{namespace}", pod="{pod}"}`
	svc := &Service{cfg: cfg}
	from := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)

	links := svc.LinksFor(kube.Pod{
		Context: "prod-eu", Cluster: "prod-eu-1", Namespace: "apps", Name: "api-123",
	}, &search.TimeRange{From: from, To: to})

	if !strings.Contains(links.GrafanaLogs, "from=1795608000000") {
		t.Fatalf("missing from ms: %s", links.GrafanaLogs)
	}
	if !strings.Contains(links.GrafanaLogs, "to=1795615200000") {
		t.Fatalf("missing to ms: %s", links.GrafanaLogs)
	}
	if links.LokiQuery != `{namespace="apps", pod="api-123"}` {
		t.Fatalf("loki query = %s", links.LokiQuery)
	}
}

func TestActionOptionsFor(t *testing.T) {
	svc := &Service{cfg: config.Default()}
	from := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	opts := svc.ActionOptionsFor(&search.TimeRange{From: from, To: to})
	if !opts.From.Equal(from) || !opts.To.Equal(to) {
		t.Fatalf("options range = %s -> %s", opts.From, opts.To)
	}
}
