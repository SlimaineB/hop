package providers

import (
	"fmt"
	"strings"
)

type Set struct {
	providers []TicketProvider
	byID      map[string]TicketProvider
}

func (s *Set) All() []TicketProvider {
	return append([]TicketProvider(nil), s.providers...)
}

func (s *Set) Get(id string) (TicketProvider, bool) {
	provider, ok := s.byID[strings.ToLower(strings.TrimSpace(id))]
	return provider, ok
}

func (s *Set) IDs() []string {
	ids := make([]string, 0, len(s.byID))
	for id := range s.byID {
		ids = append(ids, id)
	}
	return ids
}

func (s *Set) DetectKey(key string) []TicketProvider {
	var matches []TicketProvider
	for _, provider := range s.providers {
		if provider.IsKey(key) {
			matches = append(matches, provider)
		}
	}
	return matches
}

func (s *Set) Default(defaultID string) (TicketProvider, error) {
	if defaultID != "" {
		provider, ok := s.Get(defaultID)
		if !ok {
			return nil, fmt.Errorf("default provider %q is not configured", defaultID)
		}
		return provider, nil
	}
	switch len(s.providers) {
	case 0:
		return nil, fmt.Errorf("no issue providers configured")
	case 1:
		return s.providers[0], nil
	default:
		return nil, fmt.Errorf("multiple issue providers configured; set issues.default_provider")
	}
}
