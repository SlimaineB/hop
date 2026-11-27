package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/servers"
	"github.com/SlimaineB/hop/internal/tui"
)

func (a *App) loadServerService() (*servers.Service, *config.Config, error) {
	cfg, _, err := config.Load(a.configPath)
	if err != nil {
		return nil, nil, err
	}
	if err := requireFeature(cfg, "servers", "VMs/servers"); err != nil {
		return nil, nil, err
	}
	return servers.New(cfg), cfg, nil
}

func (a *App) newServersCommand() *cobra.Command {
	var asJSON bool
	var check bool
	var useTUI bool
	var table bool
	cmd := &cobra.Command{
		Use:     "vm [tags...]",
		Aliases: []string{"servers", "vms"},
		Short:   "Search VMs and SSH servers",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			service, cfg, err := a.loadServerService()
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			if table || asJSON || check {
				return a.printServerSearch(cmd, service, query, asJSON, check)
			}
			if useTUI || (term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))) {
				return a.runServerTUI(service, cfg, query)
			}
			return a.printServerSearch(cmd, service, query, false, false)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().BoolVar(&check, "check", false, "run HTTP, network and SSH checks in a table")
	cmd.Flags().BoolVar(&useTUI, "tui", false, "open the interactive server TUI (default in a terminal)")
	cmd.Flags().BoolVar(&table, "table", false, "print a table instead of opening the TUI")
	return cmd
}

func (a *App) printServerSearch(cmd *cobra.Command, service *servers.Service, query string, asJSON, check bool) error {
	hits, err := service.Search(query, 200)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"servers": hits})
	}
	if len(hits) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no servers found")
		return nil
	}
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	if check {
		fmt.Fprintln(writer, "NAME\tENV\tSSH\tHOST\tTAGS\tMETRICS\tHEALTH\tMACHINE")
	} else {
		fmt.Fprintln(writer, "NAME\tENV\tSSH\tHOST\tTAGS\tSOURCE")
	}
	for _, hit := range hits {
		server := hit.Server
		tags := strings.Join(server.Tags, ",")
		if tags == "" {
			tags = "-"
		}
		if check {
			result := service.CheckServer(context.Background(), server)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				server.Name, dash(server.Env), dash(server.SSH), server.Host, tags,
				httpStatus(result.Metrics), httpStatus(result.Health), machineStatus(result.Machine))
			continue
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n",
			server.Name, dash(server.Env), dash(server.SSH), server.Host, tags, dash(server.Source))
	}
	return writer.Flush()
}

func (a *App) newServerCommand() *cobra.Command {
	var asJSON bool
	var check bool
	cmd := &cobra.Command{
		Use:   "server NAME",
		Short: "Show one VM/server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, _, err := a.loadServerService()
			if err != nil {
				return err
			}
			server, err := service.Find(args[0])
			if err != nil {
				return err
			}
			command, _ := service.SSHCommand(server.Name)
			var result *servers.Check
			if check {
				value := service.CheckServer(cmd.Context(), *server)
				result = &value
			}
			if asJSON {
				payload := map[string]any{"server": server, "ssh_command": command}
				if result != nil {
					payload["check"] = result
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(payload)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "name: %s\n", server.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "env: %s\n", dash(server.Env))
			fmt.Fprintf(cmd.OutOrStdout(), "ssh: %s\n", dash(server.SSH))
			fmt.Fprintf(cmd.OutOrStdout(), "host: %s\n", dash(server.Host))
			fmt.Fprintf(cmd.OutOrStdout(), "user: %s\n", dash(server.User))
			fmt.Fprintf(cmd.OutOrStdout(), "port: %d\n", server.Port)
			fmt.Fprintf(cmd.OutOrStdout(), "tags: %s\n", dash(strings.Join(server.Tags, ",")))
			fmt.Fprintf(cmd.OutOrStdout(), "metrics: %s\n", dash(server.MetricsURL))
			fmt.Fprintf(cmd.OutOrStdout(), "health: %s\n", dash(server.HealthURL))
			fmt.Fprintf(cmd.OutOrStdout(), "network checks: %t · ports: %s\n", server.NetworkChecks, portList(server.CheckPorts))
			fmt.Fprintf(cmd.OutOrStdout(), "ssh command: %s\n", command)
			if result != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "metrics: %s\n", httpStatus(result.Metrics))
				fmt.Fprintf(cmd.OutOrStdout(), "health: %s\n", httpStatus(result.Health))
				printMachine(cmd, result.Machine)
				printNetwork(cmd, result.Network)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().BoolVar(&check, "check", false, "run HTTP, network and SSH checks")
	return cmd
}

func (a *App) newSSHCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh NAME [command...]",
		Short: "Open an SSH session to a VM/server",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, _, err := a.loadServerService()
			if err != nil {
				return err
			}
			sshArgs, err := service.SSHArgs(args[0])
			if err != nil {
				return err
			}
			if len(args) > 1 {
				sshArgs = append(sshArgs, args[1:]...)
			}
			process := exec.CommandContext(cmd.Context(), "ssh", sshArgs...)
			process.Stdin = os.Stdin
			process.Stdout = os.Stdout
			process.Stderr = os.Stderr
			return process.Run()
		},
	}
}

func (a *App) runServerTUI(service *servers.Service, cfg *config.Config, query string) error {
	return runServerTUI(service, newWebLauncher(nil, cfg), query)
}

func runServerTUI(service *servers.Service, launcher *webLauncher, query string) error {
	defer launcher.Close()
	engine, diagErr := newDiagEngine(launcher.cfg)
	if diagErr != nil {
		return diagErr
	}
	program := tea.NewProgram(
		tui.NewServers(tui.ServerOptions{
			Service:     service,
			Initial:     query,
			OpenWeb:     launcher.OpenServer,
			Diagnostics: engine,
		}),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}

func printMachine(cmd *cobra.Command, info servers.MachineInfo) {
	if info.Error != "" && !info.OK {
		fmt.Fprintf(cmd.OutOrStdout(), "machine: error: %s\n", info.Error)
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "machine: %s %s · kernel %s\n", dash(info.OS), dash(info.Hostname), dash(info.Kernel))
	fmt.Fprintf(cmd.OutOrStdout(), "uptime: %s\n", dash(info.Uptime))
	fmt.Fprintf(cmd.OutOrStdout(), "load: %s · cpus: %d\n", dash(info.Load), info.CPUs)
	fmt.Fprintf(cmd.OutOrStdout(), "memory: %s / %s\n", servers.FormatBytes(info.MemoryUsed), servers.FormatBytes(info.MemoryTotal))
	fmt.Fprintf(cmd.OutOrStdout(), "disk: %s / %s (%s%%)\n", servers.FormatBytes(info.DiskUsed), servers.FormatBytes(info.DiskTotal), dash(info.DiskPercent))
	if len(info.FailedUnits) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "failed units: %s\n", strings.Join(info.FailedUnits, ", "))
	}
}

func printNetwork(cmd *cobra.Command, network servers.NetworkInfo) {
	if !network.Checked {
		fmt.Fprintln(cmd.OutOrStdout(), "network: disabled")
		return
	}
	switch {
	case network.Ping.OK:
		fmt.Fprintf(cmd.OutOrStdout(), "network: %s · ping %dms\n", dash(network.Target), network.Ping.LatencyMS)
	case network.Ping.Error != "":
		fmt.Fprintf(cmd.OutOrStdout(), "network: %s · ping %s\n", dash(network.Target), network.Ping.Error)
	default:
		fmt.Fprintf(cmd.OutOrStdout(), "network: %s · no ping response\n", dash(network.Target))
	}
	for _, port := range network.Ports {
		state := "closed"
		if port.Open {
			state = "open"
		} else if port.State != "" {
			state = port.State
		}
		line := fmt.Sprintf("port %d: %s", port.Port, state)
		if port.LatencyMS > 0 {
			line += fmt.Sprintf(" · %dms", port.LatencyMS)
		}
		fmt.Fprintln(cmd.OutOrStdout(), line)
	}
}

func portList(ports []int) string {
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		values = append(values, fmt.Sprintf("%d", port))
	}
	if len(values) == 0 {
		return "SSH only"
	}
	return strings.Join(values, ",")
}

func httpStatus(check servers.HTTPCheck) string {
	if strings.TrimSpace(check.URL) == "" {
		return "not configured"
	}
	if check.OK {
		return fmt.Sprintf("ok %d · %dms", check.StatusCode, check.LatencyMS)
	}
	if check.Error != "" {
		return "error: " + check.Error
	}
	return fmt.Sprintf("error %d · %dms", check.StatusCode, check.LatencyMS)
}

func machineStatus(info servers.MachineInfo) string {
	if info.Error != "" && !info.OK {
		return "error: " + info.Error
	}
	return fmt.Sprintf("ok · %s · %s", dash(info.Uptime), dash(info.Load))
}

func dash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
