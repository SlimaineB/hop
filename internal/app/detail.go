package app

import (
	"context"

	"github.com/SlimaineB/teu/internal/actions"
	"github.com/SlimaineB/teu/internal/config"
	"github.com/SlimaineB/teu/internal/kube"
	"github.com/SlimaineB/teu/internal/search"
)

type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster,omitempty"`
	Env       string `json:"env,omitempty"`
	Display   string `json:"display,omitempty"`
	OrgID     string `json:"org_id,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type Detail struct {
	Pod       kube.PodDetail       `json:"pod"`
	Context   ContextInfo          `json:"context"`
	Links     config.RenderedLinks `json:"links"`
	Commands  []actions.Command    `json:"commands"`
	TimeRange *search.TimeRange    `json:"time_range,omitempty"`
}

func (s *Service) Detail(ctx context.Context, ref kube.PodRef, rng *search.TimeRange) (*Detail, error) {
	detail, err := s.provider.GetPod(ctx, ref)
	if err != nil {
		return nil, err
	}
	enrichPod(&detail.Pod, s.cfg)
	resolved := s.cfg.Resolve(ref.Context, detail.Pod.Cluster)
	info := ContextInfo{
		Name:    ref.Context,
		Cluster: detail.Pod.Cluster,
		Env:     resolved.Env,
		Display: resolved.Display,
		OrgID:   resolved.OrgID,
	}
	if resolved.Workspace != nil {
		info.Workspace = resolved.Workspace.Name
	}
	return &Detail{
		Pod:       *detail,
		Context:   info,
		Links:     s.LinksFor(detail.Pod, rng),
		Commands:  actions.Commands(detail.Pod, s.ActionOptionsFor(rng)),
		TimeRange: rng,
	}, nil
}
