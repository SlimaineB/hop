package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/SlimaineB/hop/internal/actions"
	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/web"
)

const (
	hopContextCookie     = "hop_context"
	hopContextSwitchPath = "/__hop/context"
)

type hopContextRuntime struct {
	service  *app.Service
	launcher *webLauncher
	handler  http.Handler
}

func (r *hopContextRuntime) close() {
	if r.launcher != nil {
		r.launcher.Close()
	}
	if r.service != nil {
		r.service.Close()
	}
}

type hopContextMux struct {
	mu          sync.Mutex
	app         *App
	set         *config.ContextSet
	infos       []web.ContextInfo
	defaultName string
	runtimes    map[string]*hopContextRuntime
}

func newHopContextMux(a *App, set *config.ContextSet) *hopContextMux {
	infos := make([]web.ContextInfo, 0, len(set.Contexts))
	for _, ctx := range set.Contexts {
		infos = append(infos, web.ContextInfo{Name: ctx.Name, Label: ctx.Name})
	}
	defaultName := a.contextName
	if defaultName == "" {
		defaultName = set.DefaultName
	}
	return &hopContextMux{
		app:         a,
		set:         set,
		infos:       infos,
		defaultName: defaultName,
		runtimes:    map[string]*hopContextRuntime{},
	}
}

func (m *hopContextMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == hopContextSwitchPath {
		m.handleSwitch(w, r)
		return
	}

	if requested := strings.TrimSpace(r.URL.Query().Get(hopContextCookie)); requested != "" {
		if _, ok := m.set.Find(requested); ok {
			setHopContextCookie(w, requested)
			next := *r.URL
			query := next.Query()
			query.Del(hopContextCookie)
			next.RawQuery = query.Encode()
			http.Redirect(w, r, next.RequestURI(), http.StatusFound)
			return
		}
	}
	name := m.activeName(r)
	runtime, err := m.runtime(name)
	if err != nil {
		http.Error(w, "hop context "+name+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	runtime.handler.ServeHTTP(w, r)
}

func (m *hopContextMux) activeName(r *http.Request) string {
	m.mu.Lock()
	defaultName := m.defaultName
	m.mu.Unlock()
	if cookie, err := r.Cookie(hopContextCookie); err == nil {
		if _, ok := m.set.Find(cookie.Value); ok {
			return cookie.Value
		}
	}
	if _, ok := m.set.Find(defaultName); ok {
		return defaultName
	}
	if ctx, ok := m.set.Default(); ok {
		return ctx.Name
	}
	return defaultName
}

func (m *hopContextMux) runtime(name string) (*hopContextRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if runtime, ok := m.runtimes[name]; ok {
		return runtime, nil
	}
	ctx, ok := m.set.Find(name)
	if !ok {
		return nil, fmt.Errorf("unknown context")
	}

	cfg, path, err := config.Load(ctx.Path)
	if err != nil {
		return nil, err
	}
	applyRuntimeOverrides(cfg, m.app.flags)
	service, err := app.New(cfg)
	if err != nil {
		return nil, err
	}
	launcher := newWebLauncher(service, cfg, path)
	server, err := launcher.newServer(ctx.Name, m.infos)
	if err != nil {
		service.Close()
		return nil, err
	}
	runtime := &hopContextRuntime{
		service:  service,
		launcher: launcher,
		handler:  server.Handler(),
	}
	m.runtimes[name] = runtime
	return runtime, nil
}

func (m *hopContextMux) handleSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !sameRequestOrigin(r) {
		http.Error(w, "cross-origin context switch refused", http.StatusForbidden)
		return
	}
	body := http.MaxBytesReader(w, r.Body, 4096)
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		http.Error(w, "invalid context payload", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(payload.Name)
	if _, ok := m.set.Find(name); !ok {
		http.Error(w, "unknown hop context", http.StatusBadRequest)
		return
	}

	setHopContextCookie(w, name)
	m.mu.Lock()
	m.defaultName = name
	_ = m.set.SetDefault(name)
	m.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (m *hopContextMux) Close() {
	m.mu.Lock()
	runtimes := m.runtimes
	m.runtimes = map[string]*hopContextRuntime{}
	m.mu.Unlock()
	for _, runtime := range runtimes {
		runtime.close()
	}
}

func (a *App) serveWebMulti(cmd *cobra.Command, query string) error {
	set := a.contexts
	if set == nil || len(set.Contexts) < 2 {
		return fmt.Errorf("multiple hop contexts are not available")
	}

	selected := a.contextName
	ctx, ok := set.Find(selected)
	if !ok {
		ctx, ok = set.Default()
	}
	if !ok {
		return fmt.Errorf("no hop context selected")
	}
	cfg, _, err := config.Load(ctx.Path)
	if err != nil {
		return err
	}
	applyRuntimeOverrides(cfg, a.flags)

	address := net.JoinHostPort(cfg.UI.Bind, strconv.Itoa(cfg.UI.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	mux := newHopContextMux(a, set)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()

	base := listenerURL(cfg.UI.Bind, listener)
	target := base + "/?" + hopContextCookie + "=" + url.QueryEscape(ctx.Name)
	if query != "" {
		target = base + "/search?q=" + url.QueryEscape(query) + "&" + hopContextCookie + "=" + url.QueryEscape(ctx.Name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "hop contexts: %s\n", strings.Join(set.Names(), ", "))
	fmt.Fprintf(cmd.OutOrStdout(), "hop UI: %s\n", target)
	if !a.flags.noOpen && cfg.UI.BrowserEnabled() {
		if err := actions.OpenBrowser(target); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "hop: browser unavailable: %v\n", err)
		}
	}

	sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	mux.Close()
	return nil
}

func listenerURL(bind string, listener net.Listener) string {
	host := bind
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	port := 0
	if tcp, ok := listener.Addr().(*net.TCPAddr); ok {
		port = tcp.Port
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func setHopContextCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     hopContextCookie,
		Value:    name,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   365 * 24 * 60 * 60,
	})
}

func sameRequestOrigin(r *http.Request) bool {
	if site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); site != "" &&
		site != "same-origin" && site != "same-site" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Host == r.Host
}
