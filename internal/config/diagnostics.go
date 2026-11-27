package config

import (
	"strings"
	"time"
)

const defaultDiagnosticTimeout = 10 * time.Second

type Diagnostics struct {
	Disabled []string                `yaml:"disabled,omitempty"`
	Checks   []DiagnosticCheckConfig `yaml:"checks,omitempty"`
}

type DiagnosticCheckConfig struct {
	ID          string               `yaml:"id"`
	Title       string               `yaml:"title,omitempty"`
	Description string               `yaml:"description,omitempty"`
	Kinds       []string             `yaml:"kinds,omitempty"`
	Severity    string               `yaml:"severity,omitempty"`
	When        *DiagnosticCondition `yaml:"when,omitempty"`
	Command     string               `yaml:"command,omitempty"`
	Timeout     Duration             `yaml:"timeout,omitempty"`
	Expect      DiagnosticExpect     `yaml:"expect,omitempty"`
	Facts       []string             `yaml:"facts,omitempty"`
	Remedy      string               `yaml:"remedy,omitempty"`
}

type DiagnosticCondition struct {
	All    []DiagnosticCondition `yaml:"all,omitempty"`
	Any    []DiagnosticCondition `yaml:"any,omitempty"`
	Not    *DiagnosticCondition  `yaml:"not,omitempty"`
	Field  string                `yaml:"field,omitempty"`
	Op     string                `yaml:"op,omitempty"`
	Value  any                   `yaml:"value,omitempty"`
	Values []any                 `yaml:"values,omitempty"`
}

type DiagnosticExpect struct {
	ExitCode          *int   `yaml:"exit_code,omitempty"`
	StdoutContains    string `yaml:"stdout_contains,omitempty"`
	StdoutNotContains string `yaml:"stdout_not_contains,omitempty"`
	StdoutMatches     string `yaml:"stdout_matches,omitempty"`
	StderrContains    string `yaml:"stderr_contains,omitempty"`
	StderrNotContains string `yaml:"stderr_not_contains,omitempty"`
	StderrMatches     string `yaml:"stderr_matches,omitempty"`
}

func normalizeDiagnostics(cfg *Diagnostics) {
	cfg.Disabled = cleanList(cfg.Disabled)
	checks := make([]DiagnosticCheckConfig, 0, len(cfg.Checks))
	for _, check := range cfg.Checks {
		check.ID = strings.ToLower(strings.TrimSpace(check.ID))
		if check.ID == "" {
			continue
		}
		check.Title = strings.TrimSpace(check.Title)
		if check.Title == "" {
			check.Title = check.ID
		}
		check.Description = strings.TrimSpace(check.Description)
		check.Command = strings.TrimSpace(check.Command)
		check.Remedy = strings.TrimSpace(check.Remedy)
		check.Kinds = normalizeDiagnosticKinds(check.Kinds)
		if len(check.Kinds) == 0 {
			if check.Command != "" {
				check.Kinds = []string{"vm"}
			} else {
				check.Kinds = []string{"pod", "vm", "app"}
			}
		}
		check.Severity = normalizeSeverity(check.Severity)
		check.Facts = cleanList(check.Facts)
		if check.Timeout.Get() <= 0 {
			check.Timeout = Duration(defaultDiagnosticTimeout)
		}
		if check.When != nil {
			normalizeDiagnosticCondition(check.When)
		}
		check.Expect.StdoutContains = strings.TrimSpace(check.Expect.StdoutContains)
		check.Expect.StdoutNotContains = strings.TrimSpace(check.Expect.StdoutNotContains)
		check.Expect.StdoutMatches = strings.TrimSpace(check.Expect.StdoutMatches)
		check.Expect.StderrContains = strings.TrimSpace(check.Expect.StderrContains)
		check.Expect.StderrNotContains = strings.TrimSpace(check.Expect.StderrNotContains)
		check.Expect.StderrMatches = strings.TrimSpace(check.Expect.StderrMatches)
		checks = append(checks, check)
	}
	cfg.Checks = checks
}

func normalizeDiagnosticCondition(condition *DiagnosticCondition) {
	if condition == nil {
		return
	}
	condition.Field = strings.TrimSpace(condition.Field)
	condition.Op = strings.ToLower(strings.TrimSpace(condition.Op))
	if condition.Op == "" {
		condition.Op = "eq"
	}
	for i := range condition.All {
		normalizeDiagnosticCondition(&condition.All[i])
	}
	for i := range condition.Any {
		normalizeDiagnosticCondition(&condition.Any[i])
	}
	if condition.Not != nil {
		normalizeDiagnosticCondition(condition.Not)
	}
}

func normalizeDiagnosticKinds(kinds []string) []string {
	out := make([]string, 0, len(kinds))
	seen := map[string]bool{}
	for _, kind := range kinds {
		kind = strings.ToLower(strings.TrimSpace(kind))
		switch kind {
		case "pod", "pods", "k8s", "kubernetes":
			kind = "pod"
		case "vm", "vms", "server", "servers", "ssh":
			kind = "vm"
		case "app", "apps", "application", "applications", "service":
			kind = "app"
		default:
			continue
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	return out
}

func normalizeSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "crit", "error", "err", "fatal":
		return "critical"
	case "info", "notice", "ok":
		return "info"
	case "warning", "warn":
		return "warning"
	default:
		return "warning"
	}
}
