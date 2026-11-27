package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/providers/jira"
)

func TestTicketKeyFromInput(t *testing.T) {
	provider, err := jira.New("jira", config.ProviderConfig{
		BaseURL: "https://jira.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"OPS-123":                               "OPS-123",
		"ops-123":                               "OPS-123",
		"https://jira.example.com/browse/OPS-1": "OPS-1",
	}
	for input, want := range cases {
		got, ok := normalizedTicketKey(provider, input)
		if !ok || got != want {
			t.Fatalf("normalizedTicketKey(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"checkout timeout", "checkout-timeout", "https://example.com/issue"} {
		if got, ok := normalizedTicketKey(provider, input); ok {
			t.Fatalf("normalizedTicketKey(%q) unexpectedly matched %q", input, got)
		}
	}
}

func TestProviderAliasSupportsPrintFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := `providers:
  gitlab-lab:
    type: gitlab
    base_url: https://gitlab.example.com
    defaults:
      project: lab/hop
`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runCLI(t, "--config", path, "gitlab", "1", "--print")
	if err != nil {
		t.Fatalf("hop gitlab 1 --print: %v\nstderr: %s", err, stderr)
	}
	want := "https://gitlab.example.com/lab/hop/-/issues/1"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want URL %q", stdout, want)
	}
}
