package cli

import (
	"reflect"
	"testing"
)

func TestNormalizeArgsKeepsRelativeDurationsAsTerms(t *testing.T) {
	got := normalizeArgs([]string{"hop", "poddy", "-2h", "ns:apps", "-30m", "--limit", "5"})
	want := []string{"hop", "poddy", "last:2h", "ns:apps", "last:30m", "--limit", "5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeArgs() = %v, want %v", got, want)
	}
}

func TestNormalizeArgsLeavesFlagsAndExplicitSeparator(t *testing.T) {
	got := normalizeArgs([]string{"hop", "-1d", "--", "-2h"})
	want := []string{"hop", "last:1d", "--", "-2h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeArgs() = %v, want %v", got, want)
	}
	if got := normalizeArgs([]string{"hop", "-h", "--timeout", "-3s"}); !reflect.DeepEqual(got, []string{"hop", "-h", "--timeout", "-3s"}) {
		t.Fatalf("normalizeArgs() changed flags: %v", got)
	}
}

func TestCategoryCommands(t *testing.T) {
	root := NewRootCommand()
	found := map[string]bool{}
	for _, command := range root.Commands() {
		found[command.Name()] = true
	}
	for _, name := range []string{"demo", "features", "all", "git", "pkg", "pod", "vm", "link", "issue"} {
		if !found[name] {
			t.Fatalf("missing command %q", name)
		}
	}
}
