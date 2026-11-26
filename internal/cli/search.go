package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/search"
)

func (a *App) newSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "search [terms...]",
		Aliases: []string{"find"},
		Short:   "Non-interactive search (table, JSON, or plain)",
		Long: `Search pods without a UI.

Term order does not matter. Time tokens restrict Grafana/Loki/Mimir links
and log commands:
  12h 14h, -2h, from:2026-11-25T12:00 to:2026-11-25T14:00`,
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

			mode := outputTable
			switch {
			case a.flags.json:
				mode = outputJSON
			case a.flags.plain:
				mode = outputPlain
			}
			return a.printSearch(cmd, service, strings.Join(args, " "), mode)
		},
	}
	addFilterFlags(cmd, a)
	cmd.Flags().BoolVar(&a.flags.json, "json", false, "full JSON output")
	cmd.Flags().BoolVar(&a.flags.plain, "plain", false, "one tab-separated line per pod")
	cmd.Flags().BoolVar(&a.flags.refresh, "refresh", false, "force a refresh of all clusters")
	cmd.Flags().DurationVar(&a.flags.timeout, "timeout", 0, "per-cluster request timeout")
	cmd.Flags().IntVar(&a.flags.concurrency, "concurrency", 0, "number of clusters queried in parallel")
	return cmd
}

func (a *App) printSearch(cmd *cobra.Command, service *app.Service, query string, mode outputMode) error {
	result, err := service.Search(cmd.Context(), app.SearchRequest{
		Query:        query,
		Contexts:     splitList(a.flags.contexts),
		Namespaces:   splitList(a.flags.namespaces),
		Env:          a.flags.env,
		Limit:        a.flags.limit,
		ForceRefresh: a.flags.refresh,
	})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	if label := timeRangeLabel(result.Query.TimeRange); label != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "logs/metrics range: %s\n", label)
	}

	switch mode {
	case outputJSON:
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	case outputPlain:
		for _, hit := range result.Hits {
			env := hit.Pod.Env
			if env == "" {
				env = "-"
			}
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%d%%\n",
				hit.Pod.Name, hit.Pod.Namespace, hit.Pod.Context, env, hit.Percent())
		}
		if len(result.Hits) == 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "no pods found")
		}
		return nil
	default:
		if len(result.Hits) == 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "no pods found")
			return nil
		}
		writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "SCORE\tPOD\tNAMESPACE\tCONTEXTE\tENV\tSTATUT\tAGE\tNODE")
		for _, hit := range result.Hits {
			pod := hit.Pod
			status := pod.Phase
			if pod.Reason != "" {
				status = pod.Reason
			}
			env := pod.Env
			if env == "" {
				env = "-"
			}
			node := pod.Node
			if node == "" {
				node = "-"
			}
			fmt.Fprintf(writer, "%d%%\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				hit.Percent(), pod.Name, pod.Namespace, pod.Context, env, status, shortAge(pod.Age()), node)
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%d result(s) · %d eligible pod(s) · %d total\n",
			result.Matched, result.Eligible, result.Total)
		return nil
	}
}

func timeRangeLabel(rng *search.TimeRange) string {
	if rng == nil || rng.From.IsZero() || rng.To.IsZero() {
		return ""
	}
	from := rng.From.Local()
	to := rng.To.Local()
	if from.Year() == to.Year() && from.YearDay() == to.YearDay() {
		return from.Format("15:04") + " → " + to.Format("15:04")
	}
	return from.Format("02/01 15:04") + " → " + to.Format("02/01 15:04")
}

func shortAge(value time.Duration) string {
	if value <= 0 {
		return "-"
	}
	switch {
	case value < time.Minute:
		return fmt.Sprintf("%ds", int(value.Seconds()))
	case value < time.Hour:
		return fmt.Sprintf("%dm", int(value.Minutes()))
	case value < 24*time.Hour:
		return fmt.Sprintf("%dh", int(value.Hours()))
	default:
		return fmt.Sprintf("%dd", int(value.Hours()/24))
	}
}
