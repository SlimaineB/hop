package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const podDiagInterval = 10 * time.Second

func (m *Model) enterDiagMode() {
	m.diagMode = true
	m.diagLive = true
	m.diagOffset = 0
	m.diagErr = nil
}

func (m *Model) startDiag() tea.Cmd {
	hit, ok := m.selected()
	if !ok || m.svc == nil || m.diagnostics == nil {
		m.diagLoading = false
		if !ok {
			m.status = "no pod selected"
		}
		return nil
	}
	id := m.selectedKey()
	m.diagSeq++
	seq := m.diagSeq
	m.diagID = id
	m.diagLoading = true
	m.diagErr = nil
	m.diag = nil
	ref := hit.Pod.Ref()
	rng := m.timeRange
	svc := m.svc
	engine := m.diagnostics
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		detail, err := svc.Detail(ctx, ref, rng)
		if err != nil {
			return diagMsg{seq: seq, id: id, err: err}
		}
		statsCtx, statsCancel := context.WithTimeout(ctx, 8*time.Second)
		stats, _ := svc.PodStats(statsCtx, ref)
		statsCancel()
		report := engine.DiagnosePod(ctx, detail.Pod, stats)
		return diagMsg{seq: seq, id: id, report: report}
	}
}

func (m *Model) scheduleDiagTick() tea.Cmd {
	m.diagTickSeq++
	seq := m.diagTickSeq
	return tea.Tick(podDiagInterval, func(time.Time) tea.Msg {
		return diagTickMsg{seq: seq}
	})
}

func (m Model) handleDiagKey(key string) (tea.Model, tea.Cmd) {
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
	case "r":
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
		cmd := m.moveCursor(-1)
		return m, tea.Batch(cmd, m.startDiag())
	case "down", "j":
		cmd := m.moveCursor(1)
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

func (m Model) diagView() string {
	target := "no pod selected"
	if hit, ok := m.selected(); ok {
		target = hit.Pod.Ref().String()
		if hit.Pod.Env != "" {
			target += "  ·  " + hit.Pod.Env
		}
	}
	hints := "↑↓ select pod · r refresh · l live on/off · pgup/pgdn scroll · t/esc back · ? help · q quit"
	if m.diagLive {
		hints += "  ·  live " + podDiagInterval.String()
	}
	return diagScreenView("pod diagnostics", target, m.diag, m.diagLoading, m.diagErr, m.width, m.height, m.diagOffset, hints)
}
