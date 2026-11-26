package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/servers"
	"github.com/SlimaineB/hop/internal/version"
)

func (a *App) newContextsCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "contexts",
		Aliases: []string{"ctx"},
		Short:   "List discovered Kubernetes contexts",
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

			contexts, err := service.Contexts(cmd.Context())
			if err != nil && len(contexts) == 0 {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"contexts": contexts,
					"error":    errorString(err),
				})
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "NAME\tENV\tCLUSTER\tDISPLAY\tWORKSPACE")
			for _, context := range contexts {
				workspace := context.Workspace
				if workspace == "" {
					workspace = "-"
				}
				display := context.Display
				if display == "" {
					display = "-"
				}
				env := context.Env
				if env == "" {
					env = "-"
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
					context.Name, env, context.Cluster, display, workspace)
			}
			if err := writer.Flush(); err != nil {
				return err
			}
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "sortie JSON")
	return cmd
}

func (a *App) newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the hop configuration",
	}
	cmd.AddCommand(
		a.newConfigShortcutCommand(),
		a.newConfigSourcesCommand(),
		a.newConfigToggleCommand(true),
		a.newConfigToggleCommand(false),
		&cobra.Command{
			Use:   "init",
			Short: "Create an example configuration",
			RunE: func(cmd *cobra.Command, args []string) error {
				path := a.configPath
				if path == "" {
					path = os.Getenv(config.EnvConfig)
				}
				created, err := config.Init(path)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "config created: %s\n", created)
				return nil
			},
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print the effective config path",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Fprintln(cmd.OutOrStdout(), a.resolvedConfigPath())
				return nil
			},
		},
		&cobra.Command{
			Use:   "show",
			Short: "Print the resolved configuration",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, _, err := config.Load(a.configPath)
				if err != nil {
					return err
				}
				data, err := yaml.Marshal(cfg)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(data)
				return err
			},
		},
		a.newConfigServerCommand(),
		&cobra.Command{
			Use:   "check",
			Short: "Validate the configuration and kubeconfigs",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, path, err := config.Load(a.configPath)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "config: %s\n", path)
				fmt.Fprintf(cmd.OutOrStdout(), "concurrency: %d · timeout: %s · cache: %s\n",
					cfg.Kube.Concurrency, cfg.Kube.RequestTimeout.Get(), cfg.Kube.CacheTTL.Get())
				warnings := 0
				for _, kubeconfig := range resolvedKubeconfigs(cfg) {
					if _, err := os.Stat(kubeconfig); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: kubeconfig not found: %s\n", kubeconfig)
						warnings++
						continue
					}
					fmt.Fprintf(cmd.OutOrStdout(), "kubeconfig: %s\n", kubeconfig)
				}
				if cfg.Issues.DefaultProvider != "" {
					if _, ok := cfg.Providers[cfg.Issues.DefaultProvider]; !ok {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: issues.default_provider %q is not configured\n", cfg.Issues.DefaultProvider)
						warnings++
					}
				}
				for id, provider := range cfg.Providers {
					if strings.TrimSpace(provider.Type) == "" {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: provider %s has no type\n", id)
						warnings++
					}
				}
				for _, shortcut := range cfg.Shortcuts {
					if strings.TrimSpace(shortcut.Name) == "" {
						fmt.Fprintln(cmd.ErrOrStderr(), "warning: shortcut without a name")
						warnings++
						continue
					}
					if !config.ValidShortcutURL(shortcut.URL) {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: shortcut %s has an invalid URL: %s\n", shortcut.Name, shortcut.URL)
						warnings++
					}
				}
				if warnings > 0 {
					return fmt.Errorf("%d warning(s)", warnings)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "config OK")
				return nil
			},
		},
	)
	return cmd
}

func (a *App) newConfigServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the VM/server inventory",
	}
	var (
		sshAlias  string
		host      string
		user      string
		port      int
		env       string
		proxyJump string
		metrics   string
		health    string
		tags      []string
		noSSH     bool
		asJSON    bool
	)
	add := &cobra.Command{
		Use:   "add NAME [user@host] [tags...]",
		Short: "Add or update a VM/server",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("server name is required")
			}
			positionalTags := append([]string{}, args[1:]...)
			if len(positionalTags) > 0 && strings.Contains(positionalTags[0], "@") && host == "" && user == "" {
				parts := strings.SplitN(positionalTags[0], "@", 2)
				user = parts[0]
				host = parts[1]
				positionalTags = positionalTags[1:]
			}
			allTags := append(append([]string{}, tags...), positionalTags...)
			entry := config.ServerConfig{
				Name:       name,
				SSH:        strings.TrimSpace(sshAlias),
				Host:       strings.TrimSpace(host),
				User:       strings.TrimSpace(user),
				Port:       port,
				ProxyJump:  strings.TrimSpace(proxyJump),
				Env:        strings.TrimSpace(env),
				Tags:       allTags,
				MetricsURL: strings.TrimSpace(metrics),
				HealthURL:  strings.TrimSpace(health),
			}
			if noSSH {
				disabled := false
				entry.SSHChecks = &disabled
			}
			if entry.SSH == "" && entry.Host == "" {
				entry.SSH = name
			}
			path, err := config.UpsertServer(a.resolvedConfigPath(), entry)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "server saved: %s (%s)\n", name, path)
			return nil
		},
	}
	add.Flags().StringVar(&sshAlias, "ssh", "", "SSH alias or target")
	add.Flags().StringVar(&host, "host", "", "hostname or IP")
	add.Flags().StringVar(&user, "user", "", "SSH user")
	add.Flags().IntVar(&port, "port", 0, "SSH port")
	add.Flags().StringVar(&env, "env", "", "environment")
	add.Flags().StringVar(&proxyJump, "proxy-jump", "", "SSH ProxyJump")
	add.Flags().StringVar(&metrics, "metrics-url", "", "Prometheus metrics URL")
	add.Flags().StringVar(&health, "health-url", "", "health endpoint URL")
	add.Flags().StringSliceVar(&tags, "tags", nil, "tags (comma separated or repeated)")
	add.Flags().BoolVar(&noSSH, "no-ssh-checks", false, "disable SSH machine checks")

	list := &cobra.Command{
		Use:   "list",
		Short: "List discovered VMs/servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			service := servers.New(cfg)
			inventory, err := service.List()
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"servers": inventory})
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "NAME\tSSH\tHOST\tUSER\tPORT\tENV\tTAGS\tSOURCE")
			for _, server := range inventory {
				sshName := server.SSH
				if sshName == "" {
					sshName = "-"
				}
				envName := server.Env
				if envName == "" {
					envName = "-"
				}
				source := server.Source
				if source == "" {
					source = "-"
				}
				tagText := "-"
				if len(server.Tags) > 0 {
					tagText = strings.Join(server.Tags, ",")
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
					server.Name, sshName, server.Host, server.User, server.Port, envName, tagText, source)
			}
			return writer.Flush()
		},
	}
	list.Flags().BoolVar(&asJSON, "json", false, "JSON output")

	remove := &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a VM/server from the hop config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.RemoveServer(a.resolvedConfigPath(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "server removed: %s (%s)\n", args[0], path)
			return nil
		},
	}

	cmd.AddCommand(add, list, remove)
	return cmd
}

func (a *App) newConfigShortcutCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shortcut",
		Short: "Manage browser shortcuts for hop link",
	}
	var description string
	var extraKeywords []string
	add := &cobra.Command{
		Use:   "add NAME URL [keywords...]",
		Short: "Add or update a browser shortcut",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			rawURL := strings.TrimSpace(args[1])
			if !config.ValidShortcutURL(rawURL) {
				return fmt.Errorf("invalid http(s) URL: %s", rawURL)
			}
			keywords := append(append([]string{}, extraKeywords...), args[2:]...)
			path, err := config.UpsertShortcut(a.resolvedConfigPath(), config.ShortcutConfig{
				Name:        name,
				URL:         rawURL,
				Keywords:    keywords,
				Description: description,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "shortcut saved: %s (%s)\n", name, path)
			return nil
		},
	}
	add.Flags().StringVar(&description, "description", "", "shortcut description")
	add.Flags().StringSliceVar(&extraKeywords, "keywords", nil, "keywords (comma separated or repeated)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List configured browser shortcuts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			hits := config.MatchShortcuts(cfg.Shortcuts, nil)
			return printShortcuts(cmd, hits, false)
		},
	}
	remove := &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a browser shortcut",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.RemoveShortcut(a.resolvedConfigPath(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "shortcut removed: %s (%s)\n", args[0], path)
			return nil
		},
	}
	cmd.AddCommand(add, list, remove)
	return cmd
}

func (a *App) newCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the on-disk pod snapshot cache",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "clear",
			Short: "Delete cached snapshots",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, _, err := config.Load(a.configPath)
				if err != nil {
					return err
				}
				service, err := app.New(cfg)
				if err != nil {
					return err
				}
				defer service.Close()
				count, err := service.ClearCache()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%d file(s) deleted in %s\n", count, service.CacheDir())
				return nil
			},
		},
		&cobra.Command{
			Use:   "dir",
			Short: "Print the cache directory",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, _, err := config.Load(a.configPath)
				if err != nil {
					return err
				}
				service, err := app.New(cfg)
				if err != nil {
					return err
				}
				defer service.Close()
				dir := service.CacheDir()
				if dir == "" {
					fmt.Fprintln(cmd.OutOrStdout(), "cache disabled")
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout(), dir)
				return nil
			},
		},
	)
	return cmd
}

func (a *App) newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
		},
	}
}

func resolvedKubeconfigs(cfg *config.Config) []string {
	if len(cfg.Kube.Kubeconfigs) > 0 {
		return cfg.Kube.Kubeconfigs
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return filepath.SplitList(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".kube", "config")}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
