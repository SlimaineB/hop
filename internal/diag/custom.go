package diag

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

type CommandRunner interface {
	RunCommand(ctx context.Context, server servers.Server, command string) (servers.CommandResult, error)
}

type Target struct {
	Kind   string
	Name   string
	Facts  map[string]any
	Pod    *kube.PodDetail
	Stats  *kube.PodStats
	Server *servers.Server
	Check  *servers.Check
	App    *config.AppConfig
}

type Rule interface {
	ID() string
	Kinds() []string
	Run(ctx context.Context, target Target, runner CommandRunner) []Finding
}

type customRule struct {
	cfg      config.DiagnosticCheckConfig
	cond     *condition
	severity Severity
	kinds    []string
	id       string
	match    *regexp.Regexp
}

func compileRule(cfg config.DiagnosticCheckConfig) (*customRule, error) {
	rule := &customRule{
		cfg:      cfg,
		severity: severityFromConfig(cfg.Severity),
		kinds:    append([]string(nil), cfg.Kinds...),
		id:       "custom-" + cfg.ID,
	}
	if cfg.When != nil {
		cond, err := compileCondition(*cfg.When)
		if err != nil {
			return nil, fmt.Errorf("diagnostic check %q: %w", cfg.ID, err)
		}
		rule.cond = cond
	}
	if pattern := strings.TrimSpace(cfg.Expect.StdoutMatches); pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("diagnostic check %q stdout_matches: %w", cfg.ID, err)
		}
		rule.match = re
	}
	if pattern := strings.TrimSpace(cfg.Expect.StderrMatches); pattern != "" {
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("diagnostic check %q stderr_matches: %w", cfg.ID, err)
		}
	}
	return rule, nil
}

func (r *customRule) ID() string {
	return r.id
}

func (r *customRule) Kinds() []string {
	return append([]string(nil), r.kinds...)
}

func (r *customRule) Run(ctx context.Context, target Target, runner CommandRunner) []Finding {
	if !kindAllowed(r.kinds, target.Kind) {
		return nil
	}
	if r.cond != nil {
		ok, err := r.cond.evaluate(target.Facts)
		if err != nil {
			return []Finding{{
				ID:       r.id + "-invalid",
				Severity: SeverityWarning,
				Title:    r.cfg.Title,
				Detail:   "The custom check condition could not be evaluated.",
				Evidence: []string{err.Error()},
			}}
		}
		if !ok {
			return nil
		}
	}
	if r.cfg.Command == "" {
		return []Finding{{
			ID:       r.id,
			Severity: r.severity,
			Title:    r.cfg.Title,
			Detail:   customDetail(r.cfg),
			Evidence: r.evidence(target),
			Remedy:   r.cfg.Remedy,
		}}
	}
	if target.Kind != "vm" || target.Server == nil {
		return []Finding{{
			ID:       r.id + "-skipped",
			Severity: SeverityInfo,
			Title:    r.cfg.Title + " (skipped)",
			Detail:   "Command checks are only available on VM targets.",
			Evidence: r.evidence(target),
		}}
	}
	if runner == nil {
		return []Finding{{
			ID:       r.id + "-skipped",
			Severity: SeverityInfo,
			Title:    r.cfg.Title + " (skipped)",
			Detail:   "No SSH command runner is available.",
			Evidence: r.evidence(target),
		}}
	}
	if !target.Server.SSHChecks {
		return []Finding{{
			ID:       r.id + "-skipped",
			Severity: SeverityInfo,
			Title:    r.cfg.Title + " (skipped)",
			Detail:   "SSH checks are disabled for this VM.",
			Evidence: r.evidence(target),
		}}
	}
	timeout := r.cfg.Timeout.Get()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := runner.RunCommand(commandCtx, *target.Server, r.cfg.Command)
	if err != nil {
		evidence := r.evidence(target)
		evidence = append(evidence, "command: "+r.cfg.Command, "error: "+err.Error())
		return []Finding{{
			ID:       r.id + "-error",
			Severity: r.severity,
			Title:    r.cfg.Title,
			Detail:   firstNonEmpty(r.cfg.Description, "The custom command could not be executed."),
			Evidence: evidence,
			Remedy:   r.cfg.Remedy,
		}}
	}
	failures := commandFailures(r.cfg.Expect, result, r.match)
	if len(failures) == 0 {
		return nil
	}
	evidence := r.evidence(target)
	evidence = append(evidence, failures...)
	evidence = append(evidence, commandEvidence(result)...)
	return []Finding{{
		ID:       r.id,
		Severity: r.severity,
		Title:    r.cfg.Title,
		Detail:   firstNonEmpty(r.cfg.Description, "The custom command check did not match its expectations."),
		Evidence: evidence,
		Remedy:   r.cfg.Remedy,
	}}
}

func (r *customRule) evidence(target Target) []string {
	evidence := []string{"target: " + target.Name, "check: " + r.cfg.ID}
	for _, field := range r.cfg.Facts {
		value, ok := lookupFact(target.Facts, field)
		if !ok {
			evidence = append(evidence, field+": <missing>")
			continue
		}
		evidence = append(evidence, field+": "+formatFactValue(value))
	}
	return evidence
}

func commandFailures(expect config.DiagnosticExpect, result servers.CommandResult, stdoutMatch *regexp.Regexp) []string {
	failures := []string{}
	wantExit := 0
	if expect.ExitCode != nil {
		wantExit = *expect.ExitCode
	}
	if result.ExitCode != wantExit {
		failures = append(failures, fmt.Sprintf("exit code: got %d, want %d", result.ExitCode, wantExit))
	}
	if expect.StdoutContains != "" && !strings.Contains(result.Stdout, expect.StdoutContains) {
		failures = append(failures, "stdout does not contain "+strconvQuote(expect.StdoutContains))
	}
	if expect.StdoutNotContains != "" && strings.Contains(result.Stdout, expect.StdoutNotContains) {
		failures = append(failures, "stdout contains "+strconvQuote(expect.StdoutNotContains))
	}
	if stdoutMatch != nil && !stdoutMatch.MatchString(result.Stdout) {
		failures = append(failures, "stdout does not match "+strconvQuote(expect.StdoutMatches))
	}
	if expect.StderrContains != "" && !strings.Contains(result.Stderr, expect.StderrContains) {
		failures = append(failures, "stderr does not contain "+strconvQuote(expect.StderrContains))
	}
	if expect.StderrNotContains != "" && strings.Contains(result.Stderr, expect.StderrNotContains) {
		failures = append(failures, "stderr contains "+strconvQuote(expect.StderrNotContains))
	}
	if expect.StderrMatches != "" {
		re, err := regexp.Compile(expect.StderrMatches)
		if err != nil || !re.MatchString(result.Stderr) {
			failures = append(failures, "stderr does not match "+strconvQuote(expect.StderrMatches))
		}
	}
	return failures
}

func commandEvidence(result servers.CommandResult) []string {
	evidence := []string{fmt.Sprintf("exit code: %d", result.ExitCode)}
	if result.Stdout != "" {
		evidence = append(evidence, "stdout: "+truncate(result.Stdout, 600))
	}
	if result.Stderr != "" {
		evidence = append(evidence, "stderr: "+truncate(result.Stderr, 600))
	}
	return evidence
}

func customDetail(cfg config.DiagnosticCheckConfig) string {
	if cfg.Description != "" {
		return cfg.Description
	}
	return "The custom diagnostic condition matched."
}

func severityFromConfig(value string) Severity {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "crit", "error", "err", "fatal":
		return SeverityCritical
	case "info", "notice", "ok":
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

func kindAllowed(kinds []string, kind string) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func formatFactValue(value any) string {
	switch value.(type) {
	case map[string]any, map[string]string, []any, []string:
		encoded, err := json.Marshal(value)
		if err == nil {
			return truncate(string(encoded), 300)
		}
	}
	return truncate(fmt.Sprint(value), 300)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func strconvQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%q", value)
	}
	return string(encoded)
}
