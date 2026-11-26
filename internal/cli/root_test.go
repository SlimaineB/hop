package cli

import (
	"reflect"
	"testing"
)

func TestNormalizeArgsKeepsRelativeDurationsAsTerms(t *testing.T) {
	got := normalizeArgs([]string{"teu", "poddy", "-2h", "ns:apps", "-30m", "--limit", "5"})
	want := []string{"teu", "poddy", "last:2h", "ns:apps", "last:30m", "--limit", "5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeArgs() = %v, want %v", got, want)
	}
}

func TestNormalizeArgsLeavesFlagsAndExplicitSeparator(t *testing.T) {
	got := normalizeArgs([]string{"teu", "-1d", "--", "-2h"})
	want := []string{"teu", "last:1d", "--", "-2h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeArgs() = %v, want %v", got, want)
	}
	if got := normalizeArgs([]string{"teu", "-h", "--timeout", "-3s"}); !reflect.DeepEqual(got, []string{"teu", "-h", "--timeout", "-3s"}) {
		t.Fatalf("normalizeArgs() changed flags: %v", got)
	}
}

func TestCategoryCommands(t *testing.T) {
	root := NewRootCommand()
	found := map[string]bool{}
	for _, command := range root.Commands() {
		found[command.Name()] = true
	}
	for _, name := range []string{"pod", "vm", "link", "issue"} {
		if !found[name] {
			t.Fatalf("missing command %q", name)
		}
	}
}
