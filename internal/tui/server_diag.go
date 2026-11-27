package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const serverDiagInterval = 12 * time.Second

func (m *ServerModel) enterDiagMode() {
	m.diagMode = true
	m.diagLive = true
	m.diagOffset = 0
	m.diagErr = nil
}

func (m *ServerModel) startDiag() tea.Cmd {
	hit, ok := m.selected()
	if !ok || m.svc == nil || m.diagnostics == nil {
		m.diagLoading = false
		if !ok {
			m.status = "no server selected"
		}
		return nil
	}
	name := hit.Server.Name
	m.diagSeq++
	seq := m.diagSeq
	m.diagName = name
	m.diagLoading = true
	m.diagErr = nil
	m.diag = nil
	server := hit.Server
	svc := m.svc
	engine := m.diagnostics
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		check := svc.CheckServer(ctx, server)
		report := engine.DiagnoseServer(ctx, server, check, svc)
		return serverDiagMsg{seq: seq, name: name, report: report}
	}
}

func (m *ServerModel) scheduleDiagTick() tea.Cmd {
	m.diagTickSeq++
	seq := m.diagTickSeq
	return tea.Tick(serverDiagInterval, func(time.Time) tea.Msg {
		return serverDiagTickMsg{seq: seq}
	})
}

func (m ServerModel) handleDiagKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "t", "esc":
		m.diagMode = false
		m.diagTickSeq++
		return m, nil
	case "?":
		m.help = true
		return m, nil
	case "r", "c":
		return m, tea.Batch(m.spinner.Tick, m.startDiag())
	case "l":
		m.diagLive = !m.diagLive
		if m.diagLive {
			return m, tea.Batch(m.spinner.Tick, m.startDiag(), m.scheduleDiagTick())
		}
		m.diagTickSeq++
		m.status = "diagnostic live refresh paused"
		return m, nil
	case "up", "k":
		next, cmd := m.moveCursor(-1)
		m = next.(ServerModel)
		return m, tea.Batch(cmd, m.startDiag())
	case "down", "j":
		next, cmd := m.moveCursor(1)
		m = next.(ServerModel)
		return m, tea.Batch(cmd, m.startDiag())
	case "pgup", "shift+pgup":
		m.diagOffset = max(0, m.diagOffset-5)
		return m, nil
	case "pgdown", "shift+pgdown":
		m.diagOffset += 5
		return m, nil
	case "home":
		m.diagOffset = 0
		return m, nil
	case "end":
		if m.diag != nil {
			m.diagOffset = max(0, len(m.diag.Findings)-1)
		}
		return m, nil
	}
	return m, nil
}

func (m ServerModel) diagView() string {
	target := "no server selected"
	if hit, ok := m.selected(); ok {
		target = hit.Server.Name
		if hit.Server.Env != "" {
			target += "  ·  " + hit.Server.Env
		}
		if hit.Server.Host != "" {
			target += "  ·  " + hit.Server.Host
		}
	}
	hints := "↑↓ select VM · r/c refresh · l live on/off · pgup/pgdn scroll · t/esc back · ? help · q quit"
	if m.diagLive {
		hints += "  ·  live " + serverDiagInterval.String()
	}
	return diagScreenView("VM diagnostics", target, m.diag, m.diagLoading, m.diagErr, m.width, m.height, m.diagOffset, hints)
}
