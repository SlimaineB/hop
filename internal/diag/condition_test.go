package diag

import (
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func TestConditionOperators(t *testing.T) {
	facts := map[string]any{
		"pod": map[string]any{
			"restarts": 7,
			"phase":    "Running",
			"labels":   map[string]any{"app.kubernetes.io/name": "checkout"},
			"images":   []any{"ghcr.io/acme/api:v1", "busybox"},
		},
	}
	cases := []struct {
		name string
		spec config.DiagnosticCondition
		want bool
	}{
		{name: "eq", spec: config.DiagnosticCondition{Field: "pod.phase", Op: "eq", Value: "Running"}, want: true},
		{name: "in", spec: config.DiagnosticCondition{Field: "pod.phase", Op: "in", Values: []any{"Failed", "Running"}}, want: true},
		{name: "gt", spec: config.DiagnosticCondition{Field: "pod.restarts", Op: "gt", Value: 3}, want: true},
		{name: "contains", spec: config.DiagnosticCondition{Field: "pod.images", Op: "contains", Value: "busybox"}, want: true},
		{name: "matches", spec: config.DiagnosticCondition{Field: "pod.images", Op: "matches", Value: `acme/api:v\d+`}, want: true},
		{name: "dotted label", spec: config.DiagnosticCondition{Field: "pod.labels.app.kubernetes.io/name", Op: "eq", Value: "checkout"}, want: true},
		{name: "all", spec: config.DiagnosticCondition{All: []config.DiagnosticCondition{
			{Field: "pod.phase", Op: "eq", Value: "Running"},
			{Field: "pod.restarts", Op: "gte", Value: 7},
		}}, want: true},
		{name: "any", spec: config.DiagnosticCondition{Any: []config.DiagnosticCondition{
			{Field: "pod.phase", Op: "eq", Value: "Failed"},
			{Field: "pod.restarts", Op: "lt", Value: 10},
		}}, want: true},
		{name: "not", spec: config.DiagnosticCondition{Not: &config.DiagnosticCondition{Field: "pod.phase", Op: "eq", Value: "Failed"}}, want: true},
		{name: "exists", spec: config.DiagnosticCondition{Field: "pod.images", Op: "exists"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond, err := compileCondition(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			got, err := cond.evaluate(facts)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("evaluate = %v, want %v", got, tc.want)
			}
		})
	}
}
