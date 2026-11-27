package kube

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestIsAWSExec(t *testing.T) {
	if !isAWSExec(&clientcmdapi.ExecConfig{Command: "/usr/local/bin/aws", Args: []string{"eks", "get-token"}}) {
		t.Fatal("expected aws eks get-token to be detected")
	}
	if isAWSExec(&clientcmdapi.ExecConfig{Command: "aws", Args: []string{"s3", "ls"}}) {
		t.Fatal("did not expect aws s3 to be detected")
	}
	if isAWSExec(nil) {
		t.Fatal("nil exec config must not match")
	}
}

func TestAWSTokenFileCache(t *testing.T) {
	provider := &ClientGoProvider{tokenDir: t.TempDir()}
	execConfig := &clientcmdapi.ExecConfig{Command: "aws", Args: []string{"eks", "get-token", "--cluster-name", "one"}}
	fingerprint := awsFingerprint(execConfig, "https://one.example.com")
	token := awsToken{Token: "token-one", ExpiresAt: time.Now().Add(10 * time.Minute)}
	provider.saveTokenFile("ctx-one", fingerprint, token)

	loaded, ok := provider.loadTokenFile("ctx-one", fingerprint)
	if !ok || loaded.Token != token.Token {
		t.Fatalf("loaded = %+v, ok = %v", loaded, ok)
	}
	if _, ok := provider.loadTokenFile("ctx-one", "other-fingerprint"); ok {
		t.Fatal("fingerprint mismatch must invalidate cached token")
	}
	provider.saveTokenFile("ctx-one", fingerprint, awsToken{Token: "expired", ExpiresAt: time.Now().Add(-time.Minute)})
	if _, ok := provider.loadTokenFile("ctx-one", fingerprint); ok {
		t.Fatal("expired token must not be reused")
	}

	other := awsFingerprint(execConfig, "https://two.example.com")
	provider.saveTokenFile("ctx-two", other, awsToken{Token: "token-two", ExpiresAt: time.Now().Add(10 * time.Minute)})
	entries, err := os.ReadDir(provider.tokenDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("token files = %d, want one per context", len(entries))
	}
}

func TestRunAWSTokenParsesOutput(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "aws")
	body := "#!/bin/sh\nprintf '%s' '{\"status\":{\"token\":\"sts-token\",\"expirationTimestamp\":\"2099-01-01T00:00:00Z\"}}'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	token, err := runAWSToken(context.Background(), "ctx", &clientcmdapi.ExecConfig{
		Command: script,
		Args:    []string{"eks", "get-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.Token != "sts-token" {
		t.Fatalf("token = %q", token.Token)
	}
	if !token.ExpiresAt.After(time.Now()) {
		t.Fatalf("expiry = %v, want future", token.ExpiresAt)
	}
}
