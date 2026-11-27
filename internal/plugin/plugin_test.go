package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

type fakeSource struct {
	id        string
	selectors []string
	local     bool
	network   bool
	items     []Item
	err       error
}

func (f *fakeSource) ID() string          { return f.id }
func (f *fakeSource) Title() string       { return strings.ToUpper(f.id) }
func (f *fakeSource) Kind() string        { return f.id }
func (f *fakeSource) Selectors() []string { return f.selectors }
func (f *fakeSource) Search(_ context.Context, _ Query) (Result, error) {
	f.network = true
	if f.err != nil {
		return Result{}, f.err
	}
	return Result{ID: f.id, Title: f.Title(), Kind: f.Kind(), Items: f.items}, nil
}
func (f *fakeSource) SearchLocal(_ Query) (Result, error) {
	f.local = true
	return Result{ID: f.id, Title: f.Title(), Kind: f.Kind(), Items: f.items}, nil
}

func TestRegistryFiltersBySelector(t *testing.T) {
	pods := &fakeSource{id: "pods", items: []Item{{ID: "p1", Title: "pod"}}}
	repos := &fakeSource{id: "repos", selectors: []string{"git", "repos"}, items: []Item{{ID: "r1", Title: "repo"}}}
	registry := NewRegistry()
	if err := registry.Register(pods); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(repos); err != nil {
		t.Fatal(err)
	}

	response := registry.Search(context.Background(), Query{Raw: "git hop", Limit: 10})
	if len(response.Sections) != 1 || response.Sections[0].ID != "repos" {
		t.Fatalf("sections = %+v", response.Sections)
	}
	if response.Total != 1 {
		t.Fatalf("total = %d", response.Total)
	}
	if pods.network {
		t.Fatal("pods source should not have been queried")
	}

	response = registry.Search(context.Background(), Query{Raw: "hop", Limit: 10})
	if len(response.Sections) != 2 || response.Total != 2 {
		t.Fatalf("sections = %+v", response.Sections)
	}
}

func TestRegistryRestrictsToSources(t *testing.T) {
	pods := &fakeSource{id: "pods", items: []Item{{ID: "p1", Title: "pod"}}}
	repos := &fakeSource{id: "repos", items: []Item{{ID: "r1", Title: "repo"}}}
	registry := NewRegistry()
	_ = registry.Register(pods)
	_ = registry.Register(repos)

	response := registry.Search(context.Background(), Query{Raw: "api", Limit: 10, Sources: []string{"repos"}})
	if len(response.Sections) != 1 || response.Sections[0].ID != "repos" {
		t.Fatalf("sections = %+v", response.Sections)
	}
	if pods.network {
		t.Fatal("pods source should not have been queried")
	}
}

func TestRegistryRestrictedSourceCanReturnEverything(t *testing.T) {
	repos := &fakeSource{id: "repos", items: []Item{{ID: "r1", Title: "repo"}}}
	registry := NewRegistry()
	_ = registry.Register(repos)

	response := registry.Search(context.Background(), Query{Raw: "", Limit: 10, Sources: []string{"repos"}})
	if len(response.Sections) != 1 || response.Total != 1 {
		t.Fatalf("sections = %+v", response.Sections)
	}
}

func TestRegistryLocalAndErrors(t *testing.T) {
	source := &fakeSource{id: "repos", selectors: []string{"git"}, items: []Item{{ID: "r1", Title: "repo"}}}
	registry := NewRegistry()
	_ = registry.Register(source)

	response := registry.Search(context.Background(), Query{Raw: "git", Limit: 10, Local: true})
	if !source.local || source.network {
		t.Fatalf("local=%v network=%v", source.local, source.network)
	}
	if len(response.Sections) != 1 || len(response.Sections[0].Items) != 1 {
		t.Fatalf("response = %+v", response)
	}

	failing := NewRegistry()
	_ = failing.Register(&fakeSource{id: "broken", err: errors.New("boom")})
	response = failing.Search(context.Background(), Query{Raw: "x"})
	if len(response.Errors) != 1 || !strings.Contains(response.Errors[0], "broken: boom") {
		t.Fatalf("errors = %+v", response.Errors)
	}
	if len(response.Sections) != 1 || response.Sections[0].Errors[0] != "boom" {
		t.Fatalf("sections = %+v", response.Sections)
	}
}

func TestRegistryLimitAndDuplicate(t *testing.T) {
	registry := NewRegistry()
	source := &fakeSource{id: "pods", items: []Item{{ID: "1"}, {ID: "2"}, {ID: "3"}}}
	_ = registry.Register(source)
	if err := registry.Register(source); err == nil {
		t.Fatal("expected duplicate source error")
	}
	response := registry.Search(context.Background(), Query{Raw: "x", Limit: 2})
	if len(response.Sections[0].Items) != 2 || response.Total != 2 {
		t.Fatalf("response = %+v", response)
	}
}

func TestQuerySelectors(t *testing.T) {
	query := Query{Raw: "git hop metrics vm:prod"}
	if got := query.WithoutSelectors("git").Terms; strings.Join(got, " ") != "hop metrics vm:prod" {
		t.Fatalf("terms = %v", got)
	}
	if got := StripSelectors("git hop repos", "git", "repos"); got != "hop" {
		t.Fatalf("strip = %q", got)
	}
	if !HasSelector("ticket OPS-1", "ticket", "issue") {
		t.Fatal("expected ticket selector")
	}
	if HasSelector("api prod", "ticket", "issue") {
		t.Fatal("unexpected ticket selector")
	}
}

func TestCommandSource(t *testing.T) {
	source := NewCommandSource(config.PluginConfig{
		ID:      "hello",
		Title:   "Hello",
		Kind:    "hello",
		Command: "sh",
		Args:    []string{"-c", `read -r line; printf '%s' '{"items":[{"id":"a","title":"first"},{"id":"b","title":"second"}],"errors":["warn"]}'`},
	})
	result, err := source.Search(context.Background(), Query{Raw: "x", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Title != "first" {
		t.Fatalf("items = %+v", result.Items)
	}
	if len(result.Errors) != 1 || result.Errors[0] != "warn" {
		t.Fatalf("errors = %+v", result.Errors)
	}
}
