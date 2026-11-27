package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/SlimaineB/hop/internal/diag"
)

func diagSeverityStyle(severity diag.Severity) lipgloss.Style {
	switch severity {
	case diag.SeverityCritical:
		return styleErr
	case diag.SeverityWarning:
		return styleWarn
	default:
		return styleOK
	}
}

func diagSeverityChip(severity diag.Severity) string {
	return diagSeverityStyle(severity).Bold(true).Render(strings.ToUpper(string(severity)))
}

func diagSeverityRank(severity diag.Severity) int {
	switch severity {
	case diag.SeverityCritical:
		return 0
	case diag.SeverityWarning:
		return 1
	default:
		return 2
	}
}

func sortDiagReports(reports []*diag.Report) {
	sort.SliceStable(reports, func(i, j int) bool {
		left, right := diagSeverityRank(reports[i].Verdict), diagSeverityRank(reports[j].Verdict)
		if left != right {
			return left < right
		}
		return reports[i].Target < reports[j].Target
	})
}

func diagCounts(reports []*diag.Report) (critical, warning, healthy int) {
	for _, report := range reports {
		switch report.Verdict {
		case diag.SeverityCritical:
			critical++
		case diag.SeverityWarning:
			warning++
		default:
			healthy++
		}
	}
	return critical, warning, healthy
}

func diagDuration(value time.Duration) string {
	if value <= 0 {
		return "0ms"
	}
	if value < time.Second {
		return fmt.Sprintf("%dms", value.Milliseconds())
	}
	return value.Round(time.Millisecond).String()
}

func diagPanelContent(report *diag.Report, loading bool, err error, width, height, offset int) string {
	width = max(30, width)
	height = max(8, height)
	lines := []string{}
	switch {
	case loading && report == nil:
		lines = append(lines, styleMuted.Render("running diagnostics in parallel…"))
	case err != nil:
		lines = append(lines, styleErr.Render("error: "+clip(err.Error(), width-4)))
	case report == nil:
		lines = append(lines, styleMuted.Render("no diagnostic report yet"))
	default:
		meta := "checked " + report.CheckedAt.Local().Format("15:04:05")
		if report.Duration > 0 {
			meta += " · " + diagDuration(report.Duration)
		}
		lines = append(lines,
			diagSeverityChip(report.Verdict)+" "+styleQuery.Render(report.Summary),
			styleDim.Render(meta),
			"",
		)
		maxProbeRows := max(0, min(len(report.Probes), 8))
		if maxProbeRows > 0 {
			lines = append(lines, styleSection.Render("PROBES"))
			for _, probe := range report.Probes[:maxProbeRows] {
				state := "ok"
				style := styleOK
				if !probe.OK {
					state = strings.ToUpper(string(probe.Severity))
					style = diagSeverityStyle(probe.Severity)
				}
				detail := probe.Summary
				if probe.Error != "" && probe.Error != probe.Summary {
					detail += " — " + probe.Error
				}
				line := fmt.Sprintf("%-8s %-24s %s", state, clip(probe.Title, 24), clip(detail, max(12, width-48)))
				lines = append(lines, style.Render(clip(line, width-4))+styleDim.Render(" "+diagDuration(probe.Duration)))
			}
			if len(report.Probes) > maxProbeRows {
				lines = append(lines, styleDim.Render(fmt.Sprintf("… %d more probes", len(report.Probes)-maxProbeRows)))
			}
			lines = append(lines, "")
		}
		lines = append(lines, styleSection.Render(fmt.Sprintf("FINDINGS  %d", len(report.Findings))))
		if len(report.Findings) == 0 {
			lines = append(lines, styleMuted.Render("no finding"))
		}
		if offset < 0 {
			offset = 0
		}
		if offset > 0 && offset >= len(report.Findings) {
			offset = max(0, len(report.Findings)-1)
		}
		rowsPerFinding := 4
		maxFindings := max(1, (height-len(lines)-4)/rowsPerFinding)
		if len(report.Findings) > maxFindings {
			visible := report.Findings[offset:min(len(report.Findings), offset+maxFindings)]
			for _, finding := range visible {
				lines = append(lines, diagFindingLines(finding, width-4)...)
			}
			lines = append(lines, styleDim.Render(fmt.Sprintf("… %d more findings (pgup/pgdown to scroll)", len(report.Findings)-len(visible))))
		} else {
			for _, finding := range report.Findings {
				lines = append(lines, diagFindingLines(finding, width-4)...)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func diagFindingLines(finding diag.Finding, width int) []string {
	lines := []string{diagSeverityChip(finding.Severity) + " " + styleQuery.Render(clip(finding.Title, max(12, width-14)))}
	if finding.Detail != "" {
		lines = append(lines, "  "+styleMuted.Render(clip(finding.Detail, max(10, width-4))))
	}
	for _, evidence := range finding.Evidence {
		lines = append(lines, "  "+styleDim.Render("· "+clip(evidence, max(10, width-6))))
	}
	if finding.Remedy != "" {
		lines = append(lines, "  "+styleHelpCmd.Render("next: "+clip(finding.Remedy, max(10, width-10))))
	}
	return lines
}

func diagPanelView(report *diag.Report, loading bool, err error, width, height, offset int) string {
	width = max(30, width)
	height = max(8, height)
	return styleBox.Width(width - 2).Height(height).Render(diagPanelContent(report, loading, err, width-4, height-2, offset))
}

func diagScreenView(title, target string, report *diag.Report, loading bool, err error, width, height, offset int, hints string) string {
	width = max(60, width)
	height = max(20, height)
	header := styleTitle.Render("hop · "+title) + "\n" +
		styleQuery.Render(target) + "\n" +
		styleMuted.Render(clip(diagStatusLine(report, loading, err), width-2))
	footer := styleDim.Render(strings.Repeat("─", max(1, width-2))) + "\n" + styleDim.Render(clip(hints, width-2))
	bodyHeight := height - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	body := diagPanelView(report, loading, err, width, bodyHeight, offset)
	return strings.Join([]string{header, body, footer}, "\n")
}

func diagStatusLine(report *diag.Report, loading bool, err error) string {
	parts := []string{}
	if loading {
		parts = append(parts, "refreshing…")
	}
	if err != nil {
		parts = append(parts, "error: "+err.Error())
	} else if report != nil {
		parts = append(parts, diagSeverityChip(report.Verdict), report.Summary)
		if report.Duration > 0 {
			parts = append(parts, diagDuration(report.Duration))
		}
	}
	return strings.Join(parts, "  ·  ")
}
