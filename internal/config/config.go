package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/glob"
	"gopkg.in/yaml.v3"
)

const EnvConfig = "HOP_CONFIG"

var envPatterns = []struct {
	env string
	re  *regexp.Regexp
}{
	{"preprod", regexp.MustCompile(`(?i)(^|[\s-_.])(preprod|pre-prod|pprod)([\s-_.]|$)`)},
	{"prod", regexp.MustCompile(`(?i)(^|[\s-_.])(prod|production|prd)([\s-_.]|$)`)},
	{"staging", regexp.MustCompile(`(?i)(^|[\s-_.])(staging|stage|stg)([\s-_.]|$)`)},
	{"dev", regexp.MustCompile(`(?i)(^|[\s-_.])(dev|development|develop)([\s-_.]|$)`)},
	{"test", regexp.MustCompile(`(?i)(^|[\s-_.])(test|qa|uat|recette)([\s-_.]|$)`)},
	{"sandbox", regexp.MustCompile(`(?i)(^|[\s-_.])(sandbox|sbx|lab)([\s-_.]|$)`)},
	{"local", regexp.MustCompile(`(?i)(^|[\s-_.])(local|kind|k3d|k3s|minikube|docker-desktop)([\s-_.]|$)`)},
}

func Default() *Config {
	return &Config{
		Kube: Kube{
			Concurrency:    12,
			RequestTimeout: Duration(10 * time.Second),
			CacheTTL:       Duration(20 * time.Second),
		},
		UI: UI{
			Bind:  "127.0.0.1",
			Port:  0,
			Theme: "dark",
		},
		Search: Search{
			MinScore:  0.35,
			Limit:     50,
			LiveLimit: 200,
		},
		Git: Git{
			Roots:    []string{"~/dev", "~/src", "~/code", "~/projects"},
			MaxDepth: 3,
			CacheTTL: Duration(30 * time.Second),
		},
	}
}

func DefaultPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "hop", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "hop", "config.yaml")
	}
	return filepath.Join(home, ".config", "hop", "config.yaml")
}

func Load(explicitPath string) (*Config, string, error) {
	configPath := explicitPath
	explicit := configPath != ""
	if configPath == "" {
		configPath = os.Getenv(EnvConfig)
		explicit = configPath != ""
	}
	if configPath == "" {
		configPath = DefaultPath()
	}
	configPath = ExpandPath(configPath)

	cfg := Default()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return cfg, configPath, nil
		}
		return nil, configPath, fmt.Errorf("read config %s: %w", configPath, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, configPath, fmt.Errorf("parse config %s: %w", configPath, err)
	}
	cfg.Normalize()
	return cfg, configPath, nil
}

func (c *Config) Normalize() {
	if c.Kube.Concurrency <= 0 {
		c.Kube.Concurrency = 12
	}
	if c.Kube.Concurrency > 256 {
		c.Kube.Concurrency = 256
	}
	if c.Kube.RequestTimeout.Get() <= 0 {
		c.Kube.RequestTimeout = Duration(10 * time.Second)
	}
	if c.Kube.CacheTTL.Get() < 0 {
		c.Kube.CacheTTL = 0
	}
	if c.UI.Bind == "" {
		c.UI.Bind = "127.0.0.1"
	}
	if c.UI.Port < 0 || c.UI.Port > 65535 {
		c.UI.Port = 0
	}
	if c.UI.Theme == "" {
		c.UI.Theme = "dark"
	}
	if c.Search.MinScore <= 0 {
		c.Search.MinScore = 0.35
	}
	if c.Search.Limit <= 0 {
		c.Search.Limit = 50
	}
	if c.Search.LiveLimit <= 0 {
		c.Search.LiveLimit = 200
	}
	for i := range c.Kube.Kubeconfigs {
		c.Kube.Kubeconfigs[i] = ExpandPath(c.Kube.Kubeconfigs[i])
	}
	for i := range c.Workspaces {
		if c.Workspaces[i].Context == "" {
			c.Workspaces[i].Context = c.Workspaces[i].Name
		}
	}
	if c.SSHConfig != "" {
		c.SSHConfig = ExpandPath(c.SSHConfig)
	}
	for i := range c.Shortcuts {
		shortcut := &c.Shortcuts[i]
		shortcut.Name = strings.TrimSpace(shortcut.Name)
		shortcut.URL = strings.TrimSpace(shortcut.URL)
		shortcut.Description = strings.TrimSpace(shortcut.Description)
		shortcut.Keywords = cleanList(shortcut.Keywords)
	}
	c.Issues.DefaultProvider = strings.ToLower(strings.TrimSpace(c.Issues.DefaultProvider))
	if c.Providers != nil {
		normalizedProviders := make(map[string]ProviderConfig, len(c.Providers))
		for id, provider := range c.Providers {
			id = strings.ToLower(strings.TrimSpace(id))
			if id == "" {
				continue
			}
			provider.Type = strings.ToLower(strings.TrimSpace(provider.Type))
			provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
			provider.Username = strings.TrimSpace(provider.Username)
			provider.UsernameEnv = strings.TrimSpace(provider.UsernameEnv)
			provider.TokenEnv = strings.TrimSpace(provider.TokenEnv)
			provider.AuthScheme = strings.ToLower(strings.TrimSpace(provider.AuthScheme))
			provider.Keywords = cleanList(provider.Keywords)
			if provider.Defaults != nil {
				defaults := make(map[string]string, len(provider.Defaults))
				for key, value := range provider.Defaults {
					key = strings.ToLower(strings.TrimSpace(key))
					if key == "" {
						continue
					}
					defaults[key] = strings.TrimSpace(value)
				}
				provider.Defaults = defaults
			}
			if provider.Timeout.Get() <= 0 {
				provider.Timeout = Duration(15 * time.Second)
			}
			normalizedProviders[id] = provider
		}
		c.Providers = normalizedProviders
	}
	for i := range c.Servers {
		server := &c.Servers[i]
		server.Name = strings.TrimSpace(server.Name)
		server.SSH = strings.TrimSpace(server.SSH)
		server.Host = strings.TrimSpace(server.Host)
		server.User = strings.TrimSpace(server.User)
		server.ProxyJump = strings.TrimSpace(server.ProxyJump)
		server.Env = strings.TrimSpace(server.Env)
		server.MetricsURL = strings.TrimSpace(server.MetricsURL)
		server.HealthURL = strings.TrimSpace(server.HealthURL)
		server.Tags = cleanList(server.Tags)
	}
	for i := range c.Git.Roots {
		c.Git.Roots[i] = ExpandPath(c.Git.Roots[i])
	}
	for i := range c.Git.Repos {
		c.Git.Repos[i] = ExpandPath(c.Git.Repos[i])
	}
	c.Git.Roots = cleanList(c.Git.Roots)
	c.Git.Repos = cleanList(c.Git.Repos)
	c.Git.Exclude = cleanList(c.Git.Exclude)
	if c.Git.MaxDepth <= 0 {
		c.Git.MaxDepth = 3
	}
	if c.Git.MaxDepth > 12 {
		c.Git.MaxDepth = 12
	}
	if c.Git.CacheTTL.Get() <= 0 {
		c.Git.CacheTTL = Duration(30 * time.Second)
	}
	if c.Git.Providers != nil {
		providers := make(map[string]string, len(c.Git.Providers))
		for host, provider := range c.Git.Providers {
			host = strings.ToLower(strings.TrimSpace(host))
			provider = strings.ToLower(strings.TrimSpace(provider))
			if host == "" || provider == "" {
				continue
			}
			providers[host] = provider
		}
		c.Git.Providers = providers
	}
	if c.Packages.CacheTTL.Get() <= 0 {
		c.Packages.CacheTTL = Duration(5 * time.Minute)
	}
	if c.Packages.Timeout.Get() <= 0 {
		c.Packages.Timeout = Duration(8 * time.Second)
	}
	if c.Packages.TagLimit <= 0 {
		c.Packages.TagLimit = 50
	}
	c.Sources.Disabled = normalizeDisabled(c.Sources.Disabled)
	c.Packages.Images = cleanList(c.Packages.Images)
	for i := range c.Packages.Registries {
		registry := &c.Packages.Registries[i]
		registry.Name = strings.TrimSpace(registry.Name)
		registry.Host = strings.ToLower(strings.TrimSpace(registry.Host))
		registry.Host = strings.TrimPrefix(registry.Host, "https://")
		registry.Host = strings.TrimPrefix(registry.Host, "http://")
		registry.Host = strings.TrimSuffix(registry.Host, "/")
		registry.Type = strings.ToLower(strings.TrimSpace(registry.Type))
		registry.Username = strings.TrimSpace(registry.Username)
		registry.UsernameEnv = strings.TrimSpace(registry.UsernameEnv)
		registry.TokenEnv = strings.TrimSpace(registry.TokenEnv)
		registry.Images = cleanList(registry.Images)
		if registry.Type == "" {
			lower := registry.Host
			switch {
			case strings.Contains(lower, "ghcr"):
				registry.Type = "ghcr"
			case strings.Contains(lower, "gitlab"):
				registry.Type = "gitlab"
			case strings.Contains(lower, "docker"):
				registry.Type = "docker"
			default:
				registry.Type = "registry"
			}
		}
	}
	plugins := make([]PluginConfig, 0, len(c.Plugins))
	for _, plugin := range c.Plugins {
		plugin.ID = strings.TrimSpace(plugin.ID)
		plugin.Title = strings.TrimSpace(plugin.Title)
		plugin.Kind = strings.ToLower(strings.TrimSpace(plugin.Kind))
		plugin.Command = strings.TrimSpace(plugin.Command)
		plugin.Selectors = cleanList(plugin.Selectors)
		if plugin.ID == "" || plugin.Command == "" {
			continue
		}
		if plugin.Kind == "" {
			plugin.Kind = "generic"
		}
		if plugin.Title == "" {
			plugin.Title = plugin.ID
		}
		if plugin.Timeout.Get() <= 0 {
			plugin.Timeout = Duration(5 * time.Second)
		}
		plugins = append(plugins, plugin)
	}
	c.Plugins = plugins
}

func ExpandPath(p string) string {
	if p == "" {
		return p
	}
	p = os.ExpandEnv(p)
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func (c *Config) MatchingWorkspace(context string) *Workspace {
	var best *Workspace
	for i := range c.Workspaces {
		ws := &c.Workspaces[i]
		if ws.Context == "" {
			continue
		}
		if matchGlob(ws.Context, context) {
			if best == nil || len(ws.Context) > len(best.Context) {
				best = ws
			}
		}
	}
	return best
}

type Resolved struct {
	Context   string
	Cluster   string
	Env       string
	OrgID     string
	Display   string
	Workspace *Workspace
	Links     Links
}

func (c *Config) Resolve(context, cluster string) Resolved {
	ws := c.MatchingWorkspace(context)
	links := c.Links
	env := ""
	orgID := ""
	display := ""
	if ws != nil {
		links = MergeLinks(c.Links, ws.Links)
		env = ws.Env
		display = ws.Display
		orgID = ws.OrgID
	}
	if env == "" {
		env = InferEnv(context)
	}
	if display == "" {
		display = context
	}
	return Resolved{
		Context:   context,
		Cluster:   cluster,
		Env:       env,
		OrgID:     orgID,
		Display:   display,
		Workspace: ws,
		Links:     links,
	}
}

func (c *Config) LinksFor(context, cluster string) Links {
	return c.Resolve(context, cluster).Links
}

func MergeLinks(base, overlay Links) Links {
	return Links{
		Grafana: Grafana{
			URL:          firstNonEmpty(overlay.Grafana.URL, base.Grafana.URL),
			DashboardURL: firstNonEmpty(overlay.Grafana.DashboardURL, base.Grafana.DashboardURL),
			PodURL:       firstNonEmpty(overlay.Grafana.PodURL, base.Grafana.PodURL),
			LogsURL:      firstNonEmpty(overlay.Grafana.LogsURL, base.Grafana.LogsURL),
		},
		Loki: Loki{
			URL:        firstNonEmpty(overlay.Loki.URL, base.Loki.URL),
			ExploreURL: firstNonEmpty(overlay.Loki.ExploreURL, base.Loki.ExploreURL),
			Query:      firstNonEmpty(overlay.Loki.Query, base.Loki.Query),
		},
		Mimir: Mimir{
			URL:        firstNonEmpty(overlay.Mimir.URL, base.Mimir.URL),
			ExploreURL: firstNonEmpty(overlay.Mimir.ExploreURL, base.Mimir.ExploreURL),
			Query:      firstNonEmpty(overlay.Mimir.Query, base.Mimir.Query),
		},
	}
}

func InferEnv(context string) string {
	for _, candidate := range envPatterns {
		if candidate.re.MatchString(context) {
			return candidate.env
		}
	}
	return ""
}

func (c *Config) Fingerprint() string {
	data, _ := yaml.Marshal(c)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

func matchGlob(pattern, value string) bool {
	if glob.Match(pattern, value) {
		return true
	}
	// Also allow matching the pattern against each segment of a context.
	for _, segment := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == ':' || r == '@' || r == '.'
	}) {
		if segment == pattern {
			return true
		}
	}
	return false
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
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

func UpsertServer(configPath string, server ServerConfig) (string, error) {
	server.Name = strings.TrimSpace(server.Name)
	if server.Name == "" {
		return "", fmt.Errorf("server name is required")
	}
	if configPath == "" {
		configPath = DefaultPath()
	}
	configPath = ExpandPath(configPath)
	doc, isNew, err := loadConfigNode(configPath)
	if err != nil {
		return configPath, err
	}
	root, err := configMapping(doc)
	if err != nil {
		return configPath, err
	}
	serversNode := mappingValue(root, "servers")
	if serversNode == nil {
		serversNode = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "servers"},
			serversNode,
		)
	}
	if serversNode.Kind != yaml.SequenceNode {
		return configPath, fmt.Errorf("config servers must be a list")
	}

	var entry yaml.Node
	if err := entry.Encode(server); err != nil {
		return configPath, fmt.Errorf("encode server: %w", err)
	}
	replaced := false
	for i, item := range serversNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if name := mappingValue(item, "name"); name != nil && name.Value == server.Name {
			serversNode.Content[i] = &entry
			replaced = true
			break
		}
	}
	if !replaced {
		serversNode.Content = append(serversNode.Content, &entry)
	}
	if err := writeConfigNode(configPath, doc, isNew); err != nil {
		return configPath, err
	}
	return configPath, nil
}

func RemoveServer(configPath, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("server name is required")
	}
	if configPath == "" {
		configPath = DefaultPath()
	}
	configPath = ExpandPath(configPath)
	doc, _, err := loadConfigNode(configPath)
	if err != nil {
		return configPath, err
	}
	root, err := configMapping(doc)
	if err != nil {
		return configPath, err
	}
	serversNode := mappingValue(root, "servers")
	if serversNode == nil || serversNode.Kind != yaml.SequenceNode {
		return configPath, fmt.Errorf("server not found: %s", name)
	}
	for i, item := range serversNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if value := mappingValue(item, "name"); value != nil && value.Value == name {
			serversNode.Content = append(serversNode.Content[:i], serversNode.Content[i+1:]...)
			if err := writeConfigNode(configPath, doc, false); err != nil {
				return configPath, err
			}
			return configPath, nil
		}
	}
	return configPath, fmt.Errorf("server not found: %s", name)
}

func UpsertShortcut(configPath string, shortcut ShortcutConfig) (string, error) {
	shortcut.Name = strings.TrimSpace(shortcut.Name)
	if shortcut.Name == "" {
		return "", fmt.Errorf("shortcut name is required")
	}
	if shortcut.URL == "" {
		return "", fmt.Errorf("shortcut URL is required")
	}
	if configPath == "" {
		configPath = DefaultPath()
	}
	configPath = ExpandPath(configPath)
	doc, isNew, err := loadConfigNode(configPath)
	if err != nil {
		return configPath, err
	}
	root, err := configMapping(doc)
	if err != nil {
		return configPath, err
	}
	shortcutsNode := mappingValue(root, "shortcuts")
	if shortcutsNode == nil {
		shortcutsNode = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "shortcuts"},
			shortcutsNode,
		)
	}
	if shortcutsNode.Kind != yaml.SequenceNode {
		return configPath, fmt.Errorf("config shortcuts must be a list")
	}
	var entry yaml.Node
	if err := entry.Encode(shortcut); err != nil {
		return configPath, fmt.Errorf("encode shortcut: %w", err)
	}
	replaced := false
	for i, item := range shortcutsNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if name := mappingValue(item, "name"); name != nil && name.Value == shortcut.Name {
			shortcutsNode.Content[i] = &entry
			replaced = true
			break
		}
	}
	if !replaced {
		shortcutsNode.Content = append(shortcutsNode.Content, &entry)
	}
	if err := writeConfigNode(configPath, doc, isNew); err != nil {
		return configPath, err
	}
	return configPath, nil
}

func RemoveShortcut(configPath, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("shortcut name is required")
	}
	if configPath == "" {
		configPath = DefaultPath()
	}
	configPath = ExpandPath(configPath)
	doc, _, err := loadConfigNode(configPath)
	if err != nil {
		return configPath, err
	}
	root, err := configMapping(doc)
	if err != nil {
		return configPath, err
	}
	shortcutsNode := mappingValue(root, "shortcuts")
	if shortcutsNode == nil || shortcutsNode.Kind != yaml.SequenceNode {
		return configPath, fmt.Errorf("shortcut not found: %s", name)
	}
	for i, item := range shortcutsNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if value := mappingValue(item, "name"); value != nil && value.Value == name {
			shortcutsNode.Content = append(shortcutsNode.Content[:i], shortcutsNode.Content[i+1:]...)
			if err := writeConfigNode(configPath, doc, false); err != nil {
				return configPath, err
			}
			return configPath, nil
		}
	}
	return configPath, fmt.Errorf("shortcut not found: %s", name)
}

func loadConfigNode(configPath string) (*yaml.Node, bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, false, fmt.Errorf("read config %s: %w", configPath, err)
		}
		doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		return doc, true, nil
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		return doc, true, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false, fmt.Errorf("parse config %s: %w", configPath, err)
	}
	return &doc, false, nil
}

func configMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config root must be a mapping")
	}
	return doc, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func writeConfigNode(configPath string, doc *yaml.Node, includeHeader bool) error {
	var buf bytes.Buffer
	if includeHeader {
		buf.WriteString("# hop configuration\n")
		buf.WriteString("# VM/server inventory can be added with: hop config server add NAME [user@host] [tags...]\n\n")
	}
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", configPath, err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

const initTemplate = `# hop configuration
# Search pods across every kubeconfig context, then jump to Grafana / k9s.
#
# Placeholders available in every URL/query template:
#   {context} {cluster} {env} {namespace} {pod} {container} {owner} {from} {to}
#   {from_unix} {to_unix} (secondes) · {from_ms} {to_ms} (millisecondes, Grafana)
#   {grafana} {loki} {mimir}
#   URL-encoded variants: {context_q} {cluster_q} {env_q} {namespace_q} {pod_q} {container_q} {owner_q}
#
# {from}/{to} are RFC3339 timestamps. Default: last hour.
# Tokens like 12h 14h, -2h, from:... to:... restrict this range.

kube:
  # Empty = use $KUBECONFIG or ~/.kube/config. List every file you want to merge.
  kubeconfigs: []
  include_contexts: []      # glob patterns, empty = all contexts
  exclude_contexts: []      # e.g. ["*-readonly", "old-*"]
  concurrency: 12           # parallel clusters
  request_timeout: 10s
  cache_ttl: 20s            # repeat searches are instant within this window
  all_namespaces: true
  namespaces: []            # restrict listing, e.g. ["apps", "observability"]
  exclude_namespaces: []    # e.g. ["kube-system"]
  resolve_owners: false     # extra calls to resolve ReplicaSet -> Deployment

ui:
  bind: 127.0.0.1
  port: 0                   # 0 = random free port
  open_browser: true
  theme: dark

# SSH aliases from this file are discovered automatically. Add servers entries
# to attach tags, metrics URLs and explicit SSH settings. 'hop supervision uat vm'
# searches VMs only and skips Kubernetes when the vm tag is present.
ssh_config: ""              # default: ~/.ssh/config
servers: []
#  - name: sup-uat-01
#    ssh: sup-uat-01
#    tags: [supervision, uat, vm]
#    metrics_url: "http://sup-uat-01:9100/metrics"
#    health_url: "http://sup-uat-01:8080/healthz"

# Browser shortcuts for 'hop link'. Keywords are matched against the command
# arguments, so 'hop link jira' or 'hop link ticket' opens the same URL.
shortcuts:
  - name: grafana
    keywords: [grafana, metrics, dashboards]
    url: "https://grafana.example.com"
  - name: swagger
    keywords: [swagger, api, openapi]
    url: "https://api.example.com/swagger"
  - name: jira
    keywords: [jira, ticket, bug]
    url: "https://jira.example.com"

# Issue providers. 'hop issue OPS-123' opens a ticket, 'hop issue new' creates
# one. If several providers match, hop prompts a menu. Tokens come from the
# environment and are never stored in this file.
issues:
  default_provider: ""
providers: {}
#  jira:
#    type: jira
#    base_url: "https://company.atlassian.net"
#    username_env: JIRA_USER
#    token_env: JIRA_TOKEN
#    auth_scheme: basic   # basic (Cloud) or bearer (PAT / Server)
#    keywords: [jira, ticket, bug]
#    defaults:
#      project: OPS
#      issue_type: Task

search:
  min_score: 0.35
  limit: 50
  live_limit: 200

# Default observability endpoints. Per-workspace blocks override these.
links:
  grafana:
    url: "https://grafana.example.com"
    pod_url: "{grafana}/d/k8s/pods?from={from_ms}&to={to_ms}&var-cluster={cluster}&var-namespace={namespace}&var-pod={pod}"
    logs_url: "{grafana}/explore?from={from_ms}&to={to_ms}&left={grafana}&var-namespace={namespace}&var-pod={pod}"
    dashboard_url: "{grafana}/d/k8s/overview?from={from_ms}&to={to_ms}&var-cluster={cluster}&var-namespace={namespace}"
  loki:
    url: "https://loki.example.com"
    query: '{namespace="{namespace}", pod="{pod}"}'
    explore_url: "{grafana}/explore?from={from_ms}&to={to_ms}&left={namespace}&var-pod={pod}"
  mimir:
    url: "https://mimir.example.com"
    query: 'sum(rate(container_cpu_usage_seconds_total{namespace="{namespace}", pod="{pod}"}[5m]))'
    explore_url: "{grafana}/explore?from={from_ms}&to={to_ms}&left={namespace}&var-pod={pod}"

workspaces:
  - context: "*-prod-*"
    env: prod
    display: "PRODUCTION"
    links:
      grafana:
        url: "https://grafana.prod.example.com"
      loki:
        url: "https://loki.prod.example.com"
  - context: "*-staging-*"
    env: staging
    display: "STAGING"
  - context: "*-dev-*"
    env: dev
    display: "DEV"
`

func Init(configPath string) (string, error) {
	if configPath == "" {
		configPath = DefaultPath()
	}
	configPath = ExpandPath(configPath)
	if _, err := os.Stat(configPath); err == nil {
		return configPath, fmt.Errorf("config already exists: %s", configPath)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return configPath, err
	}
	if err := os.WriteFile(configPath, []byte(initTemplate), 0o600); err != nil {
		return configPath, err
	}
	return configPath, nil
}
