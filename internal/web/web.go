package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/dashboard"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/plugin"
	"github.com/SlimaineB/hop/internal/search"
	"github.com/SlimaineB/hop/internal/servers"
	"github.com/SlimaineB/hop/internal/version"
)

//go:embed templates/*.html static/*
var content embed.FS

type Backend interface {
	Contexts(ctx context.Context) ([]kube.Context, error)
	Search(ctx context.Context, req app.SearchRequest) (*app.SearchResult, error)
	SearchLocal(req app.SearchRequest) (*app.SearchResult, error)
	Detail(ctx context.Context, ref kube.PodRef, rng *search.TimeRange) (*app.Detail, error)
	Stats() app.Stats
}

type Options struct {
	Bind      string
	Port      int
	Theme     string
	Servers   *servers.Service
	Shortcuts []config.ShortcutConfig
	LiveLimit int
	Registry  *plugin.Registry
	Stats     func() app.Stats
	Dashboard *dashboard.Service
}

type Server struct {
	backend   Backend
	options   Options
	registry  *plugin.Registry
	dashboard *dashboard.Service
	tmpl      *template.Template
	mux       *http.ServeMux

	mu      sync.Mutex
	http    *http.Server
	ln      net.Listener
	started bool
}

func New(backend Backend, options Options) (*Server, error) {
	if options.Bind == "" {
		options.Bind = "127.0.0.1"
	}
	if options.Theme == "" {
		options.Theme = "dark"
	}
	funcs := template.FuncMap{
		"age":          humanAge,
		"pct":          func(score float64) int { return int(score*100 + 0.5) },
		"statusClass":  statusClass,
		"statusText":   statusText,
		"hasLinks":     func(links config.RenderedLinks) bool { return links.HasAny() },
		"fmtTime":      formatTime,
		"version":      func() string { return version.Version },
		"json":         toJSON,
		"lower":        strings.ToLower,
		"urlquery":     url.QueryEscape,
		"keys":         sortedKeys,
		"truncate":     truncate,
		"fmtBytes":     servers.FormatBytes,
		"httpClass":    httpClass,
		"httpText":     httpText,
		"machineClass": machineClass,
		"dash":         dash,
		"safeURL":      safeURL,
	}
	tmpl, err := template.New("hop").Funcs(funcs).ParseFS(content, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	registry := options.Registry
	if registry == nil {
		registry = plugin.NewRegistry()
	}
	server := &Server{
		backend:   backend,
		options:   options,
		registry:  registry,
		dashboard: options.Dashboard,
		tmpl:      tmpl,
		mux:       http.NewServeMux(),
	}
	server.routes()
	return server, nil
}

func (s *Server) routes() {
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	staticHandler := http.FileServer(http.FS(staticFS))

	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/dashboard", s.handleDashboard)
	s.mux.HandleFunc("/search", s.handleIndex)
	s.mux.HandleFunc("/pod", s.handlePod)
	s.mux.HandleFunc("/server", s.handleServer)
	s.mux.HandleFunc("/servers", s.handleServers)
	s.mux.Handle("/static/", http.StripPrefix("/static/", staticHandler))
	s.mux.HandleFunc("/api/dashboard", s.handleAPIDashboard)
	s.mux.HandleFunc("/api/dashboard/servers", s.handleAPIDashboardServers)
	s.mux.HandleFunc("/api/search", s.handleAPISearch)
	s.mux.HandleFunc("/api/unified", s.handleAPIUnified)
	s.mux.HandleFunc("/api/pod", s.handleAPIPod)
	s.mux.HandleFunc("/api/servers", s.handleAPIServers)
	s.mux.HandleFunc("/api/server", s.handleAPIServer)
	s.mux.HandleFunc("/api/contexts", s.handleAPIContexts)
	s.mux.HandleFunc("/api/stats", s.handleAPIStats)
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	s.mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Server) Handler() http.Handler { return s.withSecurity(s.mux) }

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	address := net.JoinHostPort(s.options.Bind, strconv.Itoa(s.options.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	s.ln = listener
	s.http = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	s.started = true
	go func() {
		_ = s.http.Serve(listener)
	}()
	return nil
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return net.JoinHostPort(s.options.Bind, strconv.Itoa(s.options.Port))
	}
	return s.ln.Addr().String()
}

func (s *Server) BaseURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	host := s.options.Bind
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	port := s.options.Port
	if s.ln != nil {
		if tcp, ok := s.ln.Addr().(*net.TCPAddr); ok {
			port = tcp.Port
		}
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	server := s.http
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

func (s *Server) withSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/pod" || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(true)
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func onlyGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func safeURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return value
}

func httpClass(check servers.HTTPCheck) string {
	if strings.TrimSpace(check.URL) == "" {
		return "muted"
	}
	if check.OK {
		return "ok"
	}
	return "err"
}

func httpText(check servers.HTTPCheck) string {
	if strings.TrimSpace(check.URL) == "" {
		return "not configured"
	}
	if check.OK {
		return "ok " + strconv.Itoa(check.StatusCode) + " · " + strconv.FormatInt(check.LatencyMS, 10) + "ms"
	}
	if check.Error != "" {
		return check.Error
	}
	return "error " + strconv.Itoa(check.StatusCode) + " · " + strconv.FormatInt(check.LatencyMS, 10) + "ms"
}

func machineClass(info servers.MachineInfo) string {
	if info.OK {
		return "ok"
	}
	return "err"
}

func dash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
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

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func rangeLabel(rng *search.TimeRange) string {
	if rng == nil || rng.From.IsZero() || rng.To.IsZero() {
		return ""
	}
	return rng.From.Local().Format("02/01 15:04") + " → " + rng.To.Local().Format("02/01 15:04")
}

func statusClass(pod kube.Pod) string {
	switch {
	case pod.Phase == "Running" && pod.Ready:
		return "ok"
	case pod.Phase == "Failed":
		return "err"
	case pod.Phase == "Succeeded":
		return "muted"
	case pod.Phase == "Pending" || !pod.Ready:
		return "warn"
	default:
		return "muted"
	}
}

func statusText(pod kube.Pod) string {
	if pod.Reason != "" {
		return pod.Reason
	}
	return pod.Phase
}

func toJSON(value any) template.JS {
	data, err := json.Marshal(value)
	if err != nil {
		return template.JS("null")
	}
	return template.JS(data)
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit || limit <= 0 {
		return value
	}
	return string(runes[:limit-1]) + "…"
}
