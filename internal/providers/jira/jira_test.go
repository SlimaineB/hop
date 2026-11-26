package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SlimaineB/teu/internal/config"
	"github.com/SlimaineB/teu/internal/providers"
)

func TestProviderSearchGetCreate(t *testing.T) {
	t.Setenv("TEST_JIRA_TOKEN", "secret")
	t.Setenv("TEST_JIRA_USER", "me@example.com")
	var created map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "me@example.com" || password != "secret" {
			t.Fatalf("unexpected auth: %v %q %q", ok, user, password)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/search":
			if !strings.Contains(r.URL.Query().Get("jql"), `text ~ "checkout"`) {
				t.Fatalf("unexpected jql: %s", r.URL.Query().Get("jql"))
			}
			_, _ = w.Write([]byte(`{"issues":[{"key":"OPS-1","fields":{"summary":"Checkout timeout","status":{"name":"Open"},"updated":"2026-11-25T10:00:00.000+0000"}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/OPS-1":
			_, _ = w.Write([]byte(`{"key":"OPS-1","fields":{"summary":"Checkout timeout","status":{"name":"Open"},"updated":"2026-11-25T10:00:00.000+0000"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":"OPS-2","self":"http://jira/rest/api/2/issue/2"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	provider, err := New("jira-prod", config.ProviderConfig{
		Type:        "jira",
		BaseURL:     server.URL,
		UsernameEnv: "TEST_JIRA_USER",
		TokenEnv:    "TEST_JIRA_TOKEN",
		Defaults:    map[string]string{"project": "OPS", "issue_type": "Task"},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if !provider.IsKey("OPS-123") || provider.IsKey("checkout-timeout") {
		t.Fatalf("IsKey mismatch")
	}

	tickets, err := provider.Search(context.Background(), "checkout", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(tickets) != 1 || tickets[0].Key != "OPS-1" || tickets[0].Status != "Open" {
		t.Fatalf("tickets = %+v", tickets)
	}
	if tickets[0].URL != server.URL+"/browse/OPS-1" {
		t.Fatalf("url = %s", tickets[0].URL)
	}

	ticket, err := provider.Get(context.Background(), "OPS-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ticket.Title != "Checkout timeout" {
		t.Fatalf("ticket = %+v", ticket)
	}

	createdTicket, err := provider.Create(context.Background(), providers.TicketDraft{
		Title:  "Pod api crashloop",
		Body:   "details",
		Fields: map[string]string{"type": "Bug"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if createdTicket.Key != "OPS-2" {
		t.Fatalf("created = %+v", createdTicket)
	}
	fields := created["fields"].(map[string]any)
	if fields["summary"] != "Pod api crashloop" || fields["description"] != "details" {
		t.Fatalf("fields = %+v", fields)
	}
	if fields["project"].(map[string]any)["key"] != "OPS" {
		t.Fatalf("project = %+v", fields["project"])
	}
	if fields["issuetype"].(map[string]any)["name"] != "Bug" {
		t.Fatalf("issuetype = %+v", fields["issuetype"])
	}
}

func TestProviderRequiresTokenForAPI(t *testing.T) {
	t.Setenv("TEST_EMPTY_TOKEN", "")
	provider, err := New("jira", config.ProviderConfig{
		Type:     "jira",
		BaseURL:  "https://jira.example.com",
		TokenEnv: "TEST_EMPTY_TOKEN",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.Search(context.Background(), "test", 5); err == nil || !strings.Contains(err.Error(), "token is not set") {
		t.Fatalf("expected token error, got %v", err)
	}
	if provider.URL("OPS-1") != "https://jira.example.com/browse/OPS-1" {
		t.Fatalf("url = %s", provider.URL("OPS-1"))
	}
}

func TestProviderRejectsInvalidBaseURL(t *testing.T) {
	_, err := New("jira", config.ProviderConfig{Type: "jira", BaseURL: "not-a-url"})
	if err == nil {
		t.Fatal("expected error")
	}
}
