package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// EnvContext selects a hop context without changing the persisted default.
	EnvContext = "HOP_CONTEXT"

	contextDirName   = "contexts"
	contextStateName = "active-context"
	contextYAMLExt   = ".yaml"
	contextYMLExt    = ".yml"
)

// Context is a named hop configuration file. A context can represent an
// environment, a team, a set of applications, or anything else the user needs.
type Context struct {
	Name string
	Path string
}

// ContextSet is the set of hop contexts discovered next to the base config.
type ContextSet struct {
	BasePath    string
	Dir         string
	StatePath   string
	DefaultName string
	Contexts    []Context
}

// LoadContexts discovers contexts from the directory next to configPath.
//
// The base config file (usually ~/.config/hop/config.yaml) is exposed as the
// "default" context when it exists. Additional contexts live in
// ~/.config/hop/contexts/<name>.yaml. The last selected context is persisted in
// ~/.config/hop/active-context.
func LoadContexts(configPath string) (*ContextSet, error) {
	configPath = ExpandPath(configPath)
	baseDir := filepath.Dir(configPath)
	set := &ContextSet{
		BasePath:  configPath,
		Dir:       filepath.Join(baseDir, contextDirName),
		StatePath: filepath.Join(baseDir, contextStateName),
	}

	seen := map[string]string{}
	if _, err := os.Stat(configPath); err == nil {
		set.Contexts = append(set.Contexts, Context{Name: "default", Path: configPath})
		seen["default"] = configPath
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read hop default config %s: %w", configPath, err)
	}

	entries, err := os.ReadDir(set.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read hop contexts %s: %w", set.Dir, err)
	}
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := filepath.Ext(entry.Name())
			if !strings.EqualFold(ext, contextYAMLExt) && !strings.EqualFold(ext, contextYMLExt) {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ext)
			if !ValidContextName(name) {
				return nil, fmt.Errorf("invalid hop context name %q: use letters, digits, dot, dash or underscore", name)
			}
			path := filepath.Join(set.Dir, entry.Name())
			if previous, ok := seen[name]; ok {
				return nil, fmt.Errorf("hop context %q is defined twice: %s and %s", name, previous, path)
			}
			seen[name] = path
			set.Contexts = append(set.Contexts, Context{Name: name, Path: path})
		}
	}

	sort.Slice(set.Contexts, func(i, j int) bool { return set.Contexts[i].Name < set.Contexts[j].Name })

	if active := readActiveContext(set.StatePath); active != "" {
		if _, ok := set.Find(active); ok {
			set.DefaultName = active
		}
	}
	if set.DefaultName == "" {
		if _, ok := set.Find("default"); ok {
			set.DefaultName = "default"
		} else if len(set.Contexts) > 0 {
			set.DefaultName = set.Contexts[0].Name
		}
	}
	return set, nil
}

// ValidContextName reports whether name can safely be used as a context name,
// CLI value, cookie value and file name.
func ValidContextName(name string) bool {
	if name == "" || name == "." || name == ".." || name[0] == '.' {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Find returns a context by name.
func (s *ContextSet) Find(name string) (Context, bool) {
	if s == nil {
		return Context{}, false
	}
	for _, ctx := range s.Contexts {
		if ctx.Name == name {
			return ctx, true
		}
	}
	return Context{}, false
}

// Default returns the persisted/default context, falling back to the first one.
func (s *ContextSet) Default() (Context, bool) {
	if s == nil {
		return Context{}, false
	}
	if ctx, ok := s.Find(s.DefaultName); ok {
		return ctx, true
	}
	if len(s.Contexts) == 0 {
		return Context{}, false
	}
	return s.Contexts[0], true
}

// Names returns the discovered context names.
func (s *ContextSet) Names() []string {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.Contexts))
	for _, ctx := range s.Contexts {
		names = append(names, ctx.Name)
	}
	return names
}

// SetDefault persists name as the default context for future launches.
func (s *ContextSet) SetDefault(name string) error {
	if _, ok := s.Find(name); !ok {
		return fmt.Errorf("unknown hop context %q", name)
	}
	if err := os.MkdirAll(filepath.Dir(s.StatePath), 0o755); err != nil {
		return fmt.Errorf("create hop context state directory: %w", err)
	}
	if err := os.WriteFile(s.StatePath, []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("write hop context state %s: %w", s.StatePath, err)
	}
	s.DefaultName = name
	return nil
}

// ContextPath returns the file path for a named context, even if it does not
// exist yet. It is used by commands that create a new context.
func (s *ContextSet) ContextPath(name string) (string, error) {
	if !ValidContextName(name) {
		return "", fmt.Errorf("invalid hop context name %q: use letters, digits, dot, dash or underscore", name)
	}
	if s == nil {
		return "", fmt.Errorf("hop contexts are not configured")
	}
	if ctx, ok := s.Find(name); ok {
		return ctx.Path, nil
	}
	return filepath.Join(s.Dir, name+contextYAMLExt), nil
}

func readActiveContext(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(data))
	if !ValidContextName(name) {
		return ""
	}
	return name
}
