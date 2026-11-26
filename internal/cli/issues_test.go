package cli

import "testing"

func TestTicketKeyFromInput(t *testing.T) {
	cases := map[string]string{
		"OPS-123":                               "OPS-123",
		"ops-123":                               "OPS-123",
		"https://jira.example.com/browse/OPS-1": "OPS-1",
	}
	for input, want := range cases {
		got, ok := ticketKeyFromInput(input)
		if !ok || got != want {
			t.Fatalf("ticketKeyFromInput(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"checkout timeout", "checkout-timeout", "https://example.com/issue"} {
		if got, ok := ticketKeyFromInput(input); ok {
			t.Fatalf("ticketKeyFromInput(%q) unexpectedly matched %q", input, got)
		}
	}
}
