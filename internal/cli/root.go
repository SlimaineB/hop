package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/servers"
)

type App struct {
	configPath string
	flags      runtimeFlags
}

type runtimeFlags struct {
	contexts    []string
	namespaces  []string
	env         string
	limit       int
	minScore    float64
	refresh     bool
	noOpen      bool
	bind        string
	port        int
	timeout     time.Duration
	concurrency int
	json        bool
	plain       bool
}

type outputMode int

const (
	outputTable outputMode = iota
	outputJSON
	outputPlain
)

var relativeArgPattern = regexp.MustCompile(`^-(\d+(?:\.\d+)?)([smhdwSMHDW])$`)

// normalizeArgs turns -2h into last:2h before Cobra sees it as an unknown flag.
// The search parser understands both forms; this keeps the ergonomic dash form.
func normalizeArgs(args []string) []string {
	out := append([]string(nil), args...)
	valueFlags := map[string]bool{
		"--bind": true, "--concurrency": true, "--config": true, "--context": true,
		"--env": true, "--limit": true, "--min-score": true, "--namespace": true, "--port": true, "--timeout": true,
	}
	for i := 1; i < len(out); i++ {
		if out[i] == "--" {
			break
		}
		if strings.HasPrefix(out[i], "--") {
			name, _, hasValue := strings.Cut(out[i], "=")
			if valueFlags[name] && !hasValue {
				i++
			}
			continue
		}
		match := relativeArgPattern.FindStringSubmatch(out[i])
		if match == nil {
			continue
		}
		out[i] = "last:" + strings.ToLower(match[1]+match[2])
	}
	return out
}

func Execute() int {
	os.Args = normalizeArgs(os.Args)
	root := NewRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "hop:", err)
		return 1
	}
	return 0
}

func NewRootCommand() *cobra.Command {
	a := &App{}
	var providerPrint bool
	root := &cobra.Command{
		Use:   "hop [terms...]",
		Short: "Find a pod or VM, then open logs, metrics or SSH",
		Long: `hop searches pods, VMs, Git repositories, packages and tickets in one cockpit.

With no arguments, it opens the unified local web UI.
With arguments, it opens the pod search TUI unless a selector switches modes.
Prefer the explicit categories when possible:
  hop all <terms...>   unified cockpit across every enabled source
  hop pod <terms...>   search Kubernetes pods
  hop vm <terms...>    search VMs and SSH servers
  hop git <terms...>   search local Git repositories
  hop pkg <terms...>   search container images and registries
  hop link [terms...]  open browser shortcuts (TUI with no terms)
  hop issue <key...>   open, search or create tickets
  hop diag pod <terms>  diagnose a pod and its related resources
  hop diag vm NAME      diagnose a VM/server
  hop diag apps         live app-probe TUI in a terminal, table with --table
  hop diag app NAME     diagnose one configured application
  hop demo             full local demo without Kubernetes or external services
  hop features         list, enable or disable optional features

Provider aliases are generated from the config, so 'hop jira OPS-123'
works without a --provider flag.

Examples:
  hop
  hop all git hop
  hop pod api prod -2h
  hop vm supervision uat
  hop link grafana
  hop issue OPS-123
  hop jira new "Pod api crashloop"
  hop demo
  hop features disable pods tickets`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := config.Load(a.configPath)
			if err != nil {
				return err
			}
			_ = path
			applyRuntimeOverrides(cfg, a.flags)

			if id, ok := providerAlias(cfg, args); ok {
				return a.runProviderAlias(cmd, cfg, id, args[1:], providerPrint)
			}

			query := strings.Join(args, " ")
			if len(args) > 0 && servers.HasVMSelector(query) {
				serverService := servers.New(cfg)
				if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
					return a.printServerSearch(cmd, serverService, query, false, false)
				}
				return a.runServerTUI(serverService, cfg, query)
			}

			if len(args) > 0 {
				if err := requireFeature(cfg, "pods", "Kubernetes pods"); err != nil {
					return err
				}
			}
			service, err := app.New(cfg)
			if err != nil {
				return err
			}
			defer service.Close()

			if len(args) == 0 {
				return a.serveWeb(cmd, service, cfg, "")
			}

			if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
				if _, err := a.resolveEnv(cfg, query, false); err != nil {
					return err
				}
				return a.printSearch(cmd, service, query, outputTable)
			}
			return a.runTUI(service, cfg, query)
		},
	}

	root.PersistentFlags().StringVar(&a.configPath, "config", "", "config file path (default: ~/.config/hop/config.yaml)")
	addFilterFlags(root, a)
	addWebFlags(root, a)
	root.Flags().BoolVar(&a.flags.refresh, "refresh", false, "force a refresh of all clusters")
	root.Flags().BoolVar(&providerPrint, "print", false, "print the URL instead of opening the browser for provider aliases")
	root.Flags().DurationVar(&a.flags.timeout, "timeout", 0, "per-cluster request timeout")
	root.Flags().IntVar(&a.flags.concurrency, "concurrency", -1, "max clusters queried in parallel (-1 = config, 0 = all selected)")

	root.AddCommand(
		a.newDemoCommand(),
		a.newAllCommand(),
		a.newGitCommand(),
		a.newPkgCommand(),
		a.newPodCommand(),
		a.newDiagCommand(),
		a.newSearchCommand(),
		a.newWebCommand(),
		a.newServersCommand(),
		a.newServerCommand(),
		a.newSSHCommand(),
		a.newContainersCommand(),
		a.newLinkCommand(),
		a.newIssueCommand(),
		a.newContextsCommand(),
		a.newFeaturesCommand(),
		a.newConfigCommand(),
		a.newCacheCommand(),
		a.newVersionCommand(),
	)
	return root
}

func addFilterFlags(cmd *cobra.Command, a *App) {
	flags := cmd.Flags()
	flags.StringArrayVar(&a.flags.contexts, "context", nil, "restrict to one or more contexts (repeatable)")
	flags.StringArrayVar(&a.flags.namespaces, "namespace", nil, "restrict to one or more namespaces (repeatable)")
	flags.StringVar(&a.flags.env, "env", "", "select an environment (dev/uat/prod by default; all scans every context)")
	flags.IntVar(&a.flags.limit, "limit", 0, "maximum number of results")
	flags.Float64Var(&a.flags.minScore, "min-score", 0, "minimum relevance score (0-1), overrides search.min_score")
}

func addWebFlags(cmd *cobra.Command, a *App) {
	flags := cmd.Flags()
	flags.BoolVar(&a.flags.noOpen, "no-open", false, "do not open the browser")
	flags.StringVar(&a.flags.bind, "bind", "", "web UI bind address (default: config)")
	flags.IntVar(&a.flags.port, "port", -1, "web UI port (0 = random)")
}

func applyRuntimeOverrides(cfg *config.Config, flags runtimeFlags) {
	if flags.bind != "" {
		cfg.UI.Bind = flags.bind
	}
	if flags.port >= 0 {
		cfg.UI.Port = flags.port
	}
	if flags.timeout > 0 {
		cfg.Kube.RequestTimeout = config.Duration(flags.timeout)
	}
	if flags.concurrency >= 0 {
		cfg.Kube.Concurrency = flags.concurrency
	}
	if flags.minScore > 0 {
		cfg.Search.MinScore = flags.minScore
	}
	cfg.Normalize()
}

func (a *App) resolvedConfigPath() string {
	path := a.configPath
	if path == "" {
		path = os.Getenv(config.EnvConfig)
	}
	if path == "" {
		path = config.DefaultPath()
	}
	return config.ExpandPath(path)
}

func splitList(values []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}
