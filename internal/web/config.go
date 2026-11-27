package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"gopkg.in/yaml.v3"
)

const maxConfigUpload = 1 << 20

type configPageData struct {
	Title      string
	Script     string
	ConfigPath string
	ReadOnly   bool
}

type configSummary struct {
	Apps        int `json:"apps"`
	Servers     int `json:"servers"`
	Shortcuts   int `json:"shortcuts"`
	Providers   int `json:"providers"`
	Workspaces  int `json:"workspaces"`
	Kubeconfigs int `json:"kubeconfigs"`
	GitRoots    int `json:"git_roots"`
	Registries  int `json:"registries"`
	Plugins     int `json:"plugins"`
}

type configView struct {
	Path      string        `json:"path"`
	Exists    bool          `json:"exists"`
	ReadOnly  bool          `json:"read_only"`
	Revision  string        `json:"revision"`
	YAML      string        `json:"yaml"`
	Summary   configSummary `json:"summary"`
	Error     string        `json:"error,omitempty"`
	UpdatedAt *time.Time    `json:"updated_at,omitempty"`
}

type configSaveRequest struct {
	Revision string `json:"revision"`
	YAML     string `json:"yaml"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if !onlyGET(w, r) {
		return
	}
	if r.URL.Path != "/config" {
		http.NotFound(w, r)
		return
	}
	s.render(w, http.StatusOK, "config.html", configPageData{
		Title:      "Config",
		Script:     "config.js",
		ConfigPath: s.options.ConfigPath,
		ReadOnly:   strings.TrimSpace(s.options.ConfigPath) == "",
	})
}

func (s *Server) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		view, err := s.loadConfigView()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPost:
		s.saveConfig(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) loadConfigView() (configView, error) {
	path := strings.TrimSpace(s.options.ConfigPath)
	if path == "" {
		view := configView{
			ReadOnly: true,
			YAML:     s.options.ConfigSnapshot,
			Summary:  configSummary{Apps: len(s.options.Apps), Shortcuts: len(s.options.Shortcuts)},
			Error:    "This UI instance uses demo or synthetic config. Start with `hop web` to edit a real config file.",
		}
		if summary, err := summarizeConfig([]byte(view.YAML)); err == nil {
			view.Summary = summary
		}
		return view, nil
	}

	data, exists, updated, err := readConfigFile(path)
	if err != nil {
		return configView{}, err
	}
	view := configView{
		Path:     path,
		Exists:   exists,
		Revision: hashConfig(data),
		YAML:     string(data),
	}
	if updated != nil {
		view.UpdatedAt = updated
	}
	if !exists {
		view.YAML = defaultConfigYAML()
	}
	summary, parseErr := summarizeConfig(data)
	if parseErr != nil {
		view.Error = parseErr.Error()
	}
	if !exists {
		view.Error = ""
	}
	view.Summary = summary
	return view, nil
}

func (s *Server) saveConfig(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(s.options.ConfigPath)
	if path == "" {
		writeError(w, http.StatusConflict, errors.New("config editing is disabled for this UI instance"))
		return
	}
	if !sameOriginRequest(r) {
		writeError(w, http.StatusForbidden, errors.New("cross-origin config writes are not allowed"))
		return
	}
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, errors.New("content type must be application/json"))
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxConfigUpload)
	defer body.Close()
	var request configSaveRequest
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if !strings.HasSuffix(request.YAML, "\n") {
		request.YAML += "\n"
	}
	if strings.TrimSpace(request.YAML) == "" {
		writeError(w, http.StatusBadRequest, errors.New("config cannot be empty"))
		return
	}

	current, _, _, err := readConfigFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if request.Revision != hashConfig(current) {
		writeError(w, http.StatusConflict, errors.New("config changed on disk; reload before saving"))
		return
	}

	summary, err := summarizeConfig([]byte(request.YAML))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := writeConfigFile(path, []byte(request.YAML)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	data := []byte(request.YAML)
	updated := time.Now()
	writeJSON(w, http.StatusOK, configView{
		Path:      path,
		Exists:    true,
		Revision:  hashConfig(data),
		YAML:      request.YAML,
		Summary:   summary,
		UpdatedAt: &updated,
	})
}

func readConfigFile(path string) ([]byte, bool, *time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil, nil
		}
		return nil, false, nil, fmt.Errorf("read config: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, true, nil, nil
	}
	updated := info.ModTime()
	return data, true, &updated, nil
}

func writeConfigFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".hop-config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func summarizeConfig(data []byte) (configSummary, error) {
	var summary configSummary
	if len(bytes.TrimSpace(data)) == 0 {
		return summary, nil
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return summary, fmt.Errorf("invalid yaml: %w", err)
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return summary, nil
		}
		node = *node.Content[0]
	}
	if node.Kind != 0 && node.Kind != yaml.MappingNode {
		return summary, errors.New("invalid yaml: top-level document must be a mapping")
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return summary, fmt.Errorf("invalid config: %w", err)
	}
	cfg.Normalize()
	summary = configSummary{
		Apps:        len(cfg.Apps),
		Servers:     len(cfg.Servers),
		Shortcuts:   len(cfg.Shortcuts),
		Providers:   len(cfg.Providers),
		Workspaces:  len(cfg.Workspaces),
		Kubeconfigs: len(cfg.Kube.Kubeconfigs),
		GitRoots:    len(cfg.Git.Roots),
		Registries:  len(cfg.Packages.Registries),
		Plugins:     len(cfg.Plugins),
	}
	return summary, nil
}

func defaultConfigYAML() string {
	data, err := yaml.Marshal(config.Default())
	if err != nil {
		return ""
	}
	return "# hop configuration\n# Edit and save from the local hop UI.\n" + string(data)
}

func hashConfig(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sameOriginRequest(r *http.Request) bool {
	if site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}
