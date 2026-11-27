package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/search"
)

func TestViewModelRenders(t *testing.T) {
	model := New(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := updated.View()
	if !strings.Contains(view, "hop") {
		t.Fatalf("view missing title: %q", view)
	}
	if !strings.Contains(view, "RESULTS") || !strings.Contains(view, "PREVIEW") {
		t.Fatalf("view missing panes")
	}
}

func TestViewHelp(t *testing.T) {
	model := New(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	view := updated.View()
	if !strings.Contains(view, "SEARCH") || !strings.Contains(view, "logs/metrics range") {
		t.Fatalf("help view incomplete: %q", view)
	}
	if !strings.Contains(view, "kubectl exec") {
		t.Fatalf("help view missing exec action: %q", view)
	}
}

func TestViewRendersNodeAndRange(t *testing.T) {
	pod := kube.Pod{
		Context: "prod-eu", Namespace: "apps", Name: "api-123",
		Phase: "Running", Ready: true, Node: "node-1",
		ReadyContainers: 1, ContainersCount: 1,
		StartTime: time.Now().Add(-2 * time.Hour),
	}
	model := New(Options{})
	model.hits = []app.HitView{{Hit: search.Hit{Score: 0.9}, Pod: pod}}
	model.result = &app.SearchResult{
		Query:    search.Query{Raw: "api 12h 14h"},
		Hits:     model.hits,
		Eligible: 1,
		Total:    1,
	}
	model.timeRange = &search.TimeRange{
		From: time.Date(2026, 11, 25, 12, 0, 0, 0, time.Local),
		To:   time.Date(2026, 11, 25, 14, 0, 0, 0, time.Local),
	}
	model.detail = &app.Detail{
		Pod: kube.PodDetail{
			Pod: pod,
			Node: &kube.NodeInfo{
				Name: "node-1", Ready: true, InternalIP: "10.0.1.2",
				OperatingSystem: "linux", Architecture: "amd64",
				KubeletVersion: "v1.30.1", ContainerRuntime: "containerd://1.7.0",
				AllocatableCPU: "7500m", CapacityCPU: "8",
				AllocatableMemory: "30Gi", CapacityMemory: "32Gi",
				AllocatablePods: "100", CapacityPods: "110",
				Taints:     []kube.NodeTaint{{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"}},
				Conditions: []kube.NodeCondition{{Type: "Ready", Status: "True"}},
			},
		},
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 48})
	view := updated.View()
	for _, needle := range []string{"NODE", "node-1", "10.0.1.2", "containerd", "12:00 → 14:00"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("view missing %q", needle)
		}
	}
}

func TestViewMetricsAndLogs(t *testing.T) {
	pod := kube.Pod{
		Context: "prod-eu", Namespace: "apps", Name: "api-123",
		Phase: "Running", Ready: true, Node: "node-1",
		ReadyContainers: 1, ContainersCount: 1,
		CPULimit: 1000, MemoryLimit: 2 * 1024 * 1024 * 1024,
		StartTime: time.Now().Add(-time.Hour),
	}
	model := New(Options{})
	model.hits = []app.HitView{{Hit: search.Hit{Score: 0.9}, Pod: pod}}
	model.stats = &kube.PodStats{
		Source:    "kubelet",
		FetchedAt: time.Now(),
		CPU:       500,
		Memory:    1024 * 1024 * 1024,
		Network:   kube.NetworkStats{RxBytes: 1000, TxBytes: 2000},
		Disk:      kube.DiskStats{UsedBytes: 50, CapacityBytes: 100},
		Node: &kube.NodeUsage{
			Name: "node-1", Ready: true, CPU: 1000, CPUCapacity: 4000,
			Memory: 2 * 1024 * 1024 * 1024, MemoryCapacity: 8 * 1024 * 1024 * 1024,
		},
		Containers: []kube.ContainerStats{{Name: "api", CPU: 500, Memory: 1024 * 1024 * 1024}},
	}
	model.metricsMode = true
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 48})
	view := updated.View()
	for _, needle := range []string{"RESOURCES", "NETWORK", "CPU", "node-1", "api"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("metrics view missing %q: %q", needle, view)
		}
	}

	model.metricsMode = false
	model.logsMode = true
	model.logs = "2026-11-27T10:00:00Z [api] INFO started\n2026-11-27T10:00:01Z [api] ERROR boom\n"
	model.logsAt = time.Now()
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view = updated.View()
	for _, needle := range []string{"logs", "INFO started", "ERROR boom"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("logs view missing %q: %q", needle, view)
		}
	}
}

func TestPodSignalsShowFailure(t *testing.T) {
	model := New(Options{})
	pod := kube.Pod{
		Phase: "Running", Ready: false, ReadyContainers: 0, ContainersCount: 1,
		Reason: "CrashLoopBackOff", Restarts: 14,
	}
	signals := model.podSignals(pod)
	found := false
	for _, signal := range signals {
		if signal.severity >= 2 && strings.Contains(signal.text, "CrashLoopBackOff") {
			found = true
		}
	}
	if !found {
		t.Fatalf("signals = %+v", signals)
	}
}

func TestMetricsViewScrolls(t *testing.T) {
	pod := kube.Pod{Namespace: "apps", Name: "api", Phase: "Running", Ready: true, ReadyContainers: 1, ContainersCount: 1, Node: "node-1"}
	model := New(Options{})
	model.hits = []app.HitView{{Hit: search.Hit{Score: 0.9}, Pod: pod}}
	model.stats = &kube.PodStats{
		Source: "kubelet", FetchedAt: time.Now(),
		CPU: 500, Memory: 1024, Disk: kube.DiskStats{UsedBytes: 5, CapacityBytes: 10, Inodes: 100, InodesUsed: 50},
		Node: &kube.NodeUsage{Name: "node-1", Ready: true, ReadyKnown: true, CPU: 1, CPUCapacity: 2, Memory: 3, MemoryCapacity: 4,
			Disk: kube.DiskStats{UsedBytes: 5, CapacityBytes: 10, AvailableBytes: 5, Inodes: 100, InodesUsed: 50}, MemoryAvailable: 1, WorkingSetBytes: 2},
		Containers: []kube.ContainerStats{{Name: "api", CPU: 500, Memory: 1024, Rootfs: kube.DiskStats{UsedBytes: 1}, Logs: kube.DiskStats{UsedBytes: 2}}},
	}
	model.metricsMode = true
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = updated.(Model)
	if model.metricsMaxOffset() == 0 {
		t.Fatal("expected metrics content to overflow")
	}
	model.metricsOffset = model.metricsMaxOffset()
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.metricsOffset >= model.metricsMaxOffset() {
		t.Fatalf("offset did not move: offset=%d max=%d", model.metricsOffset, model.metricsMaxOffset())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	model = updated.(Model)
	if model.metricsOffset != 0 {
		t.Fatalf("g did not reset offset: %d", model.metricsOffset)
	}
}

func TestViewFitsTerminal(t *testing.T) {
	pod := kube.Pod{
		Context: "prod-eu", Env: "prod", Namespace: "verylongnamespace",
		Name:  "api-7f9c-very-long-replicaset-name-abcde",
		Phase: "Running", Ready: false, ReadyContainers: 1, ContainersCount: 2,
		Restarts: 14, Reason: "CrashLoopBackOff", Node: "ip-10-0-1-2.ec2.internal",
		StartTime: time.Now().Add(-3 * time.Hour),
	}
	model := New(Options{})
	model.hits = []app.HitView{{Hit: search.Hit{Score: 0.87}, Pod: pod}}
	model.result = &app.SearchResult{
		Hits: model.hits, Eligible: 1, Total: 1234,
		Meta: app.Meta{TotalPods: 1234, Truncated: true, EstimatedTotal: 9999},
	}
	model.detail = &app.Detail{Pod: kube.PodDetail{Pod: pod, Node: &kube.NodeInfo{
		Name: pod.Node, Ready: false, InternalIP: "10.0.1.2",
		AllocatableCPU: "7500m", AllocatableMemory: "30Gi", AllocatablePods: "100",
	}}}
	model.stats = &kube.PodStats{
		Source: "kubelet", FetchedAt: time.Now(), CPU: 780, Memory: 1400 * 1024 * 1024,
		Disk: kube.DiskStats{UsedBytes: 120 * 1024 * 1024, CapacityBytes: 1024 * 1024 * 1024},
	}
	model.logs = "2026-11-27T10:00:00Z [api] INFO started\n2026-11-27T10:00:01Z [api] ERROR panic: a deliberately long line that must be clipped\n"

	for _, size := range [][2]int{{40, 20}, {60, 20}, {80, 24}, {100, 30}, {140, 46}} {
		for _, mode := range []string{"main", "metrics", "logs"} {
			model.metricsMode = mode == "metrics"
			model.logsMode = mode == "logs"
			updated, _ := model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := updated.View()
			lines := strings.Split(view, "\n")
			if len(lines) > size[1] {
				t.Fatalf("%s %dx%d rendered %d lines", mode, size[0], size[1], len(lines))
			}
			for _, line := range lines {
				if width := lipgloss.Width(line); width > size[0] {
					t.Fatalf("%s %dx%d line width %d: %q", mode, size[0], size[1], width, line)
				}
			}
		}
	}
}
