package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
