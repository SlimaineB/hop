package kube

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListPodsInNamespaceCapsResults(t *testing.T) {
	objects := make([]runtime.Object, 0, 600)
	for i := 0; i < 600; i++ {
		objects = append(objects, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%03d", i),
				Namespace: "default",
			},
		})
	}
	client := fake.NewClientset(objects...)

	pods, estimate, err := listPodsInNamespace(context.Background(), client, metav1.NamespaceAll, 550)
	if err != nil {
		t.Fatalf("listPodsInNamespace: %v", err)
	}
	if len(pods) != 550 {
		t.Fatalf("capped pods = %d, want 550", len(pods))
	}
	if estimate < 550 {
		t.Fatalf("estimate = %d, want >= 550", estimate)
	}

	all, estimate, err := listPodsInNamespace(context.Background(), client, metav1.NamespaceAll, 0)
	if err != nil {
		t.Fatalf("listPodsInNamespace unlimited: %v", err)
	}
	if len(all) != 600 || estimate != 600 {
		t.Fatalf("unlimited pods = %d estimate = %d, want 600/600", len(all), estimate)
	}
}
