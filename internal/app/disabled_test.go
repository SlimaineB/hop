package app

import (
	"context"
	"testing"

	"github.com/SlimaineB/hop/internal/config"
)

func TestNewSkipsKubernetesWhenPodsDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Kube.Kubeconfigs = []string{"/definitely/not/a/kubeconfig"}
	cfg.Sources.Disabled = []string{"k8s"}

	service, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer service.Close()

	snapshot, err := service.Warm(context.Background(), WarmRequest{ForceRefresh: true})
	if err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if len(snapshot.Pods) != 0 {
		t.Fatalf("pods = %d", len(snapshot.Pods))
	}
	contexts, err := service.Contexts(context.Background())
	if err != nil {
		t.Fatalf("Contexts: %v", err)
	}
	if len(contexts) != 0 {
		t.Fatalf("contexts = %+v", contexts)
	}
}
