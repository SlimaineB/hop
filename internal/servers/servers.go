package servers

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/search"
)

type Server struct {
	Name          string   `json:"name"`
	SSH           string   `json:"ssh,omitempty"`
	Host          string   `json:"host,omitempty"`
	User          string   `json:"user,omitempty"`
	Port          int      `json:"port,omitempty"`
	ProxyJump     string   `json:"proxy_jump,omitempty"`
	Env           string   `json:"env,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	MetricsURL    string   `json:"metrics_url,omitempty"`
	HealthURL     string   `json:"health_url,omitempty"`
	SSHChecks     bool     `json:"ssh_checks"`
	NetworkChecks bool     `json:"network_checks"`
	CheckPorts    []int    `json:"check_ports,omitempty"`
	Source        string   `json:"source,omitempty"`
}

type Hit struct {
	Server  Server         `json:"server"`
	Score   float64        `json:"score"`
	Percent int            `json:"percent"`
	Matches []search.Match `json:"matches,omitempty"`
	Reasons []string       `json:"reasons,omitempty"`
}

type Service struct {
	cfg     *config.Config
	sshPath string

	mu          sync.RWMutex
	snapshot    []Server
	snapshotSet bool
}

func New(cfg *config.Config) *Service {
	if cfg == nil {
		cfg = config.Default()
	}
	path := cfg.SSHConfig
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".ssh", "config")
		}
	}
	return &Service{cfg: cfg, sshPath: path}
}

func (s *Service) Config() *config.Config {
	return s.cfg
}

func (s *Service) List() ([]Server, error) {
	type serverEntry struct {
		server   Server
		explicit bool
	}
	entries := map[string]serverEntry{}
	ssh := loadSSHConfig(s.sshPath)
	for _, alias := range ssh.aliases {
		server := serverFromSSH(alias, ssh.resolve(alias))
		server.Source = "ssh"
		entries[strings.ToLower(server.Name)] = serverEntry{server: server}
	}
	for i := range s.cfg.Servers {
		server, err := s.serverFromConfig(s.cfg.Servers[i], ssh)
		if err != nil {
			return nil, err
		}
		entries[strings.ToLower(server.Name)] = serverEntry{server: server, explicit: true}
	}
	out := make([]Server, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.server)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Service) serverFromConfig(entry config.ServerConfig, ssh *sshConfig) (Server, error) {
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		return Server{}, fmt.Errorf("server entry without a name")
	}
	server := Server{
		Name:       name,
		SSH:        strings.TrimSpace(entry.SSH),
		Host:       strings.TrimSpace(entry.Host),
		User:       strings.TrimSpace(entry.User),
		Port:       entry.Port,
		ProxyJump:  strings.TrimSpace(entry.ProxyJump),
		Env:        strings.TrimSpace(entry.Env),
		Tags:       entry.Tags,
		MetricsURL: strings.TrimSpace(entry.MetricsURL),
		HealthURL:  strings.TrimSpace(entry.HealthURL),
		CheckPorts: normalizePorts(entry.CheckPorts),
		Source:     "config",
	}
	if server.SSH == "" && server.Host == "" {
		server.SSH = name
	}
	resolveSSH(&server, ssh)
	if server.Env == "" {
		server.Env = config.InferEnv(strings.Join([]string{server.Name, server.Host, strings.Join(server.Tags, " ")}, " "))
	}
	server.Tags = mergeTags(server.Tags, aliasTokens(server.Name, server.SSH))
	server.SSHChecks = entry.SSHChecks == nil || *entry.SSHChecks
	server.NetworkChecks = entry.NetworkChecks == nil || *entry.NetworkChecks
	server.MetricsURL = defaultMetricsURL(server)
	return server, nil
}

func serverFromSSH(alias string, options map[string]string) Server {
	server := Server{
		Name:      alias,
		SSH:       alias,
		Host:      options["hostname"],
		User:      options["user"],
		ProxyJump: options["proxyjump"],
		Source:    "ssh",
	}
	if server.Host == "" {
		server.Host = alias
	}
	server.Port = parsePort(options["port"])
	resolveSSH(&server, nil)
	server.Env = config.InferEnv(strings.Join([]string{server.Name, server.Host, strings.Join(server.Tags, " ")}, " "))
	server.Tags = mergeTags(nil, aliasTokens(server.Name))
	server.SSHChecks = true
	server.NetworkChecks = true
	server.MetricsURL = defaultMetricsURL(server)
	return server
}

func resolveSSH(server *Server, ssh *sshConfig) {
	target := server.SSH
	if target == "" {
		target = server.Name
	}
	if ssh != nil {
		options := ssh.resolve(target)
		if server.Host == "" {
			server.Host = options["hostname"]
		}
		if server.User == "" {
			server.User = options["user"]
		}
		if server.Port == 0 {
			server.Port = parsePort(options["port"])
		}
		if server.ProxyJump == "" {
			server.ProxyJump = options["proxyjump"]
		}
	}
	if server.Host == "" {
		server.Host = target
	}
	if server.Port == 0 {
		server.Port = 22
	}
}

func normalizePorts(values []int) []int {
	out := make([]int, 0, len(values))
	seen := map[int]bool{}
	for _, port := range values {
		if port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	return out
}

func parsePort(value string) int {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

func defaultMetricsURL(server Server) string {
	if server.MetricsURL != "" {
		return server.MetricsURL
	}
	host := server.Host
	if host == "" {
		host = server.Name
	}
	return "http://" + net.JoinHostPort(host, "9100") + "/metrics"
}

func mergeTags(explicit, derived []string) []string {
	out := make([]string, 0, len(explicit)+len(derived))
	seen := map[string]bool{}
	for _, value := range append(append([]string{}, explicit...), derived...) {
		for _, tag := range strings.Split(value, ",") {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}

func aliasTokens(values ...string) []string {
	var out []string
	for _, value := range values {
		out = append(out, strings.FieldsFunc(value, func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
		})...)
	}
	return out
}

func (s *Service) Find(name string) (*Server, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if strings.EqualFold(list[i].Name, strings.TrimSpace(name)) {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("server not found: %s", name)
}

func (s *Service) Search(raw string, limit int) ([]Hit, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.snapshot = list
	s.snapshotSet = true
	s.mu.Unlock()
	return s.searchList(list, raw, limit)
}

// SearchLocal ranks against the last inventory snapshot instead of re-reading
// the SSH config and hop config on every keystroke. It is used by the TUI to
// filter live like the pod TUI; call Refresh to pick up inventory changes.
func (s *Service) SearchLocal(raw string, limit int) ([]Hit, error) {
	s.mu.RLock()
	list := s.snapshot
	ready := s.snapshotSet
	s.mu.RUnlock()
	if !ready {
		fresh, err := s.List()
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.snapshot = fresh
		s.snapshotSet = true
		s.mu.Unlock()
		list = fresh
	}
	return s.searchList(list, raw, limit)
}

// Refresh invalidates the in-memory inventory snapshot.
func (s *Service) Refresh() {
	s.mu.Lock()
	s.snapshot = nil
	s.snapshotSet = false
	s.mu.Unlock()
}

func (s *Service) searchList(list []Server, raw string, limit int) ([]Hit, error) {
	query := search.Parse(normalizeQuery(raw))
	candidates := make([]search.Candidate, 0, len(list))
	byID := make(map[string]Server, len(list))
	for _, server := range list {
		candidate := search.NewCandidate(server.Name)
		candidate.Add(search.FieldServer, server.Name)
		candidate.Add(search.FieldHost, server.Host)
		candidate.Add(search.FieldSSH, server.SSH)
		candidate.Add(search.FieldUser, server.User)
		candidate.Add(search.FieldEnv, server.Env)
		for _, tag := range server.Tags {
			candidate.Add(search.FieldTag, tag)
		}
		candidate.Boost = 0
		candidates = append(candidates, candidate)
		byID[server.Name] = server
	}
	ranked := search.NewEngine().Rank(query, candidates, limit)
	hits := make([]Hit, 0, len(ranked))
	for _, hit := range ranked {
		server, ok := byID[hit.Candidate.ID]
		if !ok {
			continue
		}
		if len(query.Terms) > 0 {
			if len(hit.Matches) < len(query.Terms) {
				continue
			}
			if hit.Score < s.cfg.Search.MinScore {
				continue
			}
		}
		hits = append(hits, Hit{
			Server:  server,
			Score:   hit.Score,
			Percent: hit.Percent(),
			Matches: hit.Matches,
			Reasons: hit.Reasons,
		})
	}
	return hits, nil
}

func normalizeQuery(raw string) string {
	tokens := strings.Fields(raw)
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		lower := strings.ToLower(strings.Trim(token, `"'`))
		if lower == "vm" || lower == "vms" {
			continue
		}
		if strings.HasPrefix(lower, "name:") {
			token = "server:" + token[len("name:"):]
		}
		out = append(out, token)
	}
	return strings.Join(out, " ")
}

func HasVMSelector(raw string) bool {
	for _, token := range strings.Fields(raw) {
		switch strings.ToLower(strings.Trim(token, `"'`)) {
		case "vm", "vms":
			return true
		}
	}
	return false
}

func (s *Service) SSHArgs(name string) ([]string, error) {
	server, err := s.Find(name)
	if err != nil {
		return nil, err
	}
	args := s.sshConfigArgs()
	return append(args, sshArgsFor(*server)...), nil
}

func (s *Service) sshConfigArgs() []string {
	if s.sshPath == "" {
		return nil
	}
	if _, err := os.Stat(s.sshPath); err != nil {
		return nil
	}
	return []string{"-F", s.sshPath}
}

func (s *Service) SSHCommand(name string) (string, error) {
	args, err := s.SSHArgs(name)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "ssh")
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " "), nil
}

func sshArgsFor(server Server) []string {
	args := []string{"-o", "ConnectTimeout=10"}
	args = append(args, sshTargetArgs(server)...)
	return args
}

func sshTargetArgs(server Server) []string {
	if server.SSH != "" {
		return []string{server.SSH}
	}
	var args []string
	if server.User != "" {
		args = append(args, "-l", server.User)
	}
	if server.Port > 0 && server.Port != 22 {
		args = append(args, "-p", strconv.Itoa(server.Port))
	}
	if server.ProxyJump != "" {
		args = append(args, "-J", server.ProxyJump)
	}
	return append(args, server.Host)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	safe := true
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune("-_./:@%+=,", r) {
			safe = false
			break
		}
	}
	if safe {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
