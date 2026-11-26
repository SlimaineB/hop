package repos

import (
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func TestParseRemote(t *testing.T) {
	cases := []struct {
		raw    string
		host   string
		path   string
		scheme string
		ok     bool
	}{
		{"git@github.com:SlimaineB/hop.git", "github.com", "SlimaineB/hop", "https", true},
		{"https://gitlab.com/group/project.git", "gitlab.com", "group/project", "https", true},
		{"http://git.example.com/team/repo", "git.example.com", "team/repo", "http", true},
		{"ssh://git@gitlab.com/group/repo.git", "gitlab.com", "group/repo", "https", true},
		{"", "", "", "", false},
		{"/local/path", "", "", "", false},
	}
	for _, tc := range cases {
		host, path, scheme, ok := parseRemote(tc.raw)
		if ok != tc.ok || host != tc.host || path != tc.path || scheme != tc.scheme {
			t.Fatalf("parseRemote(%q) = %q %q %q %v, want %q %q %q %v",
				tc.raw, host, path, scheme, ok, tc.host, tc.path, tc.scheme, tc.ok)
		}
	}
}

func TestDescribeRemoteLinks(t *testing.T) {
	provider, web, links := describeRemote("git@github.com:SlimaineB/hop.git", "main", "abc123", nil)
	if provider != "github" || web != "https://github.com/SlimaineB/hop" {
		t.Fatalf("provider=%q web=%q", provider, web)
	}
	if links.Branch != web+"/tree/main" || links.Commit != web+"/commit/abc123" {
		t.Fatalf("links = %+v", links)
	}
	if links.NewMergeRequest != web+"/compare/main?expand=1" || links.MergeRequests != web+"/pulls" {
		t.Fatalf("links = %+v", links)
	}

	provider, web, links = describeRemote("https://gitlab.com/group/project.git", "dev", "def456", nil)
	if provider != "gitlab" || web != "https://gitlab.com/group/project" {
		t.Fatalf("provider=%q web=%q", provider, web)
	}
	if links.Branch != web+"/-/tree/dev" || links.Commit != web+"/-/commit/def456" {
		t.Fatalf("links = %+v", links)
	}

	provider, _, links = describeRemote("https://git.example.com/team/repo.git", "main", "abc", nil)
	if provider != "git" || links.Branch != "" || links.Commit != "" {
		t.Fatalf("provider=%q links=%+v", provider, links)
	}
}

func TestDescribeRemoteOverride(t *testing.T) {
	provider, _, links := describeRemote("https://git.example.com/team/repo.git", "main", "abc", map[string]string{"git.example.com": "gitlab"})
	if provider != "gitlab" || links.Branch == "" {
		t.Fatalf("provider=%q links=%+v", provider, links)
	}
}

func TestRankRepos(t *testing.T) {
	items := []Repo{
		{Name: "hop", Path: "/dev/hop", Branch: "main", Subject: "cockpit search"},
		{Name: "website", Path: "/dev/website", Branch: "main", Subject: "landing page"},
		{Name: "api", Path: "/dev/api", Branch: "feature/hop", Subject: "endpoints"},
	}
	hits := rank(items, "hop", 10)
	if len(hits) != 2 || hits[0].Repo.Name != "hop" {
		t.Fatalf("hits = %+v", hits)
	}
	hits = rank(items, "branch:feature", 10)
	if len(hits) != 1 || hits[0].Repo.Name != "api" {
		t.Fatalf("branch hits = %+v", hits)
	}
	hits = rank(items, "repo:web", 10)
	if len(hits) != 1 || hits[0].Repo.Name != "website" {
		t.Fatalf("repo hits = %+v", hits)
	}
	if hits := rank(items, "missing", 10); len(hits) != 0 {
		t.Fatalf("unexpected hits = %+v", hits)
	}
}

func TestServiceEnabledAndNotReady(t *testing.T) {
	if (*Service)(nil).Enabled() {
		t.Fatal("nil service should be disabled")
	}
	service := New(&config.Config{})
	if service.Enabled() {
		t.Fatal("service without roots should be disabled")
	}
	service = New(&config.Config{Git: config.Git{Roots: []string{"/tmp"}}})
	if !service.Enabled() {
		t.Fatal("service with roots should be enabled")
	}
	if _, err := service.SearchLocal("x", 0); err != ErrNotReady {
		t.Fatalf("err = %v", err)
	}
}
