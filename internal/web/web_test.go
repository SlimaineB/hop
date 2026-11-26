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
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/plugins"
	"github.com/SlimaineB/hop/internal/search"
)

type fakeBackend struct {
	detail *app.Detail
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
	if !strings.Contains(index.Body.String(), "Find anything") {
		t.Fatalf("index body missing hero")
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
	for _, needle := range []string{"api-123", "node-1", "10.0.1.2", "Logs/metrics range", "kubectl", "Topology", "topology-data", "Ingress"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("pod body missing %q", needle)
		}
	}
	if backend.rng == nil {
		t.Fatalf("backend did not receive time range")
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
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
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
