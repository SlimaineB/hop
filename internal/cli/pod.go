package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
)

func (a *App) newPodCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pod [terms...]",
		Aliases: []string{"pods"},
		Short:   "Search Kubernetes pods",
		Long: `Search pods across all Kubernetes contexts.

Without terms, lists all pods. In a terminal this opens the pod TUI; otherwise
it prints a table. Use --json or --plain for non-interactive output.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			if err := requireFeature(cfg, "pods", "Kubernetes pods"); err != nil {
				return err
			}
			applyRuntimeOverrides(cfg, a.flags)
			service, err := app.New(cfg)
			if err != nil {
				return err
			}
			defer service.Close()

			mode := outputTable
			switch {
			case a.flags.json:
				mode = outputJSON
			case a.flags.plain:
				mode = outputPlain
			}
			if mode != outputTable || !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
				return a.printSearch(cmd, service, strings.Join(args, " "), mode)
			}
			return a.runTUI(service, cfg, strings.Join(args, " "))
		},
	}
	addFilterFlags(cmd, a)
	cmd.Flags().BoolVar(&a.flags.json, "json", false, "full JSON output")
	cmd.Flags().BoolVar(&a.flags.plain, "plain", false, "one tab-separated line per pod")
	cmd.Flags().BoolVar(&a.flags.refresh, "refresh", false, "force a refresh of all clusters")
	cmd.Flags().DurationVar(&a.flags.timeout, "timeout", 0, "per-cluster request timeout")
	cmd.Flags().IntVar(&a.flags.concurrency, "concurrency", -1, "max clusters queried in parallel (-1 = config, 0 = all selected)")
	return cmd
}
