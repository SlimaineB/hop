package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/demo"
	"github.com/SlimaineB/hop/internal/web"
)

func (a *App) newDemoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "demo",
		Aliases: []string{"showcase"},
		Short:   "Launch a full local demo with synthetic data",
		Long: `Launch a self-contained demo with synthetic pods, VMs, containers, repositories,
packages, tickets and shortcuts.

The demo does not require Kubernetes, Jira, GitLab, Docker, Podman or SSH. It
starts the web UI on a local port and opens it in your browser.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runtime, err := demo.New()
			if err != nil {
				return err
			}
			defer runtime.Close()

			cfg := runtime.Config
			applyRuntimeOverrides(cfg, a.flags)
			server, err := web.New(runtime.Backend, web.Options{
				Bind:        cfg.UI.Bind,
				Port:        cfg.UI.Port,
				Theme:       cfg.UI.Theme,
				Servers:     runtime.Servers,
				Shortcuts:   cfg.Shortcuts,
				LiveLimit:   cfg.Search.LiveLimit,
				Registry:    runtime.Registry,
				Stats:       runtime.App.Stats,
				Dashboard:   runtime.Dashboard,
				Apps:        cfg.Apps,
				Diagnostics: cfg.Diagnostics,
			})
			if err != nil {
				return err
			}
			if err := server.Start(); err != nil {
				return err
			}
			target := server.BaseURL()
			fmt.Fprintf(cmd.OutOrStdout(), "hop demo: %s\n", target)
			fmt.Fprintln(cmd.OutOrStdout(), "synthetic data: 12 pods, 6 VMs, 3 app probes, 5 containers, 5 repos, 5 packages, 5 tickets, 5 shortcuts")
			if !a.flags.noOpen && cfg.UI.BrowserEnabled() {
				if err := actions.OpenBrowser(target); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "hop: browser unavailable: %v\n", err)
				}
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return server.Shutdown(shutdownCtx)
		},
	}
	addWebFlags(cmd, a)
	return cmd
}
