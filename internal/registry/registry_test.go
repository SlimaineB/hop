package registry

import (
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func TestSplitReference(t *testing.T) {
	cases := []struct {
		ref  string
		host string
		repo string
		ok   bool
	}{
		{"ghcr.io/SlimaineB/hop:latest", "ghcr.io", "SlimaineB/hop", true},
		{"registry.gitlab.com/group/project/image@sha256:deadbeef", "registry.gitlab.com", "group/project/image", true},
		{"nginx", "docker.io", "library/nginx", true},
		{"bitnami/redis:7", "docker.io", "bitnami/redis", true},
		{"localhost:5000/team/app", "localhost:5000", "team/app", true},
		{"", "", "", false},
		{"ghcr.io/", "", "", false},
	}
	for _, tc := range cases {
		host, repo, ok := splitReference(tc.ref)
		if ok != tc.ok || host != tc.host || repo != tc.repo {
			t.Fatalf("splitReference(%q) = %q %q %v, want %q %q %v",
				tc.ref, host, repo, ok, tc.host, tc.repo, tc.ok)
		}
	}
}

func TestDescribeImage(t *testing.T) {
	web, links := describeImage("ghcr.io", "SlimaineB/hop", "ghcr")
	if !strings.Contains(web, "SlimaineB") || links.Web != web {
		t.Fatalf("web=%q links=%+v", web, links)
	}
	web, _ = describeImage("registry.gitlab.com", "group/project/image", "gitlab")
	if web != "https://gitlab.com/group/project/-/container_registry" {
		t.Fatalf("web = %q", web)
	}
	if web, _ := describeImage("registry.example.com", "team/app", "registry"); web != "" {
		t.Fatalf("web = %q", web)
	}
}

func TestCandidateAndReferences(t *testing.T) {
	service := New(&config.Config{})
	service.SetImages("ghcr.io/SlimaineB/hop:main", "nginx:latest", "ghcr.io/SlimaineB/hop:main")
	refs := service.references()
	if len(refs) != 2 {
		t.Fatalf("refs = %v", refs)
	}
	candidate, ok := service.candidate("ghcr.io/SlimaineB/hop:main")
	if !ok || candidate.image.Repository != "SlimaineB/hop" || candidate.image.Provider != "ghcr" {
		t.Fatalf("candidate = %+v ok=%v", candidate, ok)
	}
	if candidate.image.WebURL == "" || candidate.image.Pull != "docker pull ghcr.io/SlimaineB/hop:main" {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestScoreImage(t *testing.T) {
	image := candidate{image: Image{Repository: "SlimaineB/hop", Registry: "ghcr.io", Provider: "ghcr", Tags: []string{"main", "v1"}}}
	score, _, ok := scoreImage(image, nil)
	if !ok || score != 0 {
		t.Fatalf("score=%d ok=%v", score, ok)
	}
	score, reasons, ok := scoreImage(image, lowerTerms([]string{"hop"}))
	if !ok || score == 0 {
		t.Fatalf("score=%d reasons=%v ok=%v", score, reasons, ok)
	}
	if _, _, ok := scoreImage(image, lowerTerms([]string{"missing"})); ok {
		t.Fatal("missing term should not match")
	}
	if _, _, ok := scoreImage(image, lowerTerms([]string{"v1"})); !ok {
		t.Fatal("tag should match")
	}
}
