package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/config"
)

var linkFieldLabels = []string{"name", "url", "keywords", "description"}

type LinkOptions struct {
	Shortcuts []config.ShortcutConfig
	Initial   string
	OpenURL   func(string) error
	CopyText  func(string) error
	Save      func(config.ShortcutConfig) error
	Remove    func(string) error
	Reload    func() ([]config.ShortcutConfig, error)
}

type linkForm struct {
	originalName string
	inputs       []textinput.Model
	focus        int
	err          string
}

func newLinkForm(shortcut *config.ShortcutConfig) *linkForm {
	values := []string{"", "", "", ""}
	if shortcut != nil {
		values[0] = shortcut.Name
		values[1] = shortcut.URL
		values[2] = strings.Join(shortcut.Keywords, ", ")
		values[3] = shortcut.Description
	}
	limits := []int{80, 500, 300, 240}
	placeholders := []string{"ex: grafana", "ex: https://grafana.example.com", "ex: metrics, dashboards", "ex: observability dashboards"}
	inputs := make([]textinput.Model, len(linkFieldLabels))
	for i := range inputs {
		input := textinput.New()
		input.Prompt = ""
		input.Placeholder = placeholders[i]
		input.CharLimit = limits[i]
		input.Width = 72
		input.SetValue(values[i])
		inputs[i] = input
	}
	form := &linkForm{inputs: inputs}
	if shortcut != nil {
		form.originalName = shortcut.Name
	}
	form.focusField(0)
	return form
}

func (f *linkForm) focusField(index int) {
	if index < 0 {
		index = len(f.inputs) - 1
	}
	if index >= len(f.inputs) {
		index = 0
	}
	f.focus = index
	for i := range f.inputs {
		if i == index {
			f.inputs[i].Focus()
			f.inputs[i].CursorEnd()
		} else {
			f.inputs[i].Blur()
		}
	}
}

func (f *linkForm) resize(width int) {
	for i := range f.inputs {
		f.inputs[i].Width = max(24, min(100, width-30))
	}
}

type LinkModel struct {
	opts LinkOptions

	all   []config.ShortcutConfig
	hits  []config.ShortcutHit
	query string
	input textinput.Model

	width  int
	height int

	cursor int
	offset int

	editing       bool
	help          bool
	form          *linkForm
	confirmDelete bool
	status        string
}

func NewLinks(opts LinkOptions) LinkModel {
	if opts.OpenURL == nil {
		opts.OpenURL = actions.OpenBrowser
	}
	if opts.CopyText == nil {
		opts.CopyText = actions.Copy
	}
	input := textinput.New()
	input.Placeholder = "filter shortcuts"
	input.Prompt = "› "
	input.CharLimit = 200
	input.Width = 72

	model := LinkModel{
		opts:   opts,
		all:    append([]config.ShortcutConfig(nil), opts.Shortcuts...),
		query:  strings.TrimSpace(opts.Initial),
		input:  input,
		width:  110,
		height: 32,
	}
	model.input.SetValue(model.query)
	model.input.CursorEnd()
	model.applyFilter("")
	return model
}

func (m LinkModel) Init() tea.Cmd {
	return nil
}

func (m LinkModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(24, min(84, m.width-16))
		if m.form != nil {
			m.form.resize(m.width)
		}
		m.ensureVisible()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case statusMsg:
		if msg.err != nil {
			m.status = msg.text + ": " + msg.err.Error()
		} else {
			m.status = msg.text
		}
		return m, nil
	}
	return m, nil
}

func (m LinkModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.form != nil {
		return m.handleFormKey(msg)
	}
	if m.help {
		switch key {
		case "esc", "?", "q", "enter":
			m.help = false
		}
		return m, nil
	}
	if m.confirmDelete {
		switch key {
		case "y", "enter":
			return m.deleteSelected()
		case "n", "esc":
			m.confirmDelete = false
			m.status = ""
		}
		return m, nil
	}
	if m.editing {
		switch key {
		case "enter", "tab":
			m.editing = false
			m.input.Blur()
			m.query = strings.TrimSpace(m.input.Value())
			m.input.SetValue(m.query)
			m.applyFilter("")
			return m, nil
		case "esc":
			m.editing = false
			m.input.Blur()
			m.input.SetValue(m.query)
			m.applyFilter("")
			return m, nil
		}
		before := m.input.Value()
		focus := m.selectedName()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != before {
			m.query = strings.TrimSpace(m.input.Value())
			m.applyFilter(focus)
		}
		return m, cmd
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.help = true
	case "/", "i":
		m.editing = true
		m.input.Focus()
		m.input.SetValue(m.query)
		m.input.CursorEnd()
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.visibleRows())
	case "pgdown":
		m.moveCursor(m.visibleRows())
	case "g":
		m.cursor = 0
		m.ensureVisible()
	case "G":
		m.cursor = max(0, len(m.hits)-1)
		m.ensureVisible()
	case "enter", "o":
		return m, m.openCmd()
	case "y":
		return m, m.copyCmd()
	case "a":
		m.form = newLinkForm(nil)
		m.form.resize(m.width)
		m.status = ""
	case "e":
		if hit, ok := m.selected(); ok {
			shortcut := hit.Shortcut
			m.form = newLinkForm(&shortcut)
			m.form.resize(m.width)
			m.status = ""
		}
	case "d":
		if _, ok := m.selected(); ok {
			m.confirmDelete = true
			m.status = ""
		}
	case "r":
		m.reload(m.selectedName())
	case "esc":
		m.status = ""
	}
	return m, nil
}

func (m LinkModel) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.form = nil
		m.status = ""
		return m, nil
	case "tab", "down":
		m.form.focusField(m.form.focus + 1)
		return m, nil
	case "shift+tab", "up":
		m.form.focusField(m.form.focus - 1)
		return m, nil
	case "enter":
		return m.saveForm()
	}
	var cmd tea.Cmd
	input := m.form.inputs[m.form.focus]
	input, cmd = input.Update(msg)
	m.form.inputs[m.form.focus] = input
	m.form.err = ""
	return m, cmd
}

func (m LinkModel) saveForm() (tea.Model, tea.Cmd) {
	form := m.form
	name := strings.TrimSpace(form.inputs[0].Value())
	rawURL := strings.TrimSpace(form.inputs[1].Value())
	if name == "" {
		form.err = "shortcut name is required"
		form.focusField(0)
		return m, nil
	}
	if !config.ValidShortcutURL(rawURL) {
		form.err = "URL must be a valid http(s) address"
		form.focusField(1)
		return m, nil
	}
	if m.opts.Save == nil {
		form.err = "saving is not available"
		return m, nil
	}
	shortcut := config.ShortcutConfig{
		Name:        name,
		URL:         rawURL,
		Keywords:    splitLinkKeywords(form.inputs[2].Value()),
		Description: strings.TrimSpace(form.inputs[3].Value()),
	}
	if err := m.opts.Save(shortcut); err != nil {
		form.err = err.Error()
		return m, nil
	}
	warning := ""
	if form.originalName != "" && form.originalName != name {
		if m.opts.Remove != nil {
			if err := m.opts.Remove(form.originalName); err != nil {
				warning = "renamed, but old entry not removed: " + err.Error()
			}
		}
	}
	m.form = nil
	m.status = "saved " + name
	if m.opts.Reload == nil {
		m.upsertLocal(shortcut)
		if form.originalName != "" && form.originalName != name {
			m.removeLocal(form.originalName)
		}
	}
	m.reload(name)
	if warning != "" {
		m.status = warning
	}
	return m, nil
}

func (m LinkModel) deleteSelected() (tea.Model, tea.Cmd) {
	hit, ok := m.selected()
	m.confirmDelete = false
	if !ok {
		return m, nil
	}
	name := hit.Shortcut.Name
	if m.opts.Remove == nil {
		m.status = "delete is not available"
		return m, nil
	}
	if err := m.opts.Remove(name); err != nil {
		m.status = "delete: " + err.Error()
		return m, nil
	}
	m.status = "deleted " + name
	if m.opts.Reload == nil {
		m.removeLocal(name)
		m.applyFilter("")
		return m, nil
	}
	m.reload("")
	return m, nil
}

func (m LinkModel) openCmd() tea.Cmd {
	hit, ok := m.selected()
	if !ok {
		return nil
	}
	name := hit.Shortcut.Name
	url := hit.Shortcut.URL
	if !config.ValidShortcutURL(url) {
		return func() tea.Msg {
			return statusMsg{text: "open " + name, err: fmt.Errorf("invalid URL %s", url)}
		}
	}
	open := m.opts.OpenURL
	return func() tea.Msg {
		if err := open(url); err != nil {
			return statusMsg{text: "open " + name, err: err}
		}
		return statusMsg{text: "opened " + name}
	}
}

func (m LinkModel) copyCmd() tea.Cmd {
	hit, ok := m.selected()
	if !ok {
		return nil
	}
	url := hit.Shortcut.URL
	copyText := m.opts.CopyText
	return func() tea.Msg {
		if err := copyText(url); err != nil {
			return statusMsg{text: "copy URL", err: err}
		}
		return statusMsg{text: "copied " + url}
	}
}

func (m *LinkModel) reload(focus string) {
	if m.opts.Reload == nil {
		m.applyFilter(focus)
		return
	}
	shortcuts, err := m.opts.Reload()
	if err != nil {
		m.status = "reload: " + err.Error()
		m.applyFilter(focus)
		return
	}
	m.all = shortcuts
	m.applyFilter(focus)
}

func (m *LinkModel) upsertLocal(shortcut config.ShortcutConfig) {
	for i := range m.all {
		if m.all[i].Name == shortcut.Name {
			m.all[i] = shortcut
			return
		}
	}
	m.all = append(m.all, shortcut)
}

func (m *LinkModel) removeLocal(name string) {
	for i := range m.all {
		if m.all[i].Name == name {
			m.all = append(m.all[:i], m.all[i+1:]...)
			return
		}
	}
}

func (m *LinkModel) applyFilter(focus string) {
	m.hits = config.MatchShortcuts(m.all, strings.Fields(m.query))
	if focus != "" {
		for i, hit := range m.hits {
			if hit.Shortcut.Name == focus {
				m.cursor = i
				break
			}
		}
	}
	if m.cursor >= len(m.hits) {
		m.cursor = max(0, len(m.hits)-1)
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.ensureVisible()
}

func (m LinkModel) selected() (config.ShortcutHit, bool) {
	if m.cursor < 0 || m.cursor >= len(m.hits) {
		return config.ShortcutHit{}, false
	}
	return m.hits[m.cursor], true
}

func (m LinkModel) selectedName() string {
	hit, ok := m.selected()
	if !ok {
		return ""
	}
	return hit.Shortcut.Name
}

func (m *LinkModel) moveCursor(delta int) {
	if len(m.hits) == 0 {
		return
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.hits) {
		next = len(m.hits) - 1
	}
	m.cursor = next
	m.ensureVisible()
}

func (m *LinkModel) ensureVisible() {
	rows := max(1, m.visibleRows())
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

func (m LinkModel) visibleRows() int {
	return max(4, max(20, m.height)-8)
}

func (m LinkModel) View() string {
	if m.form != nil {
		return m.formView()
	}
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
	listWidth := width * 50 / 100
	if listWidth < 42 {
		listWidth = 42
	}
	if listWidth > width-36 {
		listWidth = width - 36
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

func (m LinkModel) headerView(width int) string {
	title := styleTitle.Render("hop") + styleSubtitle.Render("  ·  browser shortcuts")
	var queryLine string
	switch {
	case m.editing:
		queryLine = m.input.View()
	case m.query == "":
		queryLine = stylePrompt.Render("› ") + styleMuted.Render("(all shortcuts)") + styleDim.Render("   / to filter")
	default:
		queryLine = stylePrompt.Render("› ") + styleQuery.Render(clip(m.query, max(8, width-28))) + styleDim.Render("   / to filter")
	}
	parts := []string{fmt.Sprintf("%d/%d shortcuts", len(m.hits), len(m.all))}
	if m.confirmDelete {
		parts = append(parts, styleWarn.Render("delete "+m.selectedName()+"? y/N"))
	}
	if m.status != "" {
		parts = append(parts, styleDim.Render(clip(m.status, max(24, width/2))))
	}
	if len(parts) == 0 {
		parts = append(parts, styleDim.Render("ready"))
	}
	return strings.Join([]string{title, queryLine, strings.Join(parts, styleDim.Render("  ·  "))}, "\n")
}

func (m LinkModel) listView(width, height int) string {
	rows := max(1, height-2)
	content := []string{styleSection.Render(clip(fmt.Sprintf("SHORTCUTS  %d", len(m.hits)), width-4))}
	if len(m.hits) == 0 {
		if m.query == "" {
			content = append(content,
				styleMuted.Render("  no shortcuts"),
				styleDim.Render("  press a to add one"),
			)
		} else {
			content = append(content, styleMuted.Render("  no shortcuts match this filter"))
		}
	}
	offset := m.offset
	if offset > len(m.hits) {
		offset = len(m.hits)
	}
	nameWidth := max(12, width*35/100)
	for i := offset; i < len(m.hits) && len(content) < rows; i++ {
		shortcut := m.hits[i].Shortcut
		marker := "  "
		if i == m.cursor {
			marker = "› "
		}
		meta := strings.Join(shortcut.Keywords, ",")
		if meta == "" {
			meta = shortcut.Description
		}
		if meta == "" {
			meta = shortcut.URL
		}
		meta = clip(meta, max(8, width-nameWidth-6))
		if i == m.cursor {
			plain := marker + padRight(clip(shortcut.Name, nameWidth), nameWidth) + " " + meta
			content = append(content, styleSelected.Render(" "+clip(plain, width-5)))
		} else {
			line := marker + padRight(clip(shortcut.Name, nameWidth), nameWidth) + " " + styleMuted.Render(meta)
			content = append(content, " "+line)
		}
	}
	for len(content) < rows {
		content = append(content, "")
	}
	if len(content) > rows {
		content = content[:rows]
	}
	return styleBox.Width(width - 2).Height(rows).Render(strings.Join(content, "\n"))
}

func (m LinkModel) previewView(width, height int) string {
	rows := max(1, height-2)
	hit, ok := m.selected()
	if !ok {
		content := []string{
			styleSection.Render(clip("PREVIEW", width-4)),
			styleMuted.Render("  select a shortcut with ↑↓ / j k"),
			styleDim.Render("  a adds a new shortcut"),
		}
		for len(content) < rows {
			content = append(content, "")
		}
		return styleBox.Width(width - 2).Height(rows).Render(strings.Join(content, "\n"))
	}
	shortcut := hit.Shortcut
	keywords := strings.Join(shortcut.Keywords, " · ")
	if keywords == "" {
		keywords = "-"
	}
	description := shortcut.Description
	if description == "" {
		description = "-"
	}
	content := []string{
		styleSection.Render(clip("PREVIEW", width-4)),
		styleTitle.Render(clip(shortcut.Name, width-4)),
		"",
		styleMuted.Render("url     ") + styleQuery.Render(clip(shortcut.URL, max(10, width-12))),
		styleMuted.Render("keywords ") + clip(keywords, max(10, width-12)),
		styleMuted.Render("about    ") + clip(description, max(10, width-12)),
		"",
		styleDim.Render("enter open · y copy URL · e edit · d delete"),
	}
	for len(content) < rows {
		content = append(content, "")
	}
	if len(content) > rows {
		content = content[:rows]
	}
	return styleBox.Width(width - 2).Height(rows).Render(strings.Join(content, "\n"))
}

func (m LinkModel) footerView(width int) string {
	keys := [][2]string{
		{"↑↓", "move"}, {"/", "filter"}, {"enter", "open"}, {"y", "copy"},
		{"e", "edit"}, {"a", "add"}, {"d", "delete"}, {"r", "reload"},
		{"?", "help"}, {"q", "quit"},
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, styleHelpKey.Render(key[0])+" "+styleDim.Render(key[1]))
	}
	footer := strings.Join(parts, "  ")
	return styleDim.Render(strings.Repeat("─", max(1, width))) + "\n" + lipgloss.NewStyle().MaxWidth(max(1, width)).Render(footer)
}

func (m LinkModel) formView() string {
	width := max(60, m.width)
	title := "add shortcut"
	if m.form.originalName != "" {
		title = "edit " + m.form.originalName
	}
	lines := []string{styleTitle.Render("hop · " + title), ""}
	for i, label := range linkFieldLabels {
		marker := "  "
		labelStyle := styleMuted
		if i == m.form.focus {
			marker = "› "
			labelStyle = styleQuery
		}
		lines = append(lines, labelStyle.Render(marker+padRight(label, 12))+" "+m.form.inputs[i].View())
	}
	if m.form.err != "" {
		lines = append(lines, "", styleErr.Render(m.form.err))
	}
	lines = append(lines, "", styleDim.Render("tab next · shift+tab previous · enter save · esc cancel"))
	return styleBox.Width(max(60, min(140, width-4))).Render(strings.Join(lines, "\n"))
}

func (m LinkModel) helpView() string {
	lines := []string{
		styleTitle.Render("hop · browser shortcuts"),
		"",
		styleHelpKey.Render("↑↓ / j k") + styleDim.Render("     move selection"),
		styleHelpKey.Render("/ or i") + styleDim.Render("      filter shortcuts"),
		styleHelpKey.Render("enter / o") + styleDim.Render("   open in the browser"),
		styleHelpKey.Render("y") + styleDim.Render("           copy the URL"),
		styleHelpKey.Render("e") + styleDim.Render("           edit the selected shortcut"),
		styleHelpKey.Render("a") + styleDim.Render("           add a shortcut"),
		styleHelpKey.Render("d") + styleDim.Render("           delete the selected shortcut"),
		styleHelpKey.Render("r") + styleDim.Render("           reload the config"),
		styleHelpKey.Render("g / G") + styleDim.Render("       first / last"),
		styleHelpKey.Render("q") + styleDim.Render("           quit"),
		"",
		styleDim.Render("Shortcuts are stored in the hop config under 'shortcuts:'."),
	}
	return styleBox.Width(max(60, m.width-2)).Render(strings.Join(lines, "\n"))
}

func splitLinkKeywords(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		key := strings.ToLower(field)
		if field == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, field)
	}
	return out
}
