package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/cache"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/glob"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/search"
)

type Service struct {
	cfg      *config.Config
	provider kube.Provider
	engine   *search.Engine
	cache    *cache.Store

	fetchMu sync.Mutex

	mu         sync.RWMutex
	snapshot   *Snapshot
	candidates []search.Candidate
	podsByID   map[string]kube.Pod

	metricsMu sync.Mutex
	metrics   *kube.MetricsResult
	metricsAt time.Time
}

type Snapshot struct {
	Pods           []kube.Pod          `json:"pods"`
	Contexts       []kube.Context      `json:"contexts"`
	Errors         []kube.ContextError `json:"errors,omitempty"`
	FetchedAt      time.Time           `json:"fetched_at"`
	Duration       time.Duration       `json:"duration"`
	FromCache      bool                `json:"from_cache"`
	CacheAge       time.Duration       `json:"cache_age"`
	Truncated      bool                `json:"truncated,omitempty"`
	EstimatedTotal int64               `json:"estimated_total,omitempty"`
}

type WarmRequest struct {
	Contexts     []string
	ForceRefresh bool
}

func New(cfg *config.Config) (*Service, error) {
	provider := kube.NewClientGo(kube.Options{
		Kubeconfigs:     cfg.Kube.Kubeconfigs,
		IncludeContexts: cfg.Kube.IncludeContexts,
		ExcludeContexts: cfg.Kube.ExcludeContexts,
		RequestTimeout:  cfg.Kube.RequestTimeout.Get(),
	})
	store, err := cache.New()
	if err != nil {
		store = nil
	}
	return &Service{
		cfg:      cfg,
		provider: provider,
		engine:   search.NewEngine(),
		cache:    store,
		podsByID: map[string]kube.Pod{},
	}, nil
}

func (s *Service) Config() *config.Config { return s.cfg }

func (s *Service) ActionOptions() actions.Options {
	return actions.Options{Kubeconfigs: s.cfg.Kube.Kubeconfigs}
}

func (s *Service) ActionOptionsFor(rng *search.TimeRange) actions.Options {
	opts := s.ActionOptions()
	if rng != nil {
		opts.From = rng.From
		opts.To = rng.To
	}
	return opts
}

func (s *Service) Contexts(ctx context.Context) ([]kube.Context, error) {
	contexts, err := s.provider.Contexts(ctx)
	for i := range contexts {
		resolved := s.cfg.Resolve(contexts[i].Name, contexts[i].Cluster)
		contexts[i].Env = resolved.Env
		contexts[i].Display = resolved.Display
		if resolved.Workspace != nil {
			contexts[i].Workspace = resolved.Workspace.Name
		}
	}
	return contexts, err
}

func (s *Service) currentFresh() *Snapshot {
	ttl := s.cfg.Kube.CacheTTL.Get()
	if ttl <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot != nil && time.Since(s.snapshot.FetchedAt) < ttl {
		return s.snapshot
	}
	return nil
}

func (s *Service) Warm(ctx context.Context, req WarmRequest) (*Snapshot, error) {
	if !req.ForceRefresh {
		if snap := s.currentFresh(); snap != nil {
			return snap, nil
		}
	}

	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()

	if !req.ForceRefresh {
		if snap := s.currentFresh(); snap != nil {
			return snap, nil
		}
	}

	ttl := s.cfg.Kube.CacheTTL.Get()
	key := s.cacheKey(req)
	var cached Snapshot
	if !req.ForceRefresh && s.cache != nil && ttl > 0 {
		if savedAt, ok, err := s.cache.Load(key, &cached); err == nil && ok && time.Since(savedAt) <= ttl {
			cached.FromCache = true
			cached.CacheAge = time.Since(savedAt)
			s.install(&cached)
			return &cached, nil
		}
	}

	namespaces := s.cfg.Kube.Namespaces
	listContexts := req.Contexts
	if len(listContexts) == 0 {
		listContexts = nil
	}
	result, err := s.provider.ListPods(ctx, kube.ListOptions{
		Contexts:          listContexts,
		Namespaces:        namespaces,
		ExcludeNamespaces: s.cfg.Kube.ExcludeNamespaces,
		Concurrency:       s.cfg.Kube.Concurrency,
		Timeout:           s.cfg.Kube.RequestTimeout.Get(),
		ResolveOwners:     s.cfg.Kube.ResolveOwners,
		MaxPods:           s.cfg.Kube.MaxPods,
		MaxPodsPerContext: s.cfg.Kube.MaxPodsPerContext,
	})
	if err != nil {
		if s.cache != nil {
			var stale Snapshot
			if _, ok, loadErr := s.cache.Load(key, &stale); loadErr == nil && ok {
				stale.FromCache = true
				stale.CacheAge = time.Since(stale.FetchedAt)
				stale.Errors = append(stale.Errors, kube.ContextError{Context: "*", Error: err.Error()})
				s.install(&stale)
				return &stale, nil
			}
		}
		return nil, err
	}

	snap := &Snapshot{
		Pods:           result.Pods,
		Contexts:       result.Contexts,
		Errors:         result.Errors,
		FetchedAt:      result.FetchedAt,
		Duration:       result.Duration,
		Truncated:      result.Truncated,
		EstimatedTotal: result.EstimatedTotal,
	}
	s.enrich(snap)
	if s.cache != nil && ttl > 0 && len(snap.Errors) == 0 {
		_ = s.cache.Save(key, snap)
	}
	s.install(snap)
	return snap, nil
}

func (s *Service) enrich(snap *Snapshot) {
	for i := range snap.Contexts {
		resolved := s.cfg.Resolve(snap.Contexts[i].Name, snap.Contexts[i].Cluster)
		snap.Contexts[i].Env = resolved.Env
		snap.Contexts[i].Display = resolved.Display
		if resolved.Workspace != nil {
			snap.Contexts[i].Workspace = resolved.Workspace.Name
		}
	}
	for i := range snap.Pods {
		enrichPod(&snap.Pods[i], s.cfg)
	}
	sort.SliceStable(snap.Pods, func(i, j int) bool {
		return snap.Pods[i].StartTime.After(snap.Pods[j].StartTime)
	})
}

func enrichPod(pod *kube.Pod, cfg *config.Config) {
	resolved := cfg.Resolve(pod.Context, pod.Cluster)
	pod.Env = resolved.Env
	pod.Display = resolved.Display
	if pod.Workload == "" {
		pod.Workload = pod.OwnerName
	}
}

func (s *Service) install(snap *Snapshot) {
	candidates, index := buildCandidates(snap.Pods)
	s.mu.Lock()
	s.snapshot = snap
	s.candidates = candidates
	s.podsByID = index
	s.mu.Unlock()
}

func (s *Service) Snapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

func (s *Service) ClearCache() (int, error) {
	if s.cache == nil {
		return 0, nil
	}
	return s.cache.Clear()
}

func (s *Service) CacheDir() string {
	if s.cache == nil {
		return ""
	}
	return s.cache.Dir()
}

func (s *Service) Close() error { return s.provider.Close() }

func (s *Service) cacheKey(req WarmRequest) string {
	var b strings.Builder
	b.WriteString(s.cfg.Fingerprint())
	b.WriteByte('\n')
	b.WriteString("contexts=")
	b.WriteString(strings.Join(req.Contexts, ","))
	b.WriteByte('\n')
	b.WriteString("namespaces=")
	b.WriteString(strings.Join(s.cfg.Kube.Namespaces, ","))
	b.WriteByte('\n')
	b.WriteString("exclude=")
	b.WriteString(strings.Join(s.cfg.Kube.ExcludeNamespaces, ","))
	b.WriteByte('\n')
	b.WriteString("kubeconfig=")
	b.WriteString(strings.Join(s.cfg.Kube.Kubeconfigs, string(os.PathListSeparator)))
	b.WriteByte('\n')
	b.WriteString("env_kubeconfig=")
	b.WriteString(os.Getenv("KUBECONFIG"))
	sum := sha256.Sum256([]byte(b.String()))
	return "pods-" + hex.EncodeToString(sum[:])[:16]
}

const idSeparator = "\x1f"

func podID(pod kube.Pod) string {
	return pod.Context + idSeparator + pod.Namespace + idSeparator + pod.Name
}

func PodIDFromRef(ref kube.PodRef) string {
	return ref.Context + idSeparator + ref.Namespace + idSeparator + ref.Name
}

func buildCandidates(pods []kube.Pod) ([]search.Candidate, map[string]kube.Pod) {
	candidates := make([]search.Candidate, 0, len(pods))
	index := make(map[string]kube.Pod, len(pods))
	for i := range pods {
		pod := pods[i]
		id := podID(pod)
		index[id] = pod
		c := search.Candidate{ID: id}
		if pod.Ready {
			c.Boost += 0.01
		}
		if pod.Phase == "Running" {
			c.Boost += 0.01
		}
		c.Add(search.FieldPod, pod.Name)
		c.Add(search.FieldNamespace, pod.Namespace)
		c.Add(search.FieldContext, pod.Context)
		c.Add(search.FieldCluster, pod.Cluster)
		c.Add(search.FieldEnv, pod.Env)
		c.Add(search.FieldOwner, pod.OwnerName)
		if pod.Workload != pod.OwnerName {
			c.Add(search.FieldOwner, pod.Workload)
		}
		c.Add(search.FieldNode, pod.Node)
		c.Add(search.FieldPhase, pod.Phase)
		for key, value := range pod.Labels {
			if value == "" {
				continue
			}
			c.Add(search.FieldLabel, key+"="+value)
			c.Add(search.FieldLabel, value)
		}
		for _, container := range pod.Containers {
			c.Add(search.FieldContainer, container.Name)
			c.Add(search.FieldImage, container.Image)
		}
		candidates = append(candidates, c)
	}
	return candidates, index
}

func matchEnv(pattern string, env string) bool {
	for _, part := range strings.Split(pattern, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.EqualFold(part, env) || glob.Match(strings.ToLower(part), strings.ToLower(env)) {
			return true
		}
	}
	return false
}

func filterCandidates(candidates []search.Candidate, index map[string]kube.Pod, req SearchRequest) []search.Candidate {
	if len(req.Contexts) == 0 && len(req.Namespaces) == 0 && req.Env == "" {
		return candidates
	}
	out := make([]search.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		pod, ok := index[candidate.ID]
		if !ok {
			continue
		}
		if len(req.Contexts) > 0 && !glob.MatchAny(req.Contexts, pod.Context) {
			continue
		}
		if len(req.Namespaces) > 0 && !glob.MatchAny(req.Namespaces, pod.Namespace) {
			continue
		}
		if req.Env != "" && !matchEnv(req.Env, pod.Env) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func (s *Service) mustResolve(pod kube.Pod) config.Resolved {
	return s.cfg.Resolve(pod.Context, pod.Cluster)
}
