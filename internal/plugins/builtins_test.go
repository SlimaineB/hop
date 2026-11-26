package plugins

import (
	"context"
	"testing"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
)

type podsOnly struct{}

func (podsOnly) Search(context.Context, app.SearchRequest) (*app.SearchResult, error) {
	return &app.SearchResult{}, nil
}
func (podsOnly) SearchLocal(app.SearchRequest) (*app.SearchResult, error) {
	return &app.SearchResult{}, nil
}
func (podsOnly) Stats() app.Stats        { return app.Stats{} }
func (podsOnly) Snapshot() *app.Snapshot { return nil }

func TestBuildSkipsDisabledSources(t *testing.T) {
	cfg := &config.Config{}
	cfg.Sources.Disabled = []string{"pod", "link", "custom"}
	registry := Build(cfg, Deps{
		Pods:      podsOnly{},
		Shortcuts: []config.ShortcutConfig{{Name: "grafana", URL: "https://example.com"}},
	})
	if sources := registry.Sources(); len(sources) != 0 {
		t.Fatalf("sources = %+v", sources)
	}

	registry = Build(&config.Config{}, Deps{Pods: podsOnly{}})
	if sources := registry.Sources(); len(sources) != 1 || sources[0].ID() != "pods" {
		t.Fatalf("sources = %+v", sources)
	}
}

func TestCatalogMatchesBuiltinIDs(t *testing.T) {
	want := map[string]bool{
		"pods": true, "servers": true, "repos": true,
		"packages": true, "tickets": true, "shortcuts": true,
	}
	for _, info := range Catalog() {
		if !want[info.ID] {
			t.Fatalf("unexpected catalog entry %+v", info)
		}
		delete(want, info.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing catalog entries: %v", want)
	}
}
