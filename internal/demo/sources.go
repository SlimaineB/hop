package demo

import (
	"context"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/providers"
	containerregistry "github.com/SlimaineB/hop/internal/registry"
	"github.com/SlimaineB/hop/internal/repos"
)

type staticSource struct {
	id        string
	title     string
	kind      string
	selectors []string
	items     []plugin.Item
}

func (s *staticSource) ID() string          { return s.id }
func (s *staticSource) Title() string       { return s.title }
func (s *staticSource) Kind() string        { return s.kind }
func (s *staticSource) Selectors() []string { return append([]string(nil), s.selectors...) }

func (s *staticSource) Search(_ context.Context, query plugin.Query) (plugin.Result, error) {
	return s.search(query), nil
}

func (s *staticSource) SearchLocal(query plugin.Query) (plugin.Result, error) {
	return s.search(query), nil
}

func (s *staticSource) search(query plugin.Query) plugin.Result {
	terms := query.WithoutSelectors(s.selectors...).Terms
	items := make([]plugin.Item, 0, len(s.items))
	for _, item := range s.items {
		score := 60
		if len(terms) > 0 {
			haystack := strings.ToLower(strings.Join([]string{
				item.Title, item.Subtitle, item.Description, item.Badge, item.URL, item.Copy, metaText(item.Meta),
			}, " "))
			matched := 0
			for _, term := range terms {
				term = strings.ToLower(strings.TrimSpace(term))
				if term != "" && strings.Contains(haystack, term) {
					matched++
				}
			}
			if matched == 0 {
				continue
			}
			score = 50 + matched*15
			if score > 99 {
				score = 99
			}
		}
		item.Score = score
		item.Percent = score
		items = append(items, item)
	}
	plugin.SortItems(items)
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return plugin.Result{ID: s.ID(), Title: s.Title(), Kind: s.Kind(), Items: items, Total: len(items)}
}

func metaText(meta map[string]string) string {
	parts := make([]string, 0, len(meta))
	for key, value := range meta {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, " ")
}

func demoSources() []plugin.Source {
	return []plugin.Source{
		&staticSource{
			id: "containers", title: "Containers", kind: "containers",
			selectors: []string{"container", "containers", "ctr", "docker", "podman"},
			items: []plugin.Item{
				{ID: "checkout-api-1", Title: "checkout-api-1", Subtitle: "ghcr.io/acme/checkout-api:1.18.2 · running", Description: "compose/checkout · 3h", Badge: "docker", Copy: "checkout-api-1", Meta: map[string]string{"engine": "docker", "project": "checkout", "state": "running"}},
				{ID: "checkout-worker-1", Title: "checkout-worker-1", Subtitle: "ghcr.io/acme/checkout-worker:1.18.2 · running", Description: "compose/checkout · 8h", Badge: "docker", Copy: "checkout-worker-1", Meta: map[string]string{"engine": "docker", "project": "checkout", "state": "running"}},
				{ID: "payments-api-1", Title: "payments-api-1", Subtitle: "ghcr.io/acme/payments-api:2.4.0 · restarting", Description: "compose/payments · 42m", Badge: "podman", Copy: "payments-api-1", Meta: map[string]string{"engine": "podman", "project": "payments", "state": "restarting"}},
				{ID: "postgres-1", Title: "postgres-1", Subtitle: "docker.io/postgres:16.3 · healthy", Description: "compose/payments · 3d", Badge: "docker", Copy: "postgres-1", Meta: map[string]string{"engine": "docker", "project": "payments", "state": "running"}},
				{ID: "grafana-1", Title: "grafana-1", Subtitle: "docker.io/grafana/grafana:11.2.0 · running", Description: "compose/observability · 4d", Badge: "docker", Copy: "grafana-1", Meta: map[string]string{"engine": "docker", "project": "observability", "state": "running"}},
			},
		},
		&staticSource{
			id: "repos", title: "Repositories", kind: "repos",
			selectors: []string{"git", "repos"},
			items: []plugin.Item{
				repoItem("checkout-api", "/home/dev/acme/checkout-api", "main", "feat: add retry budget to payment calls", "2 changed", "1.18.2", "https://gitlab.example.com/acme/checkout-api"),
				repoItem("checkout-web", "/home/dev/acme/checkout-web", "main", "chore: bump checkout-api client", "clean", "4.7.0", "https://gitlab.example.com/acme/checkout-web"),
				repoItem("payments-api", "/home/dev/acme/payments-api", "fix/crashloop", "fix: guard nil provider config", "3 changed", "2.4.0", "https://gitlab.example.com/acme/payments-api"),
				repoItem("platform-infra", "/home/dev/acme/platform-infra", "main", "feat: add staging node pool", "clean", "0.31.2", "https://gitlab.example.com/acme/platform-infra"),
				repoItem("hop", "/home/dev/teu", "main", "feat: add local demo mode", "5 changed", "0.8.0", "https://github.com/SlimaineB/hop"),
			},
		},
		&staticSource{
			id: "packages", title: "Packages", kind: "packages",
			selectors: []string{"pkg", "packages", "registry"},
			items: []plugin.Item{
				packageItem("ghcr.io", "acme/checkout-api", "ghcr", "1.18.2", "1.18.1", "1.18.0", "latest"),
				packageItem("ghcr.io", "acme/payments-api", "ghcr", "2.4.0", "2.3.9", "2.3.8", "latest"),
				packageItem("registry.gitlab.com", "acme/auth-api", "gitlab", "3.9.1", "3.9.0", "3.8.7"),
				packageItem("quay.io", "prometheus/prometheus", "quay", "v2.54.1", "v2.54.0", "v2.53.2"),
				packageItem("docker.io", "grafana/grafana", "docker", "11.2.0", "11.1.5", "11.0.3"),
			},
		},
		&staticSource{
			id: "tickets", title: "Tickets", kind: "tickets",
			selectors: []string{"ticket", "tickets", "issue", "issues"},
			items: []plugin.Item{
				ticketItem("jira", "OPS-142", "Checkout latency above SLO", "In progress", "https://jira.example.com/browse/OPS-142", "2h ago"),
				ticketItem("jira", "OPS-138", "Payments API crashloop after deploy", "In review", "https://jira.example.com/browse/OPS-138", "5h ago"),
				ticketItem("jira", "PLAT-77", "Upgrade staging Kubernetes to 1.33", "To do", "https://jira.example.com/browse/PLAT-77", "1d ago"),
				ticketItem("gitlab", "acme/checkout-api!241", "Add retry budget for payment calls", "Open", "https://gitlab.example.com/acme/checkout-api/-/merge_requests/241", "18m ago"),
				ticketItem("gitlab", "acme/payments-api!88", "Guard nil provider configuration", "Merged", "https://gitlab.example.com/acme/payments-api/-/merge_requests/88", "3h ago"),
			},
		},
	}
}

func repoItem(name, path, branch, subject, state, version, web string) plugin.Item {
	changed := 0
	if state != "clean" {
		changed = 2
	}
	repo := repos.Repo{
		Name: name, Path: path, Branch: branch, Provider: "gitlab", WebURL: web,
		Commit: "9f3c2a1e7b8d", CommitShort: "9f3c2a1", Subject: subject, Author: "demo",
		CommittedAt: time.Now().Add(-37 * time.Minute), Dirty: state != "clean", Changed: changed,
		Links: repos.Links{
			Repo: web, Branch: web + "/-/tree/" + branch, Commit: web + "/-/commit/9f3c2a1e7b8d",
			MergeRequests: web + "/-/merge_requests", NewMergeRequest: web + "/-/merge_requests/new",
		},
	}
	return plugin.Item{
		ID: name, Title: name, Subtitle: branch + " · " + subject, Description: state + " · " + version,
		Badge: "gitlab", URL: web, Copy: path,
		Meta:    map[string]string{"branch": branch, "version": version, "commit": repo.CommitShort},
		Payload: repos.Hit{Repo: repo, Score: 1, Percent: 92},
	}
}

func packageItem(host, repository, provider string, tags ...string) plugin.Item {
	image := containerregistry.Image{
		Reference: host + "/" + repository + ":" + tags[0], Registry: host, Repository: repository,
		Provider: provider, Tags: tags, WebURL: "https://" + host + "/" + repository,
		Pull: host + "/" + repository + ":" + tags[0],
	}
	return plugin.Item{
		ID: host + "/" + repository, Title: repository, Subtitle: strings.Join(tags, ", "),
		Badge: provider, URL: image.WebURL, Copy: image.Pull,
		Meta:    map[string]string{"registry": host, "tags": strings.Join(tags, ",")},
		Payload: containerregistry.Hit{Image: image, Score: 1, Percent: 90},
	}
}

func ticketItem(provider, key, title, status, url, updated string) plugin.Item {
	ticket := providers.Ticket{Provider: provider, Key: key, Title: title, Status: status, URL: url, Updated: updated}
	return plugin.Item{
		ID: provider + ":" + key, Title: key, Subtitle: title, Description: status, Badge: provider,
		URL: url, Copy: key, Meta: map[string]string{"status": status, "updated": updated}, Payload: ticket,
	}
}
