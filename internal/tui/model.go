package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/search"
)

type Options struct {
	Service      *app.Service
	Initial      string
	Contexts     []string
	Namespaces   []string
	Env          string
	Limit        int
	ForceRefresh bool
	OpenWeb      func(ref kube.PodRef, query string) (string, error)
}

type Model struct {
	svc  *app.Service
	opts Options

	query string
	input textinput.Model

	spinner spinner.Model
	width   int
	height  int

	result    *app.SearchResult
	hits      []app.HitView
	cursor    int
	offset    int
	loading   bool
	err       error
	timeRange *search.TimeRange
	status    string

	detail        *app.Detail
	detailErr     error
	detailLoading bool
	detailID      string
	detailSeq     int

	searchSeq int
	localSeq  int
	editing   bool
	help      bool
}

type searchMsg struct {
	seq    int
	local  bool
	result *app.SearchResult
	err    error
}

type detailMsg struct {
	seq    int
	id     string
	detail *app.Detail
	err    error
}

type statusMsg struct {
	text string
	err  error
}

type actionDoneMsg struct {
	label string
	err   error
}

func New(opts Options) Model {
	input := textinput.New()
	input.Placeholder = "pod ns:dev env:prod 12h 14h"
	input.Prompt = "› "
	input.CharLimit = 256
	input.Width = 72

	model := Model{
		svc:     opts.Service,
		opts:    opts,
		query:   strings.TrimSpace(opts.Initial),
		input:   input,
		spinner: newSpinner(),
		width:   120,
		height:  32,
	}
	model.input.SetValue(model.query)
	model.input.CursorEnd()
	return model
}

func newSpinner() spinner.Model {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(stylePrompt))
	return sp
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.searchCmd(false))
}

func (m Model) searchCmd(force bool) tea.Cmd {
	if m.svc == nil {
		return nil
	}
	seq := m.searchSeq
	req := app.SearchRequest{
		Query:        m.query,
		Contexts:     m.opts.Contexts,
		Namespaces:   m.opts.Namespaces,
		Env:          m.opts.Env,
		Limit:        m.opts.Limit,
		ForceRefresh: force || m.opts.ForceRefresh,
	}
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		result, err := svc.Search(ctx, req)
		return searchMsg{seq: seq, result: result, err: err}
	}
}

func (m Model) localSearchCmd() tea.Cmd {
	if m.svc == nil {
		return nil
	}
	query := m.query
	if m.editing {
		query = strings.TrimSpace(m.input.Value())
	}
	seq := m.localSeq
	req := app.SearchRequest{
		Query:      query,
		Contexts:   m.opts.Contexts,
		Namespaces: m.opts.Namespaces,
		Env:        m.opts.Env,
		Limit:      m.opts.Limit,
	}
	svc := m.svc
	return func() tea.Msg {
		result, err := svc.SearchLocal(req)
		if errors.Is(err, app.ErrNotReady) {
			return nil
		}
		return searchMsg{seq: seq, local: true, result: result, err: err}
	}
}

func (m *Model) loadDetail() tea.Cmd {
	hit, ok := m.selected()
	if !ok {
		m.detail = nil
		m.detailErr = nil
		m.detailLoading = false
		m.detailID = ""
		return nil
	}
	if m.svc == nil {
		return nil
	}
	id := podKey(hit.Pod)
	m.detailSeq++
	seq := m.detailSeq
	m.detailID = id
	m.detailLoading = true
	m.detail = nil
	m.detailErr = nil
	ref := hit.Pod.Ref()
	rng := m.timeRange
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		detail, err := svc.Detail(ctx, ref, rng)
		return detailMsg{seq: seq, id: id, detail: detail, err: err}
	}
}

func (m *Model) applyResult(result *app.SearchResult) {
	m.result = result
	m.hits = result.Hits
	m.timeRange = result.Query.TimeRange
	if m.cursor >= len(m.hits) {
		m.cursor = max(0, len(m.hits)-1)
	}
	if len(m.hits) == 0 {
		m.detail = nil
		m.detailErr = nil
		m.detailLoading = false
		m.detailID = ""
	}
	m.ensureVisible()
}

func (m Model) selected() (app.HitView, bool) {
	if m.cursor < 0 || m.cursor >= len(m.hits) {
		return app.HitView{}, false
	}
	return m.hits[m.cursor], true
}

func (m Model) selectedKey() string {
	hit, ok := m.selected()
	if !ok {
		return ""
	}
	return podKey(hit.Pod)
}

func (m Model) actionOptions() (app.HitView, bool, bool) {
	hit, ok := m.selected()
	if !ok || m.svc == nil {
		return app.HitView{}, false, false
	}
	return hit, true, true
}

func (m Model) visibleRows() int {
	rows := m.height - 12
	if rows < 3 {
		rows = 3
	}
	return rows
}

func (m *Model) ensureVisible() {
	rows := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *Model) moveCursor(delta int) tea.Cmd {
	if len(m.hits) == 0 {
		return nil
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.hits) {
		next = len(m.hits) - 1
	}
	if next == m.cursor {
		return nil
	}
	m.cursor = next
	m.ensureVisible()
	return m.loadDetail()
}

func (m *Model) startEditing() {
	m.editing = true
	m.input.SetValue(m.query)
	m.input.CursorEnd()
	m.input.Focus()
}

func (m *Model) cancelEditing() tea.Cmd {
	m.editing = false
	m.input.Blur()
	m.input.SetValue(m.query)
	m.localSeq++
	return m.localSearchCmd()
}

func (m *Model) commitEditing() tea.Cmd {
	m.query = strings.TrimSpace(m.input.Value())
	m.input.SetValue(m.query)
	m.input.Blur()
	m.editing = false
	m.loading = true
	m.status = ""
	m.searchSeq++
	m.localSeq++
	return m.searchCmd(false)
}

func (m Model) openWebCmd() tea.Cmd {
	hit, ok := m.selected()
	if !ok {
		return nil
	}
	if m.opts.OpenWeb == nil {
		return func() tea.Msg {
			return statusMsg{text: "web launch not configured"}
		}
	}
	open := m.opts.OpenWeb
	ref := hit.Pod.Ref()
	query := m.query
	return func() tea.Msg {
		target, err := open(ref, query)
		if err != nil {
			if target != "" {
				return statusMsg{text: "web: " + target, err: err}
			}
			return statusMsg{text: "web", err: err}
		}
		if target != "" {
			return statusMsg{text: "web: " + target}
		}
		return statusMsg{text: "web opened"}
	}
}

func (m Model) refreshCmd() tea.Cmd {
	m.loading = true
	m.status = ""
	m.searchSeq++
	m.localSeq++
	return m.searchCmd(true)
}

func podKey(pod kube.Pod) string {
	return pod.Context + "\x1f" + pod.Namespace + "\x1f" + pod.Name
}

func rangeText(rng *search.TimeRange) string {
	if rng == nil || rng.From.IsZero() || rng.To.IsZero() {
		return ""
	}
	from := rng.From.Local()
	to := rng.To.Local()
	if from.Year() == to.Year() && from.YearDay() == to.YearDay() {
		return from.Format("15:04") + " → " + to.Format("15:04")
	}
	return from.Format("02/01 15:04") + " → " + to.Format("02/01 15:04")
}

func shortDuration(value time.Duration) string {
	if value <= 0 {
		return "0ms"
	}
	if value < time.Second {
		return fmt.Sprintf("%dms", value.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", value.Seconds())
}
