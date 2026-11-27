package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/search"
)

var ErrNotReady = errors.New("pod snapshot is not ready")

type SearchRequest struct {
	Query        string
	Contexts     []string
	Namespaces   []string
	Env          string
	Limit        int
	ForceRefresh bool
	WarmContexts []string
}

type HitView struct {
	search.Hit
	Pod   kube.Pod             `json:"pod"`
	Links config.RenderedLinks `json:"links"`
}

type Meta struct {
	FetchedAt      time.Time           `json:"fetched_at"`
	FetchDuration  time.Duration       `json:"fetch_duration"`
	FromCache      bool                `json:"from_cache"`
	CacheAge       time.Duration       `json:"cache_age"`
	Contexts       []kube.Context      `json:"contexts"`
	Errors         []kube.ContextError `json:"errors,omitempty"`
	TotalPods      int                 `json:"total_pods"`
	Truncated      bool                `json:"truncated,omitempty"`
	EstimatedTotal int64               `json:"estimated_total,omitempty"`
}

type SearchResult struct {
	Query          search.Query  `json:"query"`
	Hits           []HitView     `json:"hits"`
	Total          int           `json:"total"`
	Eligible       int           `json:"eligible"`
	Matched        int           `json:"matched"`
	Meta           Meta          `json:"meta"`
	Stats          Stats         `json:"stats"`
	SearchDuration time.Duration `json:"search_duration"`
}

type Stats struct {
	Total      int `json:"total"`
	Running    int `json:"running"`
	Ready      int `json:"ready"`
	NotReady   int `json:"not_ready"`
	Pending    int `json:"pending"`
	Failed     int `json:"failed"`
	Restarts   int `json:"restarts"`
	Contexts   int `json:"contexts"`
	Namespaces int `json:"namespaces"`
}

func (s *Service) Search(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	start := time.Now()
	if strings.EqualFold(strings.TrimSpace(req.Env), "all") || strings.TrimSpace(req.Env) == "*" {
		req.Env = ""
	}
	env := s.requestEnv(req)
	warmContexts, err := s.warmContexts(ctx, req, env)
	if err != nil {
		return nil, err
	}
	snap, err := s.Warm(ctx, WarmRequest{Contexts: warmContexts, ForceRefresh: req.ForceRefresh})
	if err != nil {
		return nil, err
	}
	return s.rank(snap, req, start), nil
}

func (s *Service) requestEnv(req SearchRequest) string {
	if env := strings.TrimSpace(req.Env); env != "" && !strings.EqualFold(env, "all") && env != "*" {
		return env
	}
	return EnvironmentFromQuery(s.cfg, req.Query)
}

// EnvironmentFromQuery resolves an environment from env: filters or from a
// free-form term that exactly matches one of the configured environments.
func EnvironmentFromQuery(cfg *config.Config, query string) string {
	parsed := search.Parse(query)
	values := make([]string, 0, len(parsed.Filters[search.FieldEnv]))
	for _, value := range parsed.Filters[search.FieldEnv] {
		if strings.HasPrefix(strings.TrimSpace(value), "!") {
			continue
		}
		values = append(values, value)
	}
	if len(values) > 0 {
		return strings.Join(values, ",")
	}
	if cfg == nil {
		return ""
	}
	seen := map[string]bool{}
	for _, term := range parsed.Terms {
		env, ok := cfg.Kube.MatchEnvironment(term)
		if !ok || seen[env] {
			continue
		}
		seen[env] = true
		values = append(values, env)
	}
	return strings.Join(values, ",")
}

func (s *Service) warmContexts(ctx context.Context, req SearchRequest, env string) ([]string, error) {
	if len(req.WarmContexts) > 0 {
		return req.WarmContexts, nil
	}
	if len(req.Contexts) > 0 {
		return req.Contexts, nil
	}
	if env == "" || strings.EqualFold(env, "all") || env == "*" {
		return nil, nil
	}
	contexts, err := s.Contexts(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(contexts))
	for _, clusterCtx := range contexts {
		if matchEnv(env, clusterCtx.Env) {
			names = append(names, clusterCtx.Name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no Kubernetes context matches environment %q", env)
	}
	return names, nil
}

func (s *Service) SearchLocal(req SearchRequest) (*SearchResult, error) {
	snap := s.Snapshot()
	if snap == nil {
		return nil, ErrNotReady
	}
	return s.rank(snap, req, time.Now()), nil
}

func (s *Service) rank(snap *Snapshot, req SearchRequest, start time.Time) *SearchResult {
	if strings.EqualFold(strings.TrimSpace(req.Env), "all") || strings.TrimSpace(req.Env) == "*" {
		req.Env = ""
	}
	if req.Env == "" {
		req.Env = s.requestEnv(req)
	}
	query := search.Parse(req.Query)

	s.mu.RLock()
	candidates := s.candidates
	index := s.podsByID
	version := s.snapshotVersion
	s.mu.RUnlock()

	filtered := s.filteredCandidates(candidates, index, version, req)
	limit := req.Limit
	if limit <= 0 {
		limit = s.cfg.Search.Limit
	}
	hits := s.engine.RankMin(query, filtered, limit, s.cfg.Search.MinScore)

	views := make([]HitView, 0, len(hits))
	for _, hit := range hits {
		pod, ok := index[hit.Candidate.ID]
		if !ok {
			continue
		}
		views = append(views, HitView{
			Hit:   hit,
			Pod:   pod,
			Links: s.LinksFor(pod, query.TimeRange),
		})
	}

	meta := Meta{TotalPods: len(snap.Pods)}
	if snap != nil {
		meta = Meta{
			FetchedAt:      snap.FetchedAt,
			FetchDuration:  snap.Duration,
			FromCache:      snap.FromCache,
			CacheAge:       snap.CacheAge,
			Contexts:       snap.Contexts,
			Errors:         snap.Errors,
			TotalPods:      len(snap.Pods),
			Truncated:      snap.Truncated,
			EstimatedTotal: snap.EstimatedTotal,
		}
	}

	return &SearchResult{
		Query:          query,
		Hits:           views,
		Total:          len(snap.Pods),
		Eligible:       len(filtered),
		Matched:        len(views),
		Meta:           meta,
		Stats:          s.Stats(),
		SearchDuration: time.Since(start),
	}
}

func (s *Service) filteredCandidates(candidates []search.Candidate, index map[string]kube.Pod, version uint64, req SearchRequest) []search.Candidate {
	if len(req.Contexts) == 0 && len(req.Namespaces) == 0 && req.Env == "" {
		return candidates
	}
	key := fmt.Sprintf("%d\x1f%s\x1f%s\x1f%s", version, strings.Join(req.Contexts, "\x1e"), strings.Join(req.Namespaces, "\x1e"), req.Env)
	s.filterMu.Lock()
	defer s.filterMu.Unlock()
	if s.filterVersion == version && s.filterKey == key {
		return s.filtered
	}
	filtered := filterCandidates(candidates, index, req)
	s.filterVersion = version
	s.filterKey = key
	s.filtered = filtered
	return filtered
}

func (s *Service) LinksFor(pod kube.Pod, rng *search.TimeRange) config.RenderedLinks {
	resolved := s.mustResolve(pod)
	container := ""
	if len(pod.Containers) > 0 {
		container = pod.Containers[0].Name
	}
	owner := pod.OwnerName
	if owner == "" {
		owner = pod.Workload
	}
	data := config.TemplateData{
		Context:   pod.Context,
		Cluster:   pod.Cluster,
		Env:       pod.Env,
		OrgID:     resolved.OrgID,
		Namespace: pod.Namespace,
		Pod:       pod.Name,
		Container: container,
		Owner:     owner,
	}
	if rng != nil {
		data.From = rng.From
		data.To = rng.To
	}
	return resolved.Links.Render(data)
}

func (s *Service) Stats() Stats {
	snap := s.Snapshot()
	stats := Stats{}
	if snap == nil {
		return stats
	}
	namespaces := map[string]bool{}
	contexts := map[string]bool{}
	for _, pod := range snap.Pods {
		stats.Total++
		namespaces[pod.Context+"/"+pod.Namespace] = true
		contexts[pod.Context] = true
		stats.Restarts += int(pod.Restarts)
		switch pod.Phase {
		case "Running":
			stats.Running++
		case "Pending":
			stats.Pending++
		case "Failed":
			stats.Failed++
		}
		if pod.Ready {
			stats.Ready++
		} else {
			stats.NotReady++
		}
	}
	stats.Contexts = len(contexts)
	stats.Namespaces = len(namespaces)
	return stats
}
