package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent  = lipgloss.Color("#22d3ee")
	colorAccent2 = lipgloss.Color("#a78bfa")
	colorAccent3 = lipgloss.Color("#60a5fa")
	colorText    = lipgloss.Color("#e5e7eb")
	colorMuted   = lipgloss.Color("#94a3b8")
	colorDim     = lipgloss.Color("#64748b")
	colorOK      = lipgloss.Color("#34d399")
	colorWarn    = lipgloss.Color("#fbbf24")
	colorErr     = lipgloss.Color("#fb7185")
	colorPanel   = lipgloss.Color("#0f172a")
	colorPanel2  = lipgloss.Color("#111c31")
	colorLine    = lipgloss.Color("#263244")

	styleBrand    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleSubtitle = lipgloss.NewStyle().Foreground(colorDim)
	styleMuted    = lipgloss.NewStyle().Foreground(colorMuted)
	styleDim      = lipgloss.NewStyle().Foreground(colorDim)
	styleQuery    = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	stylePrompt   = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	styleSection  = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	styleLabel    = lipgloss.NewStyle().Foreground(colorDim)
	styleOK       = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn     = lipgloss.NewStyle().Foreground(colorWarn)
	styleErr      = lipgloss.NewStyle().Foreground(colorErr)
	styleInfo     = lipgloss.NewStyle().Foreground(colorAccent3)
	styleRange    = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	styleSelected = lipgloss.NewStyle().
			Background(colorPanel2).
			Foreground(lipgloss.Color("#f8fafc")).
			Bold(true)
	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorLine).
			Padding(0, 1)
	styleBoxActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent2).
			Padding(0, 1)
	styleHelpKey = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleHelpCmd = lipgloss.NewStyle().Foreground(colorAccent3)

	styleChip      = lipgloss.NewStyle().Foreground(colorMuted)
	styleChipOK    = lipgloss.NewStyle().Foreground(colorOK).Bold(true)
	styleChipWarn  = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	styleChipErr   = lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	styleChipInfo  = lipgloss.NewStyle().Foreground(colorAccent3).Bold(true)
	styleAlertOK   = lipgloss.NewStyle().Foreground(colorOK).Background(lipgloss.Color("#08281f")).Padding(0, 1)
	styleAlertWarn = lipgloss.NewStyle().Foreground(colorWarn).Background(lipgloss.Color("#2b2109")).Padding(0, 1)
	styleAlertErr  = lipgloss.NewStyle().Foreground(colorErr).Background(lipgloss.Color("#310f18")).Padding(0, 1)

	styleLogTime = lipgloss.NewStyle().Foreground(colorDim)
	styleLogErr  = lipgloss.NewStyle().Foreground(colorErr)
	styleLogWarn = lipgloss.NewStyle().Foreground(colorWarn)
	styleLogInfo = lipgloss.NewStyle().Foreground(colorAccent3)
	styleLogBody = lipgloss.NewStyle().Foreground(colorText)
)
