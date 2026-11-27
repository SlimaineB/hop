package cli

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/diag"
	"github.com/SlimaineB/hop/internal/tui"
)

func newDiagEngine(cfg *config.Config) (*diag.Engine, error) {
	return diag.NewEngine(cfg.Diagnostics)
}

func runDiagAppsTUI(engine *diag.Engine, apps []config.AppConfig) error {
	program := tea.NewProgram(
		tui.NewDiagnostics(tui.DiagnosticsOptions{
			Engine: engine,
			Apps:   apps,
			Live:   true,
		}),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}
