package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/kube"
)

func (m Model) View() string {
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
	title := styleTitle.Render("hop") + styleSubtitle.Render("  ·  pods, logs, metrics")
	var queryLine string
	if m.editing {
		queryLine = m.input.View()
	} else {
		query := m.query
		if query == "" {
			queryLine = stylePrompt.Render("› ") + styleMuted.Render("(all pods)") + styleDim.Render("   / to edit")
		} else {
			query = clip(query, max(8, width-26))
			queryLine = stylePrompt.Render("› ") + styleQuery.Render(query) + styleDim.Render("   / to edit")
		}
	}

	meta := m.metaLine(width)
	return strings.Join([]string{title, queryLine, meta}, "\n")
}

func (m Model) metaLine(width int) string {
	var parts []string
	switch {
	case m.err != nil:
		parts = append(parts, styleErr.Render("error: "+clip(m.err.Error(), max(24, width/2))))
	case m.loading:
		parts = append(parts, m.spinner.View()+" searching")
	case m.result != nil:
		parts = append(parts,
			fmt.Sprintf("%d pods", m.result.Meta.TotalPods),
			fmt.Sprintf("%d results", len(m.hits)),
			"fetch "+shortDuration(m.result.Meta.FetchDuration),
			"ranking "+shortDuration(m.result.SearchDuration),
		)
		if m.result.Meta.FromCache {
			parts = append(parts, "cache "+shortDuration(m.result.Meta.CacheAge))
		}
	default:
		parts = append(parts, styleMuted.Render("initializing..."))
	}
	if label := rangeText(m.timeRange); label != "" {
		parts = append(parts, styleRange.Render("◷ "+label))
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
	return strings.Join(parts, styleDim.Render("  ·  "))
}

func (m Model) listView(width, height int) string {
	rows := max(1, height-2)
	content := make([]string, 0, rows)

	header := fmt.Sprintf("RESULTS  %d", len(m.hits))
	if m.result != nil {
		header = fmt.Sprintf("RESULTS  %d / %d eligible", len(m.hits), m.result.Eligible)
	}
	content = append(content, styleSection.Render(clip(header, width-4)))

	switch {
	case len(m.hits) == 0 && m.loading:
		content = append(content, styleMuted.Render("  searching..."))
	case len(m.hits) == 0 && m.err != nil:
		content = append(content, styleErr.Render(clip("  "+m.err.Error(), width-4)))
	case len(m.hits) == 0:
		content = append(content, styleMuted.Render("  no pod matches"))
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
	return styleBox.Width(width - 2).Height(rows).Render(strings.Join(content, "\n"))
}

func (m Model) rowView(hit app.HitView, index, width int) string {
	pod := hit.Pod
	marker := "  "
	if index == m.cursor {
		marker = "› "
	}
	glyph, glyphStyle := statusGlyph(pod)
	env := pod.Env
	if env == "" {
		env = "?"
	}
	meta := env + " · " + pod.Namespace + " · " + displayName(pod)
	score := fmt.Sprintf("%3d%%", hit.Percent())

	nameWidth := max(8, width*38/100)
	metaWidth := max(0, width-nameWidth-10)
	name := padRight(clip(pod.Name, nameWidth), nameWidth)
	meta = padRight(clip(meta, metaWidth), metaWidth)

	line := marker + glyphStyle.Render(glyph) + " " + name + " " +
		styleMuted.Render(meta) + " " + scoreStyle(hit.Score).Render(score)
	if index == m.cursor {
		return styleSelected.Render(line)
	}
	return line
}

func (m Model) previewView(width, height int) string {
	rows := max(1, height-2)
	content := []string{styleSection.Render(clip("PREVIEW", width-4))}

	hit, ok := m.selected()
	if !ok {
		content = append(content,
			styleMuted.Render("  select a pod with ↑↓ / j k"),
			styleDim.Render("  ⏎ or o opens the full web page"),
		)
		return m.renderPreviewBox(content, rows, width)
	}

	pod := hit.Pod
	glyph, glyphStyle := statusGlyph(pod)
	statusMeta := fmt.Sprintf("  ·  ready %d/%d  ·  restarts %d",
		pod.ReadyContainers, pod.ContainersCount, pod.Restarts)
	content = append(content,
		styleTitle.Render(clip(pod.Name, width-4)),
		glyphStyle.Render(glyph)+" "+podStatusText(pod)+
			styleDim.Render(clip(statusMeta, max(0, width-12))),
	)
	content = append(content,
		kvLine("context", displayName(pod), width-4, styleMuted),
		kvLine("namespace", pod.Namespace, width-4, styleMuted),
		kvLine("env", envOr(pod.Env), width-4, styleMuted),
		kvLine("owner", ownerName(pod), width-4, styleMuted),
		kvLine("age", humanAge(pod.StartTime), width-4, styleMuted),
		kvLine("node", nodeOr(pod), width-4, styleMuted),
		kvLine("pod ip", valueOr(pod.PodIP), width-4, styleMuted),
		kvLine("host ip", valueOr(pod.HostIP), width-4, styleMuted),
	)
	if len(hit.Reasons) > 0 {
		content = append(content, kvLine("match", strings.Join(hit.Reasons, " · "), width-4, styleDim))
	}

	node := m.detailNode()
	switch {
	case node != nil:
		content = append(content, styleSection.Render(clip("NODE", width-4)))
		state, stateStyle := "NotReady", styleErr
		if node.Ready {
			state, stateStyle = "Ready", styleOK
		}
		content = append(content,
			kvLine("name", node.Name, width-4, styleMuted),
			kvLine("state", state, width-4, stateStyle),
			kvLine("ip", valueOr(node.InternalIP), width-4, styleMuted),
			kvLine("os", strings.TrimSpace(node.OperatingSystem+"/"+node.Architecture)+" · "+valueOr(node.KernelVersion), width-4, styleMuted),
			kvLine("runtime", valueOr(node.ContainerRuntime), width-4, styleMuted),
			kvLine("kubelet", valueOr(node.KubeletVersion), width-4, styleMuted),
			kvLine("cpu", node.AllocatableCPU+" / "+node.CapacityCPU, width-4, styleMuted),
			kvLine("memory", node.AllocatableMemory+" / "+node.CapacityMemory, width-4, styleMuted),
			kvLine("pods", node.AllocatablePods+" / "+node.CapacityPods, width-4, styleMuted),
		)
		if len(node.Taints) > 0 {
			content = append(content, kvLine("taints", taintsText(node.Taints), width-4, styleWarn))
		}
		if conds := nodeConditionsText(node.Conditions); conds != "" {
			content = append(content, kvLine("conditions", conds, width-4, styleDim))
		}
	case m.detail != nil && m.detail.Pod.NodeError != "":
		content = append(content, kvLine("node", "unavailable: "+m.detail.Pod.NodeError, width-4, styleWarn))
	case m.detailErr != nil:
		content = append(content, kvLine("node", "unavailable: "+m.detailErr.Error(), width-4, styleWarn))
	case m.detailLoading:
		content = append(content, kvLine("node", "loading node info...", width-4, styleDim))
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
	return styleBox.Width(width - 2).Height(rows).Render(strings.Join(content, "\n"))
}

func (m Model) footerView(width int) string {
	hints := "↑↓ navigate · / edit · ⏎ web · s k9s · l logs · d describe · y copy · r refresh · ? help · q quit"
	return styleDim.Render(clip(hints, width))
}

func (m Model) helpView() string {
	width := max(60, m.width)
	keys := [][2]string{
		{"/ or i", "edit the search"},
		{"↑↓ / j k", "navigate results"},
		{"g / G", "first / last result"},
		{"⏎ or o", "open the web page (node, events, YAML)"},
		{"s", "open k9s on the pod"},
		{"l", "follow logs (from the requested range)"},
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
	lines = append(lines, "", styleDim.Render("Time tokens restrict Grafana/Loki/Mimir links and logs."))
	box := styleBox.
		BorderForeground(colorAccent2).
		Padding(1, 2).
		Width(max(48, width-6)).
		Render(strings.Join(lines, "\n"))
	return box
}

func statusGlyph(pod kube.Pod) (string, lipgloss.Style) {
	switch {
	case pod.Phase == "Running" && pod.Ready:
		return "●", styleOK
	case pod.Phase == "Failed":
		return "●", styleErr
	case pod.Phase == "Pending" || !pod.Ready:
		return "●", styleWarn
	default:
		return "●", styleDim
	}
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
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func padRight(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) >= width {
		if len(runes) == width {
			return value
		}
		return string(runes[:width])
	}
	return value + strings.Repeat(" ", width-len(runes))
}
