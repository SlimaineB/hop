package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/teu/internal/actions"
	"github.com/SlimaineB/teu/internal/app"
	"github.com/SlimaineB/teu/internal/config"
	"github.com/SlimaineB/teu/internal/kube"
	"github.com/SlimaineB/teu/internal/servers"
	"github.com/SlimaineB/teu/internal/web"
)

type webLauncher struct {
	svc     *app.Service
	cfg     *config.Config
	servers *servers.Service

	mu     sync.Mutex
	server *web.Server
	base   string
}

func newWebLauncher(service *app.Service, cfg *config.Config) *webLauncher {
	launcher := &webLauncher{svc: service, cfg: cfg}
	if cfg != nil {
		launcher.servers = servers.New(cfg)
	}
	return launcher
}

func (l *webLauncher) ensure() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.server != nil && l.base != "" {
		return l.base, nil
	}
	server, err := web.New(l.svc, web.Options{
		Bind:      l.cfg.UI.Bind,
		Port:      l.cfg.UI.Port,
		Theme:     l.cfg.UI.Theme,
		Servers:   l.servers,
		Shortcuts: l.cfg.Shortcuts,
	})
	if err != nil {
		return "", err
	}
	if err := server.Start(); err != nil {
		return "", err
	}
	l.server = server
	l.base = server.BaseURL()
	return l.base, nil
}

func (l *webLauncher) OpenPod(ref kube.PodRef, query string) (string, error) {
	base, err := l.ensure()
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("context", ref.Context)
	values.Set("namespace", ref.Namespace)
	values.Set("name", ref.Name)
	if query != "" {
		values.Set("q", query)
	}
	target := base + "/pod?" + values.Encode()
	return target, actions.OpenBrowser(target)
}

func (l *webLauncher) OpenServer(name, query string) (string, error) {
	base, err := l.ensure()
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("name", name)
	if query != "" {
		values.Set("q", query)
	}
	target := base + "/server?" + values.Encode()
	return target, actions.OpenBrowser(target)
}

func (l *webLauncher) Close() {
	l.mu.Lock()
	server := l.server
	l.server = nil
	l.base = ""
	l.mu.Unlock()
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func (a *App) serveWeb(cmd *cobra.Command, service *app.Service, cfg *config.Config, query string) error {
	launcher := newWebLauncher(service, cfg)
	defer launcher.Close()

	base, err := launcher.ensure()
	if err != nil {
		return err
	}
	target := base
	if query != "" {
		target += "/?q=" + url.QueryEscape(query)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "teu UI: %s\n", target)
	if !a.flags.noOpen && cfg.UI.BrowserEnabled() {
		if err := actions.OpenBrowser(target); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "teu: browser unavailable: %v\n", err)
		}
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

func (a *App) newWebCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "web [terms...]",
		Aliases: []string{"ui", "serve"},
		Short:   "Launch the local web UI",
		Args:    cobra.ArbitraryArgs,
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
			return a.serveWeb(cmd, service, cfg, strings.Join(args, " "))
		},
	}
	addWebFlags(cmd, a)
	return cmd
}
