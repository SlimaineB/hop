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

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/plugin"
	containerregistry "github.com/SlimaineB/hop/internal/registry"
	"github.com/SlimaineB/hop/internal/repos"
	"github.com/SlimaineB/hop/internal/tui"
)

func (a *App) newAllCommand() *cobra.Command {
	var asJSON bool
	var plain bool
	var live bool
	var tickets bool
	cmd := &cobra.Command{
		Use:     "all [terms...]",
		Aliases: []string{"cockpit", "everything"},
		Short:   "Search every configured source in one cockpit",
		Long: `Search pods, VMs, repositories, packages, tickets, shortcuts and external
plugins at once. Use selectors to focus one source: vm, git, pkg, ticket, link.

In a terminal this opens the unified cockpit TUI with live watch. Otherwise it
prints grouped results.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			applyRuntimeOverrides(cfg, a.flags)
			service, err := app.New(cfg)
			if err != nil {
				return err
			}
			defer service.Close()
			launcher := newWebLauncher(service, cfg, a.resolvedConfigPath())
			defer launcher.Close()

			query := strings.Join(args, " ")
			interactive := term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
			if asJSON || plain || !interactive {
				return a.printCockpit(cmd, launcher.plugins, query, asJSON, plain, tickets)
			}
			return runUnifiedTUI(launcher.plugins, launcher, query, live, tickets)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().BoolVar(&plain, "plain", false, "one tab-separated line per result")
	cmd.Flags().BoolVar(&live, "live", true, "watch local snapshots while the TUI is open")
	cmd.Flags().BoolVar(&tickets, "tickets", false, "include network ticket searches")
	return cmd
}

func (a *App) printCockpit(cmd *cobra.Command, registry *plugin.Registry, query string, asJSON, plain, tickets bool) error {
	if registry == nil || len(registry.Sources()) == 0 {
		return fmt.Errorf("no search sources enabled; inspect with 'hop config sources'")
	}
	request := plugin.Query{
		Raw:            query,
		Limit:          60,
		IncludeNetwork: tickets || plugin.HasSelector(query, "ticket", "tickets", "issue", "issues"),
	}
	response := registry.Search(cmd.Context(), request)
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(response)
	}
	if plain {
		writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "SECTION\tID\tTITLE\tSUBTITLE\tURL\tCOPY")
		for _, section := range response.Sections {
			for _, item := range section.Items {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n",
					section.Kind, item.ID, item.Title, dash(item.Subtitle), dash(item.URL), dash(item.Copy))
			}
		}
		return writer.Flush()
	}
	out := cmd.OutOrStdout()
	if len(response.Sections) == 0 {
		fmt.Fprintln(out, "no results")
	}
	for _, section := range response.Sections {
		fmt.Fprintf(out, "\n%s (%d)\n", section.Title, len(section.Items))
		writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "  NAME\tDETAIL\tPROVIDER\tURL")
		for _, item := range section.Items {
			detail := item.Subtitle
			if detail == "" {
				detail = item.Description
			}
			fmt.Fprintf(writer, "  %s\t%s\t%s\t%s\n", item.Title, dash(detail), dash(item.Badge), dash(item.URL))
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		for _, itemErr := range section.Errors {
			fmt.Fprintf(out, "  warning: %s\n", itemErr)
		}
	}
	if len(response.Errors) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "hop: %d source warning(s)\n", len(response.Errors))
	}
	return nil
}

func runUnifiedTUI(registry *plugin.Registry, launcher *webLauncher, query string, live, tickets bool) error {
	if registry == nil || len(registry.Sources()) == 0 {
		return fmt.Errorf("no search sources enabled; inspect with 'hop config sources'")
	}
	defer launcher.Close()
	program := tea.NewProgram(
		tui.NewUnified(tui.UnifiedOptions{
			Registry: registry,
			Initial:  query,
			Limit:    80,
			Live:     live,
			Tickets:  tickets,
			OpenWeb:  launcher.OpenQuery,
		}),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}

func (a *App) newGitCommand() *cobra.Command {
	var asJSON bool
	var refresh bool
	var links bool
	cmd := &cobra.Command{
		Use:     "git [terms...]",
		Aliases: []string{"repos", "repo"},
		Short:   "Search local Git repositories",
		Long: `Search local Git repositories discovered under the configured roots.
GitHub and GitLab remotes get branch, commit and merge-request links.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			if err := requireFeature(cfg, "repos", "Git repositories"); err != nil {
				return err
			}
			service := repos.New(cfg)
			if !service.Enabled() {
				return fmt.Errorf("no Git roots configured; add git.roots or git.repos to %s", a.resolvedConfigPath())
			}
			query := strings.Join(args, " ")
			if refresh {
				if _, err := service.Refresh(cmd.Context()); err != nil {
					return err
				}
			}
			hits, err := service.Search(cmd.Context(), query, 200)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"repos": hits})
			}
			out := cmd.OutOrStdout()
			if len(hits) == 0 {
				fmt.Fprintln(out, "no repositories found")
				return nil
			}
			writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "NAME\tBRANCH\tSTATE\tCOMMIT\tSUBJECT\tPROVIDER\tPATH")
			for _, hit := range hits {
				state := "clean"
				if hit.Repo.Dirty {
					state = fmt.Sprintf("%d changed", hit.Repo.Changed)
				}
				if hit.Repo.Ahead > 0 {
					state += fmt.Sprintf(" +%d", hit.Repo.Ahead)
				}
				if hit.Repo.Behind > 0 {
					state += fmt.Sprintf(" -%d", hit.Repo.Behind)
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					hit.Repo.Name, dash(hit.Repo.Branch), state, dash(hit.Repo.CommitShort),
					dash(hit.Repo.Subject), dash(hit.Repo.Provider), hit.Repo.Path)
			}
			if err := writer.Flush(); err != nil {
				return err
			}
			if links {
				for _, hit := range hits {
					fmt.Fprintf(out, "\n%s\n", hit.Repo.Path)
					fmt.Fprintf(out, "  repo:   %s\n", dash(hit.Repo.Links.Repo))
					fmt.Fprintf(out, "  branch: %s\n", dash(hit.Repo.Links.Branch))
					fmt.Fprintf(out, "  commit: %s\n", dash(hit.Repo.Links.Commit))
					fmt.Fprintf(out, "  MRs:    %s\n", dash(hit.Repo.Links.MergeRequests))
					fmt.Fprintf(out, "  new MR: %s\n", dash(hit.Repo.Links.NewMergeRequest))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "rescan Git roots before searching")
	cmd.Flags().BoolVar(&links, "links", false, "print repository, branch, commit and MR links")
	return cmd
}

func (a *App) newPkgCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "pkg [terms...]",
		Aliases: []string{"packages", "registry"},
		Short:   "Search container images and packages",
		Long: `Search container images in GHCR, GitLab and custom registries, plus images
detected in your pods. Tags are fetched on demand.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			if err := requireFeature(cfg, "packages", "packages"); err != nil {
				return err
			}
			service := containerregistry.New(cfg)
			query := strings.Join(args, " ")
			hits, warnings, err := service.Search(cmd.Context(), query, 100)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "hop: warning: %s\n", warning)
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"packages": hits,
					"warnings": warnings,
				})
			}
			if len(hits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no packages found")
				return nil
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "IMAGE\tREGISTRY\tPROVIDER\tTAGS\tPULL")
			for _, hit := range hits {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
					hit.Image.Repository, hit.Image.Registry, dash(hit.Image.Provider),
					dash(strings.Join(hit.Image.Tags, ",")), dash(hit.Image.Pull))
			}
			return writer.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return cmd
}
