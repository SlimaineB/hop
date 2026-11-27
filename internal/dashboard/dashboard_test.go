package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

type fakePods struct {
	snapshot *app.Snapshot
	metrics  *kube.MetricsResult
}

func (f *fakePods) Snapshot() *app.Snapshot { return f.snapshot }

func (f *fakePods) Warm(context.Context, app.WarmRequest) (*app.Snapshot, error) {
	return f.snapshot, nil
}

func (f *fakePods) Metrics(context.Context, app.MetricsRequest) (*kube.MetricsResult, error) {
	return f.metrics, nil
}

type fakeServers struct {
	list   []servers.Server
	checks map[string]servers.Check
}

func (f *fakeServers) List() ([]servers.Server, error) { return f.list, nil }

func (f *fakeServers) CheckServer(_ context.Context, server servers.Server) servers.Check {
	return f.checks[server.Name]
}

func TestOverviewGroupsAndIssues(t *testing.T) {
	pods := &fakePods{
		snapshot: &app.Snapshot{Pods: []kube.Pod{
			{Context: "prod", Cluster: "prod-1", Env: "prod", Namespace: "apps", Name: "api-1", Phase: "Running", Ready: true, Node: "n1", Labels: map[string]string{"app": "api"}},
			{Context: "prod", Cluster: "prod-1", Env: "prod", Namespace: "apps", Name: "api-2", Phase: "Running", Ready: false, Restarts: 2, Node: "n1", Labels: map[string]string{"app": "api"}},
			{Context: "dev", Cluster: "dev-1", Env: "dev", Namespace: "batch", Name: "worker-1", Phase: "Pending", Node: "n2", Labels: map[string]string{"app": "worker"}},
			{Context: "dev", Cluster: "dev-1", Env: "dev", Namespace: "batch", Name: "job-1", Phase: "Succeeded", Node: "n2", Labels: map[string]string{"app": "job"}},
		}},
		metrics: &kube.MetricsResult{
			Pods: []kube.PodMetrics{
				{Context: "prod", Namespace: "apps", Name: "api-1", CPU: 100, Memory: 1 << 20},
				{Context: "prod", Namespace: "apps", Name: "api-2", CPU: 200, Memory: 2 << 20},
			},
			Nodes: []kube.NodeMetrics{
				{Context: "prod", Name: "n1", Ready: true, CPU: 300, Memory: 3 << 20, CPUCapacity: 2000, MemoryCapacity: 8 << 30},
				{Context: "dev", Name: "n2", Ready: true, CPUCapacity: 2000, MemoryCapacity: 8 << 30},
			},
		},
	}
	service := New(pods, nil, nil)
	overview, err := service.Overview(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Totals.Pods != 4 || overview.Totals.Ready != 1 || overview.Totals.NotReady != 1 || overview.Totals.Pending != 1 || overview.Totals.Failed != 0 || overview.Totals.Completed != 1 {
		t.Fatalf("totals = %+v", overview.Totals)
	}
	if overview.Totals.CPU != 300 || overview.Totals.CPUCapacity != 4000 || !overview.Totals.MetricsAvailable {
		t.Fatalf("metric totals = %+v", overview.Totals)
	}
	if len(overview.Groups["env"]) != 2 {
		t.Fatalf("env groups = %+v", overview.Groups["env"])
	}
	prod := findBucket(overview.Groups["env"], "prod")
	if prod == nil || prod.Pods != 2 || prod.Ready != 1 || prod.CPU != 300 {
		t.Fatalf("prod bucket = %+v", prod)
	}
	if len(overview.Issues) != 2 {
		t.Fatalf("issues = %+v", overview.Issues)
	}
	if overview.Issues[0].Name != "worker-1" {
		t.Fatalf("top issue = %+v", overview.Issues[0])
	}
	for _, issue := range overview.Issues {
		if issue.Name == "job-1" {
			t.Fatalf("completed pod reported as issue: %+v", issue)
		}
	}
	if len(overview.Nodes) != 2 || overview.Nodes[0].CPUCapacity != 2000 {
		t.Fatalf("nodes = %+v", overview.Nodes)
	}
}

func TestServerNetworkStatus(t *testing.T) {
	source := &fakeServers{
		list: []servers.Server{
			{Name: "tcp-only", Host: "10.0.0.1"},
			{Name: "unreachable", Host: "10.0.0.2"},
		},
		checks: map[string]servers.Check{
			"tcp-only": {
				CheckedAt: time.Now(),
				Machine:   servers.MachineInfo{Error: "SSH checks disabled"},
				Network:   servers.NetworkInfo{Checked: true, Reachable: true, Ping: servers.PingCheck{Error: "blocked"}, Ports: []servers.PortCheck{{Port: 22, Open: true, State: "open"}}},
			},
			"unreachable": {
				CheckedAt: time.Now(),
				Machine:   servers.MachineInfo{Error: "ssh failed"},
				Network:   servers.NetworkInfo{Checked: true, Reachable: false, Ping: servers.PingCheck{Error: "timeout"}, Ports: []servers.PortCheck{{Port: 22, State: "filtered"}}},
			},
		},
	}
	service := New(nil, source, nil)
	cards, err := service.ServerChecks(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if card := findServer(cards, "tcp-only"); card == nil || card.Status != "ok" || !card.NetworkReachable || card.OpenPorts != 1 {
		t.Fatalf("tcp-only card = %+v", card)
	}
	if card := findServer(cards, "unreachable"); card == nil || card.Status != "down" || card.NetworkReachable {
		t.Fatalf("unreachable card = %+v", card)
	}
}

func TestOverviewWithoutPods(t *testing.T) {
	service := New(nil, nil, nil)
	overview, err := service.Overview(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Totals.Pods != 0 || overview.Groups == nil {
		t.Fatalf("overview = %+v", overview)
	}
}

func TestServerChecksStatusAndCache(t *testing.T) {
	source := &fakeServers{
		list: []servers.Server{
			{Name: "web-1", Env: "prod", Host: "10.0.0.1"},
			{Name: "db-1", Env: "prod", Host: "10.0.0.2"},
		},
		checks: map[string]servers.Check{
			"web-1": {
				CheckedAt: time.Now(),
				Health:    servers.HTTPCheck{URL: "https://web/health", OK: true},
				Machine:   servers.MachineInfo{OK: true, CPUs: 4, MemoryTotal: 100, MemoryUsed: 40, DiskPercent: "52%"},
			},
			"db-1": {
				CheckedAt: time.Now(),
				Health:    servers.HTTPCheck{URL: "https://db/health", Error: "timeout"},
				Machine:   servers.MachineInfo{Error: "ssh failed"},
			},
		},
	}
	service := New(nil, source, nil)
	cards, err := service.ServerChecks(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Name != "db-1" || cards[0].Status != "down" {
		t.Fatalf("cards = %+v", cards)
	}
	web := findServer(cards, "web-1")
	if web == nil || web.Status != "ok" || !web.HealthOK || web.CPUs != 4 {
		t.Fatalf("web card = %+v", web)
	}

	source.checks["web-1"] = servers.Check{Health: servers.HTTPCheck{URL: "https://web/health", Error: "down"}}
	cached, err := service.ServerChecks(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if findServer(cached, "web-1").Status != "ok" {
		t.Fatalf("cache was not used: %+v", cached)
	}
	forced, err := service.ServerChecks(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if findServer(forced, "web-1").Status != "down" {
		t.Fatalf("forced check did not refresh: %+v", forced)
	}
}

func findBucket(buckets []Bucket, key string) *Bucket {
	for i := range buckets {
		if buckets[i].Key == key {
			return &buckets[i]
		}
	}
	return nil
}

func findServer(cards []ServerCard, name string) *ServerCard {
	for i := range cards {
		if cards[i].Name == name {
			return &cards[i]
		}
	}
	return nil
}
