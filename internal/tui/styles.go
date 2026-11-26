package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent  = lipgloss.Color("#67e8f9")
	colorAccent2 = lipgloss.Color("#a78bfa")
	colorText    = lipgloss.Color("#e2e8f0")
	colorMuted   = lipgloss.Color("#94a3b8")
	colorDim     = lipgloss.Color("#64748b")
	colorOK      = lipgloss.Color("#34d399")
	colorWarn    = lipgloss.Color("#fbbf24")
	colorErr     = lipgloss.Color("#fb7185")

	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleSubtitle = lipgloss.NewStyle().Foreground(colorDim)
	styleMuted    = lipgloss.NewStyle().Foreground(colorMuted)
	styleDim      = lipgloss.NewStyle().Foreground(colorDim)
	styleQuery    = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	stylePrompt   = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	styleSection  = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	styleOK       = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn     = lipgloss.NewStyle().Foreground(colorWarn)
	styleErr      = lipgloss.NewStyle().Foreground(colorErr)
	styleRange    = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	styleSelected = lipgloss.NewStyle().
			Background(lipgloss.Color("#1e293b")).
			Foreground(lipgloss.Color("#f8fafc")).
			Bold(true)
	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#334155")).
			Padding(0, 1)
	styleHelpKey = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
)
