package app

import (
	"context"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/search"
)

func (s *Service) PodLogs(ctx context.Context, ref kube.PodRef, rng *search.TimeRange) (string, error) {
	detail, err := s.provider.GetPod(ctx, ref)
	if err != nil {
		return "", err
	}
	return actions.TailLogs(ctx, detail.Pod, s.ActionOptionsFor(rng), 500)
}
