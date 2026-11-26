package app

import (
	"context"
	"time"

	"github.com/SlimaineB/hop/internal/kube"
)

type MetricsRequest struct {
	Contexts          []string
	Namespaces        []string
	ExcludeNamespaces []string
	ForceRefresh      bool
}

// Metrics returns a short-lived cache of metrics-server data. The dashboard
// polls more often than the cache TTL, so repeated renders stay cheap.
func (s *Service) Metrics(ctx context.Context, req MetricsRequest) (*kube.MetricsResult, error) {
	const ttl = 12 * time.Second
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	if !req.ForceRefresh && s.metrics != nil && time.Since(s.metricsAt) < ttl {
		return s.metrics, nil
	}
	concurrency := 6
	timeout := time.Duration(0)
	if s.cfg != nil {
		if s.cfg.Kube.Concurrency > 0 && s.cfg.Kube.Concurrency < concurrency {
			concurrency = s.cfg.Kube.Concurrency
		}
		timeout = s.cfg.Kube.RequestTimeout.Get()
	}
	result, err := s.provider.ListMetrics(ctx, kube.ListOptions{
		Contexts:          req.Contexts,
		Namespaces:        req.Namespaces,
		ExcludeNamespaces: req.ExcludeNamespaces,
		Concurrency:       concurrency,
		Timeout:           timeout,
	})
	if err != nil {
		return nil, err
	}
	s.metrics = result
	s.metricsAt = time.Now()
	return result, nil
}
