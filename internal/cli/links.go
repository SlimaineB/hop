package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/tui"
)

func (a *App) newLinkCommand() *cobra.Command {
	var listOnly bool
	var printURL bool
	var asJSON bool
	var useTUI bool
	cmd := &cobra.Command{
		Use:     "link [keywords...]",
		Aliases: []string{"links"},
		Short:   "Open a configured browser shortcut",
		Long: `Open a browser shortcut from the hop config.

Shortcuts are matched by name, keywords, description, and URL. In a terminal,
running without terms opens an interactive list. Use --list to print matching
shortcuts, or --print to output the URL.

TUI keys: enter/o open, y copy the URL, e edit, a add, d delete.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, configPath, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			if err := requireFeature(cfg, "shortcuts", "shortcuts"); err != nil {
				return err
			}
			interactive := term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
			tuiAllowed := !listOnly && !asJSON && !printURL
			if tuiAllowed && (useTUI || (len(args) == 0 && interactive)) {
				return a.runLinkTUI(configPath, cfg.Shortcuts, strings.Join(args, " "))
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
	cmd.Flags().BoolVar(&useTUI, "tui", false, "open the interactive shortcut TUI (default with no terms in a terminal)")
	return cmd
}

func (a *App) runLinkTUI(configPath string, shortcuts []config.ShortcutConfig, query string) error {
	program := tea.NewProgram(
		tui.NewLinks(tui.LinkOptions{
			Shortcuts: shortcuts,
			Initial:   query,
			OpenURL:   actions.OpenBrowser,
			CopyText:  actions.Copy,
			Save: func(shortcut config.ShortcutConfig) error {
				_, err := config.UpsertShortcut(configPath, shortcut)
				return err
			},
			Remove: func(name string) error {
				_, err := config.RemoveShortcut(configPath, name)
				return err
			},
			Reload: func() ([]config.ShortcutConfig, error) {
				cfg, _, err := config.Load(configPath)
				if err != nil {
					return nil, err
				}
				return cfg.Shortcuts, nil
			},
		}),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
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
