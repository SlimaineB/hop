package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SlimaineB/hop/internal/config"
)

func (a *App) prepareHopContext(cmd *cobra.Command) error {
	if !commandUsesHopContext(cmd) {
		return nil
	}
	if a.configPath != "" || os.Getenv(config.EnvConfig) != "" {
		if a.hopContext != "" {
			return fmt.Errorf("--config and --hop-context cannot be used together")
		}
		return nil
	}

	set, err := config.LoadContexts(config.DefaultPath())
	if err != nil {
		return err
	}
	a.contexts = set
	if len(set.Contexts) == 0 {
		return nil
	}

	name := strings.TrimSpace(a.hopContext)
	explicit := name != ""
	if name == "" {
		name = strings.TrimSpace(os.Getenv(config.EnvContext))
		explicit = name != ""
	}
	if name == "" {
		name = set.DefaultName
	}
	if name != "" {
		if _, ok := set.Find(name); !ok {
			if explicit {
				return fmt.Errorf("unknown hop context %q: choose %s", name, strings.Join(set.Names(), ", "))
			}
			name = set.DefaultName
		}
	}

	if !explicit && len(set.Contexts) > 1 && a.shouldPromptHopContext(cmd) && term.IsTerminal(int(os.Stdin.Fd())) {
		selected, err := promptHopContext(set, name)
		if err != nil {
			return err
		}
		if selected != "" {
			name = selected
			if err := set.SetDefault(selected); err != nil {
				fmt.Fprintf(os.Stderr, "hop: warning: could not persist hop context: %v\n", err)
			}
		}
	}

	ctx, ok := set.Find(name)
	if !ok {
		ctx, ok = set.Default()
	}
	if !ok {
		return nil
	}
	a.configPath = ctx.Path
	a.contextName = ctx.Name
	return nil
}

func commandUsesHopContext(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "version", "demo", "completion", "help":
		return false
	default:
		return true
	}
}

func (a *App) shouldPromptHopContext(cmd *cobra.Command) bool {
	if a.noContextPrompt {
		return false
	}
	if cmd == cmd.Root() {
		return true
	}
	switch cmd.Name() {
	case "web", "all":
		return true
	default:
		return false
	}
}

func (a *App) multiContexts() bool {
	return a.contexts != nil && len(a.contexts.Contexts) > 1
}

func (a *App) loadHopContexts() (*config.ContextSet, error) {
	if a.contexts != nil {
		return a.contexts, nil
	}
	return config.LoadContexts(config.DefaultPath())
}

func promptHopContext(set *config.ContextSet, defaultValue string) (string, error) {
	fmt.Fprintln(os.Stderr, "Select a hop context:")
	for i, ctx := range set.Contexts {
		suffix := ""
		if ctx.Name == defaultValue {
			suffix = "  (default)"
		}
		fmt.Fprintf(os.Stderr, "  %d) %s%s\n", i+1, ctx.Name, suffix)
	}
	fmt.Fprintf(os.Stderr, "Hop context [1-%d, name, Enter=%s]: ", len(set.Contexts), defaultValue)

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return defaultValue, nil
	}
	if index, err := strconv.Atoi(value); err == nil {
		if index >= 1 && index <= len(set.Contexts) {
			return set.Contexts[index-1].Name, nil
		}
		return "", fmt.Errorf("invalid hop context selection %q", value)
	}
	if ctx, ok := set.Find(value); ok {
		return ctx.Name, nil
	}
	return "", fmt.Errorf("invalid hop context selection %q", value)
}

func (a *App) newConfigContextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "List or select the active hop context",
		Long: `Manage hop contexts. Each context is a separate hop config file stored in
~/.config/hop/contexts/<name>.yaml. The base config file is the "default"
context. The last selected context is remembered for future launches.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := a.loadHopContexts()
			if err != nil {
				return err
			}
			if len(set.Contexts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no hop contexts configured")
				return nil
			}
			defaultName := set.DefaultName
			if a.contextName != "" {
				defaultName = a.contextName
			}
			fmt.Fprintf(cmd.OutOrStdout(), "default: %s\n", defaultName)
			for _, ctx := range set.Contexts {
				marker := " "
				if ctx.Name == defaultName {
					marker = "*"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %-20s %s\n", marker, ctx.Name, ctx.Path)
			}
			return nil
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "use NAME",
		Short: "Select the default hop context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := a.loadHopContexts()
			if err != nil {
				return err
			}
			if err := set.SetDefault(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "hop context: %s\n", args[0])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "init NAME",
		Short: "Create a new hop context config file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := a.loadHopContexts()
			if err != nil {
				return err
			}
			path, err := set.ContextPath(args[0])
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("hop context %q already exists: %s", args[0], path)
			}
			created, err := config.Init(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "hop context created: %s\n", created)
			return nil
		},
	})
	return cmd
}
