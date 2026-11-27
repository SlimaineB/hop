package app

import (
	"context"
	"testing"
	"time"

	"github.com/SlimaineB/hop/internal/cache"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/kube"
)

func TestMergeCachedContextsKeepsPodsForFailedContext(t *testing.T) {
	current := &Snapshot{
		Pods: []kube.Pod{
			{Context: "ok", Namespace: "apps", Name: "api"},
		},
		Errors: []kube.ContextError{{Context: "broken", Error: "timeout"}},
	}
	cached := &Snapshot{
		Pods: []kube.Pod{
			{Context: "ok", Namespace: "apps", Name: "api"},
			{Context: "broken", Namespace: "apps", Name: "worker"},
		},
	}

	mergeCachedContexts(current, cached)

	if len(current.Pods) != 2 {
		t.Fatalf("pods = %d, want 2", len(current.Pods))
	}
	found := false
	for _, pod := range current.Pods {
		if pod.Context == "broken" && pod.Name == "worker" {
			found = true
		}
	}
	if !found {
		t.Fatal("cached pod for failed context was not merged")
	}
}

func TestMergeCachedContextsIgnoresHealthyContexts(t *testing.T) {
	current := &Snapshot{Pods: []kube.Pod{{Context: "ok", Namespace: "apps", Name: "api"}}}
	cached := &Snapshot{Pods: []kube.Pod{
		{Context: "ok", Namespace: "apps", Name: "old"},
		{Context: "broken", Namespace: "apps", Name: "worker"},
	}}

	mergeCachedContexts(current, cached)

	if len(current.Pods) != 1 || current.Pods[0].Name != "api" {
		t.Fatalf("unexpected pods: %+v", current.Pods)
	}
}

type errorProvider struct {
	contexts []kube.Context
}

func (p *errorProvider) Contexts(context.Context) ([]kube.Context, error) {
	return p.contexts, nil
}

func (p *errorProvider) ListPods(context.Context, kube.ListOptions) (*kube.ListResult, error) {
	return &kube.ListResult{
		Contexts:  p.contexts,
		Errors:    []kube.ContextError{{Context: "broken", Error: "timeout"}},
		FetchedAt: time.Now(),
	}, nil
}

func (p *errorProvider) ListMetrics(context.Context, kube.ListOptions) (*kube.MetricsResult, error) {
	return &kube.MetricsResult{}, nil
}

func (p *errorProvider) GetPod(context.Context, kube.PodRef) (*kube.PodDetail, error) {
	return nil, nil
}

func (p *errorProvider) Close() error { return nil }

func TestWarmMergesCachedPodsWhenContextFails(t *testing.T) {
	cfg := config.Default()
	cfg.Kube.CacheTTL = config.Duration(time.Minute)
	dir := t.TempDir()
	store, err := cache.NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	provider := &errorProvider{contexts: []kube.Context{{Name: "broken", Env: "prod"}}}
	service, err := NewWithProvider(cfg, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	service.cache = store

	key := service.cacheKey(WarmRequest{})
	stale := &Snapshot{
		Contexts:  provider.contexts,
		Pods:      []kube.Pod{{Context: "broken", Namespace: "apps", Name: "api"}},
		FetchedAt: time.Now().Add(-time.Minute),
	}
	if err := store.Save(key, stale); err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.Warm(context.Background(), WarmRequest{ForceRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Errors) != 1 {
		t.Fatalf("errors = %v", snapshot.Errors)
	}
	if len(snapshot.Pods) != 1 || snapshot.Pods[0].Name != "api" {
		t.Fatalf("pods = %+v, want cached api pod", snapshot.Pods)
	}
}
