package demo

import (
	"context"
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/plugin"
)

func TestRuntimeContainsEverySource(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer runtime.Close()

	snapshot := runtime.App.Snapshot()
	if snapshot == nil || len(snapshot.Pods) != 12 {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	got := map[string]bool{}
	for _, source := range runtime.Registry.Sources() {
		got[source.ID()] = true
	}
	for _, id := range []string{"pods", "servers", "containers", "repos", "packages", "tickets", "shortcuts"} {
		if !got[id] {
			t.Fatalf("source %s missing: %+v", id, got)
		}
	}

	response := runtime.Registry.Search(context.Background(), plugin.Query{Raw: "checkout", Limit: 100})
	if len(response.Sections) == 0 {
		t.Fatal("search returned no sections")
	}
}

func TestBackendDetailStatsAndLogs(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer runtime.Close()

	ref := runtime.App.Snapshot().Pods[0].Ref()
	detail, err := runtime.Backend.Detail(context.Background(), ref, nil)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.Pod.Node == nil || len(detail.Pod.Events) == 0 {
		t.Fatalf("detail = %+v", detail)
	}
	stats, err := runtime.Backend.PodStats(context.Background(), ref)
	if err != nil {
		t.Fatalf("PodStats: %v", err)
	}
	if stats.CPU <= 0 || stats.Node == nil {
		t.Fatalf("stats = %+v", stats)
	}
	logs, err := runtime.Backend.PodLogs(context.Background(), ref, nil)
	if err != nil {
		t.Fatalf("PodLogs: %v", err)
	}
	if !strings.Contains(logs, "INFO") {
		t.Fatalf("logs = %q", logs)
	}
}
