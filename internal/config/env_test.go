package config

import "testing"

func TestEnvironmentDefaults(t *testing.T) {
	cfg := Default()
	if !cfg.Kube.EnvRequired() {
		t.Fatal("environment selection should be required by default")
	}
	want := []string{"dev", "uat", "prod"}
	got := cfg.Kube.EnvironmentList()
	if len(got) != len(want) {
		t.Fatalf("environments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("environments = %v, want %v", got, want)
		}
	}
}

func TestInferEnvUAT(t *testing.T) {
	if got := InferEnv("uat-cluster"); got != "uat" {
		t.Fatalf("InferEnv = %q, want uat", got)
	}
}

func TestEnvironmentNormalization(t *testing.T) {
	cfg := Default()
	cfg.Kube.Environments = []string{" DEV ", "dev", "UAT", "", "prod"}
	cfg.Normalize()
	got := cfg.Kube.EnvironmentList()
	want := []string{"DEV", "UAT", "prod"}
	if len(got) != len(want) {
		t.Fatalf("environments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("environments = %v, want %v", got, want)
		}
	}
	if !cfg.Kube.HasEnvironment("uat") {
		t.Fatal("HasEnvironment should be case-insensitive")
	}
}
