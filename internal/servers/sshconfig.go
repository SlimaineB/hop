package servers

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/SlimaineB/teu/internal/glob"
)

type sshPattern struct {
	pattern string
	negate  bool
}

type sshBlock struct {
	patterns []sshPattern
	options  map[string]string
}

type sshConfig struct {
	aliases []string
	blocks  []sshBlock
	seen    map[string]bool
}

func loadSSHConfig(path string) *sshConfig {
	cfg := &sshConfig{seen: map[string]bool{}}
	if path == "" {
		return cfg
	}
	cfg.parse(path)
	return cfg
}

func (c *sshConfig) parse(path string) {
	path = expandSSHPath(path)
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if c.seen[abs] {
		return
	}
	c.seen[abs] = true

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	global := sshBlock{patterns: []sshPattern{{pattern: "*"}}, options: map[string]string{}}
	c.blocks = append(c.blocks, global)
	globalIndex := len(c.blocks) - 1

	var current *sshBlock
	flush := func() {
		if current != nil {
			c.blocks = append(c.blocks, *current)
			current = nil
		}
	}

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value := splitSSHLine(line)
		switch key {
		case "host":
			flush()
			block := sshBlock{options: map[string]string{}}
			for _, field := range strings.Fields(value) {
				negate := strings.HasPrefix(field, "!")
				field = strings.TrimPrefix(field, "!")
				if field == "" {
					continue
				}
				lower := strings.ToLower(field)
				block.patterns = append(block.patterns, sshPattern{pattern: lower, negate: negate})
				if !negate && !hasGlob(field) {
					c.addAlias(field)
				}
			}
			current = &block
		case "match":
			flush()
		case "include":
			flush()
			for _, include := range strings.Fields(value) {
				include = expandSSHPath(include)
				if !filepath.IsAbs(include) {
					include = filepath.Join(filepath.Dir(path), include)
				}
				matches, _ := filepath.Glob(include)
				if len(matches) == 0 {
					matches = []string{include}
				}
				for _, match := range matches {
					c.parse(match)
				}
			}
		default:
			if value == "" {
				continue
			}
			if current == nil {
				if _, exists := c.blocks[globalIndex].options[key]; !exists {
					c.blocks[globalIndex].options[key] = value
				}
				continue
			}
			if _, exists := current.options[key]; !exists {
				current.options[key] = value
			}
		}
	}
	flush()
}

func (c *sshConfig) addAlias(alias string) {
	for _, existing := range c.aliases {
		if strings.EqualFold(existing, alias) {
			return
		}
	}
	c.aliases = append(c.aliases, alias)
}

func (c *sshConfig) resolve(alias string) map[string]string {
	out := map[string]string{}
	value := strings.ToLower(alias)
	for _, block := range c.blocks {
		if !block.matches(value) {
			continue
		}
		for key, option := range block.options {
			if _, exists := out[key]; !exists {
				out[key] = option
			}
		}
	}
	return out
}

func (b sshBlock) matches(value string) bool {
	positive := false
	for _, pattern := range b.patterns {
		if pattern.negate {
			if glob.Match(pattern.pattern, value) {
				return false
			}
			continue
		}
		if glob.Match(pattern.pattern, value) || pattern.pattern == "*" {
			positive = true
		}
	}
	return positive
}

func splitSSHLine(line string) (string, string) {
	index := strings.IndexAny(line, " \t")
	if index < 0 {
		return strings.ToLower(line), ""
	}
	return strings.ToLower(strings.TrimSpace(line[:index])), strings.TrimSpace(line[index+1:])
}

func hasGlob(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

func expandSSHPath(value string) string {
	value = os.ExpandEnv(value)
	if value == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, value[2:])
		}
	}
	return value
}
