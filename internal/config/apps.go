package config

import (
	"strings"
	"time"
)

const (
	defaultAppTimeout      = 8 * time.Second
	defaultAppProbeTimeout = 3 * time.Second
	defaultLogSince        = 15 * time.Minute
	defaultLogPattern      = `(?i)\b(error|exception|fatal|panic|traceback)\b`
)

type AppConfig struct {
	Name        string           `yaml:"name"`
	Title       string           `yaml:"title,omitempty"`
	Description string           `yaml:"description,omitempty"`
	Env         string           `yaml:"env,omitempty"`
	Host        string           `yaml:"host,omitempty"`
	Port        int              `yaml:"port,omitempty"`
	HealthURL   string           `yaml:"health_url,omitempty"`
	MetricsURL  string           `yaml:"metrics_url,omitempty"`
	LogPath     string           `yaml:"log_path,omitempty"`
	LogPattern  string           `yaml:"log_pattern,omitempty"`
	LogSince    Duration         `yaml:"log_since,omitempty"`
	Timeout     Duration         `yaml:"timeout,omitempty"`
	Tags        []string         `yaml:"tags,omitempty"`
	Probes      []AppProbeConfig `yaml:"probes,omitempty"`
}

type AppProbeConfig struct {
	ID       string           `yaml:"id,omitempty"`
	Title    string           `yaml:"title,omitempty"`
	Type     string           `yaml:"type,omitempty"`
	Severity string           `yaml:"severity,omitempty"`
	URL      string           `yaml:"url,omitempty"`
	Host     string           `yaml:"host,omitempty"`
	Port     int              `yaml:"port,omitempty"`
	Path     string           `yaml:"path,omitempty"`
	Pattern  string           `yaml:"pattern,omitempty"`
	Since    Duration         `yaml:"since,omitempty"`
	Command  string           `yaml:"command,omitempty"`
	Timeout  Duration         `yaml:"timeout,omitempty"`
	Expect   DiagnosticExpect `yaml:"expect,omitempty"`
	Remedy   string           `yaml:"remedy,omitempty"`
}

func normalizeApps(apps []AppConfig) []AppConfig {
	out := make([]AppConfig, 0, len(apps))
	seenNames := map[string]bool{}
	for _, app := range apps {
		app.Name = strings.TrimSpace(app.Name)
		if app.Name == "" || seenNames[strings.ToLower(app.Name)] {
			continue
		}
		seenNames[strings.ToLower(app.Name)] = true
		app.Title = strings.TrimSpace(app.Title)
		if app.Title == "" {
			app.Title = app.Name
		}
		app.Description = strings.TrimSpace(app.Description)
		app.Host = strings.TrimSpace(app.Host)
		if app.Host == "" {
			app.Host = "127.0.0.1"
		}
		app.Env = strings.TrimSpace(app.Env)
		if app.Env == "" {
			app.Env = InferEnv(strings.Join([]string{app.Name, app.Host, strings.Join(app.Tags, " ")}, " "))
		}
		app.Tags = cleanList(app.Tags)
		app.HealthURL = strings.TrimSpace(app.HealthURL)
		app.MetricsURL = strings.TrimSpace(app.MetricsURL)
		app.LogPath = strings.TrimSpace(app.LogPath)
		app.LogPattern = strings.TrimSpace(app.LogPattern)
		if app.LogPattern == "" {
			app.LogPattern = defaultLogPattern
		}
		if app.LogSince.Get() <= 0 {
			app.LogSince = Duration(defaultLogSince)
		}
		if app.Timeout.Get() <= 0 {
			app.Timeout = Duration(defaultAppTimeout)
		}
		app.Probes = normalizeAppProbes(app)
		out = append(out, app)
	}
	return out
}

func normalizeAppProbes(app AppConfig) []AppProbeConfig {
	probes := make([]AppProbeConfig, 0, len(app.Probes))
	seen := map[string]bool{}
	for index, probe := range app.Probes {
		probe.Type = strings.ToLower(strings.TrimSpace(probe.Type))
		if probe.Type == "" {
			probe.Type = inferProbeType(probe)
		}
		switch probe.Type {
		case "http", "https", "tcp", "log", "command":
		default:
			continue
		}
		probe.ID = strings.ToLower(strings.TrimSpace(probe.ID))
		if probe.ID == "" {
			probe.ID = probe.Type
			if probe.ID == "" {
				probe.ID = "probe"
			}
		}
		if seen[probe.ID] {
			probe.ID = probe.ID + "-" + itoa(index+1)
		}
		seen[probe.ID] = true
		probe.Title = strings.TrimSpace(probe.Title)
		if probe.Title == "" {
			probe.Title = strings.ToUpper(probe.Type[:1]) + probe.Type[1:] + " probe"
		}
		probe.Severity = normalizeSeverity(probe.Severity)
		probe.URL = strings.TrimSpace(probe.URL)
		probe.Host = strings.TrimSpace(probe.Host)
		if probe.Host == "" {
			probe.Host = app.Host
		}
		probe.Path = strings.TrimSpace(probe.Path)
		if probe.Path == "" {
			probe.Path = app.LogPath
		}
		probe.Pattern = strings.TrimSpace(probe.Pattern)
		if probe.Pattern == "" {
			probe.Pattern = app.LogPattern
		}
		if probe.Since.Get() <= 0 {
			probe.Since = app.LogSince
		}
		probe.Command = strings.TrimSpace(probe.Command)
		probe.Remedy = strings.TrimSpace(probe.Remedy)
		if probe.Timeout.Get() <= 0 {
			probe.Timeout = Duration(defaultAppProbeTimeout)
			if app.Timeout.Get() > 0 {
				probe.Timeout = app.Timeout
			}
		}
		if (probe.Type == "http" || probe.Type == "https") && probe.URL == "" && probe.Port > 0 {
			probe.URL = probe.Type + "://" + probe.Host + ":" + itoa(probe.Port)
		}
		if probe.Type == "tcp" && probe.Port == 0 {
			probe.Port = app.Port
		}
		if probe.Type == "log" && probe.Path == "" {
			continue
		}
		if probe.Type == "command" && probe.Command == "" {
			continue
		}
		if probe.Type == "http" && probe.URL == "" {
			continue
		}
		if probe.Type == "tcp" && probe.Port <= 0 {
			continue
		}
		probe.Expect.StdoutContains = strings.TrimSpace(probe.Expect.StdoutContains)
		probe.Expect.StdoutNotContains = strings.TrimSpace(probe.Expect.StdoutNotContains)
		probe.Expect.StdoutMatches = strings.TrimSpace(probe.Expect.StdoutMatches)
		probe.Expect.StderrContains = strings.TrimSpace(probe.Expect.StderrContains)
		probe.Expect.StderrNotContains = strings.TrimSpace(probe.Expect.StderrNotContains)
		probe.Expect.StderrMatches = strings.TrimSpace(probe.Expect.StderrMatches)
		probes = append(probes, probe)
	}
	return probes
}

func inferProbeType(probe AppProbeConfig) string {
	switch {
	case probe.URL != "":
		return "http"
	case probe.Command != "":
		return "command"
	case probe.Path != "" || probe.Pattern != "":
		return "log"
	case probe.Port > 0:
		return "tcp"
	default:
		return ""
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
