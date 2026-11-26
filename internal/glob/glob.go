package glob

import "path"

func Match(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	if pattern == value {
		return true
	}
	ok, err := path.Match(pattern, value)
	return err == nil && ok
}

func MatchAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if Match(pattern, value) {
			return true
		}
	}
	return false
}
