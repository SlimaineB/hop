package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/diag"
)

func (a *App) newDiagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diag",
		Short: "Diagnose pods, VMs and applications",
		Long: `Run local diagnostics on a pod, a VM/server, or configured applications.

Diagnostics are deterministic checks over the data hop already collects. App
probes run in parallel: HTTP health, TCP ports, log patterns and local commands.
They do not call an external AI service.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(a.newDiagPodCommand(), a.newDiagVMCommand(), a.newDiagAppCommand(), a.newDiagAppsCommand())
	return cmd
}

func (a *App) newDiagPodCommand() *cobra.Command {
	var asJSON bool
	var first bool
	cmd := &cobra.Command{
		Use:   "pod [terms...]",
		Short: "Diagnose a Kubernetes pod",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			if err := requireFeature(cfg, "pods", "Kubernetes pods"); err != nil {
				return err
			}
			applyRuntimeOverrides(cfg, a.flags)
			if _, err := a.resolveEnv(cfg, strings.Join(args, " "), false); err != nil {
				return err
			}
			engine, err := newDiagEngine(cfg)
			if err != nil {
				return err
			}
			service, err := app.New(cfg)
			if err != nil {
				return err
			}
			defer service.Close()

			query := strings.Join(args, " ")
			result, err := service.Search(cmd.Context(), app.SearchRequest{
				Query:        query,
				Contexts:     splitList(a.flags.contexts),
				Namespaces:   splitList(a.flags.namespaces),
				Env:          a.flags.env,
				Limit:        20,
				ForceRefresh: a.flags.refresh,
			})
			if err != nil {
				return err
			}
			if len(result.Hits) == 0 {
				return fmt.Errorf("no pod found for %q", query)
			}
			hit, err := selectPodHit(result.Hits, query, first)
			if err != nil {
				printPodCandidates(cmd, result.Hits)
				return err
			}
			ref := hit.Pod.Ref()
			ctx := cmd.Context()
			detail, err := service.Detail(ctx, ref, result.Query.TimeRange)
			if err != nil {
				return err
			}
			statsCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			stats, statsErr := service.PodStats(statsCtx, ref)
			if statsErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "pod metrics unavailable: %v\n", statsErr)
			}
			report := engine.DiagnosePod(ctx, detail.Pod, stats)
			return writeDiagReport(cmd, report, asJSON)
		},
	}
	addFilterFlags(cmd, a)
	cmd.Flags().BoolVar(&a.flags.refresh, "refresh", false, "force a refresh of all clusters")
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().BoolVar(&first, "first", false, "diagnose the top match when several pods match")
	return cmd
}

func (a *App) newDiagVMCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "vm NAME",
		Aliases: []string{"server"},
		Short:   "Diagnose a VM/server",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, cfg, err := a.loadServerService()
			if err != nil {
				return err
			}
			server, err := service.Find(args[0])
			if err != nil {
				return err
			}
			engine, err := newDiagEngine(cfg)
			if err != nil {
				return err
			}
			result := service.CheckServer(cmd.Context(), *server)
			report := engine.DiagnoseServer(cmd.Context(), *server, result, service)
			return writeDiagReport(cmd, report, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return cmd
}

func (a *App) newDiagAppCommand() *cobra.Command {
	var asJSON bool
	var useTUI bool
	cmd := &cobra.Command{
		Use:   "app [NAME]",
		Short: "Diagnose one configured application",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			if len(cfg.Apps) == 0 {
				return fmt.Errorf("no applications configured; add an apps: section to %s", a.resolvedConfigPath())
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			selected, err := selectApp(cfg.Apps, name)
			if err != nil {
				return err
			}
			engine, err := newDiagEngine(cfg)
			if err != nil {
				return err
			}
			if useTUI {
				return runDiagAppsTUI(engine, []config.AppConfig{selected})
			}
			report := engine.DiagnoseApp(cmd.Context(), selected)
			return writeDiagReport(cmd, report, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().BoolVar(&useTUI, "tui", false, "open the live diagnostics TUI for this app")
	return cmd
}

func (a *App) newDiagAppsCommand() *cobra.Command {
	var asJSON bool
	var useTUI bool
	var table bool
	cmd := &cobra.Command{
		Use:     "apps",
		Aliases: []string{"applications"},
		Short:   "Run every configured application probe",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			if len(cfg.Apps) == 0 {
				return fmt.Errorf("no applications configured; add an apps: section to %s", a.resolvedConfigPath())
			}
			engine, err := newDiagEngine(cfg)
			if err != nil {
				return err
			}
			if !asJSON && (useTUI || (!table && term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd())))) {
				return runDiagAppsTUI(engine, cfg.Apps)
			}
			start := time.Now()
			reports := engine.DiagnoseApps(cmd.Context(), cfg.Apps)
			return writeAppReports(cmd, reports, asJSON, time.Since(start))
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().BoolVar(&useTUI, "tui", false, "open the live diagnostics TUI (default in a terminal)")
	cmd.Flags().BoolVar(&table, "table", false, "print a table instead of opening the TUI")
	return cmd
}

func selectApp(apps []config.AppConfig, name string) (config.AppConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		if len(apps) == 1 {
			return apps[0], nil
		}
		names := make([]string, 0, len(apps))
		for _, app := range apps {
			names = append(names, app.Name)
		}
		return config.AppConfig{}, fmt.Errorf("several applications configured (%s); specify a name", strings.Join(names, ", "))
	}
	for _, app := range apps {
		if strings.EqualFold(app.Name, name) {
			return app, nil
		}
	}
	names := make([]string, 0, len(apps))
	for _, app := range apps {
		names = append(names, app.Name)
	}
	return config.AppConfig{}, fmt.Errorf("unknown application %q; configured: %s", name, strings.Join(names, ", "))
}

func writeAppReports(cmd *cobra.Command, reports []*diag.Report, asJSON bool, duration time.Duration) error {
	out := cmd.OutOrStdout()
	sort.SliceStable(reports, func(i, j int) bool {
		left, right := appVerdictRank(reports[i].Verdict), appVerdictRank(reports[j].Verdict)
		if left != right {
			return left < right
		}
		return reports[i].Target < reports[j].Target
	})
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]any{
			"apps":       reports,
			"checked_at": time.Now(),
			"duration":   duration,
		})
	}
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "VERDICT\tAPP\tENV\tPROBES\tFINDINGS\tSUMMARY")
	for _, report := range reports {
		okProbes := 0
		for _, probe := range report.Probes {
			if probe.OK {
				okProbes++
			}
		}
		probes := fmt.Sprintf("%d/%d", okProbes, len(report.Probes))
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%s\n", strings.ToUpper(string(report.Verdict)), report.Target, appEnv(report), probes, len(report.Findings), report.Summary)
	}
	_ = writer.Flush()
	fmt.Fprintf(out, "\n%d applications checked in %s\n", len(reports), duration.Round(time.Millisecond))
	return nil
}

func appEnv(report *diag.Report) string {
	return report.Env
}

func appVerdictRank(severity diag.Severity) int {
	switch severity {
	case diag.SeverityCritical:
		return 0
	case diag.SeverityWarning:
		return 1
	default:
		return 2
	}
}

func selectPodHit(hits []app.HitView, query string, first bool) (*app.HitView, error) {
	if len(hits) == 1 {
		return &hits[0], nil
	}
	if first {
		return &hits[0], nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	matches := make([]int, 0, 1)
	for i := range hits {
		if strings.ToLower(hits[i].Pod.Name) == query {
			matches = append(matches, i)
		}
	}
	if len(matches) == 1 {
		return &hits[matches[0]], nil
	}
	return nil, fmt.Errorf("%d pods match; refine by namespace, context or exact name (or use --first)", len(hits))
}

func printPodCandidates(cmd *cobra.Command, hits []app.HitView) {
	limit := len(hits)
	if limit > 5 {
		limit = 5
	}
	for _, hit := range hits[:limit] {
		fmt.Fprintf(cmd.ErrOrStderr(), "  %s/%s/%s\n", hit.Pod.Context, hit.Pod.Namespace, hit.Pod.Name)
	}
	if len(hits) > limit {
		fmt.Fprintf(cmd.ErrOrStderr(), "  ... and %d more\n", len(hits)-limit)
	}
}

func writeDiagReport(cmd *cobra.Command, report *diag.Report, asJSON bool) error {
	out := cmd.OutOrStdout()
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Fprintf(out, "%s %s\n", strings.ToUpper(report.Kind), report.Target)
	fmt.Fprintf(out, "verdict: %s\n", report.Verdict)
	fmt.Fprintf(out, "summary: %s\n", report.Summary)
	if len(report.Probes) > 0 {
		okCount := 0
		for _, probe := range report.Probes {
			if probe.OK {
				okCount++
			}
		}
		fmt.Fprintf(out, "probes: %d/%d ok in %s\n", okCount, len(report.Probes), report.Duration.Round(time.Millisecond))
		for _, probe := range report.Probes {
			status := "ok"
			if !probe.OK {
				status = string(probe.Severity)
			}
			line := fmt.Sprintf("  [%s] %s: %s", status, probe.Title, probe.Summary)
			if probe.Error != "" && probe.Error != probe.Summary {
				line += " — " + probe.Error
			}
			fmt.Fprintln(out, line)
		}
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(out, "no findings")
		return nil
	}
	for _, finding := range report.Findings {
		fmt.Fprintf(out, "\n- [%s] %s\n", finding.Severity, finding.Title)
		if finding.Detail != "" {
			fmt.Fprintf(out, "  %s\n", finding.Detail)
		}
		if len(finding.Evidence) > 0 {
			fmt.Fprintln(out, "  evidence:")
			for _, evidence := range finding.Evidence {
				fmt.Fprintf(out, "    - %s\n", evidence)
			}
		}
		if finding.Remedy != "" {
			fmt.Fprintf(out, "  next: %s\n", finding.Remedy)
		}
	}
	return nil
}
