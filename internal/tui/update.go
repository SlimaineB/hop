package tui

import (
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
			return m, m.loadDetail()
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
			return m, tea.Batch(cmd, m.localSearchCmd())
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
	case "up", "k":
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
		return m, m.loadDetail()
	case "G":
		m.cursor = max(0, len(m.hits)-1)
		m.ensureVisible()
		return m, m.loadDetail()
	case "enter", "o":
		return m, m.openWebCmd()
	case "s":
		hit, ok, ready := m.actionOptions()
		if !ok || !ready {
			return m, nil
		}
		cmd := actions.K9s(hit.Pod, m.svc.ActionOptionsFor(m.timeRange))
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return actionDoneMsg{label: "k9s", err: err}
		})
	case "l":
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
