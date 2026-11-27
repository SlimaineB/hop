package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/diag"
)

const diagLiveInterval = 15 * time.Second

type DiagnosticsOptions struct {
	Engine *diag.Engine
	Apps   []config.AppConfig
	Live   bool
}

type DiagnosticsModel struct {
	engine *diag.Engine
	apps   []config.AppConfig

	spinner spinner.Model
	width   int
	height  int

	reports  []*diag.Report
	cursor   int
	offset   int
	loading  bool
	err      error
	status   string
	help     bool
	live     bool
	lastAt   time.Time
	duration time.Duration
	runSeq   int
	tickSeq  int
}

type diagRunMsg struct {
	seq      int
	reports  []*diag.Report
	duration time.Duration
	err      error
}

type diagAppTickMsg struct {
	seq int
}

func ensureDiagEngine(engine *diag.Engine) *diag.Engine {
	if engine != nil {
		return engine
	}
	fallback, _ := diag.NewEngine(config.Diagnostics{})
	return fallback
}

func NewDiagnostics(opts DiagnosticsOptions) DiagnosticsModel {
	return DiagnosticsModel{
		engine:  ensureDiagEngine(opts.Engine),
		apps:    opts.Apps,
		spinner: newSpinner(),
		width:   140,
		height:  38,
		live:    opts.Live,
	}
}

func (m DiagnosticsModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	if len(m.apps) > 0 {
		m.loading = true
		cmds = append(cmds, m.runCmd())
		if m.live {
			cmds = append(cmds, m.tickCmd())
		}
	}
	return tea.Batch(cmds...)
}

func (m *DiagnosticsModel) runCmd() tea.Cmd {
	if m.engine == nil {
		return func() tea.Msg { return diagRunMsg{err: errors.New("diagnostics engine unavailable")} }
	}
	if len(m.apps) == 0 {
		return func() tea.Msg { return diagRunMsg{err: errors.New("no applications configured; add an apps: section")} }
	}
	m.runSeq++
	seq := m.runSeq
	m.loading = true
	m.err = nil
	engine := m.engine
	apps := append([]config.AppConfig(nil), m.apps...)
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		reports := engine.DiagnoseApps(ctx, apps)
		sortDiagReports(reports)
		return diagRunMsg{seq: seq, reports: reports, duration: time.Since(start)}
	}
}

func (m *DiagnosticsModel) tickCmd() tea.Cmd {
	m.tickSeq++
	seq := m.tickSeq
	return tea.Tick(diagLiveInterval, func(time.Time) tea.Msg {
		return diagAppTickMsg{seq: seq}
	})
}

func (m DiagnosticsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case diagRunMsg:
		if msg.seq != m.runSeq {
			return m, nil
		}
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.reports = msg.reports
			m.duration = msg.duration
			m.lastAt = time.Now()
			if m.cursor >= len(m.reports) {
				m.cursor = max(0, len(m.reports)-1)
			}
			m.status = fmt.Sprintf("%d apps checked in %s", len(m.reports), diagDuration(msg.duration))
		} else {
			m.status = "diagnostics: " + msg.err.Error()
		}
		return m, nil
	case diagAppTickMsg:
		if msg.seq != m.tickSeq || !m.live {
			return m, nil
		}
		next := m.tickCmd()
		if m.loading {
			return m, next
		}
		return m, tea.Batch(next, m.runCmd())
	}
	return m, nil
}

func (m DiagnosticsModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.help {
		switch key {
		case "esc", "?", "q", "enter":
			m.help = false
		}
		return m, nil
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.help = true
		return m, nil
	case "r":
		return m, tea.Batch(m.spinner.Tick, m.runCmd())
	case "l":
		m.live = !m.live
		if m.live {
			return m, tea.Batch(m.spinner.Tick, m.tickCmd(), m.runCmd())
		}
		m.tickSeq++
		m.status = "live refresh paused"
		return m, nil
	case "up", "k":
		return m.moveCursor(-1), nil
	case "down", "j":
		return m.moveCursor(1), nil
	case "pgup":
		m.cursor = max(0, m.cursor-m.visibleRows())
		return m, nil
	case "pgdown":
		m.cursor = min(max(0, len(m.reports)-1), m.cursor+m.visibleRows())
		return m, nil
	case "g":
		m.cursor = 0
		return m, nil
	case "G":
		m.cursor = max(0, len(m.reports)-1)
		return m, nil
	case "home":
		m.offset = 0
		return m, nil
	case "end":
		if report := m.selectedReport(); report != nil {
			m.offset = max(0, len(report.Findings)-1)
		}
		return m, nil
	}
	return m, nil
}

func (m DiagnosticsModel) moveCursor(delta int) DiagnosticsModel {
	if len(m.reports) == 0 {
		return m
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.reports) {
		next = len(m.reports) - 1
	}
	m.cursor = next
	m.offset = 0
	return m
}

func (m DiagnosticsModel) visibleRows() int {
	return max(4, m.height-10)
}

func (m DiagnosticsModel) selectedReport() *diag.Report {
	if m.cursor < 0 || m.cursor >= len(m.reports) {
		return nil
	}
	return m.reports[m.cursor]
}

func (m DiagnosticsModel) View() string {
	if m.help {
		return m.helpView()
	}
	width := max(80, m.width)
	height := max(24, m.height)
	header := m.headerView(width)
	footer := m.footerView(width)
	bodyHeight := height - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if bodyHeight < 8 {
		bodyHeight = 8
	}
	if len(m.apps) == 0 {
		body := styleBox.Width(width - 2).Height(bodyHeight).Render(styleMuted.Render(
			"No applications configured.\n\nAdd an apps: section with a name, port, health_url or log_path:\n\n  apps:\n    - name: checkout-api\n      port: 8080\n      log_path: /var/log/checkout/app.log"))
		return strings.Join([]string{header, body, footer}, "\n")
	}
	listWidth := 52
	if width < 120 {
		listWidth = max(38, width*42/100)
	}
	panelWidth := width - listWidth - 1
	if panelWidth < 42 {
		panelWidth = 42
		listWidth = max(30, width-panelWidth-1)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.listView(listWidth, bodyHeight), " ", diagPanelView(m.selectedReport(), m.loading, m.err, panelWidth, bodyHeight, m.offset))
	return strings.Join([]string{header, body, footer}, "\n")
}

func (m DiagnosticsModel) headerView(width int) string {
	title := styleTitle.Render("hop") + styleSubtitle.Render("  ·  application diagnostics")
	parts := []string{fmt.Sprintf("%d apps", len(m.apps))}
	if m.loading {
		parts = append(parts, m.spinner.View()+" running probes")
	} else if m.err != nil {
		parts = append(parts, styleErr.Render("error"))
	} else {
		critical, warning, healthy := diagCounts(m.reports)
		if critical > 0 {
			parts = append(parts, styleErr.Render(fmt.Sprintf("%d critical", critical)))
		}
		if warning > 0 {
			parts = append(parts, styleWarn.Render(fmt.Sprintf("%d warning", warning)))
		}
		if healthy > 0 {
			parts = append(parts, styleOK.Render(fmt.Sprintf("%d ok", healthy)))
		}
	}
	if m.live {
		parts = append(parts, styleChipOK.Render("live "+diagLiveInterval.String()))
	} else {
		parts = append(parts, styleDim.Render("paused"))
	}
	if !m.lastAt.IsZero() {
		parts = append(parts, styleDim.Render("checked "+m.lastAt.Local().Format("15:04:05")+" · "+diagDuration(m.duration)))
	}
	if m.status != "" {
		parts = append(parts, styleDim.Render(clip(m.status, max(20, width/3))))
	}
	return strings.Join([]string{title, strings.Join(parts, styleDim.Render("  ·  "))}, "\n")
}

func (m DiagnosticsModel) listView(width, height int) string {
	rows := max(1, height-2)
	content := []string{styleSection.Render(clip(fmt.Sprintf("APPLICATIONS  %d", len(m.reports)), width-4))}
	if len(m.reports) == 0 {
		if m.loading {
			content = append(content, styleMuted.Render("  running probes in parallel…"))
		} else if m.err != nil {
			content = append(content, styleErr.Render("  "+clip(m.err.Error(), width-6)))
		} else {
			content = append(content, styleMuted.Render("  no report"))
		}
	}
	for i, report := range m.reports {
		if len(content) >= rows+1 {
			break
		}
		okProbes := 0
		for _, probe := range report.Probes {
			if probe.OK {
				okProbes++
			}
		}
		line := fmt.Sprintf("%s  %-22s %-8s %d/%d probes %d findings %s",
			diagSeverityChip(report.Verdict), clip(report.Target, 22), dashTUI(report.Env),
			okProbes, len(report.Probes), len(report.Findings), diagDuration(report.Duration))
		line = clip(line, width-4)
		if i == m.cursor {
			content = append(content, styleSelected.Render(" "+line))
		} else {
			content = append(content, " "+line)
		}
	}
	for len(content) < rows {
		content = append(content, "")
	}
	return styleBox.Width(width - 2).Height(height).Render(strings.Join(content, "\n"))
}

func (m DiagnosticsModel) footerView(width int) string {
	keys := [][2]string{
		{"↑↓", "select"}, {"r", "run probes"}, {"l", "live on/off"},
		{"pgup/pgdn", "scroll findings"}, {"g/G", "first/last"}, {"?", "help"}, {"q", "quit"},
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, styleHelpKey.Render(key[0])+" "+styleDim.Render(key[1]))
	}
	return styleDim.Render(strings.Repeat("─", max(1, width-2))) + "\n" + clip(strings.Join(parts, "  "), width-2)
}

func (m DiagnosticsModel) helpView() string {
	lines := []string{
		styleTitle.Render("hop · application diagnostics"),
		"",
		styleSection.Render("RUN"),
		"  probes run in parallel goroutines for every configured app",
		"  HTTP, TCP, log and command probes are merged with custom diagnostic checks",
		"",
		styleSection.Render("KEYS"),
		"  " + styleHelpKey.Render(padRight("↑↓ / j k", 12)) + " select an app",
		"  " + styleHelpKey.Render(padRight("r", 12)) + " run all probes now",
		"  " + styleHelpKey.Render(padRight("l", 12)) + " toggle live refresh every " + diagLiveInterval.String(),
		"  " + styleHelpKey.Render(padRight("pgup/pgdn", 12)) + " scroll findings in the selected report",
		"  " + styleHelpKey.Render(padRight("q", 12)) + " quit",
		"",
		styleDim.Render("Reports are sorted by severity: critical, warning, then healthy."),
	}
	return styleBox.Width(max(70, m.width-2)).Render(strings.Join(lines, "\n"))
}
