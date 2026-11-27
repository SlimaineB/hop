package kube

import (
	"context"
	"fmt"
	"time"
)

type disabledProvider struct{}

func NewDisabled() Provider {
	return disabledProvider{}
}

func (disabledProvider) Contexts(context.Context) ([]Context, error) {
	return nil, nil
}

func (disabledProvider) ListPods(context.Context, ListOptions) (*ListResult, error) {
	return &ListResult{FetchedAt: time.Now()}, nil
}

func (disabledProvider) ListMetrics(context.Context, ListOptions) (*MetricsResult, error) {
	return &MetricsResult{FetchedAt: time.Now()}, nil
}

func (disabledProvider) GetPod(context.Context, PodRef) (*PodDetail, error) {
	return nil, fmt.Errorf("pods source is disabled; enable it with: hop features enable pods")
}

func (disabledProvider) Close() error {
	return nil
}
