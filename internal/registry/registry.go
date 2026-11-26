// Package registry searches container image references and their tags in
// GitHub Container Registry and GitLab Container Registry (and any other
// Docker Registry v2 endpoint).
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/search"
)

type Links struct {
	Web  string `json:"web,omitempty"`
	Tags string `json:"tags,omitempty"`
}

type Image struct {
	Reference  string   `json:"reference"`
	Registry   string   `json:"registry"`
	Repository string   `json:"repository"`
	Provider   string   `json:"provider,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	WebURL     string   `json:"web_url,omitempty"`
	Pull       string   `json:"pull,omitempty"`
	Links      Links    `json:"links"`
}

type Hit struct {
	Image   Image    `json:"image"`
	Score   int      `json:"score"`
	Percent int      `json:"percent"`
	Reasons []string `json:"reasons,omitempty"`
}

type Service struct {
	client   *http.Client
	ttl      time.Duration
	timeout  time.Duration
	tagLimit int

	registries map[string]config.RegistryConfig
	configured []string

	mu         sync.RWMutex
	discovered map[string]time.Time
	tags       map[string]tagEntry
}

type tagEntry struct {
	tags      []string
	fetchedAt time.Time
	err       error
}

func New(cfg *config.Config) *Service {
	service := &Service{
		client:     &http.Client{Timeout: 8 * time.Second},
		ttl:        5 * time.Minute,
		timeout:    8 * time.Second,
		tagLimit:   50,
		registries: map[string]config.RegistryConfig{},
		discovered: map[string]time.Time{},
		tags:       map[string]tagEntry{},
	}
	if cfg == nil {
		return service
	}
	registries := cfg.Packages.Registries
	if len(registries) == 0 {
		registries = defaultRegistries()
	}
	for _, registry := range registries {
		host := normalizeHost(registry.Host)
		if host == "" {
			continue
		}
		registry.Host = host
		if registry.Type == "" {
			registry.Type = providerFor(host)
		}
		service.registries[host] = registry
		service.configured = append(service.configured, registry.Images...)
	}
	service.configured = append(service.configured, cfg.Packages.Images...)
	if cfg.Packages.CacheTTL.Get() > 0 {
		service.ttl = cfg.Packages.CacheTTL.Get()
	}
	if cfg.Packages.Timeout.Get() > 0 {
		service.timeout = cfg.Packages.Timeout.Get()
		service.client.Timeout = service.timeout
	}
	if cfg.Packages.TagLimit > 0 {
		service.tagLimit = cfg.Packages.TagLimit
	}
	return service
}

func defaultRegistries() []config.RegistryConfig {
	return []config.RegistryConfig{
		{Name: "ghcr", Host: "ghcr.io", Type: "ghcr"},
		{Name: "gitlab", Host: "registry.gitlab.com", Type: "gitlab"},
	}
}

func (s *Service) Enabled() bool { return s != nil }

// SetImages records image references discovered from the live pod inventory.
func (s *Service) SetImages(references ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.discovered == nil {
		s.discovered = map[string]time.Time{}
	}
	now := time.Now()
	for _, ref := range references {
		ref = strings.TrimSpace(ref)
		if ref == "" || strings.EqualFold(ref, "scratch") {
			continue
		}
		s.discovered[ref] = now
	}
	if len(s.discovered) > 200 {
		type item struct {
			ref string
			at  time.Time
		}
		items := make([]item, 0, len(s.discovered))
		for ref, at := range s.discovered {
			items = append(items, item{ref: ref, at: at})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].at.After(items[j].at) })
		for _, stale := range items[200:] {
			delete(s.discovered, stale.ref)
		}
	}
}

func (s *Service) Search(ctx context.Context, raw string, limit int) ([]Hit, []string, error) {
	return s.search(ctx, raw, limit, true)
}

func (s *Service) SearchLocal(raw string, limit int) ([]Hit, []string, error) {
	return s.search(context.Background(), raw, limit, false)
}

func (s *Service) search(ctx context.Context, raw string, limit int, fetchTags bool) ([]Hit, []string, error) {
	if !s.Enabled() {
		return nil, nil, nil
	}
	refs := s.references()
	if len(refs) == 0 {
		return nil, nil, nil
	}
	query := search.Parse(stripSelectors(raw))
	terms := lowerTerms(query.Terms)
	imageFilters := lowerTerms(query.Filters[search.FieldImage])

	candidates := make([]candidate, 0, len(refs))
	for _, ref := range refs {
		item, ok := s.candidate(ref)
		if !ok {
			continue
		}
		if !matchesFilters(item, imageFilters) {
			continue
		}
		score, reasons, ok := scoreImage(item, terms)
		if !ok {
			continue
		}
		item.score = score
		item.reasons = reasons
		candidates = append(candidates, item)
	}
	if len(candidates) == 0 {
		return nil, nil, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].image.Reference < candidates[j].image.Reference
		}
		return candidates[i].score > candidates[j].score
	})

	var warnings []string
	const maxLookups = 12
	lookup := candidates
	if len(lookup) > maxLookups {
		lookup = lookup[:maxLookups]
	}
	if fetchTags {
		warnings = s.enrich(ctx, lookup)
	} else {
		s.enrichCached(lookup)
	}
	for i := range candidates {
		if i < len(lookup) {
			candidates[i].image = lookup[i].image
		}
		score, reasons, ok := scoreImage(candidates[i], terms)
		if !ok {
			continue
		}
		candidates[i].score = score
		candidates[i].reasons = reasons
	}

	hits := make([]Hit, 0, len(candidates))
	for _, item := range candidates {
		percent := 0
		if len(terms) > 0 {
			percent = item.score / len(terms)
			if percent > 100 {
				percent = 100
			}
		}
		hits = append(hits, Hit{Image: item.image, Score: item.score, Percent: percent, Reasons: item.reasons})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Image.Reference < hits[j].Image.Reference
		}
		return hits[i].Score > hits[j].Score
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, warnings, nil
}

type candidate struct {
	image   Image
	score   int
	reasons []string
}

func (s *Service) references() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	out := make([]string, 0, len(s.configured)+len(s.discovered))
	for _, ref := range s.configured {
		ref = strings.TrimSpace(ref)
		if ref != "" && !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	for ref := range s.discovered {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

func (s *Service) candidate(ref string) (candidate, bool) {
	host, repository, ok := splitReference(ref)
	if !ok {
		return candidate{}, false
	}
	provider := providerFor(host)
	registryCfg, known := s.registries[host]
	if known && registryCfg.Type != "" {
		provider = registryCfg.Type
	}
	image := Image{
		Reference:  ref,
		Registry:   host,
		Repository: repository,
		Provider:   provider,
		Pull:       "docker pull " + ref,
	}
	image.WebURL, image.Links = describeImage(host, repository, provider)
	return candidate{image: image}, true
}

func (s *Service) enrich(ctx context.Context, items []candidate) []string {
	var warnings []string
	sem := make(chan struct{}, 6)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		go func(item *candidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tags, err := s.tagsFor(ctx, item.image.Registry, item.image.Repository)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, item.image.Reference+": "+err.Error())
				return
			}
			item.image.Tags = tags
		}(&items[i])
	}
	wg.Wait()
	return warnings
}

func (s *Service) enrichCached(items []candidate) {
	for i := range items {
		s.mu.RLock()
		entry, ok := s.tags[itemKey(items[i].image.Registry, items[i].image.Repository)]
		s.mu.RUnlock()
		if ok {
			items[i].image.Tags = append([]string(nil), entry.tags...)
		}
	}
}

func (s *Service) tagsFor(ctx context.Context, host, repository string) ([]string, error) {
	key := itemKey(host, repository)
	s.mu.RLock()
	entry, ok := s.tags[key]
	s.mu.RUnlock()
	if ok && time.Since(entry.fetchedAt) < s.ttl {
		return append([]string(nil), entry.tags...), entry.err
	}
	tags, err := s.fetchTags(ctx, host, repository)
	s.mu.Lock()
	s.tags[key] = tagEntry{tags: tags, fetchedAt: time.Now(), err: err}
	s.mu.Unlock()
	return tags, err
}

func (s *Service) fetchTags(ctx context.Context, host, repository string) ([]string, error) {
	host = normalizeHost(host)
	if host == "" {
		return nil, errors.New("empty registry host")
	}
	scheme := "https"
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		scheme = "http"
	}
	target := fmt.Sprintf("%s://%s/v2/%s/tags/list?n=%d", scheme, host, repository, s.tagLimit)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	cfg := s.registries[host]
	username, token := credentials(cfg)

	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		challenge := response.Header.Get("WWW-Authenticate")
		_ = response.Body.Close()
		bearer, tokenErr := s.fetchToken(ctx, challenge, host, repository, username, token)
		if tokenErr != nil {
			return nil, tokenErr
		}
		request, err = http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+bearer)
		response, err = s.client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, fmt.Errorf("%s: HTTP %d %s", host, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(payload.Tags)))
	if len(payload.Tags) > s.tagLimit {
		payload.Tags = payload.Tags[:s.tagLimit]
	}
	return payload.Tags, nil
}

func (s *Service) fetchToken(ctx context.Context, challenge, host, repository, username, token string) (string, error) {
	realm, params := parseChallenge(challenge)
	if realm == "" {
		return "", fmt.Errorf("%s: missing bearer realm", host)
	}
	values := url.Values{}
	if service := params["service"]; service != "" {
		values.Set("service", service)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + repository + ":pull"
	}
	values.Set("scope", scope)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, realm+"?"+values.Encode(), nil)
	if err != nil {
		return "", err
	}
	if username != "" && token != "" {
		request.SetBasicAuth(username, token)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return "", fmt.Errorf("%s: token HTTP %d %s", host, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Token == "" {
		payload.Token = payload.AccessToken
	}
	if payload.Token == "" {
		return "", errors.New("empty bearer token")
	}
	return payload.Token, nil
}

func credentials(cfg config.RegistryConfig) (string, string) {
	username := strings.TrimSpace(cfg.Username)
	if username == "" && cfg.UsernameEnv != "" {
		username = strings.TrimSpace(os.Getenv(cfg.UsernameEnv))
	}
	token := ""
	if cfg.TokenEnv != "" {
		token = strings.TrimSpace(os.Getenv(cfg.TokenEnv))
	}
	if username == "" && token != "" {
		username = "oauth2"
	}
	return username, token
}

func parseChallenge(header string) (string, map[string]string) {
	header = strings.TrimSpace(header)
	params := map[string]string{}
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return "", params
	}
	for _, part := range splitChallenge(header[len("Bearer "):]) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"`)
		params[key] = value
	}
	return params["realm"], params
}

func splitChallenge(value string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	for _, r := range value {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			current.WriteRune(r)
		case r == ',' && !inQuotes:
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

func scoreImage(item candidate, terms []string) (int, []string, bool) {
	if len(terms) == 0 {
		return 0, nil, true
	}
	reference := strings.ToLower(item.image.Reference)
	repository := strings.ToLower(item.image.Repository)
	registry := strings.ToLower(item.image.Registry)
	provider := strings.ToLower(item.image.Provider)
	tags := make([]string, 0, len(item.image.Tags))
	for _, tag := range item.image.Tags {
		tags = append(tags, strings.ToLower(tag))
	}
	score := 0
	reasons := make([]string, 0, 2)
	for _, term := range terms {
		best := 0
		reason := ""
		switch {
		case strings.EqualFold(item.image.Reference, term):
			best, reason = 100, "image"
		case strings.HasPrefix(reference, term):
			best, reason = 90, "image"
		case strings.Contains(repository, term):
			best, reason = 70, "image"
		case hasTag(tags, term):
			best, reason = 55, "tag"
		case strings.Contains(registry, term):
			best, reason = 30, "registry"
		case strings.Contains(provider, term):
			best, reason = 20, "provider"
		}
		if best == 0 {
			return 0, nil, false
		}
		score += best
		if reason != "" && !contains(reasons, reason) && len(reasons) < 2 {
			reasons = append(reasons, reason)
		}
	}
	return score, reasons, true
}

func hasTag(tags []string, term string) bool {
	for _, tag := range tags {
		if strings.Contains(tag, term) {
			return true
		}
	}
	return false
}

func matchesFilters(item candidate, filters []string) bool {
	for _, filter := range filters {
		if !strings.Contains(strings.ToLower(item.image.Reference), filter) {
			return false
		}
	}
	return true
}

func describeImage(host, repository, provider string) (string, Links) {
	switch provider {
	case "ghcr":
		segments := strings.Split(repository, "/")
		if len(segments) == 0 {
			return "", Links{}
		}
		owner := segments[0]
		web := "https://github.com/" + owner + "?tab=packages"
		return web, Links{Web: web}
	case "gitlab":
		webHost := strings.TrimPrefix(host, "registry.")
		segments := strings.Split(repository, "/")
		if len(segments) >= 2 {
			project := strings.Join(segments[:2], "/")
			web := "https://" + webHost + "/" + project + "/-/container_registry"
			return web, Links{Web: web}
		}
		web := "https://" + webHost
		return web, Links{Web: web}
	default:
		return "", Links{}
	}
}

func splitReference(ref string) (string, string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", false
	}
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	host := ""
	repository := ref
	if slash := strings.Index(ref, "/"); slash > 0 {
		first := ref[:slash]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			host = first
			repository = ref[slash+1:]
		}
	}
	if host == "" {
		host = "docker.io"
		if !strings.Contains(repository, "/") {
			repository = "library/" + repository
		}
	}
	if colon := strings.LastIndex(repository, ":"); colon > 0 {
		repository = repository[:colon]
	}
	repository = strings.Trim(repository, "/")
	if repository == "" {
		return "", "", false
	}
	return host, repository, true
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")
	if slash := strings.Index(host, "/"); slash >= 0 {
		host = host[:slash]
	}
	return host
}

func providerFor(host string) string {
	lower := strings.ToLower(host)
	switch {
	case strings.Contains(lower, "ghcr"):
		return "ghcr"
	case strings.Contains(lower, "gitlab"):
		return "gitlab"
	case strings.Contains(lower, "docker"):
		return "docker"
	default:
		return "registry"
	}
}

func HasSelector(raw string) bool {
	for _, field := range strings.Fields(raw) {
		token := strings.ToLower(strings.Trim(field, `"'`))
		if token == "pkg" || token == "pkgs" || token == "package" || token == "packages" || token == "registry" {
			return true
		}
	}
	return false
}

func stripSelectors(raw string) string {
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.ToLower(strings.Trim(field, `"'`))
		switch token {
		case "pkg", "pkgs", "package", "packages", "registry":
			continue
		}
		out = append(out, field)
	}
	return strings.Join(out, " ")
}

func lowerTerms(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func itemKey(host, repository string) string {
	return normalizeHost(host) + "/" + repository
}
