package app

import (
	"context"
	"fmt"

	"github.com/SlimaineB/hop/internal/kube"
)

func (s *Service) PodStats(ctx context.Context, ref kube.PodRef) (*kube.PodStats, error) {
	provider, ok := s.provider.(kube.StatsProvider)
	if !ok {
		return nil, fmt.Errorf("pod metrics unavailable")
	}
	node := ""
	s.mu.RLock()
	if pod, ok := s.podsByID[PodIDFromRef(ref)]; ok {
		node = pod.Node
	}
	s.mu.RUnlock()
	return provider.GetPodStats(ctx, ref, node)
}
