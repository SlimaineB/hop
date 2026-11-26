package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/config"
)

func (a *App) newLinkCommand() *cobra.Command {
	var listOnly bool
	var printURL bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "link [keywords...]",
		Aliases: []string{"links"},
		Short:   "Open a configured browser shortcut",
		Long: `Open a browser shortcut from the hop config.

Shortcuts are matched by name, keywords, description, and URL.
Use --list to see available shortcuts, or --print to output the URL.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			if len(cfg.Shortcuts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no shortcuts configured")
				fmt.Fprintln(cmd.OutOrStdout(), "add one with: hop config shortcut add grafana https://grafana.example.com metrics dashboards")
				return nil
			}
			hits := config.MatchShortcuts(cfg.Shortcuts, args)
			if listOnly || len(args) == 0 {
				return printShortcuts(cmd, hits, asJSON)
			}
			if len(hits) == 0 {
				return fmt.Errorf("no shortcut matches %q; run 'hop link --list'", strings.Join(args, " "))
			}
			selected := hits[0]
			if !config.ValidShortcutURL(selected.Shortcut.URL) {
				return fmt.Errorf("invalid shortcut URL for %s: %s", selected.Shortcut.Name, selected.Shortcut.URL)
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"shortcut": selected.Shortcut,
					"score":    selected.Score,
				})
			}
			if printURL {
				fmt.Fprintln(cmd.OutOrStdout(), selected.Shortcut.URL)
				return nil
			}
			if err := actions.OpenBrowser(selected.Shortcut.URL); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "opened %s: %s\n", selected.Shortcut.Name, selected.Shortcut.URL)
			return nil
		},
	}
	cmd.Flags().BoolVar(&listOnly, "list", false, "list matching shortcuts")
	cmd.Flags().BoolVar(&printURL, "print", false, "print the URL instead of opening it")
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return cmd
}

func printShortcuts(cmd *cobra.Command, hits []config.ShortcutHit, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"shortcuts": hits})
	}
	if len(hits) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no shortcuts match")
		return nil
	}
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "NAME\tKEYWORDS\tURL\tDESCRIPTION")
	for _, hit := range hits {
		shortcut := hit.Shortcut
		keywords := strings.Join(shortcut.Keywords, ",")
		if keywords == "" {
			keywords = "-"
		}
		description := shortcut.Description
		if description == "" {
			description = "-"
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", shortcut.Name, keywords, shortcut.URL, description)
	}
	return writer.Flush()
}
