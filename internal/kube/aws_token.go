package kube

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const awsTokenSafety = time.Minute

type awsToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type awsTokenFile struct {
	Context     string    `json:"context"`
	Fingerprint string    `json:"fingerprint"`
	Token       string    `json:"token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type awsTokenOutput struct {
	Status struct {
		Token               string `json:"token"`
		ExpirationTimestamp string `json:"expirationTimestamp"`
	} `json:"status"`
	Token string `json:"token"`
}

func isAWSExec(execConfig *clientcmdapi.ExecConfig) bool {
	if execConfig == nil {
		return false
	}
	if !strings.Contains(strings.ToLower(filepath.Base(execConfig.Command)), "aws") {
		return false
	}
	hasEKS, hasGetToken := false, false
	for _, arg := range execConfig.Args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "eks":
			hasEKS = true
		case "get-token":
			hasGetToken = true
		}
	}
	return hasEKS && hasGetToken
}

func (p *ClientGoProvider) awsToken(ctx context.Context, name string, execConfig *clientcmdapi.ExecConfig, server string) (string, time.Time, error) {
	fingerprint := awsFingerprint(execConfig, server)
	lock := p.tokenLock(fingerprint)
	lock.Lock()
	defer lock.Unlock()

	if token, ok := p.loadTokenFile(name, fingerprint); ok {
		return token.Token, token.ExpiresAt, nil
	}

	p.tokenMu.Lock()
	token, ok := p.tokens[fingerprint]
	p.tokenMu.Unlock()
	if !ok || !tokenFresh(token.ExpiresAt) {
		refreshed, err := runAWSToken(ctx, name, execConfig)
		if err != nil {
			return "", time.Time{}, err
		}
		token = refreshed
		p.tokenMu.Lock()
		p.tokens[fingerprint] = token
		p.tokenMu.Unlock()
	}

	p.saveTokenFile(name, fingerprint, token)
	return token.Token, token.ExpiresAt, nil
}

func (p *ClientGoProvider) tokenLock(key string) *sync.Mutex {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if lock, ok := p.tokenLocks[key]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	p.tokenLocks[key] = lock
	return lock
}

func runAWSToken(ctx context.Context, name string, execConfig *clientcmdapi.ExecConfig) (awsToken, error) {
	args := append([]string(nil), execConfig.Args...)
	hasOutput := false
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "--output" || lower == "-o" || strings.HasPrefix(lower, "--output=") {
			hasOutput = true
			break
		}
	}
	if !hasOutput {
		args = append(args, "--output", "json")
	}

	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, execConfig.Command, args...)
	cmd.Env = execEnv(execConfig.Env)
	out, err := cmd.Output()
	if err != nil {
		message := err.Error()
		if exit, ok := err.(*exec.ExitError); ok {
			if stderr := strings.TrimSpace(string(exit.Stderr)); stderr != "" {
				message = stderr
			}
		}
		return awsToken{}, fmt.Errorf("aws eks get-token for %s: %s", name, truncateTokenError(message))
	}

	var payload awsTokenOutput
	if err := json.Unmarshal(out, &payload); err != nil {
		return awsToken{}, fmt.Errorf("aws eks get-token for %s: parse output: %w", name, err)
	}
	token := payload.Status.Token
	if token == "" {
		token = payload.Token
	}
	if token == "" {
		return awsToken{}, fmt.Errorf("aws eks get-token for %s: empty token", name)
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	if raw := payload.Status.ExpirationTimestamp; raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return awsToken{}, fmt.Errorf("aws eks get-token for %s: parse expiration: %w", name, err)
		}
		expiresAt = parsed
	}
	if !tokenFresh(expiresAt) {
		return awsToken{}, fmt.Errorf("aws eks get-token for %s: token already expired", name)
	}
	return awsToken{Token: token, ExpiresAt: expiresAt}, nil
}

func execEnv(extra []clientcmdapi.ExecEnvVar) []string {
	values := map[string]string{}
	order := make([]string, 0, len(os.Environ())+len(extra))
	for _, item := range os.Environ() {
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, seen := values[name]; !seen {
			order = append(order, name)
		}
		values[name] = value
	}
	for _, item := range extra {
		if _, seen := values[item.Name]; !seen {
			order = append(order, item.Name)
		}
		values[item.Name] = item.Value
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		out = append(out, name+"="+values[name])
	}
	return out
}

func awsFingerprint(execConfig *clientcmdapi.ExecConfig, server string) string {
	var env []string
	for _, item := range execConfig.Env {
		env = append(env, item.Name+"="+item.Value)
	}
	for _, name := range []string{
		"AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "AWS_ROLE_ARN",
		"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE",
		"AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI",
	} {
		env = append(env, name+"="+os.Getenv(name))
	}
	payload, _ := json.Marshal(struct {
		Command string
		Args    []string
		Env     []string
		Server  string
	}{execConfig.Command, execConfig.Args, env, server})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (p *ClientGoProvider) loadTokenFile(name, fingerprint string) (awsToken, bool) {
	path, err := p.tokenPath(name)
	if err != nil {
		return awsToken{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return awsToken{}, false
	}
	var file awsTokenFile
	if err := json.Unmarshal(data, &file); err != nil {
		return awsToken{}, false
	}
	if file.Context != name || file.Fingerprint != fingerprint || file.Token == "" || !tokenFresh(file.ExpiresAt) {
		return awsToken{}, false
	}
	return awsToken{Token: file.Token, ExpiresAt: file.ExpiresAt}, true
}

func (p *ClientGoProvider) saveTokenFile(name, fingerprint string, token awsToken) {
	path, err := p.tokenPath(name)
	if err != nil {
		return
	}
	payload, err := json.Marshal(awsTokenFile{
		Context:     name,
		Fingerprint: fingerprint,
		Token:       token.Token,
		ExpiresAt:   token.ExpiresAt,
	})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, path)
}

func (p *ClientGoProvider) tokenPath(name string) (string, error) {
	dir := p.tokenDir
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil || base == "" {
			base = os.TempDir()
		}
		dir = filepath.Join(base, "hop", "aws-tokens")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(dir, "ctx-"+hex.EncodeToString(sum[:])[:16]+".json"), nil
}

func tokenFresh(expiresAt time.Time) bool {
	return !expiresAt.IsZero() && time.Now().Add(awsTokenSafety).Before(expiresAt)
}

func truncateTokenError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500] + "…"
	}
	return value
}
