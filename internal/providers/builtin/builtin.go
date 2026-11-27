package builtin

import (
	"github.com/SlimaineB/hop/internal/providers"
	"github.com/SlimaineB/hop/internal/providers/gitlab"
	"github.com/SlimaineB/hop/internal/providers/jira"
)

func Registry() *providers.Registry {
	registry := providers.NewRegistry()
	registry.Register("jira", jira.New)
	registry.Register("gitlab", gitlab.New)
	return registry
}
