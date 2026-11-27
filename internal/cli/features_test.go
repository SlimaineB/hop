package cli

import "testing"

func TestCanonicalFeatureAliases(t *testing.T) {
	cases := map[string]string{
		"jira":       "tickets",
		"kubernetes": "pods",
		"k8s":        "pods",
		"ctr":        "containers",
		"vm":         "servers",
	}
	for input, want := range cases {
		if got := canonicalFeature(input); got != want {
			t.Fatalf("canonicalFeature(%q) = %q, want %q", input, got, want)
		}
	}
}
