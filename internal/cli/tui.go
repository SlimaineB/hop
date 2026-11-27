package cli

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/tui"
)

func (a *App) runTUI(service *app.Service, cfg *config.Config, query string) error {
	if _, err := a.resolveEnv(cfg, query, true); err != nil {
		return err
	}
	launcher := newWebLauncher(service, cfg, a.resolvedConfigPath())
	defer launcher.Close()
	engine, diagErr := newDiagEngine(cfg)
	if diagErr != nil {
		return diagErr
	}

	program := tea.NewProgram(
		tui.New(tui.Options{
			Service:      service,
			Initial:      query,
			Contexts:     splitList(a.flags.contexts),
			Namespaces:   splitList(a.flags.namespaces),
			Env:          a.flags.env,
			Limit:        a.flags.limit,
			ForceRefresh: a.flags.refresh,
			OpenWeb:      launcher.OpenPod,
			Diagnostics:  engine,
		}),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}
