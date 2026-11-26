package config

import (
	"strings"
	"testing"
	"time"
)

func TestRenderUsesRequestedTimeRange(t *testing.T) {
	from := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	links := Links{
		Grafana: Grafana{
			URL:     "https://grafana.example.com",
			LogsURL: "{grafana}/explore?from={from_unix}&to={to_unix}&from_ms={from_ms}&to_ms={to_ms}&start={from_q}&end={to_q}",
		},
		Loki: Loki{
			URL:        "https://loki.example.com",
			Query:      `{namespace="{namespace}", pod="{pod}"}`,
			ExploreURL: "{grafana}/explore?left={loki}&from={from}&to={to}",
		},
		Mimir: Mimir{
			URL:        "https://mimir.example.com",
			ExploreURL: "{mimir}/graph?from={from_unix}&to={to_unix}",
		},
	}

	rendered := links.Render(TemplateData{
		Context:   "prod-eu",
		Cluster:   "prod-eu-1",
		Env:       "prod",
		Namespace: "apps",
		Pod:       "api-123",
		Container: "api",
		Owner:     "api",
		From:      from,
		To:        to,
	})

	if !strings.Contains(rendered.GrafanaLogs, "from=1795608000") {
		t.Fatalf("grafana logs url missing from unix: %s", rendered.GrafanaLogs)
	}
	if !strings.Contains(rendered.GrafanaLogs, "to=1795615200") {
		t.Fatalf("grafana logs url missing to unix: %s", rendered.GrafanaLogs)
	}
	if !strings.Contains(rendered.GrafanaLogs, "from_ms=1795608000000") {
		t.Fatalf("grafana logs url missing from ms: %s", rendered.GrafanaLogs)
	}
	if !strings.Contains(rendered.LokiExplore, "2026-11-25T12:00:00Z") {
		t.Fatalf("loki explore url missing RFC3339 from: %s", rendered.LokiExplore)
	}
	if !strings.Contains(rendered.MimirExplore, "from=1795608000") {
		t.Fatalf("mimir explore url missing from unix: %s", rendered.MimirExplore)
	}
	if rendered.LokiQuery != `{namespace="apps", pod="api-123"}` {
		t.Fatalf("loki query = %s", rendered.LokiQuery)
	}
}
