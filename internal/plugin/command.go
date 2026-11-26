package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/config"
)

// CommandSource runs an external process for each search. The process receives
// the JSON Query on stdin and must write {"items": [...], "errors": [...],
// "meta": ...} on stdout. This keeps plugins language-agnostic.
type CommandSource struct {
	cfg config.PluginConfig
}

func NewCommandSource(cfg config.PluginConfig) *CommandSource {
	return &CommandSource{cfg: cfg}
}

func (c *CommandSource) ID() string    { return c.cfg.ID }
func (c *CommandSource) Kind() string  { return c.cfg.Kind }
func (c *CommandSource) Title() string { return c.cfg.Title }

func (c *CommandSource) Selectors() []string {
	return append([]string(nil), c.cfg.Selectors...)
}

func (c *CommandSource) Search(ctx context.Context, query Query) (Result, error) {
	return c.run(ctx, query)
}

func (c *CommandSource) SearchLocal(query Query) (Result, error) {
	query.Local = true
	return c.run(context.Background(), query)
}

func (c *CommandSource) run(ctx context.Context, query Query) (Result, error) {
	timeout := c.cfg.Timeout.Get()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	input, err := json.Marshal(query)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(runCtx, c.cfg.Command, c.cfg.Args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = os.Environ()
	for key, value := range c.cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return Result{}, fmt.Errorf("plugin %s: %s", c.cfg.ID, message)
	}

	var payload struct {
		Items  []Item   `json:"items"`
		Errors []string `json:"errors"`
		Meta   any      `json:"meta"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return Result{}, fmt.Errorf("plugin %s: invalid JSON: %w", c.cfg.ID, err)
	}
	items := payload.Items
	if query.Limit > 0 && len(items) > query.Limit {
		items = items[:query.Limit]
	}
	for i := range items {
		if items[i].ID == "" {
			items[i].ID = fmt.Sprintf("%s:%d", c.cfg.ID, i)
		}
	}
	return Result{
		ID:     c.cfg.ID,
		Title:  c.cfg.Title,
		Kind:   c.cfg.Kind,
		Items:  items,
		Total:  len(items),
		Errors: payload.Errors,
		Meta:   payload.Meta,
	}, nil
}
