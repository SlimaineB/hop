package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/providers"
)

func TestProviderSearchGetCreate(t *testing.T) {
	t.Setenv("TEST_GITLAB_TOKEN", "secret")
	var created map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "secret" {
			t.Fatalf("unexpected token: %q", r.Header.Get("PRIVATE-TOKEN"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/api/v4/projects/lab/hop/issues"):
			if r.URL.Query().Get("search") != "checkout" {
				t.Fatalf("unexpected search: %s", r.URL.Query().Get("search"))
			}
			_, _ = w.Write([]byte(`[{"iid":1,"project_id":7,"title":"Checkout timeout","state":"opened","web_url":"` + server.URL + `/lab/hop/-/issues/1","updated_at":"2026-11-25T10:00:00Z","references":{"full":"lab/hop#1","relative":"#1","short":"#1"}}]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/api/v4/projects/lab/hop/issues/1"):
			_, _ = w.Write([]byte(`{"iid":1,"title":"Checkout timeout","state":"opened","web_url":"` + server.URL + `/lab/hop/-/issues/1","references":{"full":"lab/hop#1"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/v4/projects/lab/hop/issues"):
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"iid":2,"title":"Pod api crashloop","state":"opened","web_url":"` + server.URL + `/lab/hop/-/issues/2","references":{"full":"lab/hop#2"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	provider, err := New("gitlab-lab", config.ProviderConfig{
		Type:     "gitlab",
		BaseURL:  server.URL,
		TokenEnv: "TEST_GITLAB_TOKEN",
		Defaults: map[string]string{"project": "lab/hop"},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if !provider.IsKey("lab/hop#12") || !provider.IsKey("#12") {
		t.Fatal("expected GitLab keys to match")
	}
	if provider.IsKey("checkout timeout") {
		t.Fatal("plain words must not be a ticket key")
	}

	tickets, err := provider.Search(context.Background(), "checkout", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(tickets) != 1 || tickets[0].Key != "lab/hop#1" || tickets[0].Status != "opened" {
		t.Fatalf("tickets = %+v", tickets)
	}
	if tickets[0].URL != server.URL+"/lab/hop/-/issues/1" {
		t.Fatalf("url = %s", tickets[0].URL)
	}

	ticket, err := provider.Get(context.Background(), "lab/hop#1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ticket.Title != "Checkout timeout" {
		t.Fatalf("ticket = %+v", ticket)
	}

	createdTicket, err := provider.Create(context.Background(), providers.TicketDraft{
		Title:  "Pod api crashloop",
		Body:   "details",
		Fields: map[string]string{"project": "lab/hop"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if createdTicket.Key != "lab/hop#2" {
		t.Fatalf("created = %+v", createdTicket)
	}
	if created["title"] != "Pod api crashloop" || created["description"] != "details" {
		t.Fatalf("payload = %+v", created)
	}
}

func TestNormalizeKey(t *testing.T) {
	provider := &Provider{baseURL: "https://gitlab.example.com", project: "lab/hop"}
	cases := map[string]string{
		"12":         "#12",
		"#12":        "#12",
		"lab/hop#12": "lab/hop#12",
		"https://gitlab.example.com/lab/hop/-/issues/12": "lab/hop#12",
	}
	for input, want := range cases {
		got, ok := provider.NormalizeKey(input)
		if !ok || got != want {
			t.Fatalf("NormalizeKey(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	if provider.URL("#12") != "https://gitlab.example.com/lab/hop/-/issues/12" {
		t.Fatalf("url = %s", provider.URL("#12"))
	}
}

func TestProviderRequiresTokenForAPI(t *testing.T) {
	t.Setenv("TEST_EMPTY_GITLAB_TOKEN", "")
	provider, err := New("gitlab", config.ProviderConfig{
		Type:     "gitlab",
		BaseURL:  "https://gitlab.example.com",
		TokenEnv: "TEST_EMPTY_GITLAB_TOKEN",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.Search(context.Background(), "test", 5); err == nil || !strings.Contains(err.Error(), "token is not set") {
		t.Fatalf("expected token error, got %v", err)
	}
}

func TestProviderRejectsInvalidBaseURL(t *testing.T) {
	_, err := New("gitlab", config.ProviderConfig{Type: "gitlab", BaseURL: "not-a-url"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProviderMinimalConfigSearchesProjectsAndGroups(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "secret" {
			t.Fatalf("unexpected token: %q", r.Header.Get("PRIVATE-TOKEN"))
		}
		if r.URL.Query().Get("search") != "timeout" {
			t.Fatalf("unexpected search: %s", r.URL.Query().Get("search"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/lab/hop/issues":
			_, _ = w.Write([]byte(`[{"iid":1,"project_id":7,"title":"Hop timeout","state":"opened","web_url":"` + server.URL + `/lab/hop/-/issues/1","updated_at":"2026-11-25T10:00:00Z","references":{"full":"lab/hop#1"}}]`))
		case "/api/v4/projects/lab/other/issues":
			_, _ = w.Write([]byte(`[{"iid":3,"project_id":9,"title":"Other timeout","state":"opened","web_url":"` + server.URL + `/lab/other/-/issues/3","updated_at":"2026-11-25T12:00:00Z","references":{"full":"lab/other#3"}}]`))
		case "/api/v4/groups/platform/issues":
			_, _ = w.Write([]byte(`[{"iid":2,"project_id":8,"title":"Platform timeout","state":"opened","web_url":"` + server.URL + `/platform/api/-/issues/2","updated_at":"2026-11-25T11:00:00Z","references":{"full":"platform/api#2"}}]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	provider, err := New("gitlab", config.ProviderConfig{
		BaseURL:  server.URL,
		Token:    "secret",
		Projects: []string{"lab/hop", "lab/other"},
		Groups:   []string{"platform"},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	tickets, err := provider.Search(context.Background(), "timeout", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(tickets) != 3 {
		t.Fatalf("tickets = %+v", tickets)
	}
	if tickets[0].Key != "lab/other#3" || tickets[1].Key != "platform/api#2" || tickets[2].Key != "lab/hop#1" {
		t.Fatalf("unexpected order: %+v", tickets)
	}
}
