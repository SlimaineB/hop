package config

import (
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d Duration) Get() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Value == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

type Config struct {
	Kube       Kube                      `yaml:"kube"`
	UI         UI                        `yaml:"ui"`
	Search     Search                    `yaml:"search"`
	Links      Links                     `yaml:"links"`
	Workspaces []Workspace               `yaml:"workspaces"`
	SSHConfig  string                    `yaml:"ssh_config,omitempty"`
	Servers    []ServerConfig            `yaml:"servers,omitempty"`
	Containers Containers                `yaml:"containers,omitempty"`
	Shortcuts  []ShortcutConfig          `yaml:"shortcuts,omitempty"`
	Issues     IssuesConfig              `yaml:"issues,omitempty"`
	Providers  map[string]ProviderConfig `yaml:"providers,omitempty"`
	Git        Git                       `yaml:"git,omitempty"`
	Packages   Packages                  `yaml:"packages,omitempty"`
	Plugins    []PluginConfig            `yaml:"plugins,omitempty"`
	Sources    Sources                   `yaml:"sources,omitempty"`
}

type Sources struct {
	Disabled []string `yaml:"disabled,omitempty"`
}

type Packages struct {
	Registries []RegistryConfig `yaml:"registries,omitempty"`
	Images     []string         `yaml:"images,omitempty"`
	CacheTTL   Duration         `yaml:"cache_ttl,omitempty"`
	Timeout    Duration         `yaml:"timeout,omitempty"`
	TagLimit   int              `yaml:"tag_limit,omitempty"`
}

type RegistryConfig struct {
	Name        string   `yaml:"name"`
	Host        string   `yaml:"host"`
	Type        string   `yaml:"type,omitempty"`
	Username    string   `yaml:"username,omitempty"`
	UsernameEnv string   `yaml:"username_env,omitempty"`
	TokenEnv    string   `yaml:"token_env,omitempty"`
	Images      []string `yaml:"images,omitempty"`
}

type PluginConfig struct {
	ID        string            `yaml:"id"`
	Title     string            `yaml:"title,omitempty"`
	Kind      string            `yaml:"kind,omitempty"`
	Selectors []string          `yaml:"selectors,omitempty"`
	Command   string            `yaml:"command"`
	Args      []string          `yaml:"args,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Timeout   Duration          `yaml:"timeout,omitempty"`
}

type Git struct {
	Roots     []string          `yaml:"roots,omitempty"`
	Repos     []string          `yaml:"repos,omitempty"`
	Exclude   []string          `yaml:"exclude,omitempty"`
	Providers map[string]string `yaml:"providers,omitempty"`
	MaxDepth  int               `yaml:"max_depth,omitempty"`
	CacheTTL  Duration          `yaml:"cache_ttl,omitempty"`
}

type IssuesConfig struct {
	DefaultProvider string `yaml:"default_provider,omitempty"`
}

type ProviderConfig struct {
	Type        string            `yaml:"type"`
	BaseURL     string            `yaml:"base_url,omitempty"`
	Username    string            `yaml:"username,omitempty"`
	UsernameEnv string            `yaml:"username_env,omitempty"`
	TokenEnv    string            `yaml:"token_env,omitempty"`
	AuthScheme  string            `yaml:"auth_scheme,omitempty"`
	Keywords    []string          `yaml:"keywords,omitempty"`
	Defaults    map[string]string `yaml:"defaults,omitempty"`
	Timeout     Duration          `yaml:"timeout,omitempty"`
}

type ShortcutConfig struct {
	Name        string   `yaml:"name"`
	URL         string   `yaml:"url"`
	Keywords    []string `yaml:"keywords,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

type Containers struct {
	Engines  []ContainerEngineConfig `yaml:"engines,omitempty"`
	Timeout  Duration                `yaml:"timeout,omitempty"`
	CacheTTL Duration                `yaml:"cache_ttl,omitempty"`
}

type ContainerEngineConfig struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type,omitempty"`
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
}

type ServerConfig struct {
	Name          string   `yaml:"name"`
	SSH           string   `yaml:"ssh,omitempty"`
	Host          string   `yaml:"host,omitempty"`
	User          string   `yaml:"user,omitempty"`
	Port          int      `yaml:"port,omitempty"`
	ProxyJump     string   `yaml:"proxy_jump,omitempty"`
	Env           string   `yaml:"env,omitempty"`
	Tags          []string `yaml:"tags,omitempty"`
	MetricsURL    string   `yaml:"metrics_url,omitempty"`
	HealthURL     string   `yaml:"health_url,omitempty"`
	SSHChecks     *bool    `yaml:"ssh_checks,omitempty"`
	NetworkChecks *bool    `yaml:"network_checks,omitempty"`
	CheckPorts    []int    `yaml:"check_ports,omitempty"`
}

type Kube struct {
	Kubeconfigs       []string `yaml:"kubeconfigs"`
	IncludeContexts   []string `yaml:"include_contexts"`
	ExcludeContexts   []string `yaml:"exclude_contexts"`
	Concurrency       int      `yaml:"concurrency"`
	RequestTimeout    Duration `yaml:"request_timeout"`
	CacheTTL          Duration `yaml:"cache_ttl"`
	AllNamespaces     *bool    `yaml:"all_namespaces"`
	Namespaces        []string `yaml:"namespaces"`
	ExcludeNamespaces []string `yaml:"exclude_namespaces"`
	ResolveOwners     bool     `yaml:"resolve_owners"`
	MaxPods           int      `yaml:"max_pods,omitempty"`
	MaxPodsPerContext int      `yaml:"max_pods_per_context,omitempty"`
}

func (k Kube) ListAllNamespaces() bool {
	return k.AllNamespaces == nil || *k.AllNamespaces
}

type UI struct {
	Bind        string `yaml:"bind"`
	Port        int    `yaml:"port"`
	OpenBrowser *bool  `yaml:"open_browser"`
	Theme       string `yaml:"theme"`
}

func (u UI) BrowserEnabled() bool {
	return u.OpenBrowser == nil || *u.OpenBrowser
}

type Search struct {
	MinScore  float64 `yaml:"min_score"`
	Limit     int     `yaml:"limit"`
	LiveLimit int     `yaml:"live_limit"`
}

type Links struct {
	Grafana Grafana `yaml:"grafana"`
	Loki    Loki    `yaml:"loki"`
	Mimir   Mimir   `yaml:"mimir"`
}

type Grafana struct {
	URL          string `yaml:"url"`
	DashboardURL string `yaml:"dashboard_url"`
	PodURL       string `yaml:"pod_url"`
	LogsURL      string `yaml:"logs_url"`
}

type Loki struct {
	URL        string `yaml:"url"`
	ExploreURL string `yaml:"explore_url"`
	Query      string `yaml:"query"`
}

type Mimir struct {
	URL        string `yaml:"url"`
	ExploreURL string `yaml:"explore_url"`
	Query      string `yaml:"query"`
}

type Workspace struct {
	Name    string `yaml:"name"`
	Context string `yaml:"context"`
	Env     string `yaml:"env"`
	Display string `yaml:"display"`
	OrgID   string `yaml:"org_id"`
	Links   Links  `yaml:"links"`
}
