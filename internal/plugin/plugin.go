// Package plugin defines the generic search-source contract used by the hop
// cockpit. Built-in sources (pods, VMs, repos, packages, tickets, shortcuts)
// and external command plugins all implement Source.
package plugin

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SlimaineB/hop/internal/search"
)

// Query is the normalized request passed to every source.
type Query struct {
	Raw            string       `json:"raw"`
	Parsed         search.Query `json:"parsed"`
	Contexts       []string     `json:"contexts,omitempty"`
	Namespaces     []string     `json:"namespaces,omitempty"`
	Env            string       `json:"env,omitempty"`
	Limit          int          `json:"limit"`
	Local          bool         `json:"local,omitempty"`
	ForceRefresh   bool         `json:"force_refresh,omitempty"`
	IncludeNetwork bool         `json:"include_network,omitempty"`
	Sources        []string     `json:"sources,omitempty"`
}

// Item is the generic, renderable unit returned by every source. Built-in
// sources also fill Payload with their typed hit so the UI can render a rich
// row while external plugins only need the generic fields.
type Item struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Subtitle    string            `json:"subtitle,omitempty"`
	Description string            `json:"description,omitempty"`
	Badge       string            `json:"badge,omitempty"`
	URL         string            `json:"url,omitempty"`
	Copy        string            `json:"copy,omitempty"`
	Score       int               `json:"score,omitempty"`
	Percent     int               `json:"percent,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
	Payload     any               `json:"payload,omitempty"`
}

type Result struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Kind   string   `json:"kind"`
	Items  []Item   `json:"items"`
	Total  int      `json:"total"`
	Errors []string `json:"errors,omitempty"`
	Meta   any      `json:"meta,omitempty"`
}

// Source is a searchable section of the cockpit.
type Source interface {
	ID() string
	Title() string
	Kind() string
	// Selectors are query tokens that focus the search on this source (e.g.
	// "vm", "git", "pkg").
	Selectors() []string
	Search(ctx context.Context, query Query) (Result, error)
}

// LocalSource can answer from a warm in-memory snapshot without network I/O.
type LocalSource interface {
	SearchLocal(query Query) (Result, error)
}

type Registry struct {
	mu      sync.RWMutex
	sources []Source
	ids     map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{ids: map[string]bool{}}
}

func (r *Registry) Register(source Source) error {
	if source == nil {
		return nil
	}
	id := strings.TrimSpace(source.ID())
	if id == "" {
		return errText("plugin: source without an ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ids == nil {
		r.ids = map[string]bool{}
	}
	if r.ids[id] {
		return errText("plugin: duplicate source " + id)
	}
	r.ids[id] = true
	r.sources = append(r.sources, source)
	return nil
}

func (r *Registry) Sources() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Source(nil), r.sources...)
}

type Response struct {
	Query    search.Query  `json:"query"`
	Sections []Result      `json:"sections"`
	Errors   []string      `json:"errors,omitempty"`
	Duration time.Duration `json:"duration"`
	Total    int           `json:"total"`
}

func (r *Registry) Search(ctx context.Context, query Query) Response {
	start := time.Now()
	if query.Parsed.Raw == "" {
		query.Parsed = search.Parse(query.Raw)
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	sources := r.active(query.Raw, query.Sources)

	results := make([]Result, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Add(1)
		go func(index int, source Source) {
			defer wg.Done()
			result, err := runSource(ctx, source, query)
			if err != nil {
				result = Result{
					ID:     source.ID(),
					Title:  source.Title(),
					Kind:   source.Kind(),
					Errors: []string{err.Error()},
				}
			}
			if result.ID == "" {
				result.ID = source.ID()
			}
			if result.Title == "" {
				result.Title = source.Title()
			}
			if result.Kind == "" {
				result.Kind = source.Kind()
			}
			if query.Limit > 0 && len(result.Items) > query.Limit {
				result.Items = result.Items[:query.Limit]
			}
			result.Total = len(result.Items)
			results[index] = result
		}(i, source)
	}
	wg.Wait()

	sections := make([]Result, 0, len(results))
	errors := make([]string, 0)
	total := 0
	for _, result := range results {
		if len(result.Items) == 0 && len(result.Errors) == 0 {
			continue
		}
		for _, itemErr := range result.Errors {
			errors = append(errors, result.ID+": "+itemErr)
		}
		sections = append(sections, result)
		total += len(result.Items)
	}
	return Response{
		Query:    query.Parsed,
		Sections: sections,
		Errors:   errors,
		Duration: time.Since(start),
		Total:    total,
	}
}

func runSource(ctx context.Context, source Source, query Query) (Result, error) {
	if query.Local {
		if local, ok := source.(LocalSource); ok {
			return local.SearchLocal(query)
		}
	}
	return source.Search(ctx, query)
}

func (r *Registry) active(raw string, only []string) []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wanted := map[string]bool{}
	for _, id := range only {
		id = strings.ToLower(strings.TrimSpace(id))
		if id != "" {
			wanted[id] = true
		}
	}
	tokens := tokens(raw)
	selected := make([]Source, 0, len(r.sources))
	matched := false
	for _, source := range r.sources {
		if len(wanted) > 0 {
			if wanted[strings.ToLower(source.ID())] {
				selected = append(selected, source)
				matched = true
			}
			continue
		}
		for _, selector := range source.Selectors() {
			if tokens[strings.ToLower(strings.TrimSpace(selector))] {
				selected = append(selected, source)
				matched = true
				break
			}
		}
	}
	if matched || len(wanted) > 0 {
		return selected
	}
	return append([]Source(nil), r.sources...)
}

func tokens(raw string) map[string]bool {
	out := map[string]bool{}
	for _, field := range strings.Fields(raw) {
		token := strings.ToLower(strings.Trim(field, `"'`))
		if token != "" {
			out[token] = true
		}
	}
	return out
}

// WithoutSelectors returns the parsed query with the given selector tokens
// removed from Terms (filters and time range are preserved).
func (q Query) WithoutSelectors(selectors ...string) search.Query {
	parsed := q.Parsed
	if parsed.Raw == "" {
		parsed = search.Parse(q.Raw)
	}
	blocked := map[string]bool{}
	for _, selector := range selectors {
		blocked[strings.ToLower(strings.TrimSpace(selector))] = true
	}
	terms := make([]string, 0, len(parsed.Terms))
	for _, term := range parsed.Terms {
		if blocked[strings.ToLower(strings.Trim(term, `"'`))] {
			continue
		}
		terms = append(terms, term)
	}
	parsed.Terms = terms
	return parsed
}

// HasSelector reports whether raw contains one of the given selector tokens.
func HasSelector(raw string, selectors ...string) bool {
	want := map[string]bool{}
	for _, selector := range selectors {
		selector = strings.ToLower(strings.TrimSpace(selector))
		if selector != "" {
			want[selector] = true
		}
	}
	if len(want) == 0 {
		return false
	}
	for token := range tokens(raw) {
		if want[token] {
			return true
		}
	}
	return false
}

func StripSelectors(raw string, selectors ...string) string {
	blocked := map[string]bool{}
	for _, selector := range selectors {
		blocked[strings.ToLower(strings.TrimSpace(selector))] = true
	}
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if blocked[strings.ToLower(strings.Trim(field, `"'`))] {
			continue
		}
		out = append(out, field)
	}
	return strings.Join(out, " ")
}

// SortItems ranks a generic slice in place.
func SortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
		}
		return items[i].Score > items[j].Score
	})
}

type errText string

func (e errText) Error() string { return string(e) }
