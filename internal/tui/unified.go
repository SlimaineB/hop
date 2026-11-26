package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/plugin"
)

type UnifiedOptions struct {
	Registry *plugin.Registry
	Initial  string
	Limit    int
	Live     bool
	Tickets  bool
	OpenURL  func(url string) error
	OpenWeb  func(query string) (string, error)
}

type unifiedEntry struct {
	sectionID    string
	sectionTitle string
	sectionKind  string
	item         plugin.Item
}

type UnifiedModel struct {
	registry *plugin.Registry
	opts     UnifiedOptions

	query string
	input textinput.Model

	spinner spinner.Model
	width   int
	height  int

	sections []plugin.Result
	entries  []unifiedEntry
	errors   []string
	cursor   int
	offset   int
	loading  bool
	live     bool
	err      error
	status   string
	editing  bool
	help     bool

	seq      int
	localSeq int
	tickSeq  int
}

type unifiedSearchMsg struct {
	seq   int
	local bool
	resp  plugin.Response
	err   error
}

type unifiedTickMsg struct{ seq int }

func NewUnified(opts UnifiedOptions) UnifiedModel {
	input := textinput.New()
	input.Placeholder = "api prod · git hop · pkg nginx · OPS-123"
	input.Prompt = "› "
	input.CharLimit = 256
	input.Width = 72

	if opts.Limit <= 0 {
		opts.Limit = 60
	}
	model := UnifiedModel{
		registry: opts.Registry,
		opts:     opts,
		query:    strings.TrimSpace(opts.Initial),
		input:    input,
		spinner:  newSpinner(),
		width:    130,
		height:   34,
		live:     opts.Live,
	}
	model.input.SetValue(model.query)
	model.input.CursorEnd()
	return model
}

func (m UnifiedModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, m.searchCmd()}
	if m.live {
		cmds = append(cmds, m.scheduleTick())
	}
	return tea.Batch(cmds...)
}

func (m UnifiedModel) searchCmd() tea.Cmd {
	if m.registry == nil {
		return nil
	}
	seq := m.seq
	query := m.query
	request := m.queryFor(query)
	reg := m.registry
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp := reg.Search(ctx, request)
		return unifiedSearchMsg{seq: seq, resp: resp}
	}
}

func (m UnifiedModel) localSearchCmd() tea.Cmd {
	if m.registry == nil {
		return nil
	}
	seq := m.localSeq
	query := m.query
	if m.editing {
		query = strings.TrimSpace(m.input.Value())
	}
	request := m.queryFor(query)
	request.Local = true
	reg := m.registry
	return func() tea.Msg {
		resp := reg.Search(context.Background(), request)
		return unifiedSearchMsg{seq: seq, local: true, resp: resp}
	}
}

func (m UnifiedModel) queryFor(raw string) plugin.Query {
	return plugin.Query{
		Raw:            raw,
		Limit:          m.opts.Limit,
		IncludeNetwork: m.opts.Tickets || plugin.HasSelector(raw, "ticket", "tickets", "issue", "issues"),
	}
}

func (m UnifiedModel) scheduleTick() tea.Cmd {
	m.tickSeq++
	seq := m.tickSeq
	return tea.Tick(2500*time.Millisecond, func(time.Time) tea.Msg {
		return unifiedTickMsg{seq: seq}
	})
}

func (m UnifiedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case unifiedSearchMsg:
		if msg.local {
			if msg.seq != m.localSeq {
				return m, nil
			}
		} else {
			if msg.seq != m.seq {
				return m, nil
			}
			m.loading = false
		}
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.apply(msg.resp)
		return m, nil

	case unifiedTickMsg:
		if msg.seq != m.tickSeq || !m.live {
			return m, nil
		}
		cmds := []tea.Cmd{m.scheduleTick()}
		if !m.loading {
			cmds = append(cmds, m.localSearchCmd())
		}
		return m, tea.Batch(cmds...)

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
			m.status = msg.label
		}
		return m, nil
	}
	return m, nil
}

func (m *UnifiedModel) apply(resp plugin.Response) {
	m.sections = resp.Sections
	m.errors = resp.Errors
	entries := make([]unifiedEntry, 0, resp.Total)
	for _, section := range resp.Sections {
		for _, item := range section.Items {
			entries = append(entries, unifiedEntry{
				sectionID:    section.ID,
				sectionTitle: section.Title,
				sectionKind:  section.Kind,
				item:         item,
			})
		}
	}
	m.entries = entries
	if m.cursor >= len(m.entries) {
		m.cursor = max(0, len(m.entries)-1)
	}
	m.ensureVisible()
}

func (m UnifiedModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			m.editing = false
			m.query = strings.TrimSpace(m.input.Value())
			m.input.SetValue(m.query)
			m.localSeq++
			m.seq++
			m.loading = true
			m.status = ""
			return m, tea.Batch(m.spinner.Tick, m.searchCmd())
		case "esc":
			m.editing = false
			m.input.SetValue(m.query)
			m.input.CursorEnd()
			m.localSeq++
			return m, m.localSearchCmd()
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
		m.editing = true
		m.input.Focus()
		m.input.SetValue(m.query)
		m.input.CursorEnd()
		return m, nil
	case "up", "k":
		return m.moveCursor(-1)
	case "down", "j":
		return m.moveCursor(1)
	case "pgup":
		return m.moveCursor(-m.visibleRows())
	case "pgdown":
		return m.moveCursor(m.visibleRows())
	case "g":
		m.cursor = 0
		m.ensureVisible()
		return m, nil
	case "G":
		m.cursor = max(0, len(m.entries)-1)
		m.ensureVisible()
		return m, nil
	case "enter", "o":
		return m, m.openCmd()
	case "y":
		return m, m.copyCmd()
	case "w":
		return m, m.webCmd()
	case "l":
		m.live = !m.live
		if m.live {
			m.status = "live watch on"
			return m, tea.Batch(m.spinner.Tick, m.scheduleTick(), m.localSearchCmd())
		}
		m.status = "live watch off"
		return m, nil
	case "r":
		m.seq++
		m.localSeq++
		m.loading = true
		m.status = ""
		return m, tea.Batch(m.spinner.Tick, m.searchCmd())
	case "esc":
		m.status = ""
	}
	return m, nil
}

func (m UnifiedModel) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if len(m.entries) == 0 {
		return m, nil
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.entries) {
		next = len(m.entries) - 1
	}
	if next == m.cursor {
		return m, nil
	}
	m.cursor = next
	m.ensureVisible()
	return m, nil
}

func (m *UnifiedModel) ensureVisible() {
	rows := max(1, m.visibleRows())
	line := m.lineForEntry(m.cursor)
	if line < m.offset {
		m.offset = line
	}
	if line >= m.offset+rows {
		m.offset = line - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m UnifiedModel) lineForEntry(index int) int {
	line := 0
	section := ""
	for i, entry := range m.entries {
		if entry.sectionID != section {
			section = entry.sectionID
			line++
		}
		if i == index {
			return line
		}
		line++
	}
	return line
}

func (m UnifiedModel) visibleRows() int {
	height := max(20, m.height)
	return max(4, height-9)
}

func (m UnifiedModel) selected() (unifiedEntry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return unifiedEntry{}, false
	}
	return m.entries[m.cursor], true
}

func (m UnifiedModel) openCmd() tea.Cmd {
	entry, ok := m.selected()
	if !ok {
		return nil
	}
	if entry.item.URL == "" {
		if entry.item.Copy != "" {
			return m.copyCmd()
		}
		m.status = "no URL for this result"
		return nil
	}
	open := m.opts.OpenURL
	if open == nil {
		open = actions.OpenBrowser
	}
	target := entry.item.URL
	return func() tea.Msg {
		return actionDoneMsg{label: "opened " + target, err: open(target)}
	}
}

func (m UnifiedModel) copyCmd() tea.Cmd {
	entry, ok := m.selected()
	if !ok {
		return nil
	}
	value := entry.item.Copy
	if value == "" {
		value = entry.item.URL
	}
	if value == "" {
		m.status = "nothing to copy"
		return nil
	}
	return func() tea.Msg {
		if err := actions.Copy(value); err != nil {
			return statusMsg{text: "copy", err: err}
		}
		return statusMsg{text: "copied " + clip(value, 48)}
	}
}

func (m UnifiedModel) webCmd() tea.Cmd {
	if m.opts.OpenWeb == nil {
		m.status = "web UI unavailable"
		return nil
	}
	open := m.opts.OpenWeb
	query := m.query
	return func() tea.Msg {
		target, err := open(query)
		if err != nil {
			return actionDoneMsg{label: "web", err: err}
		}
		return actionDoneMsg{label: "web " + target}
	}
}

func (m UnifiedModel) View() string {
	if m.help {
		return m.helpView()
	}
	width := max(60, m.width)
	height := max(20, m.height)
	header := m.headerView(width)
	footer := m.footerView(width)
	bodyHeight := height - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	listWidth := width * 56 / 100
	if listWidth < 46 {
		listWidth = 46
	}
	if listWidth > width-34 {
		listWidth = width - 34
	}
	if listWidth < 30 {
		listWidth = 30
	}
	previewWidth := width - listWidth - 1
	if previewWidth < 30 {
		previewWidth = 30
		listWidth = max(28, width-previewWidth-1)
	}
	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.listView(listWidth, bodyHeight),
		" ",
		m.previewView(previewWidth, bodyHeight),
	)
	return strings.Join([]string{header, body, footer}, "\n")
}

func (m UnifiedModel) headerView(width int) string {
	title := styleTitle.Render("hop") + styleSubtitle.Render("  ·  cockpit")
	var queryLine string
	if m.editing {
		queryLine = m.input.View()
	} else if m.query == "" {
		queryLine = stylePrompt.Render("› ") + styleMuted.Render("(everything)") + styleDim.Render("   / to edit")
	} else {
		queryLine = stylePrompt.Render("› ") + styleQuery.Render(clip(m.query, max(8, width-26))) + styleDim.Render("   / to edit")
	}
	sections := make([]string, 0, len(m.sections))
	for _, section := range m.sections {
		sections = append(sections, fmt.Sprintf("%s %d", strings.ToLower(section.Title), len(section.Items)))
	}
	parts := []string{}
	switch {
	case m.err != nil:
		parts = append(parts, styleErr.Render("error: "+clip(m.err.Error(), max(24, width/2))))
	case m.loading:
		parts = append(parts, m.spinner.View()+" searching")
	default:
		parts = append(parts, fmt.Sprintf("%d results", len(m.entries)))
	}
	if len(sections) > 0 {
		parts = append(parts, styleDim.Render(clip(strings.Join(sections, " · "), max(20, width/2))))
	}
	if m.live {
		parts = append(parts, styleOK.Render("● live"))
	}
	if len(m.errors) > 0 {
		parts = append(parts, styleWarn.Render(fmt.Sprintf("%d source errors", len(m.errors))))
	}
	if m.status != "" {
		parts = append(parts, styleDim.Render(clip(m.status, max(18, width/3))))
	}
	return strings.Join([]string{title, queryLine, strings.Join(parts, styleDim.Render("  ·  "))}, "\n")
}

func (m UnifiedModel) listView(width, height int) string {
	rows := max(1, height-2)
	lines := make([]string, 0, len(m.entries)+len(m.sections))
	section := ""
	entryIndex := 0
	for _, entry := range m.entries {
		if entry.sectionID != section {
			section = entry.sectionID
			lines = append(lines, styleSection.Render(clip(strings.ToUpper(entry.sectionTitle), width-4)))
		}
		line := padRight(clip(entry.item.Title, max(14, width/3)), max(14, width/3))
		meta := entry.item.Subtitle
		if meta == "" {
			meta = entry.item.Description
		}
		if meta != "" {
			line += " " + styleMuted.Render(clip(meta, max(8, width/3)))
		}
		if entry.item.Badge != "" {
			line += " " + styleDim.Render(clip(entry.item.Badge, max(6, width-len(line)-2)))
		}
		if entryIndex == m.cursor {
			lines = append(lines, styleSelected.Render(" "+clip(line, width-4)))
		} else {
			lines = append(lines, " "+line)
		}
		entryIndex++
	}
	if len(m.entries) == 0 {
		if m.loading {
			lines = append(lines, styleMuted.Render("  searching..."))
		} else {
			lines = append(lines, styleMuted.Render("  no results"))
		}
	}
	if m.offset > len(lines) {
		m.offset = len(lines)
	}
	visible := lines[m.offset:]
	if len(visible) > rows {
		visible = visible[:rows]
	}
	content := []string{styleSection.Render(clip(fmt.Sprintf("RESULTS  %d", len(m.entries)), width-4))}
	content = append(content, visible...)
	for len(content) < rows {
		content = append(content, "")
	}
	return styleBox.Width(width - 2).Height(height).Render(strings.Join(content, "\n"))
}

func (m UnifiedModel) previewView(width, height int) string {
	entry, ok := m.selected()
	if !ok {
		return styleBox.Width(width - 2).Height(height).Render(styleMuted.Render("select a result"))
	}
	item := entry.item
	lines := []string{
		styleTitle.Render(item.Title) + " " + styleDim.Render("· "+entry.sectionKind),
		styleMuted.Render(dashTUI(item.Subtitle)),
	}
	if item.Badge != "" {
		lines = append(lines, styleSection.Render(strings.ToUpper(item.Badge)))
	}
	if item.Description != "" {
		lines = append(lines, "", styleMuted.Render(clip(item.Description, width-6)))
	}
	if len(item.Meta) > 0 {
		lines = append(lines, "", styleSection.Render("DETAILS"))
		keys := make([]string, 0, len(item.Meta))
		for key := range item.Meta {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := item.Meta[key]
			if value == "" {
				continue
			}
			lines = append(lines, styleMuted.Render(padRight(key, 10))+" "+clip(value, width-16))
		}
	}
	if item.URL != "" {
		lines = append(lines, "", styleSection.Render("OPEN"), styleQuery.Render(clip(item.URL, width-6)))
	}
	if item.Copy != "" {
		lines = append(lines, "", styleSection.Render("COPY"), styleMuted.Render(clip(item.Copy, width-6)))
	}
	if len(m.errors) > 0 {
		lines = append(lines, "", styleWarn.Render(fmt.Sprintf("%d source errors (see hop all --json)", len(m.errors))))
	}
	lines = append(lines, "")
	lines = append(lines, styleDim.Render("enter open · y copy · w web · l live · r reload"))
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return styleBox.Width(width - 2).Height(height).Render(strings.Join(lines, "\n"))
}

func (m UnifiedModel) footerView(width int) string {
	keys := [][2]string{
		{"↑↓", "move"}, {"/", "search"}, {"enter", "open"}, {"y", "copy"},
		{"w", "web"}, {"l", "live"}, {"r", "reload"}, {"?", "help"}, {"q", "quit"},
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, styleHelpKey.Render(key[0])+" "+styleDim.Render(key[1]))
	}
	return styleDim.Render(strings.Repeat("─", max(1, width))) + "\n" + clip(strings.Join(parts, "  "), width)
}

func (m UnifiedModel) helpView() string {
	lines := []string{
		styleTitle.Render("hop · cockpit"),
		"",
		styleHelpKey.Render("↑↓ / j k") + styleDim.Render("     move selection"),
		styleHelpKey.Render("/ or i") + styleDim.Render("      edit the search query"),
		styleHelpKey.Render("enter / o") + styleDim.Render("   open the selected URL"),
		styleHelpKey.Render("y") + styleDim.Render("           copy the selected item or URL"),
		styleHelpKey.Render("w") + styleDim.Render("           open the web UI at this query"),
		styleHelpKey.Render("l") + styleDim.Render("           toggle live watch (local snapshots)"),
		styleHelpKey.Render("r") + styleDim.Render("           refresh all sources"),
		styleHelpKey.Render("g / G") + styleDim.Render("       first / last"),
		styleHelpKey.Render("q") + styleDim.Render("           quit"),
		"",
		styleDim.Render("Selectors: vm git pkg ticket link. Example: hop all git hop"),
	}
	return styleBox.Width(max(60, m.width-2)).Render(strings.Join(lines, "\n"))
}
