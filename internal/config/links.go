package config

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultLokiQuery = `{namespace="{namespace}", pod="{pod}"}`

type TemplateData struct {
	Context   string
	Cluster   string
	Env       string
	OrgID     string
	Namespace string
	Pod       string
	Container string
	Owner     string
	From      time.Time
	To        time.Time
	Grafana   string
	Loki      string
	Mimir     string
}

type RenderedLinks struct {
	GrafanaURL       string `json:"grafana_url,omitempty"`
	GrafanaPod       string `json:"grafana_pod,omitempty"`
	GrafanaLogs      string `json:"grafana_logs,omitempty"`
	GrafanaDashboard string `json:"grafana_dashboard,omitempty"`
	LokiURL          string `json:"loki_url,omitempty"`
	LokiExplore      string `json:"loki_explore,omitempty"`
	LokiQuery        string `json:"loki_query,omitempty"`
	MimirURL         string `json:"mimir_url,omitempty"`
	MimirExplore     string `json:"mimir_explore,omitempty"`
	MimirQuery       string `json:"mimir_query,omitempty"`
}

func (r RenderedLinks) HasAny() bool {
	return r.GrafanaURL != "" || r.GrafanaPod != "" || r.GrafanaLogs != "" ||
		r.LokiURL != "" || r.LokiExplore != "" || r.MimirURL != "" || r.MimirExplore != ""
}

func (l Links) Render(data TemplateData) RenderedLinks {
	if data.From.IsZero() {
		data.From = time.Now().Add(-time.Hour)
	}
	if data.To.IsZero() {
		data.To = time.Now()
	}
	data.Grafana = l.Grafana.URL
	data.Loki = l.Loki.URL
	data.Mimir = l.Mimir.URL

	lokiQuery := l.Loki.Query
	if lokiQuery == "" && l.Loki.URL != "" {
		lokiQuery = defaultLokiQuery
	}
	mimirQuery := l.Mimir.Query

	return RenderedLinks{
		GrafanaURL:       data.Grafana,
		GrafanaPod:       render(l.Grafana.PodURL, data),
		GrafanaLogs:      render(l.Grafana.LogsURL, data),
		GrafanaDashboard: render(l.Grafana.DashboardURL, data),
		LokiURL:          data.Loki,
		LokiExplore:      render(l.Loki.ExploreURL, data),
		LokiQuery:        render(lokiQuery, data),
		MimirURL:         data.Mimir,
		MimirExplore:     render(l.Mimir.ExploreURL, data),
		MimirQuery:       render(mimirQuery, data),
	}
}

func render(template string, data TemplateData) string {
	if template == "" {
		return ""
	}
	from := data.From.Format(time.RFC3339)
	to := data.To.Format(time.RFC3339)
	replacer := strings.NewReplacer(
		"{context_q}", url.QueryEscape(data.Context),
		"{cluster_q}", url.QueryEscape(data.Cluster),
		"{env_q}", url.QueryEscape(data.Env),
		"{org_id_q}", url.QueryEscape(data.OrgID),
		"{namespace_q}", url.QueryEscape(data.Namespace),
		"{pod_q}", url.QueryEscape(data.Pod),
		"{container_q}", url.QueryEscape(data.Container),
		"{owner_q}", url.QueryEscape(data.Owner),
		"{from_q}", url.QueryEscape(from),
		"{to_q}", url.QueryEscape(to),
		"{context}", data.Context,
		"{cluster}", data.Cluster,
		"{env}", data.Env,
		"{org_id}", data.OrgID,
		"{namespace}", data.Namespace,
		"{pod}", data.Pod,
		"{container}", data.Container,
		"{owner}", data.Owner,
		"{from}", from,
		"{to}", to,
		"{from_unix}", strconv.FormatInt(data.From.Unix(), 10),
		"{to_unix}", strconv.FormatInt(data.To.Unix(), 10),
		"{from_ms}", strconv.FormatInt(data.From.UnixMilli(), 10),
		"{to_ms}", strconv.FormatInt(data.To.UnixMilli(), 10),
		"{from_ns}", strconv.FormatInt(data.From.UnixNano(), 10),
		"{to_ns}", strconv.FormatInt(data.To.UnixNano(), 10),
		"{grafana}", data.Grafana,
		"{loki}", data.Loki,
		"{mimir}", data.Mimir,
	)
	return replacer.Replace(template)
}
