package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/containers"
)

func (a *App) loadContainerService() (*containers.Service, error) {
	cfg, _, err := config.Load(a.configPath)
	if err != nil {
		return nil, err
	}
	service := containers.New(cfg)
	if !service.Enabled() {
		return nil, fmt.Errorf("no Docker or Podman engine found; install one or configure containers.engines in %s", a.resolvedConfigPath())
	}
	return service, nil
}

func (a *App) newContainersCommand() *cobra.Command {
	var (
		asJSON  bool
		engine  string
		refresh bool
		running bool
	)
	cmd := &cobra.Command{
		Use:     "containers [terms...]",
		Aliases: []string{"container", "ctr", "docker", "podman"},
		Short:   "Search Docker and Podman containers",
		Long: `Search local Docker and Podman containers. Without terms, lists every
container. Selectors: container, ctr, docker, podman.

Use name:, image:, status:, label:, owner: (compose project) and env:
(engine) filters. Actions are available as subcommands.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.loadContainerService()
			if err != nil {
				return err
			}
			if refresh {
				service.Refresh()
			}
			engine = engineForCommand(cmd, engine)
			query := strings.Join(args, " ")
			hits, warnings, err := service.Search(cmd.Context(), query, 200)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "hop: warning: %s\n", warning)
			}
			if engine != "" {
				filtered := hits[:0]
				for _, hit := range hits {
					if strings.EqualFold(hit.Container.Engine, engine) {
						filtered = append(filtered, hit)
					}
				}
				hits = filtered
			}
			if running {
				filtered := hits[:0]
				for _, hit := range hits {
					if strings.EqualFold(hit.Container.State, "running") {
						filtered = append(filtered, hit)
					}
				}
				hits = filtered
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"containers": hits,
					"engines":    service.Engines(),
					"warnings":   warnings,
				})
			}
			if len(hits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no containers found")
				return nil
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "ENGINE\tNAME\tIMAGE\tSTATE\tSTATUS\tPORTS\tPROJECT")
			for _, hit := range hits {
				item := hit.Container
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					item.Engine, item.Name, dash(item.Image), dash(item.State),
					dash(item.Status), dash(item.Ports), dash(item.Project))
			}
			return writer.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().StringVar(&engine, "engine", "", "filter by engine (docker or podman)")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "refresh the container list")
	cmd.Flags().BoolVar(&running, "running", false, "only running containers")
	cmd.AddCommand(
		a.newContainerLogsCommand(),
		a.newContainerShellCommand(),
		a.newContainerInspectCommand(),
		a.newContainerStatsCommand(),
	)
	return cmd
}

func (a *App) newContainerLogsCommand() *cobra.Command {
	var follow bool
	var tail int
	var engine string
	cmd := &cobra.Command{
		Use:   "logs NAME",
		Short: "Show container logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.loadContainerService()
			if err != nil {
				return err
			}
			item, err := service.Find(cmd.Context(), engineForCommand(cmd, engine), args[0])
			if err != nil {
				return err
			}
			process, err := service.LogsCommand(cmd.Context(), item, follow, tail)
			if err != nil {
				return err
			}
			attachStdio(process)
			return process.Run()
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	cmd.Flags().IntVarP(&tail, "tail", "n", 200, "number of lines to show")
	cmd.Flags().StringVar(&engine, "engine", "", "engine when a name is ambiguous")
	return cmd
}

func (a *App) newContainerShellCommand() *cobra.Command {
	var engine string
	var noTTY bool
	cmd := &cobra.Command{
		Use:   "shell NAME [command...]",
		Short: "Open a shell or exec a command in a container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.loadContainerService()
			if err != nil {
				return err
			}
			item, err := service.Find(cmd.Context(), engineForCommand(cmd, engine), args[0])
			if err != nil {
				return err
			}
			tty := !noTTY && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
			process, err := service.ShellCommand(cmd.Context(), item, tty, args[1:])
			if err != nil {
				return err
			}
			attachStdio(process)
			return process.Run()
		},
	}
	cmd.Flags().StringVar(&engine, "engine", "", "engine when a name is ambiguous")
	cmd.Flags().BoolVar(&noTTY, "no-tty", false, "do not allocate a TTY")
	return cmd
}

func (a *App) newContainerInspectCommand() *cobra.Command {
	var engine string
	cmd := &cobra.Command{
		Use:   "inspect NAME",
		Short: "Print the raw container inspect JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.loadContainerService()
			if err != nil {
				return err
			}
			item, err := service.Find(cmd.Context(), engineForCommand(cmd, engine), args[0])
			if err != nil {
				return err
			}
			data, err := service.Inspect(cmd.Context(), item)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			if err == nil && len(data) > 0 && data[len(data)-1] != '\n' {
				_, err = fmt.Fprintln(cmd.OutOrStdout())
			}
			return err
		},
	}
	cmd.Flags().StringVar(&engine, "engine", "", "engine when a name is ambiguous")
	return cmd
}

func (a *App) newContainerStatsCommand() *cobra.Command {
	var engine string
	var stream bool
	cmd := &cobra.Command{
		Use:   "stats NAME",
		Short: "Show container resource usage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.loadContainerService()
			if err != nil {
				return err
			}
			item, err := service.Find(cmd.Context(), engineForCommand(cmd, engine), args[0])
			if err != nil {
				return err
			}
			process, err := service.StatsCommand(cmd.Context(), item, stream)
			if err != nil {
				return err
			}
			attachStdio(process)
			return process.Run()
		},
	}
	cmd.Flags().StringVar(&engine, "engine", "", "engine when a name is ambiguous")
	cmd.Flags().BoolVar(&stream, "stream", false, "stream stats instead of one snapshot")
	return cmd
}

func engineForCommand(cmd *cobra.Command, explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	name := cmd.CalledAs()
	if name == "docker" || name == "podman" {
		return name
	}
	if parent := cmd.Parent(); parent != nil {
		name = parent.CalledAs()
		if name == "docker" || name == "podman" {
			return name
		}
	}
	return ""
}

func attachStdio(process *exec.Cmd) {
	process.Stdin = os.Stdin
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
}
