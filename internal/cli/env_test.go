package cli

import (
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func TestResolveEnvRequiresSelection(t *testing.T) {
	app := &App{}
	if _, err := app.resolveEnv(config.Default(), "api", false); err == nil {
		t.Fatal("expected an environment selection error")
	}
}

func TestResolveEnvFromQuery(t *testing.T) {
	app := &App{}
	env, err := app.resolveEnv(config.Default(), "api env:uat", false)
	if err != nil {
		t.Fatal(err)
	}
	if env != "uat" {
		t.Fatalf("env = %q, want uat", env)
	}
}

func TestResolveEnvAllowsConfiguredEnv(t *testing.T) {
	app := &App{flags: runtimeFlags{env: "staging"}}
	cfg := config.Default()
	cfg.Kube.Environments = []string{"staging", "prod"}
	env, err := app.resolveEnv(cfg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if env != "staging" {
		t.Fatalf("env = %q", env)
	}
}

func TestResolveEnvRejectsUnknownEnv(t *testing.T) {
	app := &App{flags: runtimeFlags{env: "qa"}}
	_, err := app.resolveEnv(config.Default(), "", false)
	if err == nil || !strings.Contains(err.Error(), "unknown environment") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveEnvFromSearchKeyword(t *testing.T) {
	app := &App{}
	env, err := app.resolveEnv(config.Default(), "api uat", false)
	if err != nil {
		t.Fatal(err)
	}
	if env != "uat" {
		t.Fatalf("env = %q, want uat", env)
	}
}

func TestResolveEnvFromMultipleSearchKeywords(t *testing.T) {
	app := &App{}
	env, err := app.resolveEnv(config.Default(), "api dev prod", false)
	if err != nil {
		t.Fatal(err)
	}
	if env != "dev,prod" {
		t.Fatalf("env = %q, want dev,prod", env)
	}
}
