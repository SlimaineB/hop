package app

import (
	"context"
	"testing"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/kube"
)

type captureProvider struct {
	contexts []kube.Context
	last     []string
}

func (p *captureProvider) Contexts(context.Context) ([]kube.Context, error) {
	return p.contexts, nil
}

func (p *captureProvider) ListPods(_ context.Context, opts kube.ListOptions) (*kube.ListResult, error) {
	p.last = append([]string(nil), opts.Contexts...)
	return &kube.ListResult{Contexts: p.contexts, FetchedAt: time.Now()}, nil
}

func (p *captureProvider) ListMetrics(context.Context, kube.ListOptions) (*kube.MetricsResult, error) {
	return &kube.MetricsResult{}, nil
}

func (p *captureProvider) GetPod(context.Context, kube.PodRef) (*kube.PodDetail, error) {
	return nil, nil
}

func (p *captureProvider) Close() error { return nil }

func TestSearchScopesWarmupToEnvironment(t *testing.T) {
	cfg := config.Default()
	cfg.Kube.CacheTTL = 0
	provider := &captureProvider{contexts: []kube.Context{
		{Name: "prod-eu", Env: "prod"},
		{Name: "dev-eu", Env: "dev"},
	}}
	service, err := NewWithProvider(cfg, provider)
	if err != nil {
		t.Fatal(err)
	}
	service.cache = nil

	if _, err := service.Search(context.Background(), SearchRequest{Query: "api", Env: "prod"}); err != nil {
		t.Fatal(err)
	}
	if len(provider.last) != 1 || provider.last[0] != "prod-eu" {
		t.Fatalf("warm contexts = %v, want [prod-eu]", provider.last)
	}
}

func TestSearchDetectsEnvironmentTerm(t *testing.T) {
	cfg := config.Default()
	cfg.Kube.CacheTTL = 0
	provider := &captureProvider{contexts: []kube.Context{
		{Name: "prod-eu", Env: "prod"},
		{Name: "dev-eu", Env: "dev"},
		{Name: "uat-eu", Env: "uat"},
	}}
	service, err := NewWithProvider(cfg, provider)
	if err != nil {
		t.Fatal(err)
	}
	service.cache = nil

	if _, err := service.Search(context.Background(), SearchRequest{Query: "api prod"}); err != nil {
		t.Fatal(err)
	}
	if len(provider.last) != 1 || provider.last[0] != "prod-eu" {
		t.Fatalf("warm contexts = %v, want [prod-eu]", provider.last)
	}

	if _, err := service.Search(context.Background(), SearchRequest{Query: "api prod uat"}); err != nil {
		t.Fatal(err)
	}
	if len(provider.last) != 2 {
		t.Fatalf("warm contexts = %v, want prod and uat", provider.last)
	}
}
