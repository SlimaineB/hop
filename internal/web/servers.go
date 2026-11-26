package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/servers"
)

type serverData struct {
	Title      string
	Script     string
	Server     *servers.Server
	Check      *servers.Check
	SSHCommand string
	BackQuery  string
}

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if !servers.HasVMSelector(query) {
		if query == "" {
			query = "vm"
		} else {
			query += " vm"
		}
	}
	http.Redirect(w, r, "/?q="+url.QueryEscape(query), http.StatusTemporaryRedirect)
}

func (s *Server) handleServer(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.options.Servers == nil {
		s.render(w, http.StatusServiceUnavailable, "error.html", errorData{
			Title:   "Server inventory unavailable",
			Message: "The VM/server inventory is not configured for this UI instance.",
		})
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		s.render(w, http.StatusBadRequest, "error.html", errorData{
			Title:   "Invalid server link",
			Message: "Missing parameter: name is required.",
		})
		return
	}
	server, err := s.options.Servers.Find(name)
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		s.render(w, status, "error.html", errorData{
			Title:     "Unable to load server",
			Message:   err.Error(),
			BackQuery: r.URL.Query().Get("q"),
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result := s.options.Servers.CheckServer(ctx, *server)
	command, _ := s.options.Servers.SSHCommand(server.Name)
	s.render(w, http.StatusOK, "server.html", serverData{
		Title:      server.Name,
		Script:     "",
		Server:     server,
		Check:      &result,
		SSHCommand: command,
		BackQuery:  r.URL.Query().Get("q"),
	})
}

func (s *Server) handleAPIServers(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.options.Servers == nil {
		writeError(w, http.StatusServiceUnavailable, errText("server inventory unavailable"))
		return
	}
	values := r.URL.Query()
	limit := 200
	if raw := values.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 2000 {
			limit = parsed
		}
	}
	hits, err := s.options.Servers.Search(strings.TrimSpace(values.Get("q")), limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":    "servers",
		"servers": hits,
		"matched": len(hits),
		"query":   strings.TrimSpace(values.Get("q")),
	})
}

func (s *Server) handleAPIServer(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if s.options.Servers == nil {
		writeError(w, http.StatusServiceUnavailable, errText("server inventory unavailable"))
		return
	}
	values := r.URL.Query()
	name := strings.TrimSpace(values.Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, errText("name is required"))
		return
	}
	server, err := s.options.Servers.Find(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	payload := map[string]any{"server": server}
	if parsed, _ := strconv.ParseBool(values.Get("check")); parsed {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		result := s.options.Servers.CheckServer(ctx, *server)
		payload["check"] = result
	}
	if command, err := s.options.Servers.SSHCommand(server.Name); err == nil {
		payload["ssh_command"] = command
	}
	writeJSON(w, http.StatusOK, payload)
}
