package providers

import (
	"fmt"
	"sort"

	"github.com/SlimaineB/hop/internal/config"
)

type Registry struct {
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

func (r *Registry) Register(kind string, factory Factory) {
	if _, exists := r.factories[kind]; exists {
		panic("duplicate provider type: " + kind)
	}
	r.factories[kind] = factory
}

func (r *Registry) Build(id string, cfg config.ProviderConfig) (TicketProvider, error) {
	kind := cfg.Type
	if kind == "" {
		kind = id
	}
	factory, ok := r.factories[kind]
	if !ok {
		return nil, fmt.Errorf("provider %q: unknown type %q", id, kind)
	}
	provider, err := factory(id, cfg)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func (r *Registry) Load(cfg *config.Config) (*Set, error) {
	ids := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	set := &Set{
		providers: make([]TicketProvider, 0, len(ids)),
		byID:      make(map[string]TicketProvider, len(ids)),
	}
	for _, id := range ids {
		provider, err := r.Build(id, cfg.Providers[id])
		if err != nil {
			return nil, err
		}
		set.providers = append(set.providers, provider)
		set.byID[id] = provider
	}
	return set, nil
}
