package config

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SourceAliases maps user-facing names and selectors to the canonical plugin
// source ID.
var SourceAliases = map[string]string{
	"pod":          "pods",
	"pods":         "pods",
	"kube":         "pods",
	"k8s":          "pods",
	"vm":           "servers",
	"vms":          "servers",
	"server":       "servers",
	"servers":      "servers",
	"ssh":          "servers",
	"git":          "repos",
	"repo":         "repos",
	"repos":        "repos",
	"repository":   "repos",
	"repositories": "repos",
	"pkg":          "packages",
	"pkgs":         "packages",
	"package":      "packages",
	"packages":     "packages",
	"registry":     "packages",
	"registries":   "packages",
	"image":        "packages",
	"images":       "packages",
	"container":    "containers",
	"containers":   "containers",
	"ctr":          "containers",
	"docker":       "containers",
	"podman":       "containers",
	"ticket":       "tickets",
	"tickets":      "tickets",
	"issue":        "tickets",
	"issues":       "tickets",
	"link":         "shortcuts",
	"links":        "shortcuts",
	"shortcut":     "shortcuts",
	"shortcuts":    "shortcuts",
}

func CanonicalSource(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return ""
	}
	if canonical, ok := SourceAliases[id]; ok {
		return canonical
	}
	return id
}

func (c *Config) SourceDisabled(id string) bool {
	canonical := CanonicalSource(id)
	if canonical == "" {
		return false
	}
	for _, disabled := range c.Sources.Disabled {
		if CanonicalSource(disabled) == canonical {
			return true
		}
	}
	return false
}

func (c *Config) SourceEnabled(id string) bool {
	return !c.SourceDisabled(id)
}

// SetSourceEnabled updates the disabled list in place and reports whether the
// configuration changed.
func (c *Config) SetSourceEnabled(id string, enabled bool) bool {
	canonical := CanonicalSource(id)
	if canonical == "" {
		return false
	}
	changed := false
	kept := make([]string, 0, len(c.Sources.Disabled))
	for _, disabled := range c.Sources.Disabled {
		if CanonicalSource(disabled) == canonical {
			if !enabled {
				kept = append(kept, canonical)
			} else {
				changed = true
			}
			continue
		}
		kept = append(kept, disabled)
	}
	if !enabled {
		found := false
		for _, disabled := range kept {
			if disabled == canonical {
				found = true
				break
			}
		}
		if !found {
			kept = append(kept, canonical)
			changed = true
		}
	}
	c.Sources.Disabled = kept
	return changed
}

func normalizeDisabled(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		canonical := CanonicalSource(value)
		if canonical == "" || seen[canonical] {
			continue
		}
		seen[canonical] = true
		out = append(out, canonical)
	}
	sort.Strings(out)
	return out
}

// UpdateSourceEnabled persists an enable/disable toggle without rewriting the
// whole configuration file, preserving comments and formatting.
func UpdateSourceEnabled(configPath, id string, enabled bool) (string, error) {
	canonical := CanonicalSource(id)
	if canonical == "" {
		return configPath, errText("source id is required")
	}
	if configPath == "" {
		configPath = DefaultPath()
	}
	configPath = ExpandPath(configPath)
	doc, isNew, err := loadConfigNode(configPath)
	if err != nil {
		return configPath, err
	}
	root, err := configMapping(doc)
	if err != nil {
		return configPath, err
	}
	sourcesNode := mappingValue(root, "sources")
	if sourcesNode == nil {
		sourcesNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "sources"},
			sourcesNode,
		)
	}
	if sourcesNode.Kind != yaml.MappingNode {
		return configPath, errText("config sources must be a mapping")
	}
	disabledNode := mappingValue(sourcesNode, "disabled")
	if disabledNode == nil {
		disabledNode = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		sourcesNode.Content = append(sourcesNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "disabled"},
			disabledNode,
		)
	}
	if disabledNode.Kind != yaml.SequenceNode {
		return configPath, errText("config sources.disabled must be a list")
	}

	values := make([]string, 0, len(disabledNode.Content))
	for _, item := range disabledNode.Content {
		if item.Kind != yaml.ScalarNode {
			continue
		}
		canonicalValue := CanonicalSource(item.Value)
		if canonicalValue == "" || canonicalValue == canonical {
			continue
		}
		values = append(values, canonicalValue)
	}
	if !enabled {
		values = append(values, canonical)
	}
	sort.Strings(values)
	disabledNode.Content = make([]*yaml.Node, 0, len(values))
	for _, value := range values {
		disabledNode.Content = append(disabledNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: value,
		})
	}
	if len(values) == 0 {
		removeMappingKey(sourcesNode, "disabled")
		if len(sourcesNode.Content) == 0 {
			removeMappingKey(root, "sources")
		}
	}
	if err := writeConfigNode(configPath, doc, isNew); err != nil {
		return configPath, err
	}
	return configPath, nil
}

func removeMappingKey(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

type errText string

func (e errText) Error() string { return string(e) }
