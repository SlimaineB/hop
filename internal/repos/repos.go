// Package repos discovers local Git repositories and ranks them for the
// cockpit search. GitHub and GitLab remotes get web links out of the box.
package repos

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/search"
)

var ErrNotReady = errors.New("repository inventory not ready")

type Links struct {
	Repo            string `json:"repo,omitempty"`
	Branch          string `json:"branch,omitempty"`
	Commit          string `json:"commit,omitempty"`
	MergeRequests   string `json:"merge_requests,omitempty"`
	NewMergeRequest string `json:"new_merge_request,omitempty"`
}

type Repo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Branch      string    `json:"branch,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	Remote      string    `json:"remote,omitempty"`
	WebURL      string    `json:"web_url,omitempty"`
	Commit      string    `json:"commit,omitempty"`
	CommitShort string    `json:"commit_short,omitempty"`
	Subject     string    `json:"subject,omitempty"`
	Author      string    `json:"author,omitempty"`
	CommittedAt time.Time `json:"committed_at,omitempty"`
	Dirty       bool      `json:"dirty,omitempty"`
	Changed     int       `json:"changed,omitempty"`
	Ahead       int       `json:"ahead,omitempty"`
	Behind      int       `json:"behind,omitempty"`
	Upstream    string    `json:"upstream,omitempty"`
	Links       Links     `json:"links"`
}

type Hit struct {
	Repo    Repo     `json:"repo"`
	Score   int      `json:"score"`
	Percent int      `json:"percent"`
	Reasons []string `json:"reasons,omitempty"`
}

type Service struct {
	roots     []string
	explicit  []string
	exclude   map[string]bool
	providers map[string]string
	maxDepth  int
	ttl       time.Duration
	timeout   time.Duration
	gitPath   string

	mu          sync.RWMutex
	snapshot    []Repo
	snapshotSet bool
	fetchedAt   time.Time
}

func New(cfg *config.Config) *Service {
	service := &Service{
		exclude:   map[string]bool{},
		providers: map[string]string{},
		maxDepth:  3,
		ttl:       30 * time.Second,
		timeout:   8 * time.Second,
	}
	if cfg != nil {
		service.roots = append(service.roots, cfg.Git.Roots...)
		service.explicit = append(service.explicit, cfg.Git.Repos...)
		for _, name := range cfg.Git.Exclude {
			service.exclude[strings.ToLower(strings.TrimSpace(name))] = true
		}
		for host, provider := range cfg.Git.Providers {
			service.providers[strings.ToLower(strings.TrimSpace(host))] = strings.ToLower(strings.TrimSpace(provider))
		}
		if cfg.Git.MaxDepth > 0 {
			service.maxDepth = cfg.Git.MaxDepth
		}
		if cfg.Git.CacheTTL.Get() > 0 {
			service.ttl = cfg.Git.CacheTTL.Get()
		}
	}
	service.gitPath, _ = exec.LookPath("git")
	return service
}

func (s *Service) Enabled() bool {
	return s != nil && (len(s.roots) > 0 || len(s.explicit) > 0)
}

func (s *Service) Search(ctx context.Context, raw string, limit int) ([]Hit, error) {
	if !s.Enabled() {
		return nil, nil
	}
	s.mu.RLock()
	fresh := s.snapshotSet && time.Since(s.fetchedAt) < s.ttl
	s.mu.RUnlock()
	if !fresh {
		if _, err := s.Refresh(ctx); err != nil {
			return nil, err
		}
	}
	return s.SearchLocal(raw, limit)
}

func (s *Service) SearchLocal(raw string, limit int) ([]Hit, error) {
	if !s.Enabled() {
		return nil, nil
	}
	s.mu.RLock()
	if !s.snapshotSet {
		s.mu.RUnlock()
		return nil, ErrNotReady
	}
	repos := append([]Repo(nil), s.snapshot...)
	s.mu.RUnlock()
	return rank(repos, raw, limit), nil
}

func (s *Service) Refresh(ctx context.Context) ([]Repo, error) {
	if !s.Enabled() {
		return nil, nil
	}
	if s.gitPath == "" {
		return nil, errors.New("git executable not found in PATH")
	}
	dirs := s.discover(ctx)
	repos := make([]Repo, 0, len(dirs))
	var mu sync.Mutex
	workers := 6
	if len(dirs) < workers {
		workers = len(dirs)
	}
	if workers == 0 {
		workers = 1
	}
	ch := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for dir := range ch {
				repo, err := s.load(ctx, dir)
				if err != nil {
					continue
				}
				mu.Lock()
				repos = append(repos, repo)
				mu.Unlock()
			}
		}()
	}
	for _, dir := range dirs {
		ch <- dir
	}
	close(ch)
	wg.Wait()

	sort.Slice(repos, func(i, j int) bool {
		if repos[i].Name == repos[j].Name {
			return repos[i].Path < repos[j].Path
		}
		return repos[i].Name < repos[j].Name
	})
	s.mu.Lock()
	s.snapshot = repos
	s.snapshotSet = true
	s.fetchedAt = time.Now()
	s.mu.Unlock()
	return append([]Repo(nil), repos...), nil
}

func (s *Service) Find(nameOrPath string) (Repo, error) {
	value := strings.TrimSpace(nameOrPath)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, repo := range s.snapshot {
		if strings.EqualFold(repo.Name, value) || repo.Path == value {
			return repo, nil
		}
	}
	return Repo{}, fmt.Errorf("repository %q not found", nameOrPath)
}

func (s *Service) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.snapshot)
}

func (s *Service) discover(ctx context.Context) []string {
	seen := map[string]bool{}
	var out []string
	add := func(path string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, abs)
	}
	for _, dir := range s.explicit {
		if root, ok := s.toplevel(ctx, dir); ok {
			add(root)
		}
	}
	for _, root := range s.roots {
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !entry.IsDir() {
				return nil
			}
			if path == root {
				return nil
			}
			name := entry.Name()
			if name == ".git" {
				add(filepath.Dir(path))
				return filepath.SkipDir
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil && strings.Count(rel, string(os.PathSeparator)) >= s.maxDepth {
				return filepath.SkipDir
			}
			lower := strings.ToLower(name)
			if strings.HasPrefix(name, ".") || s.exclude[lower] || defaultExcludes[lower] {
				return filepath.SkipDir
			}
			return nil
		})
	}
	return out
}

func (s *Service) toplevel(ctx context.Context, dir string) (string, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", false
	}
	out, err := s.git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	out = strings.TrimSpace(out)
	return out, out != ""
}

func (s *Service) load(ctx context.Context, dir string) (Repo, error) {
	repo := Repo{Path: dir, Name: filepath.Base(dir)}
	branch, _ := s.git(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	repo.Branch = strings.TrimSpace(branch)
	if repo.Branch == "HEAD" {
		if sha, err := s.git(ctx, dir, "rev-parse", "--short", "HEAD"); err == nil {
			repo.Branch = strings.TrimSpace(sha)
		}
	}
	commit, err := s.git(ctx, dir, "log", "-1", "--format=%H%x00%h%x00%s%x00%an%x00%aI")
	if err == nil {
		parts := strings.Split(commit, "\x00")
		if len(parts) >= 5 {
			repo.Commit = strings.TrimSpace(parts[0])
			repo.CommitShort = strings.TrimSpace(parts[1])
			repo.Subject = strings.TrimSpace(parts[2])
			repo.Author = strings.TrimSpace(parts[3])
			if parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(parts[4])); parseErr == nil {
				repo.CommittedAt = parsed
			}
		}
	}
	if status, err := s.git(ctx, dir, "status", "--porcelain=v1"); err == nil {
		lines := strings.Split(strings.TrimRight(status, "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			repo.Changed = 0
		} else {
			repo.Changed = len(lines)
			repo.Dirty = true
		}
	}
	if upstream, err := s.git(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		repo.Upstream = strings.TrimSpace(upstream)
		if counts, err := s.git(ctx, dir, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
			if fields := strings.Fields(counts); len(fields) == 2 {
				repo.Behind, _ = strconv.Atoi(fields[0])
				repo.Ahead, _ = strconv.Atoi(fields[1])
			}
		}
	}
	remote, _ := s.git(ctx, dir, "remote", "get-url", "origin")
	repo.Remote = strings.TrimSpace(remote)
	if repo.Remote == "" {
		if remotes, err := s.git(ctx, dir, "remote"); err == nil {
			for _, name := range strings.Fields(remotes) {
				if value, err := s.git(ctx, dir, "remote", "get-url", name); err == nil {
					repo.Remote = strings.TrimSpace(value)
					break
				}
			}
		}
	}
	repo.Provider, repo.WebURL, repo.Links = describeRemote(repo.Remote, repo.Branch, repo.Commit, s.providers)
	if repo.WebURL == "" {
		repo.WebURL = repo.Remote
	}
	repo.Links.Repo = repo.WebURL
	return repo, nil
}

func (s *Service) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(cmdCtx, s.gitPath, full...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func describeRemote(raw, branch, commit string, overrides map[string]string) (string, string, Links) {
	host, path, scheme, ok := parseRemote(raw)
	if !ok {
		return "git", "", Links{}
	}
	provider := providerFor(host, overrides)
	web := scheme + "://" + host + "/" + path
	links := Links{Repo: web}
	if branch != "" {
		switch provider {
		case "github":
			links.Branch = web + "/tree/" + branch
			links.NewMergeRequest = web + "/compare/" + url.PathEscape(branch) + "?expand=1"
		case "gitlab":
			links.Branch = web + "/-/tree/" + branch
			links.NewMergeRequest = web + "/-/merge_requests/new?merge_request%5Bsource_branch%5D=" + url.QueryEscape(branch)
		}
	}
	if commit != "" && (provider == "github" || provider == "gitlab") {
		if provider == "github" {
			links.Commit = web + "/commit/" + commit
			links.MergeRequests = web + "/pulls"
		} else {
			links.Commit = web + "/-/commit/" + commit
			links.MergeRequests = web + "/-/merge_requests"
		}
	}
	return provider, web, links
}

func parseRemote(raw string) (host, path, scheme string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", false
	}
	scheme = "https"
	switch {
	case strings.HasPrefix(raw, "https://"):
		scheme = "https"
	case strings.HasPrefix(raw, "http://"):
		scheme = "http"
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", "", "", false
		}
		host = parsed.Hostname()
		path = strings.TrimPrefix(parsed.Path, "/")
		if parsed.Scheme == "http" {
			scheme = "http"
		}
	} else if at := strings.Index(raw, "@"); at >= 0 && strings.Contains(raw[at:], ":") {
		rest := raw[at+1:]
		host, path, _ = strings.Cut(rest, ":")
		path = strings.TrimPrefix(path, "/")
	} else {
		return "", "", "", false
	}
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimSuffix(path, "/")
	if host == "" || path == "" {
		return "", "", "", false
	}
	return host, path, scheme, true
}

func providerFor(host string, overrides map[string]string) string {
	lower := strings.ToLower(host)
	if provider, ok := overrides[lower]; ok && provider != "" {
		return provider
	}
	switch {
	case strings.Contains(lower, "github"):
		return "github"
	case strings.Contains(lower, "gitlab"):
		return "gitlab"
	default:
		return "git"
	}
}

var defaultExcludes = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
	"bin":          true,
	"obj":          true,
	"tmp":          true,
	"cache":        true,
}

func rank(repos []Repo, raw string, limit int) []Hit {
	query := search.Parse(stripSelectors(raw))
	terms := make([]string, 0, len(query.Terms))
	for _, term := range query.Terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term != "" {
			terms = append(terms, term)
		}
	}
	repoFilters := lowerAll(query.Filters[search.FieldRepo])
	branchFilters := lowerAll(query.Filters[search.FieldBranch])

	hits := make([]Hit, 0, len(repos))
	for _, repo := range repos {
		if !matchesFilters(repo, repoFilters, branchFilters) {
			continue
		}
		score, reasons, ok := scoreRepo(repo, terms)
		if !ok {
			continue
		}
		percent := 0
		if len(terms) > 0 {
			percent = score / len(terms)
			if percent > 100 {
				percent = 100
			}
		}
		hits = append(hits, Hit{Repo: repo, Score: score, Percent: percent, Reasons: reasons})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			if hits[i].Repo.Name == hits[j].Repo.Name {
				return hits[i].Repo.Path < hits[j].Repo.Path
			}
			return strings.ToLower(hits[i].Repo.Name) < strings.ToLower(hits[j].Repo.Name)
		}
		return hits[i].Score > hits[j].Score
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func matchesFilters(repo Repo, repoFilters, branchFilters []string) bool {
	for _, filter := range repoFilters {
		if !strings.Contains(strings.ToLower(repo.Name), filter) && !strings.Contains(strings.ToLower(repo.Path), filter) {
			return false
		}
	}
	for _, filter := range branchFilters {
		if !strings.Contains(strings.ToLower(repo.Branch), filter) {
			return false
		}
	}
	return true
}

func scoreRepo(repo Repo, terms []string) (int, []string, bool) {
	if len(terms) == 0 {
		return 0, nil, true
	}
	name := strings.ToLower(repo.Name)
	path := strings.ToLower(repo.Path)
	branch := strings.ToLower(repo.Branch)
	subject := strings.ToLower(repo.Subject)
	author := strings.ToLower(repo.Author)
	remote := strings.ToLower(repo.Remote)
	provider := strings.ToLower(repo.Provider)
	score := 0
	reasons := make([]string, 0, 2)
	for _, term := range terms {
		best := 0
		reason := ""
		switch {
		case name == term:
			best, reason = 100, "name"
		case strings.HasPrefix(name, term):
			best, reason = 90, "name"
		case strings.Contains(name, term):
			best, reason = 70, "name"
		case branch == term:
			best, reason = 65, "branch"
		case strings.Contains(branch, term):
			best, reason = 60, "branch"
		case strings.Contains(subject, term):
			best, reason = 50, "commit"
		case strings.Contains(author, term):
			best, reason = 40, "author"
		case strings.Contains(remote, term):
			best, reason = 25, "remote"
		case strings.Contains(path, term):
			best, reason = 15, "path"
		case strings.Contains(provider, term):
			best, reason = 10, "provider"
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

func stripSelectors(raw string) string {
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.ToLower(strings.Trim(field, `"'`))
		if token == "git" {
			continue
		}
		out = append(out, field)
	}
	return strings.Join(out, " ")
}

func HasGitSelector(raw string) bool {
	for _, field := range strings.Fields(raw) {
		token := strings.ToLower(strings.Trim(field, `"'`))
		if token == "git" {
			return true
		}
	}
	return false
}

func lowerAll(values []string) []string {
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
