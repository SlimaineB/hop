package diag

import (
	"context"
	"fmt"
	"strings"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

type Engine struct {
	rules    []Rule
	disabled map[string]bool
}

func NewEngine(cfg config.Diagnostics) (*Engine, error) {
	engine := &Engine{disabled: map[string]bool{}}
	for _, id := range cfg.Disabled {
		id = strings.ToLower(strings.TrimSpace(id))
		if id != "" {
			engine.disabled[id] = true
		}
	}
	for _, check := range cfg.Checks {
		rule, err := compileRule(check)
		if err != nil {
			return nil, err
		}
		if err := engine.Register(rule); err != nil {
			return nil, err
		}
	}
	return engine, nil
}

func (e *Engine) Register(rule Rule) error {
	if rule == nil {
		return nil
	}
	id := strings.TrimSpace(rule.ID())
	if id == "" {
		return fmt.Errorf("diagnostic rule without an ID")
	}
	for _, existing := range e.rules {
		if strings.EqualFold(existing.ID(), id) {
			return fmt.Errorf("duplicate diagnostic rule %q", id)
		}
	}
	e.rules = append(e.rules, rule)
	return nil
}

func (e *Engine) RuleIDs() []string {
	ids := make([]string, 0, len(e.rules))
	for _, rule := range e.rules {
		ids = append(ids, rule.ID())
	}
	return ids
}

func (e *Engine) DiagnosePod(ctx context.Context, detail kube.PodDetail, stats *kube.PodStats) *Report {
	report := DiagnosePod(detail, stats)
	e.apply(ctx, report, Target{
		Kind:  "pod",
		Name:  detail.Pod.Ref().String(),
		Facts: PodFacts(detail, stats),
		Pod:   &detail,
		Stats: stats,
	}, nil)
	return e.finish(report)
}

func (e *Engine) DiagnoseServer(ctx context.Context, server servers.Server, check servers.Check, runner CommandRunner) *Report {
	report := DiagnoseServer(server, check)
	e.apply(ctx, report, Target{
		Kind:   "vm",
		Name:   server.Name,
		Facts:  ServerFacts(server, check),
		Server: &server,
		Check:  &check,
	}, runner)
	return e.finish(report)
}

func (e *Engine) apply(ctx context.Context, report *Report, target Target, runner CommandRunner) {
	for _, rule := range e.rules {
		if e.disabled[strings.ToLower(rule.ID())] {
			continue
		}
		for _, finding := range rule.Run(ctx, target, runner) {
			report.Add(finding)
		}
	}
}

func (e *Engine) finish(report *Report) *Report {
	if len(e.disabled) == 0 {
		return report.Finish()
	}
	findings := report.Findings[:0]
	for _, finding := range report.Findings {
		if !e.disabled[strings.ToLower(finding.ID)] {
			findings = append(findings, finding)
		}
	}
	report.Findings = findings
	return report.Finish()
}
