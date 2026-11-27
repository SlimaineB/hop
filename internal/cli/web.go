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

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/containers"
	"github.com/SlimaineB/hop/internal/dashboard"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/plugins"
	"github.com/SlimaineB/hop/internal/providers"
	containerregistry "github.com/SlimaineB/hop/internal/registry"
	"github.com/SlimaineB/hop/internal/repos"
	"github.com/SlimaineB/hop/internal/servers"
	"github.com/SlimaineB/hop/internal/web"
)

type webLauncher struct {
	svc        *app.Service
	cfg        *config.Config
	servers    *servers.Service
	containers *containers.Service
	providers  *providers.Set
	plugins    *plugin.Registry
	dashboard  *dashboard.Service
	stats      func() app.Stats

	mu     sync.Mutex
	server *web.Server
	base   string
}

func newWebLauncher(service *app.Service, cfg *config.Config) *webLauncher {
	launcher := &webLauncher{svc: service, cfg: cfg}
	if cfg == nil {
		return launcher
	}
	launcher.servers = servers.New(cfg)
	launcher.containers = containers.New(cfg)
	if set, err := loadProviderSet(cfg); err == nil {
		launcher.providers = set
	}
	shortcuts := cfg.Shortcuts
	if cfg.SourceDisabled("shortcuts") {
		shortcuts = nil
	}
	reposService := repos.New(cfg)
	registryService := containerregistry.New(cfg)
	var pods plugins.PodSearcher
	if service != nil {
		pods = service
	}
	launcher.plugins = plugins.Build(cfg, plugins.Deps{
		Pods:             pods,
		Servers:          launcher.servers,
		Containers:       launcher.containers,
		Repos:            reposService,
		Registry:         registryService,
		Providers:        launcher.providers,
		Shortcuts:        shortcuts,
		OnImagesDetected: func(refs []string) { registryService.SetImages(refs...) },
	})
	var podSource dashboard.PodSource
	if service != nil {
		podSource = service
	}
	launcher.dashboard = dashboard.New(podSource, launcher.servers, cfg)
	launcher.stats = func() app.Stats {
		if service == nil {
			return app.Stats{}
		}
		return service.Stats()
	}
	return launcher
}

func (l *webLauncher) ensure() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.server != nil && l.base != "" {
		return l.base, nil
	}
	var backend web.Backend
	if l.svc != nil {
		backend = l.svc
	}
	server, err := web.New(backend, web.Options{
		Bind:      l.cfg.UI.Bind,
		Port:      l.cfg.UI.Port,
		Theme:     l.cfg.UI.Theme,
		Servers:   l.servers,
		Shortcuts: shortcutsOrNil(l.cfg),
		LiveLimit: l.cfg.Search.LiveLimit,
		Registry:  l.plugins,
		Stats:     l.stats,
		Dashboard: l.dashboard,
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

func (l *webLauncher) OpenQuery(query string) (string, error) {
	base, err := l.ensure()
	if err != nil {
		return "", err
	}
	target := base
	if query != "" {
		target += "/search?q=" + url.QueryEscape(query)
	}
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
		target += "/search?q=" + url.QueryEscape(query)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "hop UI: %s\n", target)
	if !a.flags.noOpen && cfg.UI.BrowserEnabled() {
		if err := actions.OpenBrowser(target); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "hop: browser unavailable: %v\n", err)
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

func shortcutsOrNil(cfg *config.Config) []config.ShortcutConfig {
	if cfg == nil || cfg.SourceDisabled("shortcuts") {
		return nil
	}
	return cfg.Shortcuts
}
