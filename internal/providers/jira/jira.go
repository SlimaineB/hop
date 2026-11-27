package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/providers"
)

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-[0-9]+$`)

type Provider struct {
	id         string
	baseURL    string
	username   string
	token      string
	authScheme string
	keywords   []string
	projects   []string
	project    string
	issueType  string
	httpClient *http.Client
}

func New(id string, cfg config.ProviderConfig) (providers.TicketProvider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("Jira provider %q: invalid base_url %q", id, cfg.BaseURL)
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		tokenEnv := cfg.TokenEnv
		if tokenEnv == "" {
			tokenEnv = "JIRA_TOKEN"
		}
		token = strings.TrimSpace(os.Getenv(tokenEnv))
	}
	username := cfg.Username
	usernameEnv := cfg.UsernameEnv
	if username == "" && usernameEnv == "" {
		usernameEnv = "JIRA_USER"
	}
	if username == "" && usernameEnv != "" {
		username = os.Getenv(usernameEnv)
	}
	authScheme := strings.ToLower(strings.TrimSpace(cfg.AuthScheme))
	if authScheme == "" {
		if username != "" {
			authScheme = "basic"
		} else {
			authScheme = "bearer"
		}
	}
	if authScheme != "basic" && authScheme != "bearer" {
		return nil, fmt.Errorf("Jira provider %q: auth_scheme must be basic or bearer", id)
	}
	if authScheme == "basic" && username == "" {
		return nil, fmt.Errorf("Jira provider %q: username or username_env is required for basic auth", id)
	}
	timeout := cfg.Timeout.Get()
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	projects := cleanProjects(cfg.Projects)
	project := strings.TrimSpace(cfg.Defaults["project"])
	if project == "" && len(projects) == 1 {
		project = projects[0]
	}
	issueType := cfg.Defaults["issue_type"]
	if issueType == "" {
		issueType = "Task"
	}
	return &Provider{
		id:         id,
		baseURL:    baseURL,
		username:   username,
		token:      token,
		authScheme: authScheme,
		keywords:   append([]string(nil), cfg.Keywords...),
		projects:   projects,
		project:    project,
		issueType:  issueType,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (p *Provider) ID() string         { return p.id }
func (p *Provider) Type() string       { return "jira" }
func (p *Provider) Keywords() []string { return append([]string(nil), p.keywords...) }

func (p *Provider) IsKey(value string) bool {
	_, ok := p.NormalizeKey(value)
	return ok
}

func (p *Provider) NormalizeKey(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		last := path.Base(strings.TrimRight(parsed.Path, "/"))
		if decoded, err := url.PathUnescape(last); err == nil {
			last = decoded
		}
		value = last
	}
	value = strings.ToUpper(strings.TrimSpace(value))
	if !keyPattern.MatchString(value) {
		return "", false
	}
	return value, true
}

func (p *Provider) URL(key string) string {
	if normalized, ok := p.NormalizeKey(key); ok {
		key = normalized
	}
	return p.baseURL + "/browse/" + url.PathEscape(strings.TrimSpace(key))
}

func (p *Provider) Search(ctx context.Context, query string, limit int) ([]providers.Ticket, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	jql := p.jql(query)
	endpoint := p.baseURL + "/rest/api/2/search?jql=" + url.QueryEscape(jql) +
		"&maxResults=" + fmt.Sprintf("%d", limit) + "&fields=summary,status,updated"
	var response struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
				Updated string `json:"updated"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := p.do(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, err
	}
	tickets := make([]providers.Ticket, 0, len(response.Issues))
	for _, issue := range response.Issues {
		tickets = append(tickets, providers.Ticket{
			Provider: p.id,
			Key:      issue.Key,
			Title:    issue.Fields.Summary,
			Status:   issue.Fields.Status.Name,
			URL:      p.URL(issue.Key),
			Updated:  issue.Fields.Updated,
		})
	}
	return tickets, nil
}

func (p *Provider) Get(ctx context.Context, key string) (providers.Ticket, error) {
	if normalized, ok := p.NormalizeKey(key); ok {
		key = normalized
	}
	key = strings.TrimSpace(key)
	endpoint := p.baseURL + "/rest/api/2/issue/" + url.PathEscape(key) + "?fields=summary,status,updated"
	var response struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name string `json:"name"`
			} `json:"status"`
			Updated string `json:"updated"`
		} `json:"fields"`
	}
	if err := p.do(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return providers.Ticket{}, err
	}
	return providers.Ticket{
		Provider: p.id,
		Key:      response.Key,
		Title:    response.Fields.Summary,
		Status:   response.Fields.Status.Name,
		URL:      p.URL(response.Key),
		Updated:  response.Fields.Updated,
	}, nil
}

func (p *Provider) Create(ctx context.Context, draft providers.TicketDraft) (providers.Ticket, error) {
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		return providers.Ticket{}, fmt.Errorf("Jira provider %q: title is required", p.id)
	}
	project := strings.TrimSpace(draft.Fields["project"])
	if project == "" {
		project = p.project
	}
	issueType := strings.TrimSpace(draft.Fields["type"])
	if issueType == "" {
		issueType = p.issueType
	}
	if project == "" {
		return providers.Ticket{}, fmt.Errorf("Jira provider %q: project is required (set providers.%s.projects or pass --project)", p.id, p.id)
	}
	payload := map[string]any{
		"fields": map[string]any{
			"project":   map[string]string{"key": project},
			"summary":   title,
			"issuetype": map[string]string{"name": issueType},
		},
	}
	if strings.TrimSpace(draft.Body) != "" {
		payload["fields"].(map[string]any)["description"] = draft.Body
	}
	var response struct {
		Key  string `json:"key"`
		Self string `json:"self"`
	}
	if err := p.do(ctx, http.MethodPost, p.baseURL+"/rest/api/2/issue", payload, &response); err != nil {
		return providers.Ticket{}, err
	}
	return providers.Ticket{
		Provider: p.id,
		Key:      response.Key,
		Title:    title,
		URL:      p.URL(response.Key),
	}, nil
}

func (p *Provider) do(ctx context.Context, method, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Jira request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("build Jira request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if p.token == "" {
		return fmt.Errorf("Jira provider %q: token is not set (configure token or token_env)", p.id)
	}
	switch p.authScheme {
	case "basic":
		request.SetBasicAuth(p.username, p.token)
	default:
		request.Header.Set("Authorization", "Bearer "+p.token)
	}
	response, err := p.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("Jira %s %s: %w", method, endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Jira %s %s: HTTP %d: %s", method, endpoint, response.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode Jira response: %w", err)
	}
	return nil
}

func (p *Provider) jql(query string) string {
	clauses := make([]string, 0, 2)
	if value := strings.TrimSpace(query); value != "" {
		clauses = append(clauses, `text ~ "`+escapeJQL(value)+`"`)
	}
	if len(p.projects) > 0 {
		projects := make([]string, len(p.projects))
		for i, project := range p.projects {
			projects[i] = `"` + escapeJQL(project) + `"`
		}
		clauses = append(clauses, "project IN ("+strings.Join(projects, ",")+")")
	}
	if len(clauses) == 0 {
		return "ORDER BY updated DESC"
	}
	return strings.Join(clauses, " AND ") + " ORDER BY updated DESC"
}

func cleanProjects(values []string) []string {
	projects := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		projects = append(projects, value)
	}
	return projects
}

func escapeJQL(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
