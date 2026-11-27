package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/diag"
	"github.com/SlimaineB/hop/internal/servers"
)

type ServerOptions struct {
	Service     *servers.Service
	Initial     string
	OpenWeb     func(name, query string) (string, error)
	Diagnostics *diag.Engine
}

type ServerModel struct {
	svc  *servers.Service
	opts ServerOptions

	query string
	input textinput.Model

	spinner spinner.Model
	width   int
	height  int

	hits    []servers.Hit
	cursor  int
	offset  int
	loading bool
	err     error
	editing bool
	help    bool
	status  string

	checks    map[string]servers.Check
	check     *servers.Check
	checkName string
	checkErr  error
	checking  bool
	checkSeq  int
	searchSeq int
	localSeq  int

	diagnostics *diag.Engine
	diagMode    bool
	diagLive    bool
	diag        *diag.Report
	diagLoading bool
	diagErr     error
	diagName    string
	diagSeq     int
	diagTickSeq int
	diagOffset  int
}

type serverSearchMsg struct {
	seq   int
	local bool
	hits  []servers.Hit
	err   error
}

type serverCheckTickMsg struct{ seq int }

type serverCheckMsg struct {
	seq    int
	name   string
	result servers.Check
	err    error
}

type serverDiagMsg struct {
	seq    int
	name   string
	report *diag.Report
	err    error
}

type serverDiagTickMsg struct {
	seq int
}

func NewServers(opts ServerOptions) ServerModel {
	input := textinput.New()
	input.Placeholder = "supervision uat vm"
	input.Prompt = "› "
	input.CharLimit = 256
	input.Width = 72

	model := ServerModel{
		svc:         opts.Service,
		opts:        opts,
		query:       strings.TrimSpace(opts.Initial),
		input:       input,
		spinner:     newSpinner(),
		width:       120,
		height:      32,
		checks:      map[string]servers.Check{},
		diagnostics: ensureDiagEngine(opts.Diagnostics),
		diagLive:    true,
	}
	model.input.SetValue(model.query)
	model.input.CursorEnd()
	return model
}

func (m ServerModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.searchCmd())
}

func (m ServerModel) searchCmd() tea.Cmd {
	if m.svc == nil {
		return nil
	}
	seq := m.searchSeq
	query := m.query
	svc := m.svc
	return func() tea.Msg {
		hits, err := svc.Search(query, 200)
		return serverSearchMsg{seq: seq, hits: hits, err: err}
	}
}

func (m ServerModel) localSearchCmd() tea.Cmd {
	if m.svc == nil {
		return nil
	}
	query := m.query
	if m.editing {
		query = strings.TrimSpace(m.input.Value())
	}
	seq := m.localSeq
	svc := m.svc
	return func() tea.Msg {
		hits, err := svc.SearchLocal(query, 200)
		return serverSearchMsg{seq: seq, local: true, hits: hits, err: err}
	}
}

func (m ServerModel) checkCmd() tea.Cmd {
	hit, ok := m.selected()
	if !ok || m.svc == nil {
		return nil
	}
	seq := m.checkSeq
	name := hit.Server.Name
	server := hit.Server
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return serverCheckMsg{seq: seq, name: name, result: svc.CheckServer(ctx, server)}
	}
}

func (m ServerModel) scheduleCheck() tea.Cmd {
	m.checkSeq++
	seq := m.checkSeq
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return serverCheckTickMsg{seq: seq}
	})
}

func (m ServerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case serverSearchMsg:
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
		m.hits = msg.hits
		if m.cursor >= len(m.hits) {
			m.cursor = max(0, len(m.hits)-1)
		}
		m.ensureVisible()
		return m, tea.Batch(m.spinner.Tick, m.scheduleCheck())

	case serverCheckTickMsg:
		if msg.seq != m.checkSeq {
			return m, nil
		}
		hit, ok := m.selected()
		if !ok {
			return m, nil
		}
		if cached, ok := m.checks[hit.Server.Name]; ok {
			m.check = &cached
			m.checkName = hit.Server.Name
			m.checkErr = nil
			m.checking = false
			return m, nil
		}
		m.checking = true
		m.checkErr = nil
		m.checkName = hit.Server.Name
		return m, tea.Batch(m.spinner.Tick, m.checkCmd())

	case serverDiagMsg:
		if msg.seq != m.diagSeq {
			return m, nil
		}
		m.diagLoading = false
		hit, ok := m.selected()
		if !ok || hit.Server.Name != msg.name {
			return m, nil
		}
		m.diagErr = msg.err
		m.diag = msg.report
		if msg.err != nil {
			m.status = "diagnostics: " + msg.err.Error()
		} else if msg.report != nil {
			m.status = "diagnostics: " + string(msg.report.Verdict)
		}
		return m, nil

	case serverDiagTickMsg:
		if msg.seq != m.diagTickSeq || !m.diagMode {
			return m, nil
		}
		next := m.scheduleDiagTick()
		if !m.diagLive || m.diagLoading {
			return m, next
		}
		return m, tea.Batch(next, m.startDiag())

	case serverCheckMsg:
		if msg.seq != m.checkSeq {
			return m, nil
		}
		hit, ok := m.selected()
		if !ok || hit.Server.Name != msg.name {
			return m, nil
		}
		m.checking = false
		if msg.err != nil {
			m.checkErr = msg.err
			m.check = nil
			m.status = "checks: " + msg.err.Error()
			return m, nil
		}
		m.checkErr = nil
		m.check = &msg.result
		m.checks[msg.name] = msg.result
		m.status = "checks complete"
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

func (m ServerModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	if m.diagMode {
		return m.handleDiagKey(key)
	}
	if m.editing {
		switch key {
		case "enter", "tab":
			m.editing = false
			m.query = strings.TrimSpace(m.input.Value())
			m.input.SetValue(m.query)
			m.localSeq++
			m.searchSeq++
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
		return m, m.scheduleCheck()
	case "G":
		m.cursor = max(0, len(m.hits)-1)
		m.ensureVisible()
		return m, m.scheduleCheck()
	case "enter", "o":
		return m, m.openCmd()
	case "s":
		hit, ok := m.selected()
		if !ok {
			return m, nil
		}
		args, err := m.svc.SSHArgs(hit.Server.Name)
		if err != nil {
			m.status = "ssh: " + err.Error()
			return m, nil
		}
		process := exec.Command("ssh", args...)
		return m, tea.ExecProcess(process, func(err error) tea.Msg {
			return actionDoneMsg{label: "ssh", err: err}
		})
	case "t":
		if _, ok := m.selected(); !ok {
			m.status = "no server selected"
			return m, nil
		}
		m.enterDiagMode()
		return m, tea.Batch(m.spinner.Tick, m.startDiag(), m.scheduleDiagTick())
	case "c":
		hit, ok := m.selected()
		if !ok {
			m.status = "no server selected"
			return m, nil
		}
		if m.svc == nil {
			m.status = "server inventory unavailable"
			return m, nil
		}
		m.checkSeq++
		m.checking = true
		m.checkErr = nil
		m.checkName = hit.Server.Name
		m.status = "running checks…"
		cmd := m.checkCmd()
		if cmd == nil {
			m.checking = false
			m.status = "checks unavailable"
			return m, nil
		}
		return m, tea.Batch(m.spinner.Tick, cmd)
	case "y":
		hit, ok := m.selected()
		if !ok {
			return m, nil
		}
		command, err := m.svc.SSHCommand(hit.Server.Name)
		if err != nil {
			m.status = "copy: " + err.Error()
			return m, nil
		}
		return m, func() tea.Msg {
			if err := actions.Copy(command); err != nil {
				return statusMsg{text: "copy", err: err}
			}
			return statusMsg{text: "ssh command copied"}
		}
	case "r":
		if m.svc != nil {
			m.svc.Refresh()
		}
		m.searchSeq++
		m.localSeq++
		m.loading = true
		m.status = ""
		return m, tea.Batch(m.spinner.Tick, m.searchCmd())
	case "esc":
		m.status = ""
	}
	return m, nil
}

func (m ServerModel) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if len(m.hits) == 0 {
		return m, nil
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.hits) {
		next = len(m.hits) - 1
	}
	if next == m.cursor {
		return m, nil
	}
	m.cursor = next
	m.ensureVisible()
	return m, m.scheduleCheck()
}

func (m *ServerModel) ensureVisible() {
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

func (m ServerModel) visibleRows() int {
	height := max(20, m.height)
	return max(4, height-8)
}

func (m ServerModel) selected() (servers.Hit, bool) {
	if m.cursor < 0 || m.cursor >= len(m.hits) {
		return servers.Hit{}, false
	}
	return m.hits[m.cursor], true
}

func (m ServerModel) openCmd() tea.Cmd {
	hit, ok := m.selected()
	if !ok {
		return nil
	}
	if m.opts.OpenWeb == nil {
		m.checkSeq++
		m.checking = true
		return tea.Batch(m.spinner.Tick, m.checkCmd())
	}
	name := hit.Server.Name
	query := m.query
	open := m.opts.OpenWeb
	return func() tea.Msg {
		_, err := open(name, query)
		return actionDoneMsg{label: "web", err: err}
	}
}

func (m ServerModel) View() string {
	if m.help {
		return m.helpView()
	}
	if m.diagMode {
		return m.diagView()
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

func (m ServerModel) headerView(width int) string {
	title := styleTitle.Render("hop") + styleSubtitle.Render("  ·  VMs & servers")
	var queryLine string
	if m.editing {
		queryLine = m.input.View()
	} else if m.query == "" {
		queryLine = stylePrompt.Render("› ") + styleMuted.Render("(all servers)") + styleDim.Render("   / to edit")
	} else {
		queryLine = stylePrompt.Render("› ") + styleQuery.Render(clip(m.query, max(8, width-26))) + styleDim.Render("   / to edit")
	}
	parts := []string{}
	switch {
	case m.err != nil:
		parts = append(parts, styleErr.Render("error: "+clip(m.err.Error(), max(24, width/2))))
	case m.loading:
		parts = append(parts, m.spinner.View()+" searching")
	default:
		parts = append(parts, fmt.Sprintf("%d servers", len(m.hits)))
	}
	if m.checking {
		parts = append(parts, m.spinner.View()+styleDim.Render(" checks…"))
	}
	if m.status != "" {
		parts = append(parts, styleDim.Render(clip(m.status, max(18, width/3))))
	}
	return strings.Join([]string{title, queryLine, strings.Join(parts, styleDim.Render("  ·  "))}, "\n")
}

func (m ServerModel) listView(width, height int) string {
	rows := max(1, height-2)
	content := []string{styleSection.Render(clip(fmt.Sprintf("SERVERS  %d", len(m.hits)), width-4))}
	if len(m.hits) == 0 {
		if m.loading {
			content = append(content, styleMuted.Render("  searching..."))
		} else {
			content = append(content, styleMuted.Render("  no servers found"))
		}
	}
	offset := m.offset
	if offset > len(m.hits) {
		offset = len(m.hits)
	}
	for i := offset; i < len(m.hits) && len(content) < rows+1; i++ {
		hit := m.hits[i]
		server := hit.Server
		line := padRight(clip(server.Name, max(12, width/3)), max(12, width/3))
		meta := server.Env
		if meta == "" {
			meta = server.Host
		}
		line += " " + styleMuted.Render(clip(meta, max(8, width/3)))
		tags := strings.Join(server.Tags, ",")
		if tags != "" {
			line += " " + styleDim.Render(clip(tags, max(6, width-len(line)-2)))
		}
		if i == m.cursor {
			content = append(content, styleSelected.Render(" "+clip(line, width-4)))
		} else {
			content = append(content, " "+line)
		}
	}
	for len(content) < rows {
		content = append(content, "")
	}
	return styleBox.Width(width - 2).Height(height).Render(strings.Join(content, "\n"))
}

func (m ServerModel) previewView(width, height int) string {
	hit, ok := m.selected()
	if !ok {
		return styleBox.Width(width - 2).Height(height).Render(styleMuted.Render("select a server"))
	}
	server := hit.Server
	lines := []string{
		styleTitle.Render(server.Name) + " " + styleDim.Render("· "+dashTUI(server.Source)),
		styleMuted.Render("env " + dashTUI(server.Env) + "  ·  host " + dashTUI(server.Host)),
		"",
		styleSection.Render("SSH"),
		styleMuted.Render("target  ") + dashTUI(server.SSH),
		styleMuted.Render("host    ") + dashTUI(server.Host) + styleDim.Render(fmt.Sprintf(":%d", server.Port)),
		styleMuted.Render("user    ") + dashTUI(server.User),
		styleMuted.Render("command ") + styleQuery.Render(fmt.Sprintf("ssh %s", dashTUI(server.SSH))),
		"",
		styleSection.Render("TAGS"),
		dashTUI(strings.Join(server.Tags, " · ")),
		"",
		styleSection.Render("CHECKS"),
	}
	if m.checkErr != nil {
		lines = append(lines, styleErr.Render("error: "+clip(m.checkErr.Error(), width-6)))
	} else if m.checking {
		lines = append(lines, m.spinner.View()+" running HTTP, ping, TCP and SSH checks…")
	} else if m.check == nil || m.checkName != server.Name {
		lines = append(lines, styleDim.Render("press c to run checks"))
	} else {
		lines = append(lines,
			checkLine("metrics", m.check.Metrics, width-6),
			checkLine("health ", m.check.Health, width-6),
		)
		info := m.check.Machine
		if info.Error != "" && !info.OK {
			lines = append(lines, styleErr.Render("machine: "+clip(info.Error, width-8)))
		} else if info.OK {
			lines = append(lines, styleMuted.Render("machine  ")+fmt.Sprintf("%s · %s · load %s", dashTUI(info.OS), dashTUI(info.Uptime), dashTUI(info.Load)))
			lines = append(lines, styleMuted.Render("memory   ")+fmt.Sprintf("%s / %s", servers.FormatBytes(info.MemoryUsed), servers.FormatBytes(info.MemoryTotal)))
			lines = append(lines, styleMuted.Render("disk     ")+fmt.Sprintf("%s / %s (%s%%)", servers.FormatBytes(info.DiskUsed), servers.FormatBytes(info.DiskTotal), dashTUI(info.DiskPercent)))
			if len(info.FailedUnits) > 0 {
				lines = append(lines, styleWarn.Render("failed   ")+clip(strings.Join(info.FailedUnits, ", "), width-10))
			}
		}
		network := m.check.Network
		if network.Checked {
			ping := "no response"
			if network.Ping.OK {
				ping = fmt.Sprintf("%dms", network.Ping.LatencyMS)
			} else if network.Reachable {
				ping = "tcp only"
			}
			ports := make([]string, 0, len(network.Ports))
			for _, port := range network.Ports {
				state := "closed"
				if port.Open {
					state = "open"
				} else if port.State != "" {
					state = port.State
				}
				ports = append(ports, fmt.Sprintf("%d %s", port.Port, state))
			}
			line := "network  " + ping
			if len(ports) > 0 {
				line += " · " + strings.Join(ports, " · ")
			}
			style := styleMuted
			if !network.Reachable {
				style = styleErr
			}
			lines = append(lines, style.Render(clip(line, width-4)))
		}
	}
	lines = append(lines, "")
	lines = append(lines, styleDim.Render("enter web · s ssh · c checks · y copy · / search"))
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return styleBox.Width(width - 2).Height(height).Render(strings.Join(lines, "\n"))
}

func checkLine(label string, check servers.HTTPCheck, width int) string {
	value := "not configured"
	style := styleDim
	if check.URL != "" {
		if check.OK {
			value = fmt.Sprintf("ok %d · %dms", check.StatusCode, check.LatencyMS)
			style = styleOK
		} else {
			value = check.Error
			if value == "" {
				value = fmt.Sprintf("error %d · %dms", check.StatusCode, check.LatencyMS)
			}
			style = styleErr
		}
	}
	value = clip(value, max(10, width-len(label)-4))
	return styleMuted.Render(label+" ") + style.Render(value)
}

func (m ServerModel) footerView(width int) string {
	keys := [][2]string{
		{"↑↓", "move"}, {"/", "search"}, {"enter", "web"}, {"s", "ssh"},
		{"c", "checks"}, {"t", "troubleshoot"}, {"y", "copy"}, {"r", "reload"}, {"?", "help"}, {"q", "quit"},
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, styleHelpKey.Render(key[0])+" "+styleDim.Render(key[1]))
	}
	return styleDim.Render(strings.Repeat("─", max(1, width))) + "\n" + clip(strings.Join(parts, "  "), width)
}

func (m ServerModel) helpView() string {
	lines := []string{
		styleTitle.Render("hop · VMs & servers"),
		"",
		styleHelpKey.Render("↑↓ / j k") + styleDim.Render("     move selection"),
		styleHelpKey.Render("/ or i") + styleDim.Render("      edit search tags"),
		styleHelpKey.Render("enter / o") + styleDim.Render("   open the unified web UI"),
		styleHelpKey.Render("s") + styleDim.Render("           open SSH"),
		styleHelpKey.Render("c") + styleDim.Render("           run HTTP, ping, TCP and SSH checks"),
		styleHelpKey.Render("t") + styleDim.Render("           open live diagnostics for the selected VM"),
		styleHelpKey.Render("y") + styleDim.Render("           copy the SSH command"),
		styleHelpKey.Render("r") + styleDim.Render("           reload the inventory"),
		styleHelpKey.Render("g / G") + styleDim.Render("       first / last"),
		styleHelpKey.Render("q") + styleDim.Render("           quit"),
		"",
		styleDim.Render("The vm tag is treated as a selector: it switches to server mode."),
	}
	return styleBox.Width(max(60, m.width-2)).Render(strings.Join(lines, "\n"))
}

func dashTUI(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
