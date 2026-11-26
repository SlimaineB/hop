package cli

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/tui"
)

func (a *App) runTUI(service *app.Service, cfg *config.Config, query string) error {
	launcher := newWebLauncher(service, cfg)
	defer launcher.Close()

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
		}),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}
