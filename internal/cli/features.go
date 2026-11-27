package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/hop/internal/config"
)

var featureAliases = map[string]string{
	"kubernetes": "pods",
	"k8s":        "pods",
	"cluster":    "pods",
	"clusters":   "pods",
	"jira":       "tickets",
	"atlassian":  "tickets",
}

func canonicalFeature(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if canonical, ok := featureAliases[id]; ok {
		return canonical
	}
	return config.CanonicalSource(id)
}

func requireFeature(cfg *config.Config, id, label string) error {
	if cfg.SourceDisabled(id) {
		return fmt.Errorf("%s is disabled; enable it with: hop features enable %s", label, id)
	}
	return nil
}

func (a *App) newFeaturesCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "features [enable|disable|list] [FEATURE...]",
		Aliases: []string{"feature", "sources"},
		Short:   "List, enable or disable optional features",
		Long: `List, enable or disable optional features without deleting their configuration.

Features: pods (Kubernetes), servers (VMs/SSH), containers, repos (Git),
packages, tickets (Jira/GitLab) and shortcuts.

Examples:
  hop features
  hop features disable pods tickets
  hop features enable pods
  hop features disable jira`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			return writeSourceRows(cmd, cfg, path, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	var listJSON bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List optional features and their enabled state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			return writeSourceRows(cmd, cfg, path, listJSON)
		},
	}
	list.Flags().BoolVar(&listJSON, "json", false, "JSON output")
	cmd.AddCommand(
		list,
		a.newFeaturesToggleCommand(true),
		a.newFeaturesToggleCommand(false),
	)
	return cmd
}

func (a *App) newFeaturesToggleCommand(enabled bool) *cobra.Command {
	verb := "enable"
	if !enabled {
		verb = "disable"
	}
	return &cobra.Command{
		Use:   verb + " FEATURE [FEATURE...]",
		Short: capitalize(verb) + " optional features",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			canonical := make([]string, 0, len(args))
			for _, raw := range args {
				id := canonicalFeature(raw)
				if id == "" {
					return fmt.Errorf("feature id is required")
				}
				canonical = append(canonical, id)
			}
			return a.toggleSources(cmd, canonical, enabled)
		},
	}
}
