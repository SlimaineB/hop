package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/plugins"
)

type sourceRow struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Kind      string   `json:"kind"`
	Selectors []string `json:"selectors,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	Enabled   bool     `json:"enabled"`
	Builtin   bool     `json:"builtin"`
}

func (a *App) newConfigSourcesCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "sources",
		Aliases: []string{"plugins", "options", "status"},
		Short:   "List search sources and their enabled state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			rows := sourceRows(cfg)
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"config":   path,
					"sources":  rows,
					"disabled": cfg.Sources.Disabled,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config: %s\n\n", path)
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "SOURCE\tSTATUS\tSELECTORS\tDETAILS")
			for _, row := range rows {
				status := "on"
				if !row.Enabled {
					status = "off"
				}
				selectors := "-"
				if len(row.Selectors) > 0 {
					selectors = strings.Join(row.Selectors, ",")
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", row.ID, status, selectors, row.Detail)
			}
			if err := writer.Flush(); err != nil {
				return err
			}
			if len(cfg.Sources.Disabled) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\ndisabled: %s\n", strings.Join(cfg.Sources.Disabled, ", "))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "\ntoggle with: hop config enable SOURCE | hop config disable SOURCE")
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return cmd
}

func (a *App) newConfigToggleCommand(enabled bool) *cobra.Command {
	verb := "enable"
	past := "enabled"
	if !enabled {
		verb = "disable"
		past = "disabled"
	}
	return &cobra.Command{
		Use:   verb + " SOURCE [SOURCE...]",
		Short: capitalize(verb) + " search sources or external plugins",
		Long: capitalize(verb) + ` search sources without deleting their configuration.

Examples:
  hop config enable pod
  hop config disable tickets
  hop config enable git pkg
  hop config disable my-external-plugin`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			known := knownSources(cfg)
			for _, raw := range args {
				id := config.CanonicalSource(raw)
				if id == "" {
					return fmt.Errorf("source id is required")
				}
				if !known[id] {
					return fmt.Errorf("unknown source %q; known: %s", raw, strings.Join(sortedSet(known), ", "))
				}
				if _, err := config.UpdateSourceEnabled(path, id, enabled); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "source %s %s\n", id, past)
			}
			return nil
		},
	}
}

func sourceRows(cfg *config.Config) []sourceRow {
	rows := make([]sourceRow, 0, len(plugins.Catalog())+len(cfg.Plugins))
	for _, info := range plugins.Catalog() {
		rows = append(rows, sourceRow{
			ID:        info.ID,
			Title:     info.Title,
			Kind:      info.Kind,
			Selectors: info.Selectors,
			Detail:    info.Description,
			Enabled:   cfg.SourceEnabled(info.ID),
			Builtin:   true,
		})
	}
	for _, external := range cfg.Plugins {
		id := config.CanonicalSource(external.ID)
		detail := "command: " + external.Command
		if len(external.Args) > 0 {
			detail += " " + strings.Join(external.Args, " ")
		}
		rows = append(rows, sourceRow{
			ID:        id,
			Title:     external.Title,
			Kind:      external.Kind,
			Selectors: external.Selectors,
			Detail:    detail,
			Enabled:   cfg.SourceEnabled(id),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Builtin != rows[j].Builtin {
			return rows[i].Builtin
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

func knownSources(cfg *config.Config) map[string]bool {
	known := map[string]bool{}
	for _, info := range plugins.Catalog() {
		known[config.CanonicalSource(info.ID)] = true
	}
	for _, external := range cfg.Plugins {
		known[config.CanonicalSource(external.ID)] = true
	}
	return known
}

func capitalize(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func sortedSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
