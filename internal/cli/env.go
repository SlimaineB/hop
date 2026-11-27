package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
)

func (a *App) resolveEnv(cfg *config.Config, query string, prompt bool) (string, error) {
	if env := strings.TrimSpace(a.flags.env); env != "" {
		if err := validateEnv(cfg, env); err != nil {
			return "", err
		}
		if isAllEnv(env) {
			return "", nil
		}
		return env, nil
	}
	if env := app.EnvironmentFromQuery(cfg, query); env != "" {
		if err := validateEnv(cfg, env); err != nil {
			return "", err
		}
		return env, nil
	}
	if !cfg.Kube.EnvRequired() || len(a.flags.contexts) > 0 {
		return "", nil
	}

	envs := cfg.Kube.EnvironmentList()
	if prompt && term.IsTerminal(int(os.Stdin.Fd())) {
		env, err := promptEnv(envs)
		if err != nil {
			return "", err
		}
		if !isAllEnv(env) {
			a.flags.env = env
		}
		return env, nil
	}
	return "", fmt.Errorf("environment required: choose %s with --env, env:<name> in the query, or --env all", strings.Join(envs, ", "))
}

func validateEnv(cfg *config.Config, env string) error {
	if isAllEnv(env) {
		return nil
	}
	for _, part := range strings.Split(env, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !cfg.Kube.HasEnvironment(part) {
			return fmt.Errorf("unknown environment %q: choose %s", part, strings.Join(cfg.Kube.EnvironmentList(), ", "))
		}
	}
	return nil
}

func isAllEnv(env string) bool {
	env = strings.TrimSpace(env)
	return strings.EqualFold(env, "all") || env == "*"
}

func promptEnv(envs []string) (string, error) {
	fmt.Fprintln(os.Stderr, "Select an environment:")
	for i, env := range envs {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, env)
	}
	fmt.Fprintf(os.Stderr, "  a) all\n")
	fmt.Fprintf(os.Stderr, "Environment [1-%d, name, or all]: ", len(envs))

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return "", fmt.Errorf("environment required")
	}
	if index, err := strconv.Atoi(value); err == nil {
		if index >= 1 && index <= len(envs) {
			return envs[index-1], nil
		}
		return "", fmt.Errorf("invalid environment selection %q", value)
	}
	if isAllEnv(value) {
		return value, nil
	}
	for _, env := range envs {
		if strings.EqualFold(env, value) {
			return env, nil
		}
	}
	return "", fmt.Errorf("invalid environment selection %q", value)
}
