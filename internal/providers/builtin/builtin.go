package builtin

import (
	"github.com/SlimaineB/teu/internal/providers"
	"github.com/SlimaineB/teu/internal/providers/jira"
)

func Registry() *providers.Registry {
	registry := providers.NewRegistry()
	registry.Register("jira", jira.New)
	return registry
}
