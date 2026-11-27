package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/SlimaineB/hop/internal/actions"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(24, min(84, m.width-16))
		m.ensureVisible()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case localSearchTickMsg:
		if msg.seq != m.localSeq {
			return m, nil
		}
		return m, m.localSearchCmd()

	case searchMsg:
		if msg.local {
			if msg.seq != m.localSeq {
				return m, nil
			}
		} else {
			if msg.seq != m.searchSeq {
				return m, nil
			}
			m.loading = false
		}
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		if msg.result == nil {
			return m, nil
		}
		previous := m.selectedKey()
		m.applyResult(msg.result)
		if !msg.local || m.selectedKey() != previous {
			return m, tea.Batch(m.loadDetail(), m.loadStatsCmd())
		}
		return m, nil

	case detailMsg:
		if msg.seq != m.detailSeq {
			return m, nil
		}
		m.detailLoading = false
		if msg.id != m.selectedKey() {
			return m, nil
		}
		if msg.err != nil {
			m.detail = nil
			m.detailErr = msg.err
			return m, nil
		}
		m.detail = msg.detail
		m.detailErr = nil
		return m, nil

	case statsMsg:
		m.applyStats(msg)
		if msg.id != m.selectedKey() {
			return m, nil
		}
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
			return statsTickMsg{id: msg.id}
		})

	case statsTickMsg:
		if msg.id != m.selectedKey() {
			return m, nil
		}
		return m, m.loadStatsCmd()

	case diagMsg:
		if msg.seq != m.diagSeq {
			return m, nil
		}
		m.diagLoading = false
		if msg.id != m.selectedKey() {
			return m, nil
		}
		m.diagErr = msg.err
		m.diag = msg.report
		if msg.err != nil {
			m.status = "diagnostics: " + msg.err.Error()
		} else {
			m.status = "diagnostics: " + string(msg.report.Verdict)
		}
		return m, nil

	case diagTickMsg:
		if msg.seq != m.diagTickSeq || !m.diagMode {
			return m, nil
		}
		next := m.scheduleDiagTick()
		if !m.diagLive || m.diagLoading {
			return m, next
		}
		return m, tea.Batch(next, m.startDiag())

	case logsMsg:
		if msg.seq != m.logsSeq || msg.id != m.logsID {
			return m, nil
		}
		m.logsLoading = false
		m.logsAt = msg.at
		m.logs = msg.content
		m.logsErr = msg.err
		m.logsOffset = max(0, m.logsLineCount()-m.logsVisibleRows())
		return m, nil

	case statusMsg:
		if msg.err != nil {
			m.status = msg.text + ": " + msg.err.Error()
		} else {
			m.status = msg.text
		}
		return m, nil

	case actionDoneMsg:
		if msg.err != nil {
			m.status = msg.label + ": " + msg.err.Error()
		} else {
			m.status = msg.label + " done"
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	if m.logsMode {
		return m.handleLogsKey(key)
	}
	if m.metricsMode {
		return m.handleMetricsKey(key)
	}
	if m.diagMode {
		return m.handleDiagKey(key)
	}
	if m.editing {
		switch key {
		case "enter", "tab":
			return m, m.commitEditing()
		case "esc":
			return m, m.cancelEditing()
		}
		before := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != before {
			m.localSeq++
			seq := m.localSeq
			return m, tea.Batch(cmd, tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg {
				return localSearchTickMsg{seq: seq}
			}))
		}
		return m, cmd
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.help = true
		return m, nil
	case "/", "i":
		m.startEditing()
		return m, nil
	case "up", "ctrl+k":
		return m, m.moveCursor(-1)
	case "down", "j":
		return m, m.moveCursor(1)
	case "pgup":
		return m, m.moveCursor(-m.visibleRows())
	case "pgdown":
		return m, m.moveCursor(m.visibleRows())
	case "g":
		m.cursor = 0
		m.ensureVisible()
		return m, tea.Batch(m.loadDetail(), m.loadStatsCmd())
	case "G":
		m.cursor = max(0, len(m.hits)-1)
		m.ensureVisible()
		return m, tea.Batch(m.loadDetail(), m.loadStatsCmd())
	case "enter", "o":
		return m, m.openWebCmd()
	case "m":
		return m, m.toggleMetrics()
	case "t":
		if _, ok := m.selected(); !ok {
			m.status = "no pod selected"
			return m, nil
		}
		m.enterDiagMode()
		return m, tea.Batch(m.spinner.Tick, m.startDiag(), m.scheduleDiagTick())
	case "l":
		return m, m.toggleLogs()
	case "s", "e":
		hit, ok, ready := m.actionOptions()
		if !ok || !ready {
			return m, nil
		}
		cmd := actions.Exec(hit.Pod, m.svc.ActionOptionsFor(m.timeRange))
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return actionDoneMsg{label: "shell", err: err}
		})
	case "k", "K":
		hit, ok, ready := m.actionOptions()
		if !ok || !ready {
			return m, nil
		}
		cmd := actions.K9s(hit.Pod, m.svc.ActionOptionsFor(m.timeRange))
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return actionDoneMsg{label: "k9s", err: err}
		})
	case "L":
		hit, ok, ready := m.actionOptions()
		if !ok || !ready {
			return m, nil
		}
		cmd := actions.Logs(hit.Pod, m.svc.ActionOptionsFor(m.timeRange))
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return actionDoneMsg{label: "logs", err: err}
		})
	case "d":
		hit, ok, ready := m.actionOptions()
		if !ok || !ready {
			return m, nil
		}
		cmd := actions.DescribePaged(hit.Pod, m.svc.ActionOptionsFor(m.timeRange))
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return actionDoneMsg{label: "describe", err: err}
		})
	case "y":
		hit, ok, ready := m.actionOptions()
		if !ok || !ready {
			return m, nil
		}
		command := actions.GetCommand(hit.Pod, m.svc.ActionOptionsFor(m.timeRange))
		return m, func() tea.Msg {
			if err := actions.Copy(command); err != nil {
				return statusMsg{text: "copy", err: err}
			}
			return statusMsg{text: "kubectl command copied"}
		}
	case "r":
		return m, tea.Batch(m.spinner.Tick, m.refreshCmd())
	case "esc":
		m.status = ""
	}
	return m, nil
}

func (m Model) handleLogsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "l":
		m.logsMode = false
		return m, nil
	case "?":
		m.help = true
		return m, nil
	case "r":
		return m, m.loadLogsCmd()
	case "m":
		return m, m.toggleMetrics()
	case "s", "e":
		hit, ok, ready := m.actionOptions()
		if !ok || !ready {
			return m, nil
		}
		cmd := actions.Exec(hit.Pod, m.svc.ActionOptionsFor(m.timeRange))
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return actionDoneMsg{label: "shell", err: err}
		})
	case "up", "k":
		if m.logsOffset > 0 {
			m.logsOffset--
		}
	case "down", "j":
		if m.logsOffset < max(0, m.logsLineCount()-m.logsVisibleRows()) {
			m.logsOffset++
		}
	case "pgup":
		m.logsOffset = max(0, m.logsOffset-m.logsVisibleRows())
	case "pgdown":
		m.logsOffset = min(max(0, m.logsLineCount()-m.logsVisibleRows()), m.logsOffset+m.logsVisibleRows())
	case "g":
		m.logsOffset = 0
	case "G":
		m.logsOffset = max(0, m.logsLineCount()-m.logsVisibleRows())
	}
	return m, nil
}

func (m Model) handleMetricsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "m":
		m.metricsMode = false
		return m, nil
	case "?":
		m.help = true
		return m, nil
	case "r":
		return m, m.loadStatsCmd()
	case "l":
		m.metricsMode = false
		m.logsMode = true
		m.logsOffset = 0
		return m, m.loadLogsCmd()
	case "s", "e":
		hit, ok, ready := m.actionOptions()
		if !ok || !ready {
			return m, nil
		}
		cmd := actions.Exec(hit.Pod, m.svc.ActionOptionsFor(m.timeRange))
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return actionDoneMsg{label: "shell", err: err}
		})
	case "up", "k":
		m.metricsOffset = max(0, m.metricsOffset-1)
	case "down", "j":
		m.metricsOffset = min(m.metricsMaxOffset(), m.metricsOffset+1)
	case "pgup":
		m.metricsOffset = max(0, m.metricsOffset-m.metricsBodyHeight())
	case "pgdown":
		m.metricsOffset = min(m.metricsMaxOffset(), m.metricsOffset+m.metricsBodyHeight())
	case "g":
		m.metricsOffset = 0
	case "G":
		m.metricsOffset = m.metricsMaxOffset()
	}
	return m, nil
}
