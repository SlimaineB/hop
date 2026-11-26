package actions

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SlimaineB/hop/internal/kube"
)

func TestCommandsIncludeTimeRangeAndNode(t *testing.T) {
	pod := kube.Pod{
		Context: "prod-eu", Namespace: "apps", Name: "api-123",
		Node: "node-1", OwnerKind: "Deployment", OwnerName: "api",
	}
	opts := Options{From: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	commands := Commands(pod, opts)

	var logs, node string
	for _, command := range commands {
		switch command.ID {
		case "logs":
			logs = command.Command
		case "node":
			node = command.Command
		}
	}
	if !strings.Contains(logs, "--since-time 2026-11-25T12:00:00Z") {
		t.Fatalf("logs command missing since-time: %s", logs)
	}
	if !strings.Contains(node, "describe node node-1") {
		t.Fatalf("node command missing: %s", node)
	}
	if strings.Contains(node, "-n apps") {
		t.Fatalf("node command should not be namespaced: %s", node)
	}
}

func TestLogsProcessUsesSinceTime(t *testing.T) {
	pod := kube.Pod{Context: "prod-eu", Namespace: "apps", Name: "api-123"}
	opts := Options{From: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	cmd := Logs(pod, opts)
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "--since-time 2026-11-25T12:00:00Z") {
		t.Fatalf("logs args missing since-time: %s", joined)
	}
}

func TestDescribePagedUsesPager(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pager uses sh")
	}
	t.Setenv("PAGER", "cat")
	pod := kube.Pod{Context: "prod-eu", Namespace: "apps", Name: "api-123"}
	cmd := DescribePaged(pod, Options{})
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "describe pod api-123") || !strings.Contains(joined, "| cat") {
		t.Fatalf("describe pager command missing pipeline: %s", joined)
	}
}

func TestOpenBrowserFallsBackAfterImmediateFailure(t *testing.T) {
	candidates := []browserCandidate{
		{name: "sh", args: []string{"-c", "exit 7"}},
		{name: "sh", args: []string{"-c", "exit 0"}},
	}
	if err := openBrowser(candidates, time.Second); err != nil {
		t.Fatalf("expected second candidate to succeed: %v", err)
	}
}

func TestOpenBrowserReportsFailures(t *testing.T) {
	err := openBrowser([]browserCandidate{{name: "sh", args: []string{"-c", "exit 7"}}}, time.Second)
	if err == nil {
		t.Fatal("expected browser opener failure")
	}
}

func TestOpenBrowserKeepsRunningOpener(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	candidates := []browserCandidate{{name: "sh", args: []string{"-c", "sleep 5"}}}
	if err := openBrowser(candidates, 50*time.Millisecond); err != nil {
		t.Fatalf("expected slow opener to count as started: %v", err)
	}
}

func TestOpenBrowserEmptyURL(t *testing.T) {
	if err := OpenBrowser(""); err == nil {
		t.Fatal("expected empty URL error")
	}
}

func TestBrowserCandidatesEscapeCmdURLOnWSL(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL candidate selection is Linux-only")
	}
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	target := "http://127.0.0.1:1234/pod?context=prod-eu&namespace=apps&name=api-123&q=12h"
	for _, candidate := range browserCandidates(target) {
		if candidate.name != "cmd.exe" {
			continue
		}
		got := candidate.args[len(candidate.args)-1]
		if !strings.Contains(got, "context=prod-eu^&namespace=apps^&name=api-123^&q=12h") {
			t.Fatalf("cmd.exe URL not escaped: %s", got)
		}
		return
	}
	t.Fatal("cmd.exe candidate missing")
}

func TestEscapeCmdArg(t *testing.T) {
	got := escapeCmdArg("a&b|c<d>e(f)g%h^i")
	want := "a^&b^|c^<d^>e^(f^)g^%h^^i"
	if got != want {
		t.Fatalf("escapeCmdArg = %q, want %q", got, want)
	}
}
