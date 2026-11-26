package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/search"
)

type indexData struct {
	Title        string
	Script       string
	InitialQuery string
	Shortcuts    []config.ShortcutConfig
}

type podData struct {
	Title      string
	Script     string
	Detail     *app.Detail
	BackQuery  string
	K9s        string
	TimeRange  *search.TimeRange
	RangeLabel string
}

type errorData struct {
	Title     string
	Script    string
	Message   string
	BackQuery string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	shortcuts := make([]config.ShortcutConfig, 0, len(s.options.Shortcuts))
	for _, shortcut := range s.options.Shortcuts {
		if safeURL(shortcut.URL) != "" {
			shortcuts = append(shortcuts, shortcut)
		}
	}
	s.render(w, http.StatusOK, "index.html", indexData{
		Title:        "Search",
		Script:       "index.js",
		InitialQuery: query,
		Shortcuts:    shortcuts,
	})
}

func (s *Server) handlePod(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.backend == nil {
		s.render(w, http.StatusServiceUnavailable, "error.html", errorData{
			Title:   "Pod search unavailable",
			Message: "This UI instance only serves the VM/server inventory.",
		})
		return
	}
	values := r.URL.Query()
	ref := kube.PodRef{
		Context:   values.Get("context"),
		Namespace: values.Get("namespace"),
		Name:      values.Get("name"),
	}
	if ref.Context == "" || ref.Namespace == "" || ref.Name == "" {
		s.render(w, http.StatusBadRequest, "error.html", errorData{
			Title:   "Invalid pod link",
			Message: "Missing parameters: context, namespace, and name are required.",
		})
		return
	}
	query := search.Parse(values.Get("q"))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	detail, err := s.backend.Detail(ctx, ref, query.TimeRange)
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		s.render(w, status, "error.html", errorData{
			Title:     "Unable to load pod",
			Message:   err.Error(),
			BackQuery: values.Get("q"),
		})
		return
	}
	k9sCommand := ""
	for _, command := range detail.Commands {
		if command.ID == "k9s" {
			k9sCommand = command.Command
			break
		}
	}
	s.render(w, http.StatusOK, "pod.html", podData{
		Title:      ref.Name,
		Script:     "pod.js",
		Detail:     detail,
		BackQuery:  values.Get("q"),
		K9s:        k9sCommand,
		TimeRange:  query.TimeRange,
		RangeLabel: rangeLabel(query.TimeRange),
	})
}

func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, errText("pod search unavailable"))
		return
	}
	values := r.URL.Query()
	limit := 100
	if raw := values.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 2000 {
			limit = parsed
		}
	}
	refresh, _ := strconv.ParseBool(values.Get("refresh"))
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result, err := s.backend.Search(ctx, app.SearchRequest{
		Query:        strings.TrimSpace(values.Get("q")),
		Contexts:     parseMulti(values["context"]),
		Namespaces:   parseMulti(values["namespace"]),
		Env:          strings.TrimSpace(values.Get("env")),
		Limit:        limit,
		ForceRefresh: refresh,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAPIUnified(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	values := r.URL.Query()
	limit := 100
	if raw := values.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 2000 {
			limit = parsed
		}
	}
	refresh, _ := strconv.ParseBool(values.Get("refresh"))
	live, _ := strconv.ParseBool(values.Get("live"))
	if !live {
		live, _ = strconv.ParseBool(values.Get("local"))
	}
	tickets, _ := strconv.ParseBool(values.Get("tickets"))
	raw := strings.TrimSpace(values.Get("q"))
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result := s.registry.Search(ctx, plugin.Query{
		Raw:            raw,
		Parsed:         search.Parse(raw),
		Contexts:       parseMulti(values["context"]),
		Namespaces:     parseMulti(values["namespace"]),
		Env:            strings.TrimSpace(values.Get("env")),
		Limit:          limit,
		ForceRefresh:   refresh,
		Local:          live,
		IncludeNetwork: tickets && !live,
	})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAPIPod(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, errText("pod search unavailable"))
		return
	}
	values := r.URL.Query()
	ref := kube.PodRef{
		Context:   values.Get("context"),
		Namespace: values.Get("namespace"),
		Name:      values.Get("name"),
	}
	if ref.Context == "" || ref.Namespace == "" || ref.Name == "" {
		writeError(w, http.StatusBadRequest, errText("context, namespace and name are required"))
		return
	}
	query := search.Parse(values.Get("q"))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	detail, err := s.backend.Detail(ctx, ref, query.TimeRange)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleAPIContexts(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.backend == nil {
		writeJSON(w, http.StatusOK, map[string]any{"contexts": []any{}, "error": ""})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	contexts, err := s.backend.Contexts(ctx)
	if err != nil && len(contexts) == 0 {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contexts": contexts, "error": errorString(err)})
}

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.options.Stats != nil {
		writeJSON(w, http.StatusOK, s.options.Stats())
		return
	}
	if s.backend == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, s.backend.Stats())
}

func parseMulti(values []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

type errText string

func (e errText) Error() string { return string(e) }

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
