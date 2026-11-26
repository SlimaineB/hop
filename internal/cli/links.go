package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/config"
)

type shortcutHit struct {
	Shortcut config.ShortcutConfig
	Score    int
}

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
			hits := matchShortcuts(cfg.Shortcuts, args)
			if listOnly || len(args) == 0 {
				return printShortcuts(cmd, hits, asJSON)
			}
			if len(hits) == 0 {
				return fmt.Errorf("no shortcut matches %q; run 'hop link --list'", strings.Join(args, " "))
			}
			selected := hits[0]
			if !validShortcutURL(selected.Shortcut.URL) {
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

func matchShortcuts(shortcuts []config.ShortcutConfig, terms []string) []shortcutHit {
	if len(shortcuts) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term != "" {
			normalized = append(normalized, term)
		}
	}
	hits := make([]shortcutHit, 0, len(shortcuts))
	for _, shortcut := range shortcuts {
		score, ok := scoreShortcut(shortcut, normalized)
		if !ok {
			continue
		}
		hits = append(hits, shortcutHit{Shortcut: shortcut, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return strings.ToLower(hits[i].Shortcut.Name) < strings.ToLower(hits[j].Shortcut.Name)
		}
		return hits[i].Score > hits[j].Score
	})
	return hits
}

func scoreShortcut(shortcut config.ShortcutConfig, terms []string) (int, bool) {
	name := strings.ToLower(shortcut.Name)
	description := strings.ToLower(shortcut.Description)
	rawURL := strings.ToLower(shortcut.URL)
	keywords := make([]string, 0, len(shortcut.Keywords))
	for _, keyword := range shortcut.Keywords {
		keywords = append(keywords, strings.ToLower(keyword))
	}
	score := 0
	for _, term := range terms {
		best := 0
		switch {
		case name == term:
			best = 100
		case keywordEquals(keywords, term):
			best = 90
		case strings.HasPrefix(name, term):
			best = 80
		case keywordPrefix(keywords, term):
			best = 70
		case strings.Contains(name, term):
			best = 60
		case keywordContains(keywords, term):
			best = 50
		case strings.Contains(description, term):
			best = 30
		case strings.Contains(rawURL, term):
			best = 20
		}
		if best == 0 {
			return 0, false
		}
		score += best
	}
	return score, true
}

func keywordEquals(keywords []string, value string) bool {
	for _, keyword := range keywords {
		if keyword == value {
			return true
		}
	}
	return false
}

func keywordPrefix(keywords []string, value string) bool {
	for _, keyword := range keywords {
		if strings.HasPrefix(keyword, value) {
			return true
		}
	}
	return false
}

func keywordContains(keywords []string, value string) bool {
	for _, keyword := range keywords {
		if strings.Contains(keyword, value) {
			return true
		}
	}
	return false
}

func validShortcutURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func printShortcuts(cmd *cobra.Command, hits []shortcutHit, asJSON bool) error {
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
