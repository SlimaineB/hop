package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/dashboard"
	"github.com/SlimaineB/hop/internal/diag"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/plugins"
	"github.com/SlimaineB/hop/internal/search"
)

type fakeBackend struct {
	detail *app.Detail
	stats  *kube.PodStats
	rng    *search.TimeRange
	result *app.SearchResult
}

func (f *fakeBackend) Contexts(context.Context) ([]kube.Context, error) { return nil, nil }

func (f *fakeBackend) Search(context.Context, app.SearchRequest) (*app.SearchResult, error) {
	return f.searchResult(), nil
}

func (f *fakeBackend) SearchLocal(app.SearchRequest) (*app.SearchResult, error) {
	return f.searchResult(), nil
}

func (f *fakeBackend) searchResult() *app.SearchResult {
	if f.result != nil {
		return f.result
	}
	return &app.SearchResult{}
}

func (f *fakeBackend) Detail(_ context.Context, _ kube.PodRef, rng *search.TimeRange) (*app.Detail, error) {
	f.rng = rng
	return f.detail, nil
}

func (f *fakeBackend) PodStats(_ context.Context, ref kube.PodRef) (*kube.PodStats, error) {
	if f.stats != nil {
		return f.stats, nil
	}
	return &kube.PodStats{Context: ref.Context, Namespace: ref.Namespace, Name: ref.Name, Source: "kubelet"}, nil
}

func (f *fakeBackend) PodLogs(context.Context, kube.PodRef, *search.TimeRange) (string, error) {
	return "2026-11-27T10:00:00Z [api] INFO started\n2026-11-27T10:00:01Z [api] ERROR boom\n", nil
}

func (f *fakeBackend) Stats() app.Stats { return app.Stats{} }

func (f *fakeBackend) Snapshot() *app.Snapshot { return nil }

func TestTemplatesRender(t *testing.T) {
	backend := &fakeBackend{detail: sampleDetail()}
	server, err := New(backend, Options{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	index := httptest.NewRecorder()
	server.Handler().ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d, body=%s", index.Code, index.Body.String())
	}
	for _, needle := range []string{"Fleet health", "Group by", "dashboard.js", "theme-toggle"} {
		if !strings.Contains(index.Body.String(), needle) {
			t.Fatalf("dashboard body missing %q", needle)
		}
	}

	searchPage := httptest.NewRecorder()
	server.Handler().ServeHTTP(searchPage, httptest.NewRequest(http.MethodGet, "/search", nil))
	if searchPage.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchPage.Code, searchPage.Body.String())
	}
	searchBody := searchPage.Body.String()
	for _, needle := range []string{"Search everything", "Kubernetes", "Repositories", "data-palette-open", "sidebar-toggle"} {
		if !strings.Contains(searchBody, needle) {
			t.Fatalf("search body missing %q", needle)
		}
	}

	domainPage := httptest.NewRecorder()
	server.Handler().ServeHTTP(domainPage, httptest.NewRequest(http.MethodGet, "/git", nil))
	if domainPage.Code != http.StatusOK {
		t.Fatalf("domain status = %d, body=%s", domainPage.Code, domainPage.Body.String())
	}
	for _, needle := range []string{"Repositories", `data-sources="repos"`, "index.js"} {
		if !strings.Contains(domainPage.Body.String(), needle) {
			t.Fatalf("domain body missing %q", needle)
		}
	}

	values := url.Values{}
	values.Set("context", "prod-eu")
	values.Set("namespace", "apps")
	values.Set("name", "api-123")
	values.Set("q", "12h 14h")
	request := httptest.NewRequest(http.MethodGet, "/pod?"+values.Encode(), nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("pod status = %d, body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, needle := range []string{"api-123", "node-1", "10.0.1.2", "Logs/metrics range", "kubectl", "Topology", "topology-data", "Ingress", "data-tabs", `role="tab"`, "panel-overview", "panel-topology", "Live usage", "data-live-metrics", "data-live-refresh", "panel-logs", "data-pod-logs"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("pod body missing %q", needle)
		}
	}
	if backend.rng == nil {
		t.Fatalf("backend did not receive time range")
	}
}

func TestPodStatsEndpoint(t *testing.T) {
	backend := &fakeBackend{stats: &kube.PodStats{
		Context: "prod-eu", Namespace: "apps", Name: "api-123",
		Source: "kubelet", CPU: 750, Memory: 512 * 1024 * 1024,
		Network: kube.NetworkStats{RxBytes: 1000, TxBytes: 2000},
		Disk:    kube.DiskStats{UsedBytes: 10, CapacityBytes: 100},
	}}
	server, err := New(backend, Options{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	values := url.Values{}
	values.Set("context", "prod-eu")
	values.Set("namespace", "apps")
	values.Set("name", "api-123")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/pod/stats?"+values.Encode(), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var stats kube.PodStats
	if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats.Source != "kubelet" || stats.CPU != 750 || stats.Disk.CapacityBytes != 100 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestPodLogsEndpoint(t *testing.T) {
	server, err := New(&fakeBackend{}, Options{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	values := url.Values{}
	values.Set("context", "prod-eu")
	values.Set("namespace", "apps")
	values.Set("name", "api-123")
	values.Set("q", "12h 14h")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/pod/logs?"+values.Encode(), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Logs  string `json:"logs"`
		Range string `json:"range"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.Logs, "ERROR boom") || payload.Range == "" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestIndexRendersShortcuts(t *testing.T) {
	server, err := New(&fakeBackend{}, Options{Shortcuts: []config.ShortcutConfig{
		{Name: "grafana", URL: "https://grafana.example.com", Keywords: []string{"metrics"}},
		{Name: "bad", URL: "javascript:alert(1)"},
	}})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/search", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	for _, needle := range []string{"Browser shortcuts", "grafana", "https://grafana.example.com"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("body missing %q", needle)
		}
	}
	if strings.Contains(body, "javascript:alert(1)") {
		t.Fatalf("unsafe shortcut URL rendered")
	}
}

type fakeDashboardPods struct {
	snapshot *app.Snapshot
}

func (f *fakeDashboardPods) Snapshot() *app.Snapshot { return f.snapshot }

func (f *fakeDashboardPods) Warm(context.Context, app.WarmRequest) (*app.Snapshot, error) {
	return f.snapshot, nil
}

func (f *fakeDashboardPods) Metrics(context.Context, app.MetricsRequest) (*kube.MetricsResult, error) {
	return &kube.MetricsResult{}, nil
}

func TestDashboardEndpoint(t *testing.T) {
	pods := &fakeDashboardPods{snapshot: &app.Snapshot{Pods: []kube.Pod{
		{Context: "prod", Namespace: "apps", Name: "api-1", Phase: "Running", Ready: true, Env: "prod", Labels: map[string]string{"app": "api"}},
	}}}
	server, err := New(&fakeBackend{}, Options{
		Dashboard: dashboard.New(pods, nil, &config.Config{}),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var overview dashboard.Overview
	if err := json.NewDecoder(response.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	if overview.Totals.Pods != 1 || len(overview.Groups["env"]) != 1 {
		t.Fatalf("overview = %+v", overview)
	}
}

func TestDiagnosticsEndpoints(t *testing.T) {
	cfg := config.Config{Apps: []config.AppConfig{{
		Name: "checkout",
		Env:  "prod",
		Probes: []config.AppProbeConfig{
			{ID: "ok", Title: "Process", Type: "command", Command: "echo ready"},
		},
	}}}
	cfg.Normalize()
	server, err := New(&fakeBackend{}, Options{Apps: cfg.Apps, Diagnostics: cfg.Diagnostics})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	page := httptest.NewRecorder()
	server.Handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("page status = %d, body=%s", page.Code, page.Body.String())
	}
	for _, needle := range []string{"App", "diag-apps", "diagnostics.js"} {
		if !strings.Contains(page.Body.String(), needle) {
			t.Fatalf("diagnostics page missing %q", needle)
		}
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/diagnostics/apps", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("api status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Apps []diag.Report `json:"apps"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Apps) != 1 || payload.Apps[0].Target != "checkout" || len(payload.Apps[0].Probes) != 1 {
		t.Fatalf("apps = %+v", payload.Apps)
	}
	if payload.Apps[0].Verdict != diag.SeverityInfo {
		t.Fatalf("verdict = %s, findings=%+v", payload.Apps[0].Verdict, payload.Apps[0].Findings)
	}
}

func TestUnifiedEndpointCombinesSections(t *testing.T) {
	backend := &fakeBackend{result: &app.SearchResult{
		Hits: []app.HitView{{Pod: kube.Pod{Name: "api-1"}}},
		Meta: app.Meta{TotalPods: 1},
	}}
	shortcuts := []config.ShortcutConfig{
		{Name: "grafana", URL: "https://grafana.example.com", Keywords: []string{"metrics"}},
	}
	registry := plugins.Build(&config.Config{}, plugins.Deps{Pods: backend, Shortcuts: shortcuts})
	server, err := New(backend, Options{Shortcuts: shortcuts, Registry: registry})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/unified?q=grafana", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var result plugin.Response
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	pods := sectionByID(result.Sections, "pods")
	if pods == nil || len(pods.Items) != 1 || pods.Items[0].Title != "api-1" {
		t.Fatalf("pods = %+v", pods)
	}
	links := sectionByID(result.Sections, "shortcuts")
	if links == nil || len(links.Items) != 1 || links.Items[0].Title != "grafana" {
		t.Fatalf("shortcuts = %+v", links)
	}
	if result.Total != 2 {
		t.Fatalf("total = %d", result.Total)
	}

	restricted := httptest.NewRecorder()
	server.Handler().ServeHTTP(restricted, httptest.NewRequest(http.MethodGet, "/api/unified?q=grafana&sources=shortcuts", nil))
	if restricted.Code != http.StatusOK {
		t.Fatalf("restricted status = %d", restricted.Code)
	}
	var restrictedResult plugin.Response
	if err := json.NewDecoder(restricted.Body).Decode(&restrictedResult); err != nil {
		t.Fatal(err)
	}
	if len(restrictedResult.Sections) != 1 || restrictedResult.Sections[0].ID != "shortcuts" {
		t.Fatalf("restricted sections = %+v", restrictedResult.Sections)
	}
}

func sectionByID(sections []plugin.Result, id string) *plugin.Result {
	for i := range sections {
		if sections[i].ID == id {
			return &sections[i]
		}
	}
	return nil
}

func sampleDetail() *app.Detail {
	pod := kube.Pod{
		Context:         "prod-eu",
		Cluster:         "prod-eu-1",
		Env:             "prod",
		Display:         "PRODUCTION",
		Namespace:       "apps",
		Name:            "api-123",
		Phase:           "Running",
		Ready:           true,
		ReadyContainers: 1,
		ContainersCount: 1,
		Node:            "node-1",
		PodIP:           "10.0.2.10",
		StartTime:       time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC),
		OwnerKind:       "Deployment",
		OwnerName:       "api",
		Containers: []kube.Container{
			{Name: "api", Image: "ghcr.io/example/api:1.0", Ready: true, State: "running"},
		},
	}
	node := &kube.NodeInfo{
		Name:              "node-1",
		Ready:             true,
		InternalIP:        "10.0.1.2",
		Hostname:          "node-1.internal",
		OperatingSystem:   "linux",
		Architecture:      "amd64",
		KernelVersion:     "6.8.0",
		ContainerRuntime:  "containerd://1.7.0",
		KubeletVersion:    "v1.30.1",
		CapacityCPU:       "8",
		CapacityMemory:    "32Gi",
		CapacityPods:      "110",
		AllocatableCPU:    "7500m",
		AllocatableMemory: "30Gi",
		AllocatablePods:   "100",
		Labels:            map[string]string{"zone": "eu-west-1a"},
		Taints:            []kube.NodeTaint{{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"}},
		Conditions: []kube.NodeCondition{
			{Type: "Ready", Status: "True", Reason: "KubeletReady", LastHeartbeatTime: time.Now()},
		},
		CreationTimestamp: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
	}
	return &app.Detail{
		Pod: kube.PodDetail{
			Pod:  pod,
			Node: node,
			Conditions: []kube.Condition{
				{Type: "Ready", Status: "True"},
			},
			Events: []kube.Event{
				{Type: "Normal", Reason: "Started", Message: "Started container api", LastSeen: time.Now()},
			},
			Labels:      map[string]string{"app": "api"},
			Annotations: map[string]string{"deployment.kubernetes.io/revision": "12"},
			Describe:    "Name: api-123",
			YAML:        "kind: Pod",
			Topology: &kube.Topology{
				Nodes: []kube.TopologyNode{
					{ID: "pod/apps/api-123", Kind: "Pod", Name: "api-123", Status: "Running", Command: "kubectl --context prod-eu -n apps describe pod api-123"},
					{ID: "ingress/apps/api", Kind: "Ingress", Name: "api", Status: "api.example.com"},
					{ID: "service/apps/api", Kind: "Service", Name: "api", Subtitle: "ClusterIP"},
				},
				Edges: []kube.TopologyEdge{
					{From: "ingress/apps/api", To: "service/apps/api", Label: "routes to"},
					{From: "service/apps/api", To: "pod/apps/api-123", Label: "selects"},
				},
			},
		},
		Context: app.ContextInfo{Name: "prod-eu", Cluster: "prod-eu-1", Env: "prod", Display: "PRODUCTION"},
		Commands: []actions.Command{
			{ID: "k9s", Label: "Open in k9s", Command: "k9s --context prod-eu -n apps"},
			{ID: "logs", Label: "Logs", Command: "kubectl logs api-123"},
		},
	}
}
