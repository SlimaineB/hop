package web

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/SlimaineB/hop/internal/diag"
)

type diagnosticsData struct {
	Title    string
	Script   string
	AppCount int
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	s.render(w, http.StatusOK, "diagnostics.html", diagnosticsData{
		Title:    "Diagnostics",
		Script:   "diagnostics.js",
		AppCount: len(s.options.Apps),
	})
}

func (s *Server) handleAPIDiagnosticsApps(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.diagEngine == nil {
		writeError(w, http.StatusServiceUnavailable, errText("diagnostics unavailable"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	reports := s.diagEngine.DiagnoseApps(ctx, s.options.Apps)
	sort.SliceStable(reports, func(i, j int) bool {
		left, right := diagnosticRank(reports[i].Verdict), diagnosticRank(reports[j].Verdict)
		if left != right {
			return left < right
		}
		return reports[i].Target < reports[j].Target
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"apps":       reports,
		"configured": len(s.options.Apps),
		"checked_at": time.Now(),
		"duration":   time.Since(start),
	})
}

func diagnosticRank(severity diag.Severity) int {
	switch severity {
	case diag.SeverityCritical:
		return 0
	case diag.SeverityWarning:
		return 1
	default:
		return 2
	}
}
