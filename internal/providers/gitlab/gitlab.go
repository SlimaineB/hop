package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/providers"
)

var (
	relativeKeyPattern = regexp.MustCompile(`^#?([0-9]+)$`)
	projectKeyPattern  = regexp.MustCompile(`^([^#\s]+)#([0-9]+)$`)
	issuePathPattern   = regexp.MustCompile(`^(.*)/-/issues/([0-9]+)/?$`)
)

type Provider struct {
	id         string
	baseURL    string
	token      string
	authScheme string
	keywords   []string
	project    string
	httpClient *http.Client
}

type issue struct {
	IID        int    `json:"iid"`
	ProjectID  int    `json:"project_id"`
	Title      string `json:"title"`
	State      string `json:"state"`
	WebURL     string `json:"web_url"`
	UpdatedAt  string `json:"updated_at"`
	References struct {
		Short    string `json:"short"`
		Relative string `json:"relative"`
		Full     string `json:"full"`
	} `json:"references"`
}

func New(id string, cfg config.ProviderConfig) (providers.TicketProvider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("GitLab provider %q: invalid base_url %q", id, cfg.BaseURL)
	}
	tokenEnv := cfg.TokenEnv
	if tokenEnv == "" {
		tokenEnv = "GITLAB_TOKEN"
	}
	token := os.Getenv(tokenEnv)
	authScheme := strings.ToLower(strings.TrimSpace(cfg.AuthScheme))
	if authScheme == "" {
		authScheme = "private-token"
	}
	switch authScheme {
	case "private-token", "private_token", "token":
		authScheme = "private-token"
	case "bearer", "oauth":
		authScheme = "bearer"
	default:
		return nil, fmt.Errorf("GitLab provider %q: auth_scheme must be private-token or bearer", id)
	}
	timeout := cfg.Timeout.Get()
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Provider{
		id:         id,
		baseURL:    baseURL,
		token:      token,
		authScheme: authScheme,
		keywords:   append([]string(nil), cfg.Keywords...),
		project:    strings.Trim(strings.TrimSpace(cfg.Defaults["project"]), "/"),
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (p *Provider) ID() string         { return p.id }
func (p *Provider) Type() string       { return "gitlab" }
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
		match := issuePathPattern.FindStringSubmatch(strings.TrimRight(parsed.Path, "/"))
		if match == nil {
			return "", false
		}
		project := strings.Trim(strings.TrimPrefix(match[1], "/"), "/")
		iid, err := strconv.Atoi(match[2])
		if err != nil || iid <= 0 || project == "" {
			return "", false
		}
		return project + "#" + strconv.Itoa(iid), true
	}
	if match := projectKeyPattern.FindStringSubmatch(value); match != nil {
		project := strings.Trim(strings.TrimSpace(match[1]), "/")
		iid, err := strconv.Atoi(match[2])
		if err != nil || iid <= 0 || project == "" {
			return "", false
		}
		return project + "#" + strconv.Itoa(iid), true
	}
	if match := relativeKeyPattern.FindStringSubmatch(value); match != nil {
		iid, err := strconv.Atoi(match[1])
		if err != nil || iid <= 0 {
			return "", false
		}
		return "#" + strconv.Itoa(iid), true
	}
	return "", false
}

func (p *Provider) URL(key string) string {
	normalized, ok := p.NormalizeKey(key)
	if !ok {
		return ""
	}
	project, iid := splitKey(normalized)
	if project == "" {
		project = p.project
	}
	if project == "" {
		return p.baseURL + "/-/issues/" + iid
	}
	return p.baseURL + "/" + escapeProject(project) + "/-/issues/" + iid
}

func (p *Provider) Search(ctx context.Context, query string, limit int) ([]providers.Ticket, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	params := url.Values{}
	params.Set("per_page", strconv.Itoa(limit))
	params.Set("order_by", "updated_at")
	params.Set("sort", "desc")
	params.Set("scope", "all")
	params.Set("state", "all")
	if strings.TrimSpace(query) != "" {
		params.Set("search", strings.TrimSpace(query))
	}
	endpoint := p.baseURL + "/api/v4/issues?" + params.Encode()
	if p.project != "" {
		endpoint = p.baseURL + "/api/v4/projects/" + url.PathEscape(p.project) + "/issues?" + params.Encode()
	}
	var response []issue
	if err := p.do(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, err
	}
	tickets := make([]providers.Ticket, 0, len(response))
	for _, item := range response {
		tickets = append(tickets, p.ticket(item))
	}
	return tickets, nil
}

func (p *Provider) Get(ctx context.Context, key string) (providers.Ticket, error) {
	normalized, ok := p.NormalizeKey(key)
	if !ok {
		return providers.Ticket{}, fmt.Errorf("GitLab provider %q: invalid issue key %q", p.id, key)
	}
	project, iid := splitKey(normalized)
	if project == "" {
		project = p.project
	}
	if project == "" {
		return providers.Ticket{}, fmt.Errorf("GitLab provider %q: project is required (set providers.%s.defaults.project)", p.id, p.id)
	}
	endpoint := p.baseURL + "/api/v4/projects/" + url.PathEscape(project) + "/issues/" + iid
	var response issue
	if err := p.do(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return providers.Ticket{}, err
	}
	return p.ticket(response), nil
}

func (p *Provider) Create(ctx context.Context, draft providers.TicketDraft) (providers.Ticket, error) {
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		return providers.Ticket{}, fmt.Errorf("GitLab provider %q: title is required", p.id)
	}
	project := strings.Trim(strings.TrimSpace(draft.Fields["project"]), "/")
	if project == "" {
		project = p.project
	}
	if project == "" {
		return providers.Ticket{}, fmt.Errorf("GitLab provider %q: project is required (set providers.%s.defaults.project)", p.id, p.id)
	}
	payload := map[string]any{"title": title}
	if strings.TrimSpace(draft.Body) != "" {
		payload["description"] = draft.Body
	}
	var response issue
	endpoint := p.baseURL + "/api/v4/projects/" + url.PathEscape(project) + "/issues"
	if err := p.do(ctx, http.MethodPost, endpoint, payload, &response); err != nil {
		return providers.Ticket{}, err
	}
	return p.ticket(response), nil
}

func (p *Provider) ticket(item issue) providers.Ticket {
	key := strings.TrimSpace(item.References.Full)
	if key == "" {
		key = strings.TrimSpace(item.References.Relative)
	}
	if key == "" {
		if p.project != "" {
			key = p.project + "#" + strconv.Itoa(item.IID)
		} else {
			key = "#" + strconv.Itoa(item.IID)
		}
	}
	target := item.WebURL
	if target == "" {
		target = p.URL(key)
	}
	return providers.Ticket{
		Provider: p.id,
		Key:      key,
		Title:    item.Title,
		Status:   item.State,
		URL:      target,
		Updated:  item.UpdatedAt,
	}
}

func (p *Provider) do(ctx context.Context, method, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode GitLab request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("build GitLab request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if p.token == "" {
		return fmt.Errorf("GitLab provider %q: token is not set (configure token_env)", p.id)
	}
	if p.authScheme == "bearer" {
		request.Header.Set("Authorization", "Bearer "+p.token)
	} else {
		request.Header.Set("PRIVATE-TOKEN", p.token)
	}
	response, err := p.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("GitLab %s %s: %w", method, endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("GitLab %s %s: HTTP %d: %s", method, endpoint, response.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode GitLab response: %w", err)
	}
	return nil
}

func splitKey(key string) (string, string) {
	project, iid, _ := strings.Cut(strings.TrimSpace(key), "#")
	return strings.Trim(project, "/"), strings.TrimSpace(iid)
}

func escapeProject(project string) string {
	segments := strings.Split(strings.Trim(project, "/"), "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	return strings.Join(segments, "/")
}
