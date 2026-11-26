package config

import (
	"net/url"
	"sort"
	"strings"
)

type ShortcutHit struct {
	Shortcut ShortcutConfig `json:"shortcut"`
	Score    int            `json:"score"`
}

// MatchShortcuts ranks shortcuts against free-form terms. Every term must
// match the shortcut name, keywords, description, or URL.
func MatchShortcuts(shortcuts []ShortcutConfig, terms []string) []ShortcutHit {
	if len(shortcuts) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term != "" {
			normalized = append(normalized, term)
		}
	}
	hits := make([]ShortcutHit, 0, len(shortcuts))
	for _, shortcut := range shortcuts {
		score, ok := scoreShortcut(shortcut, normalized)
		if !ok {
			continue
		}
		hits = append(hits, ShortcutHit{Shortcut: shortcut, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return strings.ToLower(hits[i].Shortcut.Name) < strings.ToLower(hits[j].Shortcut.Name)
		}
		return hits[i].Score > hits[j].Score
	})
	return hits
}

func scoreShortcut(shortcut ShortcutConfig, terms []string) (int, bool) {
	name := strings.ToLower(shortcut.Name)
	description := strings.ToLower(shortcut.Description)
	rawURL := strings.ToLower(shortcut.URL)
	keywords := make([]string, 0, len(shortcut.Keywords))
	for _, keyword := range shortcut.Keywords {
		keywords = append(keywords, strings.ToLower(keyword))
	}
	score := 0
	for _, term := range terms {
		best := 0
		switch {
		case name == term:
			best = 100
		case keywordEquals(keywords, term):
			best = 90
		case strings.HasPrefix(name, term):
			best = 80
		case keywordPrefix(keywords, term):
			best = 70
		case strings.Contains(name, term):
			best = 60
		case keywordContains(keywords, term):
			best = 50
		case strings.Contains(description, term):
			best = 30
		case strings.Contains(rawURL, term):
			best = 20
		}
		if best == 0 {
			return 0, false
		}
		score += best
	}
	return score, true
}

func keywordEquals(keywords []string, value string) bool {
	for _, keyword := range keywords {
		if keyword == value {
			return true
		}
	}
	return false
}

func keywordPrefix(keywords []string, value string) bool {
	for _, keyword := range keywords {
		if strings.HasPrefix(keyword, value) {
			return true
		}
	}
	return false
}

func keywordContains(keywords []string, value string) bool {
	for _, keyword := range keywords {
		if strings.Contains(keyword, value) {
			return true
		}
	}
	return false
}

func ValidShortcutURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
