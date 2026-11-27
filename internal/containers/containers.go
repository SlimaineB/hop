// Package containers discovers local Docker and Podman containers through
// their CLIs. It keeps the same search/action shape as the other hop sources.
package containers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/search"
)

type Engine struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type Container struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	Command    string            `json:"command,omitempty"`
	State      string            `json:"state,omitempty"`
	Status     string            `json:"status,omitempty"`
	Ports      string            `json:"ports,omitempty"`
	Created    string            `json:"created,omitempty"`
	RunningFor string            `json:"running_for,omitempty"`
	Project    string            `json:"project,omitempty"`
	Service    string            `json:"service,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Engine     string            `json:"engine"`
	EngineType string            `json:"engine_type,omitempty"`
}

type Hit struct {
	Container Container      `json:"container"`
	Score     float64        `json:"score"`
	Percent   int            `json:"percent"`
	Matches   []search.Match `json:"matches,omitempty"`
	Reasons   []string       `json:"reasons,omitempty"`
}

type Service struct {
	engines  []Engine
	timeout  time.Duration
	ttl      time.Duration
	minScore float64

	mu          sync.RWMutex
	snapshot    []Container
	snapshotSet bool
	fetchedAt   time.Time
}

func New(cfg *config.Config) *Service {
	service := &Service{
		timeout:  10 * time.Second,
		ttl:      15 * time.Second,
		minScore: 0.5,
	}
	if cfg == nil {
		service.engines = detectEngines()
		return service
	}
	service.minScore = cfg.Search.MinScore
	if cfg.Containers.Timeout.Get() > 0 {
		service.timeout = cfg.Containers.Timeout.Get()
	}
	if cfg.Containers.CacheTTL.Get() > 0 {
		service.ttl = cfg.Containers.CacheTTL.Get()
	}
	for _, entry := range cfg.Containers.Engines {
		engine := Engine{
			Name:    strings.TrimSpace(entry.Name),
			Type:    strings.TrimSpace(entry.Type),
			Command: strings.TrimSpace(entry.Command),
			Args:    append([]string(nil), entry.Args...),
		}
		if engine.Type == "" {
			engine.Type = inferType(engine.Name + " " + engine.Command)
		}
		if engine.Name == "" {
			engine.Name = engine.Type
		}
		if engine.Command == "" {
			engine.Command = engine.Type
		}
		if engine.Name == "" || engine.Type == "" {
			continue
		}
		service.engines = append(service.engines, engine)
	}
	if len(service.engines) == 0 {
		service.engines = detectEngines()
	}
	return service
}

func detectEngines() []Engine {
	var engines []Engine
	for _, name := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(name); err == nil {
			engines = append(engines, Engine{Name: name, Type: name, Command: name})
		}
	}
	return engines
}

func inferType(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "podman"):
		return "podman"
	case strings.Contains(lower, "docker"):
		return "docker"
	default:
		return ""
	}
}

func (s *Service) Enabled() bool { return s != nil && len(s.engines) > 0 }

func (s *Service) Engines() []Engine {
	if s == nil {
		return nil
	}
	return append([]Engine(nil), s.engines...)
}

func (s *Service) Refresh() {
	s.mu.Lock()
	s.snapshot = nil
	s.snapshotSet = false
	s.fetchedAt = time.Time{}
	s.mu.Unlock()
}

func (s *Service) List(ctx context.Context) ([]Container, []string, error) {
	if !s.Enabled() {
		return nil, nil, nil
	}
	s.mu.RLock()
	fresh := s.snapshotSet && time.Since(s.fetchedAt) < s.ttl
	items := append([]Container(nil), s.snapshot...)
	s.mu.RUnlock()
	if fresh {
		return items, nil, nil
	}
	return s.fetch(ctx)
}

func (s *Service) fetch(ctx context.Context) ([]Container, []string, error) {
	type result struct {
		items  []Container
		err    error
		engine Engine
	}
	results := make(chan result, len(s.engines))
	for _, engine := range s.engines {
		engine := engine
		go func() {
			items, err := s.listEngine(ctx, engine)
			results <- result{items: items, engine: engine, err: err}
		}()
	}
	items := make([]Container, 0, 32)
	warnings := make([]string, 0)
	for range s.engines {
		value := <-results
		if value.err != nil {
			warnings = append(warnings, value.engine.Name+": "+value.err.Error())
			continue
		}
		items = append(items, value.items...)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Engine != items[j].Engine {
			return items[i].Engine < items[j].Engine
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	s.mu.Lock()
	s.snapshot = items
	s.snapshotSet = true
	s.fetchedAt = time.Now()
	s.mu.Unlock()
	if len(items) == 0 && len(warnings) == len(s.engines) {
		return items, warnings, errors.New(strings.Join(warnings, "; "))
	}
	return items, warnings, nil
}

func (s *Service) listEngine(ctx context.Context, engine Engine) ([]Container, error) {
	args := append([]string{}, engine.Args...)
	args = append(args, "ps", "-a", "--format", "{{json .}}")
	cmdCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, engine.Command, args...)
	output, err := cmd.Output()
	if err != nil {
		if cmdCtx.Err() != nil {
			return nil, cmdCtx.Err()
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message := strings.TrimSpace(string(exitErr.Stderr))
			if message == "" {
				message = exitErr.Error()
			}
			return nil, errors.New(message)
		}
		return nil, err
	}
	return parseList(output, engine), nil
}

func (s *Service) Search(ctx context.Context, raw string, limit int) ([]Hit, []string, error) {
	if !s.Enabled() {
		return nil, nil, nil
	}
	items, warnings, err := s.List(ctx)
	if err != nil {
		return nil, warnings, err
	}
	return s.rank(items, raw, limit), warnings, nil
}

func (s *Service) SearchLocal(raw string, limit int) ([]Hit, []string, error) {
	if !s.Enabled() {
		return nil, nil, nil
	}
	s.mu.RLock()
	items := append([]Container(nil), s.snapshot...)
	ready := s.snapshotSet
	s.mu.RUnlock()
	if !ready {
		items, warnings, err := s.List(context.Background())
		if err != nil {
			return nil, warnings, err
		}
		return s.rank(items, raw, limit), warnings, nil
	}
	return s.rank(items, raw, limit), nil, nil
}

func (s *Service) Find(ctx context.Context, engineName, value string) (Container, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Container{}, errors.New("container name or ID is required")
	}
	if engineName == "" {
		if before, after, ok := strings.Cut(value, ":"); ok && s.hasEngine(before) && after != "" {
			engineName = before
			value = after
		}
	}
	items, _, err := s.List(ctx)
	if err != nil {
		return Container{}, err
	}
	matches := make([]Container, 0, 1)
	for _, item := range items {
		if engineName != "" && !strings.EqualFold(item.Engine, engineName) {
			continue
		}
		if strings.EqualFold(item.Name, value) || item.ID == value ||
			(len(value) >= 3 && strings.HasPrefix(item.ID, value)) {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return Container{}, fmt.Errorf("container not found: %s", value)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, item := range matches {
			names = append(names, item.Engine+":"+item.Name)
		}
		return Container{}, fmt.Errorf("container %q is ambiguous (%s); use --engine or engine:name", value, strings.Join(names, ", "))
	}
}

func (s *Service) hasEngine(name string) bool {
	for _, engine := range s.engines {
		if strings.EqualFold(engine.Name, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func (s *Service) CommandContext(ctx context.Context, item Container, args ...string) (*exec.Cmd, error) {
	var engine *Engine
	for i := range s.engines {
		if strings.EqualFold(s.engines[i].Name, item.Engine) {
			engine = &s.engines[i]
			break
		}
	}
	if engine == nil {
		return nil, fmt.Errorf("unknown container engine %q", item.Engine)
	}
	if item.ID == "" {
		return nil, fmt.Errorf("container %q has no ID", item.Name)
	}
	full := append([]string{}, engine.Args...)
	full = append(full, args...)
	return exec.CommandContext(ctx, engine.Command, full...), nil
}

func (s *Service) CommandLine(item Container, args ...string) string {
	var engine *Engine
	for i := range s.engines {
		if strings.EqualFold(s.engines[i].Name, item.Engine) {
			engine = &s.engines[i]
			break
		}
	}
	command := item.Engine
	if engine != nil {
		command = engine.Command
	}
	parts := []string{command}
	if engine != nil {
		parts = append(parts, engine.Args...)
	}
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

func (s *Service) LogsCommand(ctx context.Context, item Container, follow bool, tail int) (*exec.Cmd, error) {
	if tail <= 0 {
		tail = 200
	}
	args := []string{"logs", "--tail", strconv.Itoa(tail)}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, item.ID)
	return s.CommandContext(ctx, item, args...)
}

func (s *Service) ShellCommand(ctx context.Context, item Container, tty bool, command []string) (*exec.Cmd, error) {
	args := []string{"exec"}
	if tty {
		args = append(args, "-it")
	} else {
		args = append(args, "-i")
	}
	args = append(args, item.ID)
	if len(command) == 0 {
		command = []string{"sh"}
	}
	args = append(args, command...)
	return s.CommandContext(ctx, item, args...)
}

func (s *Service) StatsCommand(ctx context.Context, item Container, stream bool) (*exec.Cmd, error) {
	args := []string{"stats"}
	if !stream {
		args = append(args, "--no-stream")
	}
	args = append(args, item.ID)
	return s.CommandContext(ctx, item, args...)
}

func (s *Service) Inspect(ctx context.Context, item Container) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	cmd, err := s.CommandContext(cmdCtx, item, "inspect", item.ID)
	if err != nil {
		return nil, err
	}
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message := strings.TrimSpace(string(exitErr.Stderr))
			if message != "" {
				return nil, errors.New(message)
			}
		}
		return nil, err
	}
	return output, nil
}

func (s *Service) rank(items []Container, raw string, limit int) []Hit {
	query := search.Parse(stripSelectors(raw))
	candidates := make([]search.Candidate, 0, len(items))
	byID := make(map[string]Container, len(items))
	for _, item := range items {
		key := item.Engine + ":" + item.ID
		candidate := search.NewCandidate(key)
		candidate.Add(search.FieldPod, item.Name)
		candidate.Add(search.FieldContainer, item.Name)
		candidate.Add(search.FieldImage, item.Image)
		candidate.Add(search.FieldEnv, item.Engine)
		candidate.Add(search.FieldPhase, item.State)
		candidate.Add(search.FieldOwner, item.Project)
		candidate.Add(search.FieldTag, item.Service)
		for key, value := range item.Labels {
			candidate.Add(search.FieldLabel, key+"="+value)
		}
		candidates = append(candidates, candidate)
		byID[key] = item
	}
	ranked := search.NewEngine().RankMin(query, candidates, limit, s.minScore)
	hits := make([]Hit, 0, len(ranked))
	for _, hit := range ranked {
		item, ok := byID[hit.Candidate.ID]
		if !ok {
			continue
		}
		hits = append(hits, Hit{
			Container: item,
			Score:     hit.Score,
			Percent:   hit.Percent(),
			Matches:   hit.Matches,
			Reasons:   hit.Reasons,
		})
	}
	return hits
}

func parseList(output []byte, engine Engine) []Container {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return nil
	}
	if output[0] == '[' {
		var values []map[string]any
		if err := json.Unmarshal(output, &values); err == nil {
			items := make([]Container, 0, len(values))
			for _, value := range values {
				items = append(items, containerFromMap(value, engine))
			}
			return items
		}
	}
	items := make([]Container, 0, 16)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[]" {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			continue
		}
		items = append(items, containerFromMap(value, engine))
	}
	return items
}

func containerFromMap(value map[string]any, engine Engine) Container {
	name := strings.TrimPrefix(field(value, "Names", "Name", "names"), "/")
	id := field(value, "ID", "Id", "id")
	if name == "" {
		name = id
		if len(name) > 12 {
			name = name[:12]
		}
	}
	labels := parseLabels(value)
	state := strings.ToLower(field(value, "State", "state"))
	status := field(value, "Status", "status")
	if state == "" && status != "" {
		state = strings.ToLower(strings.Fields(status)[0])
	}
	return Container{
		ID:         id,
		Name:       name,
		Image:      field(value, "Image", "image"),
		Command:    field(value, "Command", "command"),
		State:      state,
		Status:     status,
		Ports:      field(value, "Ports", "ports"),
		Created:    field(value, "CreatedAt", "Created", "created"),
		RunningFor: field(value, "RunningFor", "running_for"),
		Project:    firstLabel(labels, "com.docker.compose.project", "io.podman.compose.project", "com.docker.compose.project.working_dir"),
		Service:    firstLabel(labels, "com.docker.compose.service", "io.podman.compose.service"),
		Labels:     labels,
		Engine:     engine.Name,
		EngineType: engine.Type,
	}
}

func field(value map[string]any, names ...string) string {
	for _, name := range names {
		for key, item := range value {
			if !strings.EqualFold(key, name) {
				continue
			}
			switch typed := item.(type) {
			case string:
				return strings.TrimSpace(typed)
			case json.Number:
				return typed.String()
			case float64:
				return strconv.FormatFloat(typed, 'f', -1, 64)
			case bool:
				return strconv.FormatBool(typed)
			case []any:
				if len(typed) == 0 {
					return ""
				}
				switch first := typed[0].(type) {
				case string:
					return strings.TrimSpace(first)
				default:
					data, _ := json.Marshal(first)
					return string(data)
				}
			case nil:
				return ""
			default:
				data, _ := json.Marshal(typed)
				return string(data)
			}
		}
	}
	return ""
}

func parseLabels(value map[string]any) map[string]string {
	labels := map[string]string{}
	for key, item := range value {
		if !strings.EqualFold(key, "Labels") && !strings.EqualFold(key, "labels") {
			continue
		}
		switch typed := item.(type) {
		case string:
			for _, pair := range strings.Split(typed, ",") {
				name, label, ok := strings.Cut(strings.TrimSpace(pair), "=")
				if ok && name != "" {
					labels[name] = label
				}
			}
		case map[string]any:
			for name, label := range typed {
				labels[name] = fmt.Sprint(label)
			}
		}
	}
	return labels
}

func firstLabel(labels map[string]string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(labels[name]); value != "" {
			return value
		}
	}
	return ""
}

func stripSelectors(raw string) string {
	tokens := strings.Fields(raw)
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		switch strings.ToLower(strings.Trim(token, `"'`)) {
		case "container", "containers", "ctr", "docker", "podman":
			continue
		}
		out = append(out, token)
	}
	return strings.Join(out, " ")
}
