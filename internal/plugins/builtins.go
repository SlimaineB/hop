// Package plugins provides the built-in cockpit sources and assembles the
// plugin registry from configuration.
package plugins

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/containers"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/providers"
	"github.com/SlimaineB/hop/internal/registry"
	"github.com/SlimaineB/hop/internal/repos"
	"github.com/SlimaineB/hop/internal/servers"
)

type PodSearcher interface {
	Search(ctx context.Context, req app.SearchRequest) (*app.SearchResult, error)
	SearchLocal(req app.SearchRequest) (*app.SearchResult, error)
	Stats() app.Stats
	Snapshot() *app.Snapshot
}

type Deps struct {
	Pods             PodSearcher
	Servers          *servers.Service
	Containers       *containers.Service
	Repos            *repos.Service
	Registry         *registry.Service
	Providers        *providers.Set
	Shortcuts        []config.ShortcutConfig
	OnImagesDetected func([]string)
}

func Build(cfg *config.Config, deps Deps) *plugin.Registry {
	registry := plugin.NewRegistry()
	for _, source := range Builtins(deps) {
		if cfg != nil && cfg.SourceDisabled(source.ID()) {
			continue
		}
		_ = registry.Register(source)
	}
	if cfg != nil {
		for _, command := range cfg.Plugins {
			if cfg.SourceDisabled(command.ID) {
				continue
			}
			_ = registry.Register(plugin.NewCommandSource(command))
		}
	}
	return registry
}

func Builtins(deps Deps) []plugin.Source {
	sources := make([]plugin.Source, 0, 7)
	if deps.Pods != nil {
		sources = append(sources, &podSource{pods: deps.Pods, onImages: deps.OnImagesDetected})
	}
	if deps.Servers != nil {
		sources = append(sources, &serverSource{servers: deps.Servers})
	}
	if deps.Containers != nil && deps.Containers.Enabled() {
		sources = append(sources, &containerSource{containers: deps.Containers})
	}
	if deps.Repos != nil && deps.Repos.Enabled() {
		sources = append(sources, &repoSource{repos: deps.Repos})
	}
	if deps.Registry != nil {
		sources = append(sources, &packageSource{registry: deps.Registry})
	}
	if deps.Providers != nil {
		sources = append(sources, &ticketSource{providers: deps.Providers})
	}
	if len(deps.Shortcuts) > 0 {
		sources = append(sources, &shortcutSource{shortcuts: deps.Shortcuts})
	}
	return sources
}

type podSource struct {
	pods     PodSearcher
	onImages func([]string)
}

func (s *podSource) ID() string          { return "pods" }
func (s *podSource) Title() string       { return "Pods" }
func (s *podSource) Kind() string        { return "pods" }
func (s *podSource) Selectors() []string { return nil }

func (s *podSource) Search(ctx context.Context, query plugin.Query) (plugin.Result, error) {
	return s.search(ctx, query, false)
}

func (s *podSource) SearchLocal(query plugin.Query) (plugin.Result, error) {
	return s.search(context.Background(), query, true)
}

func (s *podSource) search(ctx context.Context, query plugin.Query, local bool) (plugin.Result, error) {
	req := app.SearchRequest{
		Query:        query.Raw,
		Contexts:     query.Contexts,
		Namespaces:   query.Namespaces,
		Env:          query.Env,
		Limit:        query.Limit,
		ForceRefresh: query.ForceRefresh,
	}
	var result *app.SearchResult
	var err error
	if local {
		result, err = s.pods.SearchLocal(req)
	} else {
		result, err = s.pods.Search(ctx, req)
	}
	if err != nil {
		return plugin.Result{}, err
	}
	s.reportImages(result)
	items := make([]plugin.Item, 0, len(result.Hits))
	for _, hit := range result.Hits {
		pod := hit.Pod
		items = append(items, plugin.Item{
			ID:          podKey(pod),
			Title:       pod.Name,
			Subtitle:    pod.Namespace + " · " + displayContext(pod),
			Description: ownerLabel(pod),
			Badge:       pod.Env,
			Score:       hit.Percent(),
			Percent:     hit.Percent(),
			Meta: map[string]string{
				"context":   pod.Context,
				"namespace": pod.Namespace,
				"phase":     pod.Phase,
				"node":      pod.Node,
				"env":       pod.Env,
			},
			Payload: hit,
		})
	}
	plugin.SortItems(items)
	meta := struct {
		Meta     app.Meta  `json:"meta"`
		Stats    app.Stats `json:"stats"`
		Total    int       `json:"total"`
		Eligible int       `json:"eligible"`
		Matched  int       `json:"matched"`
	}{
		Meta:     result.Meta,
		Stats:    result.Stats,
		Total:    result.Total,
		Eligible: result.Eligible,
		Matched:  result.Matched,
	}
	errs := make([]string, 0, len(result.Meta.Errors))
	for _, item := range result.Meta.Errors {
		errs = append(errs, item.Context+": "+item.Error)
	}
	return plugin.Result{
		ID:     s.ID(),
		Title:  s.Title(),
		Kind:   s.Kind(),
		Items:  items,
		Total:  len(items),
		Errors: errs,
		Meta:   meta,
	}, nil
}

func (s *podSource) reportImages(result *app.SearchResult) {
	if s.onImages == nil || s.pods == nil {
		return
	}
	seen := map[string]bool{}
	images := make([]string, 0, 32)
	add := func(values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			images = append(images, value)
		}
	}
	for _, hit := range result.Hits {
		add(hit.Pod.Images...)
	}
	if snapshot := s.pods.Snapshot(); snapshot != nil {
		for _, pod := range snapshot.Pods {
			add(pod.Images...)
		}
	}
	if len(images) > 0 {
		s.onImages(images)
	}
}

type serverSource struct {
	servers *servers.Service
}

func (s *serverSource) ID() string          { return "servers" }
func (s *serverSource) Title() string       { return "VMs" }
func (s *serverSource) Kind() string        { return "servers" }
func (s *serverSource) Selectors() []string { return []string{"vm", "vms"} }

func (s *serverSource) Search(ctx context.Context, query plugin.Query) (plugin.Result, error) {
	return s.search(query, false)
}

func (s *serverSource) SearchLocal(query plugin.Query) (plugin.Result, error) {
	return s.search(query, true)
}

func (s *serverSource) search(query plugin.Query, local bool) (plugin.Result, error) {
	raw := plugin.StripSelectors(query.Raw, s.Selectors()...)
	if strings.TrimSpace(raw) == "" {
		raw = "vm"
	}
	var hits []servers.Hit
	var err error
	if local {
		hits, err = s.servers.SearchLocal(raw, query.Limit)
	} else {
		hits, err = s.servers.Search(raw, query.Limit)
	}
	if err != nil {
		return plugin.Result{}, err
	}
	items := make([]plugin.Item, 0, len(hits))
	for _, hit := range hits {
		server := hit.Server
		items = append(items, plugin.Item{
			ID:       server.Name,
			Title:    server.Name,
			Subtitle: serverSubtitle(server),
			Badge:    server.Env,
			Score:    hit.Percent,
			Percent:  hit.Percent,
			Meta: map[string]string{
				"host":   server.Host,
				"user":   server.User,
				"source": server.Source,
				"env":    server.Env,
				"tags":   strings.Join(server.Tags, ","),
			},
			Payload: hit,
		})
	}
	return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind(), Items: items, Total: len(items)}, nil
}

type containerSource struct {
	containers *containers.Service
}

func (s *containerSource) ID() string    { return "containers" }
func (s *containerSource) Title() string { return "Containers" }
func (s *containerSource) Kind() string  { return "containers" }
func (s *containerSource) Selectors() []string {
	return []string{"container", "containers", "ctr", "docker", "podman"}
}

func (s *containerSource) Search(ctx context.Context, query plugin.Query) (plugin.Result, error) {
	return s.search(ctx, query, false)
}

func (s *containerSource) SearchLocal(query plugin.Query) (plugin.Result, error) {
	return s.search(context.Background(), query, true)
}

func (s *containerSource) search(ctx context.Context, query plugin.Query, local bool) (plugin.Result, error) {
	raw := plugin.StripSelectors(query.Raw, s.Selectors()...)
	var hits []containers.Hit
	var warnings []string
	var err error
	if local {
		hits, warnings, err = s.containers.SearchLocal(raw, query.Limit)
	} else {
		hits, warnings, err = s.containers.Search(ctx, raw, query.Limit)
	}
	if err != nil {
		return plugin.Result{}, err
	}
	items := make([]plugin.Item, 0, len(hits))
	for _, hit := range hits {
		item := hit.Container
		command := []string{"exec", "-it", item.ID, "sh"}
		if !strings.EqualFold(item.State, "running") {
			command = []string{"logs", "--tail", "100", item.ID}
		}
		subtitle := item.Engine
		if item.Image != "" {
			subtitle += " · " + item.Image
		}
		description := item.Status
		if item.Project != "" || item.Service != "" {
			description = strings.Trim(strings.Join([]string{item.Project, item.Service}, " · "), " ·")
		}
		items = append(items, plugin.Item{
			ID:          item.Engine + ":" + item.ID,
			Title:       item.Name,
			Subtitle:    subtitle,
			Description: description,
			Badge:       item.State,
			Score:       int(hit.Score * 100),
			Percent:     hit.Percent,
			Copy:        s.containers.CommandLine(item, command...),
			Meta: map[string]string{
				"engine":  item.Engine,
				"image":   item.Image,
				"state":   item.State,
				"status":  item.Status,
				"ports":   item.Ports,
				"project": item.Project,
				"service": item.Service,
				"id":      item.ID,
			},
			Payload: hit,
		})
	}
	return plugin.Result{
		ID:     s.ID(),
		Title:  s.Title(),
		Kind:   s.Kind(),
		Items:  items,
		Total:  len(items),
		Errors: warnings,
	}, nil
}

type repoSource struct {
	repos *repos.Service
}

func (s *repoSource) ID() string          { return "repos" }
func (s *repoSource) Title() string       { return "Repositories" }
func (s *repoSource) Kind() string        { return "repos" }
func (s *repoSource) Selectors() []string { return []string{"git", "repos"} }

func (s *repoSource) Search(ctx context.Context, query plugin.Query) (plugin.Result, error) {
	return s.search(ctx, query, false)
}

func (s *repoSource) SearchLocal(query plugin.Query) (plugin.Result, error) {
	return s.search(context.Background(), query, true)
}

func (s *repoSource) search(ctx context.Context, query plugin.Query, local bool) (plugin.Result, error) {
	raw := plugin.StripSelectors(query.Raw, s.Selectors()...)
	var hits []repos.Hit
	var err error
	if local {
		hits, err = s.repos.SearchLocal(raw, query.Limit)
	} else {
		hits, err = s.repos.Search(ctx, raw, query.Limit)
	}
	if err != nil {
		return plugin.Result{}, err
	}
	items := make([]plugin.Item, 0, len(hits))
	for _, hit := range hits {
		repo := hit.Repo
		items = append(items, plugin.Item{
			ID:          repo.Path,
			Title:       repo.Name,
			Subtitle:    repoSubtitle(repo),
			Description: repo.Subject,
			Badge:       repo.Provider,
			URL:         repo.WebURL,
			Copy:        repo.Path,
			Score:       hit.Score,
			Percent:     hit.Percent,
			Meta: map[string]string{
				"branch":  repo.Branch,
				"path":    repo.Path,
				"remote":  repo.Remote,
				"dirty":   boolString(repo.Dirty),
				"changed": itoa(repo.Changed),
				"ahead":   itoa(repo.Ahead),
				"behind":  itoa(repo.Behind),
			},
			Payload: hit,
		})
	}
	return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind(), Items: items, Total: len(items)}, nil
}

type packageSource struct {
	registry *registry.Service
}

func (s *packageSource) ID() string    { return "packages" }
func (s *packageSource) Title() string { return "Packages" }
func (s *packageSource) Kind() string  { return "packages" }
func (s *packageSource) Selectors() []string {
	return []string{"pkg", "pkgs", "package", "packages", "registry"}
}

func (s *packageSource) Search(ctx context.Context, query plugin.Query) (plugin.Result, error) {
	return s.search(ctx, query, false)
}

func (s *packageSource) SearchLocal(query plugin.Query) (plugin.Result, error) {
	return s.search(context.Background(), query, true)
}

func (s *packageSource) search(ctx context.Context, query plugin.Query, local bool) (plugin.Result, error) {
	raw := plugin.StripSelectors(query.Raw, s.Selectors()...)
	var hits []registry.Hit
	var warnings []string
	var err error
	if local {
		hits, warnings, err = s.registry.SearchLocal(raw, query.Limit)
	} else {
		hits, warnings, err = s.registry.Search(ctx, raw, query.Limit)
	}
	if err != nil {
		return plugin.Result{}, err
	}
	items := make([]plugin.Item, 0, len(hits))
	for _, hit := range hits {
		image := hit.Image
		items = append(items, plugin.Item{
			ID:       image.Reference,
			Title:    imageName(image.Repository),
			Subtitle: image.Registry + "/" + image.Repository,
			Badge:    image.Provider,
			URL:      image.WebURL,
			Copy:     image.Pull,
			Score:    hit.Score,
			Percent:  hit.Percent,
			Meta: map[string]string{
				"registry": image.Registry,
				"tags":     strings.Join(image.Tags, ","),
			},
			Payload: hit,
		})
	}
	return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind(), Items: items, Total: len(items), Errors: warnings}, nil
}

type ticketSource struct {
	providers *providers.Set
}

func (s *ticketSource) ID() string          { return "tickets" }
func (s *ticketSource) Title() string       { return "Tickets" }
func (s *ticketSource) Kind() string        { return "tickets" }
func (s *ticketSource) Selectors() []string { return []string{"ticket", "tickets", "issue", "issues"} }

func (s *ticketSource) Search(ctx context.Context, query plugin.Query) (plugin.Result, error) {
	if query.Local || !query.IncludeNetwork {
		return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind()}, nil
	}
	terms := query.WithoutSelectors(s.Selectors()...).Terms
	if len(terms) == 0 {
		return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind()}, nil
	}
	tickets, err := s.searchTickets(ctx, terms)
	if err != nil {
		return plugin.Result{}, err
	}
	items := make([]plugin.Item, 0, len(tickets))
	for _, ticket := range tickets {
		url := ticket.URL
		if url == "" {
			if provider, ok := s.providers.Get(ticket.Provider); ok {
				url = provider.URL(ticket.Key)
			}
		}
		items = append(items, plugin.Item{
			ID:          ticket.Provider + ":" + ticket.Key,
			Title:       ticket.Key,
			Subtitle:    ticket.Title,
			Description: ticket.Status,
			Badge:       ticket.Provider,
			URL:         url,
			Copy:        ticket.Key,
			Meta:        map[string]string{"status": ticket.Status, "updated": ticket.Updated},
			Payload:     ticket,
		})
	}
	return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind(), Items: items, Total: len(items)}, nil
}

func (s *ticketSource) searchTickets(ctx context.Context, terms []string) ([]providers.Ticket, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if len(terms) == 1 {
		if matches := s.providers.DetectKey(terms[0]); len(matches) > 0 {
			if ticket, err := matches[0].Get(ctx, terms[0]); err == nil {
				return []providers.Ticket{ticket}, nil
			}
		}
	}
	query := strings.Join(terms, " ")
	var tickets []providers.Ticket
	var failures []string
	for _, provider := range s.providers.All() {
		found, err := provider.Search(ctx, query, 5)
		if err != nil {
			failures = append(failures, provider.ID()+": "+err.Error())
			continue
		}
		tickets = append(tickets, found...)
	}
	sort.SliceStable(tickets, func(i, j int) bool {
		if tickets[i].Provider != tickets[j].Provider {
			return tickets[i].Provider < tickets[j].Provider
		}
		return tickets[i].Key < tickets[j].Key
	})
	if len(tickets) > 10 {
		tickets = tickets[:10]
	}
	if len(tickets) == 0 && len(failures) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return tickets, nil
}

type shortcutSource struct {
	shortcuts []config.ShortcutConfig
}

func (s *shortcutSource) ID() string    { return "shortcuts" }
func (s *shortcutSource) Title() string { return "Shortcuts" }
func (s *shortcutSource) Kind() string  { return "shortcuts" }
func (s *shortcutSource) Selectors() []string {
	return []string{"link", "links", "shortcut", "shortcuts"}
}

func (s *shortcutSource) Search(_ context.Context, query plugin.Query) (plugin.Result, error) {
	terms := query.WithoutSelectors(s.Selectors()...).Terms
	if len(terms) == 0 {
		return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind()}, nil
	}
	hits := config.MatchShortcuts(s.shortcuts, terms)
	items := make([]plugin.Item, 0, len(hits))
	for _, hit := range hits {
		shortcut := hit.Shortcut
		items = append(items, plugin.Item{
			ID:       shortcut.Name,
			Title:    shortcut.Name,
			Subtitle: shortcut.Description,
			Badge:    "link",
			URL:      shortcut.URL,
			Copy:     shortcut.URL,
			Score:    hit.Score,
			Meta:     map[string]string{"keywords": strings.Join(shortcut.Keywords, ",")},
			Payload:  hit,
		})
	}
	return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind(), Items: items, Total: len(items)}, nil
}

func podKey(pod kube.Pod) string {
	return pod.Context + "/" + pod.Namespace + "/" + pod.Name
}

func displayContext(pod kube.Pod) string {
	if pod.Display != "" {
		return pod.Display
	}
	return pod.Context
}

func ownerLabel(pod kube.Pod) string {
	if pod.OwnerName == "" {
		return pod.Workload
	}
	if pod.OwnerKind == "" {
		return pod.OwnerName
	}
	return pod.OwnerKind + "/" + pod.OwnerName
}

func serverSubtitle(server servers.Server) string {
	if server.SSH != "" {
		return server.SSH
	}
	target := server.Host
	if server.User != "" {
		target = server.User + "@" + target
	}
	return target
}

func repoSubtitle(repo repos.Repo) string {
	parts := make([]string, 0, 3)
	if repo.Branch != "" {
		parts = append(parts, repo.Branch)
	}
	if repo.Subject != "" {
		parts = append(parts, repo.Subject)
	}
	if repo.Dirty {
		parts = append(parts, fmt.Sprintf("%d changed", repo.Changed))
	}
	return strings.Join(parts, " · ")
}

func imageName(repository string) string {
	if slash := strings.LastIndex(repository, "/"); slash >= 0 {
		return repository[slash+1:]
	}
	return repository
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func itoa(value int) string {
	return fmt.Sprintf("%d", value)
}
