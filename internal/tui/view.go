package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/kube"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

func (m Model) View() string {
	if m.help {
		return m.helpView()
	}
	if m.logsMode {
		return m.logsView()
	}
	if m.metricsMode {
		return m.metricsView()
	}
	width := max(20, m.width)
	height := max(10, m.height)

	header := m.headerView(width)
	footer := m.footerView(width)
	bodyHeight := height - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if bodyHeight < 4 {
		bodyHeight = 4
	}

	if width < 76 {
		body := m.listView(width, bodyHeight)
		return strings.Join([]string{header, body, footer}, "\n")
	}

	listWidth := width * 54 / 100
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

func (m Model) headerView(width int) string {
	brand := styleBrand.Render("◆ hop") + styleSubtitle.Render("  pods · logs · metrics · shell")
	queryLine := ""
	if m.editing {
		queryLine = m.input.View()
	} else if m.query == "" {
		queryLine = stylePrompt.Render("› ") + styleMuted.Render("all pods") +
			styleDim.Render("   / to search")
	} else {
		query := clip(m.query, max(4, width-28))
		queryLine = stylePrompt.Render("› ") + styleQuery.Render(query) +
			styleDim.Render("   / to edit")
	}
	return strings.Join([]string{clip(brand, width), clip(queryLine, width), clip(m.metaLine(width), width)}, "\n")
}

func metaChip(style lipgloss.Style, icon, text string) string {
	return style.Render(icon + " " + text)
}

func (m Model) metaLine(width int) string {
	var parts []string
	switch {
	case m.err != nil:
		parts = append(parts, styleAlertErr.Render("✖ "+clip(m.err.Error(), max(24, width/2))))
	case m.loading:
		parts = append(parts, m.spinner.View()+" scanning clusters")
	case m.result != nil:
		parts = append(parts,
			metaChip(styleChipInfo, "◈", fmt.Sprintf("%d pods", m.result.Meta.TotalPods)),
			metaChip(styleChip, "⌕", fmt.Sprintf("%d matches", len(m.hits))),
			metaChip(styleChip, "⚡", shortDuration(m.result.SearchDuration)),
		)
		if m.result.Meta.FromCache {
			parts = append(parts, metaChip(styleChip, "↻", "cache "+shortDuration(m.result.Meta.CacheAge)))
		}
		if m.result.Meta.Truncated {
			parts = append(parts, metaChip(styleChipWarn, "⚠", fmt.Sprintf("capped ~%d", m.result.Meta.EstimatedTotal)))
		}
		if alerts := m.alertCount(); alerts > 0 {
			parts = append(parts, metaChip(styleChipErr, "✖", fmt.Sprintf("%d alerts", alerts)))
		} else {
			parts = append(parts, metaChip(styleChipOK, "✓", "healthy"))
		}
	default:
		parts = append(parts, styleMuted.Render("initializing…"))
	}
	if label := rangeText(m.timeRange); label != "" {
		parts = append(parts, metaChip(styleRange, "◷", label))
	}
	if m.detailLoading {
		parts = append(parts, styleDim.Render("node…"))
	}
	if m.status != "" {
		parts = append(parts, styleDim.Render(clip(m.status, max(18, width/3))))
	}
	if len(parts) == 0 {
		return styleDim.Render("ready")
	}
	return clip(strings.Join(parts, styleDim.Render("  ·  ")), width)
}

func (m Model) alertCount() int {
	count := 0
	for _, hit := range m.hits {
		if podSeverity(hit.Pod) >= 2 {
			count++
		}
	}
	return count
}

func (m Model) listView(width, height int) string {
	rows := max(1, height-2)
	content := make([]string, 0, rows)

	header := fmt.Sprintf("▰ RESULTS  %d", len(m.hits))
	if m.result != nil {
		header = fmt.Sprintf("▰ RESULTS  %d / %d eligible", len(m.hits), m.result.Eligible)
	}
	content = append(content, styleSection.Render(clip(header, width-4)))

	switch {
	case len(m.hits) == 0 && m.loading:
		content = append(content, styleMuted.Render("  ◌ searching…"))
	case len(m.hits) == 0 && m.err != nil:
		content = append(content, styleErr.Render(clip("  ✖ "+m.err.Error(), width-4)))
	case len(m.hits) == 0:
		content = append(content, styleMuted.Render("  ⌕ no pod matches"))
	default:
		available := max(1, rows-1)
		start := m.offset
		if start < 0 {
			start = 0
		}
		if start > len(m.hits)-1 {
			start = max(0, len(m.hits)-1)
		}
		end := min(len(m.hits), start+available)
		for i := start; i < end; i++ {
			content = append(content, m.rowView(m.hits[i], i, width-4))
		}
	}

	for len(content) < rows {
		content = append(content, "")
	}
	if len(content) > rows {
		content = content[:rows]
	}
	for i, line := range content {
		content[i] = clip(line, width-4)
	}
	return styleBoxActive.Width(width - 2).Height(rows).Render(strings.Join(content, "\n"))
}

func (m Model) rowView(hit app.HitView, index, width int) string {
	pod := hit.Pod
	selected := index == m.cursor
	marker := "  "
	if selected {
		marker = stylePrompt.Render("▌ ")
	}
	glyph, glyphStyle := statusGlyph(pod)
	env := pod.Env
	if env == "" {
		env = "?"
	}
	nameWidth := min(32, max(10, width*28/100))
	nsWidth := min(22, max(8, width*16/100))
	envWidth := 7
	scoreWidth := 4
	ageWidth := 4
	if width < 50 {
		envWidth = 0
		nsWidth = 0
	}
	restartText := ""
	if pod.Restarts > 0 {
		restartText = fmt.Sprintf("↻%d", pod.Restarts)
	}
	fixed := 2 + 2 + nameWidth + 1 + envWidth + 1 + nsWidth + 1 + ageWidth + 1 + scoreWidth
	if restartText != "" {
		fixed += len(restartText) + 1
	}
	stateWidth := max(0, width-fixed)

	name := padRight(clip(pod.Name, nameWidth), nameWidth)
	state := clip(podStateText(pod), stateWidth)
	score := fmt.Sprintf("%3d%%", hit.Percent())
	age := humanAge(pod.StartTime)

	line := marker + glyphStyle.Render(glyph) + " " + styleQuery.Render(name)
	if envWidth > 0 {
		line += " " + envStyle(pod.Env).Render(padRight(clip(strings.ToUpper(env), envWidth), envWidth))
	}
	if nsWidth > 0 {
		line += " " + styleDim.Render(padRight(clip(pod.Namespace, nsWidth), nsWidth))
	}
	if stateWidth > 0 {
		line += " " + stateStyle(pod).Render(padRight(state, stateWidth))
	}
	if restartText != "" {
		line += " " + restartStyle(pod.Restarts).Render(restartText)
	}
	line += " " + styleDim.Render(padRight(age, ageWidth))
	line += " " + scoreStyle(hit.Score).Render(score)
	if selected {
		return styleSelected.Render(line)
	}
	return line
}

func envStyle(env string) lipgloss.Style {
	switch strings.ToLower(env) {
	case "prod", "production":
		return lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	case "staging", "stage", "uat", "preprod":
		return lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	case "dev", "development", "local":
		return lipgloss.NewStyle().Foreground(colorOK).Bold(true)
	default:
		return styleMuted.Bold(true)
	}
}

func restartStyle(restarts int32) lipgloss.Style {
	switch {
	case restarts >= 20:
		return styleChipErr
	case restarts >= 5:
		return styleChipWarn
	default:
		return styleDim
	}
}

func (m Model) previewView(width, height int) string {
	rows := max(1, height-2)
	content := []string{styleSection.Render(clip("◆ PREVIEW", width-4))}

	hit, ok := m.selected()
	if !ok {
		content = append(content,
			styleMuted.Render("  select a pod with ↑↓ / j k"),
			styleDim.Render("  ⏎ or o opens the full web page"),
			styleDim.Render("  l logs · m metrics · e shell"),
		)
		return m.renderPreviewBox(content, rows, width)
	}

	pod := hit.Pod
	signals := m.podSignals(pod)
	content = append(content, m.healthBanner(signals, width-4))
	content = append(content, styleTitle.Render(clip(pod.Name, width-4)))
	glyph, glyphStyle := statusGlyph(pod)
	content = append(content,
		glyphStyle.Render(glyph)+" "+stateStyle(pod).Render(podStateText(pod))+
			styleDim.Render(fmt.Sprintf("  ready %d/%d  ↻ %d", pod.ReadyContainers, pod.ContainersCount, pod.Restarts)),
	)
	content = append(content,
		kvLine("context", displayName(pod), width-4, styleMuted),
		kvLine("namespace", pod.Namespace, width-4, styleMuted),
		kvLine("env", envOr(pod.Env), width-4, envStyle(pod.Env)),
		kvLine("owner", ownerName(pod), width-4, styleMuted),
		kvLine("age", humanAge(pod.StartTime), width-4, styleMuted),
		kvLine("node", nodeOr(pod), width-4, styleMuted),
		kvLine("pod ip", valueOr(pod.PodIP), width-4, styleMuted),
	)
	if len(hit.Reasons) > 0 {
		content = append(content, kvLine("match", strings.Join(hit.Reasons, " · "), width-4, styleDim))
	}

	if issues := filterSignals(signals, 1); len(issues) > 0 {
		content = append(content, styleSection.Render(clip("◆ SIGNALS", width-4)))
		for _, issue := range issues {
			content = append(content, signalStyle(issue.severity).Render(clip("  "+issue.icon+" "+issue.text, width-4)))
		}
	}

	content = append(content, styleSection.Render(clip("◆ METRICS", width-4)))
	content = append(content, m.previewMetricLines(width-4)...)

	node := m.detailNode()
	content = append(content, styleSection.Render(clip("◆ NODE", width-4)))
	switch {
	case node != nil:
		state, stateStyle := "NotReady", styleChipErr
		if node.Ready {
			state, stateStyle = "Ready", styleChipOK
		}
		content = append(content,
			kvLine("state", state, width-4, stateStyle),
			kvLine("ip", valueOr(node.InternalIP), width-4, styleMuted),
			kvLine("os", strings.TrimSpace(node.OperatingSystem+"/"+node.Architecture)+" · "+valueOr(node.KernelVersion), width-4, styleMuted),
			kvLine("runtime", valueOr(node.ContainerRuntime), width-4, styleMuted),
			kvLine("capacity", node.AllocatableCPU+" cpu · "+node.AllocatableMemory+" ram · "+node.AllocatablePods+" pods", width-4, styleMuted),
		)
		if usage := m.nodeUsageLines(width - 4); len(usage) > 0 {
			content = append(content, usage...)
		}
		if len(node.Taints) > 0 {
			content = append(content, kvLine("taints", taintsText(node.Taints), width-4, styleWarn))
		}
		if conds := nodeConditionsText(node.Conditions); conds != "" {
			content = append(content, kvLine("conditions", conds, width-4, styleDim))
		}
	case m.detail != nil && m.detail.Pod.NodeError != "":
		content = append(content, styleWarn.Render(clip("  ▲ unavailable: "+m.detail.Pod.NodeError, width-4)))
	case m.detailErr != nil:
		content = append(content, styleWarn.Render(clip("  ▲ unavailable: "+m.detailErr.Error(), width-4)))
	case m.detailLoading:
		content = append(content, styleDim.Render("  ◌ loading node info…"))
	}

	if len(pod.Images) > 0 {
		content = append(content, kvLine("image", pod.Images[0], width-4, styleDim))
	}
	if len(pod.Labels) > 0 {
		content = append(content, kvLine("labels", labelsText(pod.Labels), width-4, styleDim))
	}
	return m.renderPreviewBox(content, rows, width)
}

func (m Model) renderPreviewBox(content []string, rows, width int) string {
	if len(content) > rows {
		content = content[:rows]
	}
	for len(content) < rows {
		content = append(content, "")
	}
	for i, line := range content {
		content[i] = clip(line, width-4)
	}
	return styleBox.Width(width - 2).Height(rows).Render(strings.Join(content, "\n"))
}

func (m Model) logsView() string {
	width := max(20, m.width)
	hit, ok := m.selected()
	name := "no pod selected"
	contextName := ""
	state := ""
	stateText := ""
	if ok {
		name = hit.Pod.Name
		contextName = displayName(hit.Pod) + "/" + hit.Pod.Namespace
		glyph, glyphStyle := statusGlyph(hit.Pod)
		state = glyphStyle.Render(glyph)
		stateText = stateStyle(hit.Pod).Render(podStateText(hit.Pod))
	}
	title := styleBrand.Render("◆ hop") + styleDim.Render("  /  logs  /  ") + styleQuery.Render(name)
	meta := state + " " + stateText + styleDim.Render("  ·  "+contextName)
	if ok {
		meta += styleDim.Render(fmt.Sprintf("  ·  %d lines", m.logsLineCount()))
	}
	if m.logsLoading {
		meta += "  " + m.spinner.View() + styleDim.Render(" loading")
	} else if !m.logsAt.IsZero() {
		meta += styleDim.Render("  ·  ↻ " + shortDuration(time.Since(m.logsAt)) + " ago")
	}
	header := strings.Join([]string{clip(title, width), clip(meta, width)}, "\n")

	footer := styleDim.Render(clip("↑↓ scroll · g/G top/bottom · r reload · e shell · m metrics · esc close · ? help · q quit", width))
	rows := m.logsVisibleRows()
	content := make([]string, 0, rows)
	switch {
	case m.logsLoading && m.logs == "":
		content = append(content, m.spinner.View()+styleMuted.Render(" fetching recent logs…"))
	case m.logsErr != nil:
		content = append(content, styleErr.Render(clip("✖ "+m.logsErr.Error(), width-4)))
		if m.logs != "" {
			content = append(content, "")
			content = append(content, m.logLines(width-4, rows-len(content))...)
		}
	case m.logs == "":
		content = append(content, styleMuted.Render("  no logs returned"))
	default:
		content = append(content, m.logLines(width-4, rows)...)
	}
	for len(content) < rows {
		content = append(content, "")
	}
	if len(content) > rows {
		content = content[:rows]
	}
	box := styleBoxActive.Width(width - 2).Height(rows).Render(strings.Join(content, "\n"))
	return strings.Join([]string{header, box, footer}, "\n")
}

func (m Model) logLines(width, rows int) []string {
	content := strings.TrimRight(m.logs, "\n")
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	start := min(max(0, m.logsOffset), max(0, len(lines)-rows))
	end := min(len(lines), start+rows)
	out := make([]string, 0, end-start)
	for _, line := range lines[start:end] {
		out = append(out, renderLogLine(line, width))
	}
	return out
}

func renderLogLine(line string, width int) string {
	line = ansiEscape.ReplaceAllString(line, "")
	line = strings.ReplaceAll(line, "\t", "  ")
	line = strings.TrimRight(line, "\r")
	line = clip(line, width)
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error"), strings.Contains(lower, "panic"), strings.Contains(lower, "fatal"),
		strings.Contains(lower, "exception"), strings.Contains(lower, "failed"):
		return styleLogErr.Render(line)
	case strings.Contains(lower, "warn"):
		return styleLogWarn.Render(line)
	case strings.Contains(lower, "info"), strings.Contains(lower, "ready"), strings.Contains(lower, "started"):
		return styleLogInfo.Render(line)
	default:
		return styleLogBody.Render(line)
	}
}

func (m Model) metricsView() string {
	width := max(20, m.width)
	hit, ok := m.selected()
	name := "no pod selected"
	state := ""
	stateText := ""
	contextName := ""
	if ok {
		name = hit.Pod.Name
		contextName = displayName(hit.Pod) + "/" + hit.Pod.Namespace
		glyph, glyphStyle := statusGlyph(hit.Pod)
		state = glyphStyle.Render(glyph)
		stateText = stateStyle(hit.Pod).Render(podStateText(hit.Pod))
	}
	title := styleBrand.Render("◆ hop") + styleDim.Render("  /  metrics  /  ") + styleQuery.Render(name)
	meta := state + " " + stateText + styleDim.Render("  ·  "+contextName)
	if m.stats != nil {
		source := m.stats.Source
		if source == "" {
			source = "metrics"
		}
		meta += styleDim.Render("  ·  " + source + "  ·  ↻ " + shortDuration(time.Since(m.stats.FetchedAt)) + " ago")
	}
	if m.statsLoading {
		meta += "  " + m.spinner.View() + styleDim.Render(" sampling")
	}
	header := strings.Join([]string{clip(title, width), clip(meta, width)}, "\n")
	footer := styleDim.Render(clip("↑↓ scroll · r refresh · l logs · e shell · m close · ? help · q quit", width))

	bodyHeight := m.metricsBodyHeight()
	content := m.metricsContent(width - 4)
	maxOffset := max(0, len(content)-bodyHeight)
	offset := min(max(0, m.metricsOffset), maxOffset)
	content = content[offset:]
	if len(content) > bodyHeight {
		content = content[:bodyHeight]
	}
	box := styleBoxActive.Width(width - 2).Height(bodyHeight).Render(strings.Join(content, "\n"))
	return strings.Join([]string{header, box, footer}, "\n")
}

func (m Model) metricsBodyHeight() int {
	height := max(10, m.height)
	return max(4, height-5)
}

func (m Model) metricsContent(width int) []string {
	hit, ok := m.selected()
	content := make([]string, 0, 32)
	content = append(content, m.healthBanner(m.podSignals(hitOrZero(hit, ok)), width))
	if m.statsErr != nil {
		content = append(content, styleWarn.Render(clip("▲ "+m.statsErr.Error(), width)))
	} else if m.statsLoading && m.stats == nil {
		content = append(content, m.spinner.View()+styleMuted.Render(" collecting cpu / ram / network / disk…"))
	} else if m.stats == nil {
		content = append(content, styleDim.Render("press r to sample metrics"))
	} else {
		stats := m.stats
		hitPod := hitOrZero(hit, ok)
		cpuBase := hitPod.CPULimit
		cpuSuffix := " limit"
		if cpuBase <= 0 && stats.Node != nil {
			cpuBase = stats.Node.CPUCapacity
			cpuSuffix = " node"
		}
		memBase := hitPod.MemoryLimit
		memSuffix := " limit"
		if memBase <= 0 && stats.Node != nil {
			memBase = stats.Node.MemoryCapacity
			memSuffix = " node"
		}
		content = append(content,
			styleSection.Render("◆ RESOURCES"),
			resourceLine("CPU", fmtCPU(stats.CPU), stats.CPU, cpuBase, cpuSuffix, fmtCPU, width),
			resourceLine("RAM", fmtBytes(stats.Memory), stats.Memory, memBase, memSuffix, fmtBytes, width),
		)
		if stats.Disk.CapacityBytes > 0 {
			content = append(content, resourceLine("DISK", fmtBytes(int64(stats.Disk.UsedBytes)), int64(stats.Disk.UsedBytes), int64(stats.Disk.CapacityBytes), " ephemeral", fmtBytes, width))
			if extras := diskExtras(stats.Disk); extras != "" {
				content = append(content, styleDim.Render("  "+extras))
			}
		}
		content = append(content,
			kvLine("requests", fmtCPU(hitPod.CPURequest)+" cpu · "+fmtBytes(hitPod.MemoryRequest)+" ram", width, styleMuted),
			kvLine("limits", fmtCPU(hitPod.CPULimit)+" cpu · "+fmtBytes(hitPod.MemoryLimit)+" ram", width, styleMuted),
		)
		if stats.WorkingSet > 0 {
			content = append(content, kvLine("working set", fmtBytes(stats.WorkingSet), width, styleMuted))
		}
		content = append(content,
			styleSection.Render("◆ NETWORK"),
			styleInfo.Render("  ↓ "+fmtRate(m.statsRxRate)+" receive")+styleDim.Render("   total "+fmtBytes(int64(stats.Network.RxBytes))),
			stylePrompt.Render("  ↑ "+fmtRate(m.statsTxRate)+" send")+styleDim.Render("      total "+fmtBytes(int64(stats.Network.TxBytes))),
		)
		if stats.Network.RxErrors > 0 || stats.Network.TxErrors > 0 {
			content = append(content, styleWarn.Render(fmt.Sprintf("  ▲ errors rx %d / tx %d", stats.Network.RxErrors, stats.Network.TxErrors)))
		}
		if len(stats.Containers) > 0 {
			content = append(content, styleSection.Render("◆ CONTAINERS"))
			for _, container := range stats.Containers {
				content = append(content, styleMuted.Render("  "+padRight(clip(container.Name, 22), 22))+
					styleInfo.Render(fmtCPU(container.CPU))+" cpu  "+
					styleDim.Render(fmtBytes(container.Memory))+" ram")
				diskParts := make([]string, 0, 2)
				if container.Rootfs.UsedBytes > 0 {
					diskParts = append(diskParts, "rootfs "+fmtBytes(int64(container.Rootfs.UsedBytes)))
				}
				if container.Logs.UsedBytes > 0 {
					diskParts = append(diskParts, "logs "+fmtBytes(int64(container.Logs.UsedBytes)))
				}
				if len(diskParts) > 0 {
					content = append(content, styleDim.Render("  "+strings.Join(diskParts, "  ")))
				}
			}
		}
		if stats.Node != nil {
			node := stats.Node
			nodeState := styleChipOK.Render("● Ready")
			switch {
			case !node.ReadyKnown:
				nodeState = styleChipWarn.Render("◌ state unknown")
			case !node.Ready:
				nodeState = styleChipErr.Render("✖ NotReady")
			}
			content = append(content,
				styleSection.Render("◆ NODE"),
				kvLine("name", node.Name, width, styleMuted),
				"  "+nodeState,
				resourceLine("CPU", fmtCPU(node.CPU), node.CPU, node.CPUCapacity, " node", fmtCPU, width),
				resourceLine("RAM", fmtBytes(node.Memory), node.Memory, node.MemoryCapacity, " node", fmtBytes, width),
			)
			if node.Disk.CapacityBytes > 0 {
				content = append(content, resourceLine("DISK", fmtBytes(int64(node.Disk.UsedBytes)), int64(node.Disk.UsedBytes), int64(node.Disk.CapacityBytes), " node", fmtBytes, width))
				if extras := diskExtras(node.Disk); extras != "" {
					content = append(content, styleDim.Render("  "+extras))
				}
			}
			if node.MemoryAvailable > 0 {
				content = append(content, kvLine("mem avail", fmtBytes(node.MemoryAvailable), width, styleMuted))
			}
			if node.WorkingSetBytes > 0 {
				content = append(content, kvLine("node working set", fmtBytes(node.WorkingSetBytes), width, styleMuted))
			}
			if node.Network.RxBytes > 0 || node.Network.TxBytes > 0 || m.statsNodeRxRate > 0 || m.statsNodeTxRate > 0 {
				content = append(content, styleInfo.Render("  NET ↓ "+fmtRate(m.statsNodeRxRate)+" ↑ "+fmtRate(m.statsNodeTxRate))+
					styleDim.Render(fmt.Sprintf("   total ↓ %s ↑ %s",
						fmtBytes(int64(node.Network.RxBytes)), fmtBytes(int64(node.Network.TxBytes)))))
			}
		}
		if len(stats.Warnings) > 0 {
			for _, warning := range stats.Warnings {
				content = append(content, styleDim.Render(clip("  ▲ "+warning, width)))
			}
		}
	}
	for i, line := range content {
		content[i] = clip(line, width)
	}
	return content
}

func diskExtras(disk kube.DiskStats) string {
	parts := make([]string, 0, 2)
	if disk.AvailableBytes > 0 {
		parts = append(parts, "available "+fmtBytes(int64(disk.AvailableBytes)))
	}
	if disk.Inodes > 0 {
		parts = append(parts, fmt.Sprintf("inodes %s/%s", formatCount(disk.InodesUsed), formatCount(disk.Inodes)))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  ·  ")
}

func formatCount(value uint64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	}
	return fmt.Sprintf("%d", value)
}

func hitOrZero(hit app.HitView, ok bool) kube.Pod {
	if !ok {
		return kube.Pod{}
	}
	return hit.Pod
}

func (m Model) footerView(width int) string {
	hints := "↑↓ nav · / search · ⏎ web · l logs · m metrics · e shell · ? help · q quit"
	return styleDim.Render(clip(hints, width))
}

func (m Model) helpView() string {
	width := max(60, m.width)
	keys := [][2]string{
		{"/ or i", "edit the search"},
		{"↑↓ / j k", "navigate results"},
		{"g / G", "first / last result"},
		{"⏎ or o", "open the web page (node, events, YAML)"},
		{"l", "open the in-app logs pane (tail + timestamps)"},
		{"L", "follow logs full-screen (kubectl logs -f)"},
		{"m", "open the live metrics view (cpu/ram/net/disk)"},
		{"e", "open a shell in the first container (kubectl exec)"},
		{"s", "open k9s on the pod"},
		{"d", "describe the pod"},
		{"y", "copy the kubectl get command"},
		{"r", "refresh all clusters"},
		{"?", "close this help"},
		{"q", "quit"},
	}
	var lines []string
	lines = append(lines, styleTitle.Render("hop · help"))
	lines = append(lines, "")
	lines = append(lines, styleSection.Render("SEARCH"))
	lines = append(lines,
		"  free-form terms in any order: "+styleMuted.Render("pod1 ns1 env1"),
		"  filters: "+styleMuted.Render("ns:dev env:prod ctx:prod-* label:app=api node:ip-10-0-1-2"),
		"  logs/metrics range: "+styleMuted.Render("12h 14h")+"  or  "+styleMuted.Render("-2h"),
		"  dates: "+styleMuted.Render("from:2026-11-25T12:00 to:2026-11-25T14:00"),
		"",
		styleSection.Render("SHORTCUTS"),
	)
	for _, item := range keys {
		lines = append(lines, "  "+styleHelpKey.Render(padRight(item[0], 12))+" "+item[1])
	}
	lines = append(lines,
		"",
		styleSection.Render("METRICS"),
		"  CPU/RAM come from metrics-server; network/disk come from kubelet",
		"  summary (nodes/proxy). Unavailable sources are shown explicitly.",
		"",
		styleDim.Render("Time tokens restrict Grafana/Loki/Mimir links and logs."),
	)
	box := styleBox.
		BorderForeground(colorAccent2).
		Padding(1, 2).
		Width(max(48, width-6)).
		Render(strings.Join(lines, "\n"))
	return box
}

type healthSignal struct {
	severity int
	icon     string
	text     string
}

func signalStyle(severity int) lipgloss.Style {
	switch {
	case severity >= 2:
		return styleChipErr
	case severity == 1:
		return styleChipWarn
	default:
		return styleChipOK
	}
}

func filterSignals(signals []healthSignal, minSeverity int) []healthSignal {
	out := make([]healthSignal, 0, len(signals))
	for _, signal := range signals {
		if signal.severity >= minSeverity {
			out = append(out, signal)
		}
	}
	return out
}

func (m Model) healthBanner(signals []healthSignal, width int) string {
	worst := 0
	count := 0
	for _, signal := range signals {
		if signal.severity > worst {
			worst = signal.severity
			count = 1
		} else if signal.severity == worst && worst > 0 {
			count++
		}
	}
	if worst == 0 {
		return styleAlertOK.Render(clip("✓ healthy  ·  no active signals", max(1, width-2)))
	}
	first := healthSignal{severity: worst}
	for _, signal := range signals {
		if signal.severity == worst {
			first = signal
			break
		}
	}
	text := first.icon + " " + first.text
	if count > 1 {
		text += fmt.Sprintf("  (+%d more)", count-1)
	}
	style := styleAlertWarn
	if worst >= 2 {
		style = styleAlertErr
	}
	return style.Render(clip(text, max(1, width-2)))
}

func (m Model) podSignals(pod kube.Pod) []healthSignal {
	var out []healthSignal
	severity := podSeverity(pod)
	reasonSeverity := reasonSeverity(pod.Reason)
	if pod.Reason != "" && reasonSeverity > 0 {
		icon := "▲"
		if reasonSeverity >= 2 {
			icon = "✖"
		}
		out = append(out, healthSignal{severity: max(reasonSeverity, severity), icon: icon, text: pod.Reason})
	}
	switch pod.Phase {
	case "Failed":
		if reasonSeverity == 0 {
			out = append(out, healthSignal{severity: 2, icon: "✖", text: "pod failed"})
		}
	case "Pending":
		out = append(out, healthSignal{severity: 1, icon: "◌", text: "pod pending"})
	}
	if pod.ContainersCount > 0 && pod.ReadyContainers < pod.ContainersCount && pod.Phase != "Succeeded" {
		out = append(out, healthSignal{
			severity: 1,
			icon:     "◐",
			text:     fmt.Sprintf("containers not ready %d/%d", pod.ReadyContainers, pod.ContainersCount),
		})
	}
	if pod.Restarts >= 20 {
		out = append(out, healthSignal{severity: 2, icon: "↻", text: fmt.Sprintf("%d restarts", pod.Restarts)})
	} else if pod.Restarts >= 5 {
		out = append(out, healthSignal{severity: 1, icon: "↻", text: fmt.Sprintf("%d restarts", pod.Restarts)})
	}
	if node := m.detailNode(); node != nil {
		if !node.Ready {
			out = append(out, healthSignal{severity: 2, icon: "✖", text: "node NotReady"})
		}
		if len(node.Taints) > 0 {
			out = append(out, healthSignal{severity: 1, icon: "▲", text: "node is tainted"})
		}
	}
	if m.detail != nil && m.detail.Pod.NodeError != "" {
		out = append(out, healthSignal{severity: 1, icon: "▲", text: "node details unavailable"})
	}
	if m.detailErr != nil && m.detail == nil && !m.detailLoading {
		out = append(out, healthSignal{severity: 1, icon: "▲", text: "pod details unavailable"})
	}
	if m.statsErr != nil {
		out = append(out, healthSignal{severity: 1, icon: "▲", text: "metrics unavailable"})
	}
	if m.stats != nil {
		if disk := m.stats.Disk; disk.CapacityBytes > 0 {
			if pct := usagePercent(int64(disk.UsedBytes), int64(disk.CapacityBytes)); pct >= 90 {
				out = append(out, healthSignal{severity: 2, icon: "✖", text: fmt.Sprintf("ephemeral disk %.0f%%", pct)})
			}
		}
		if node := m.stats.Node; node != nil {
			if node.ReadyKnown && !node.Ready {
				out = append(out, healthSignal{severity: 2, icon: "✖", text: "node NotReady"})
			}
			if pct := usagePercent(node.CPU, node.CPUCapacity); pct >= 90 {
				out = append(out, healthSignal{severity: 1, icon: "▲", text: fmt.Sprintf("node cpu %.0f%%", pct)})
			}
			if pct := usagePercent(node.Memory, node.MemoryCapacity); pct >= 90 {
				out = append(out, healthSignal{severity: 1, icon: "▲", text: fmt.Sprintf("node memory %.0f%%", pct)})
			}
			if disk := node.Disk; disk.CapacityBytes > 0 {
				if pct := usagePercent(int64(disk.UsedBytes), int64(disk.CapacityBytes)); pct >= 90 {
					out = append(out, healthSignal{severity: 2, icon: "✖", text: fmt.Sprintf("node disk %.0f%%", pct)})
				}
			}
		}
	}
	if len(out) == 0 {
		out = append(out, healthSignal{severity: 0, icon: "✓", text: "healthy"})
	}
	return out
}

func reasonSeverity(reason string) int {
	value := strings.ToLower(reason)
	switch {
	case value == "":
		return 0
	case strings.Contains(value, "crashloop"), strings.Contains(value, "imagepull"), strings.Contains(value, "errimage"),
		strings.Contains(value, "oom"), strings.Contains(value, "evicted"), strings.Contains(value, "failed"),
		strings.Contains(value, "error"), strings.Contains(value, "backoff"), strings.Contains(value, "unhealthy"),
		strings.Contains(value, "deadline"), strings.Contains(value, "createcontainer"):
		return 2
	case value == "completed", value == "succeeded", value == "running":
		return 0
	default:
		return 1
	}
}

func podSeverity(pod kube.Pod) int {
	severity := reasonSeverity(pod.Reason)
	switch pod.Phase {
	case "Failed":
		severity = max(severity, 2)
	case "Pending":
		severity = max(severity, 1)
	case "Succeeded":
		return 0
	}
	if !pod.Ready && pod.Phase != "Succeeded" {
		severity = max(severity, 1)
	}
	switch {
	case pod.Restarts >= 20:
		severity = max(severity, 2)
	case pod.Restarts >= 5:
		severity = max(severity, 1)
	}
	return severity
}

func podStateText(pod kube.Pod) string {
	if pod.Reason != "" {
		return pod.Reason
	}
	if pod.Phase == "Running" && !pod.Ready {
		return "NotReady"
	}
	if pod.Phase == "" {
		return "Unknown"
	}
	return pod.Phase
}

func stateStyle(pod kube.Pod) lipgloss.Style {
	switch podSeverity(pod) {
	case 2:
		return styleChipErr
	case 1:
		return styleChipWarn
	default:
		if pod.Phase == "Succeeded" {
			return styleDim
		}
		return styleChipOK
	}
}

func statusGlyph(pod kube.Pod) (string, lipgloss.Style) {
	switch pod.Phase {
	case "Failed":
		return "✖", styleErr
	case "Succeeded":
		return "✓", styleDim
	case "Pending":
		return "◌", styleWarn
	}
	if reasonSeverity(pod.Reason) >= 2 {
		return "✖", styleErr
	}
	if !pod.Ready {
		return "◐", styleWarn
	}
	return "●", styleOK
}

func (m Model) previewMetricLines(width int) []string {
	if m.statsLoading && m.stats == nil {
		return []string{m.spinner.View() + styleDim.Render(" collecting cpu / ram / net / disk…")}
	}
	if m.statsErr != nil {
		return []string{styleWarn.Render(clip("▲ "+m.statsErr.Error(), width))}
	}
	stats := m.stats
	if stats == nil {
		return []string{styleDim.Render("  m to load live metrics (cpu, ram, network, disk)")}
	}
	hit, _ := m.selected()
	cpuBase := hit.Pod.CPULimit
	cpuSuffix := " limit"
	if cpuBase <= 0 && stats.Node != nil {
		cpuBase = stats.Node.CPUCapacity
		cpuSuffix = " node"
	}
	memBase := hit.Pod.MemoryLimit
	memSuffix := " limit"
	if memBase <= 0 && stats.Node != nil {
		memBase = stats.Node.MemoryCapacity
		memSuffix = " node"
	}
	source := stats.Source
	if source == "" {
		source = "metrics"
	}
	age := "live"
	if !stats.FetchedAt.IsZero() {
		age = shortDuration(time.Since(stats.FetchedAt)) + " ago"
	}
	lines := []string{styleDim.Render("  " + source + " · " + age)}
	lines = append(lines,
		resourceLine("CPU", fmtCPU(stats.CPU), stats.CPU, cpuBase, cpuSuffix, fmtCPU, width),
		resourceLine("RAM", fmtBytes(stats.Memory), stats.Memory, memBase, memSuffix, fmtBytes, width),
	)
	if stats.Disk.CapacityBytes > 0 {
		lines = append(lines, resourceLine("DISK", fmtBytes(int64(stats.Disk.UsedBytes)), int64(stats.Disk.UsedBytes), int64(stats.Disk.CapacityBytes), " ephemeral", fmtBytes, width))
	}
	if stats.Network.RxBytes > 0 || stats.Network.TxBytes > 0 || m.statsRxRate > 0 || m.statsTxRate > 0 {
		lines = append(lines, styleMuted.Render(clip(fmt.Sprintf("  NET  ↓ %s  ↑ %s  ·  total ↓ %s ↑ %s",
			fmtRate(m.statsRxRate), fmtRate(m.statsTxRate),
			fmtBytes(int64(stats.Network.RxBytes)), fmtBytes(int64(stats.Network.TxBytes))), width)))
	}
	if len(stats.Warnings) > 0 {
		lines = append(lines, styleDim.Render(clip("  ▲ "+stats.Warnings[0], width)))
	}
	return lines
}

func (m Model) nodeUsageLines(width int) []string {
	stats := m.stats
	if stats == nil || stats.Node == nil {
		return nil
	}
	node := stats.Node
	lines := []string{
		resourceLine("cpu", fmtCPU(node.CPU), node.CPU, node.CPUCapacity, " node", fmtCPU, width),
		resourceLine("ram", fmtBytes(node.Memory), node.Memory, node.MemoryCapacity, " node", fmtBytes, width),
	}
	if node.Disk.CapacityBytes > 0 {
		lines = append(lines, resourceLine("disk", fmtBytes(int64(node.Disk.UsedBytes)), int64(node.Disk.UsedBytes), int64(node.Disk.CapacityBytes), " node", fmtBytes, width))
	}
	if node.Network.RxBytes > 0 || node.Network.TxBytes > 0 {
		lines = append(lines, styleDim.Render(clip(fmt.Sprintf("  net  ↓ %s ↑ %s total",
			fmtBytes(int64(node.Network.RxBytes)), fmtBytes(int64(node.Network.TxBytes))), width)))
	}
	return lines
}

func resourceLine(label, used string, usedValue, total int64, suffix string, format func(int64) string, width int) string {
	ratio := 0.0
	if total > 0 {
		ratio = usagePercent(usedValue, total) / 100
	}
	labelWidth := 5
	pct := fmt.Sprintf("%3.0f%%", ratio*100)
	if total <= 0 {
		pct = "  — "
	}
	barWidth := min(20, max(4, (width-labelWidth-9)/2))
	value := used
	if total > 0 {
		value += " / " + format(total) + suffix
	}
	remaining := max(0, width-labelWidth-barWidth-8)
	return styleLabel.Render(padRight(label, labelWidth)) + " " +
		barStyle(ratio*100).Render(bar(ratio, barWidth)) + " " +
		styleMuted.Render(pct) + " " +
		styleDim.Render(clip(value, remaining))
}

func bar(ratio float64, cells int) string {
	ratio = maxFloat(0, minFloat(1, ratio))
	filled := int(ratio*float64(cells) + 0.5)
	if filled > cells {
		filled = cells
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", cells-filled)
}

func barStyle(percent float64) lipgloss.Style {
	switch {
	case percent >= 90:
		return styleErr
	case percent >= 70:
		return styleWarn
	default:
		return styleOK
	}
}

func usagePercent(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return minFloat(100, maxFloat(0, float64(used)/float64(total)*100))
}

func fmtCPU(milli int64) string {
	if milli >= 1000 {
		return fmt.Sprintf("%.2fc", float64(milli)/1000)
	}
	return fmt.Sprintf("%dm", milli)
}

func fmtBytes(value int64) string {
	if value <= 0 {
		return "0 B"
	}
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func fmtRate(bytesPerSecond float64) string {
	if bytesPerSecond <= 0 {
		return "0 B/s"
	}
	return fmtBytes(int64(bytesPerSecond)) + "/s"
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func scoreStyle(score float64) lipgloss.Style {
	switch {
	case score >= 0.7:
		return styleOK
	case score >= 0.45:
		return styleWarn
	default:
		return styleErr
	}
}

func kvLine(label, value string, width int, valueStyle lipgloss.Style) string {
	labelWidth := 10
	valueWidth := max(4, width-labelWidth-1)
	return styleDim.Render(padRight(label, labelWidth)) + " " + valueStyle.Render(clip(value, valueWidth))
}

func (m Model) detailNode() *kube.NodeInfo {
	if m.detail == nil || m.detail.Pod.Node == nil {
		return nil
	}
	return m.detail.Pod.Node
}

func displayName(pod kube.Pod) string {
	if pod.Display != "" {
		return pod.Display
	}
	return pod.Context
}

func ownerName(pod kube.Pod) string {
	switch {
	case pod.OwnerName == "" && pod.Workload == "":
		return "—"
	case pod.OwnerName == "":
		return pod.Workload
	case pod.OwnerKind == "":
		return pod.OwnerName
	default:
		return pod.OwnerKind + "/" + pod.OwnerName
	}
}

func nodeOr(pod kube.Pod) string {
	if pod.Node == "" {
		return "—"
	}
	return pod.Node
}

func envOr(env string) string {
	if env == "" {
		return "—"
	}
	return env
}

func valueOr(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func podStatusText(pod kube.Pod) string {
	if pod.Reason != "" {
		return pod.Reason
	}
	if pod.Phase == "" {
		return "Unknown"
	}
	return pod.Phase
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	}
}

func taintsText(taints []kube.NodeTaint) string {
	parts := make([]string, 0, min(3, len(taints)))
	for i, taint := range taints {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("+%d", len(taints)-3))
			break
		}
		text := taint.Key
		if taint.Value != "" {
			text += "=" + taint.Value
		}
		text += ":" + taint.Effect
		parts = append(parts, text)
	}
	return strings.Join(parts, " · ")
}

func nodeConditionsText(conditions []kube.NodeCondition) string {
	parts := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		if condition.Type != "Ready" && condition.Status != "True" {
			continue
		}
		parts = append(parts, condition.Type+"="+condition.Status)
	}
	return strings.Join(parts, " · ")
}

func labelsText(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, min(3, len(keys)))
	for i, key := range keys {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("+%d", len(keys)-3))
			break
		}
		parts = append(parts, key+"="+labels[key])
	}
	return strings.Join(parts, " · ")
}

func clip(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(value, width, "…")
}

func padRight(value string, width int) string {
	if width <= 0 {
		return ""
	}
	valueWidth := ansi.StringWidth(value)
	if valueWidth >= width {
		return clip(value, width)
	}
	return value + strings.Repeat(" ", width-valueWidth)
}
