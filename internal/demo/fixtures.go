package demo

import (
	"fmt"
	"time"

	"github.com/SlimaineB/hop/internal/kube"
)

func demoPods(now time.Time) []kube.Pod {
	pods := []kube.Pod{
		{
			Context: "demo-prod", Namespace: "checkout", Name: "checkout-api-7d8f9c6b4-x2k1",
			Phase: "Running", Ready: true, Restarts: 0, Node: "prod-node-1",
			StartTime: now.Add(-3*time.Hour - 12*time.Minute), OwnerKind: "ReplicaSet", OwnerName: "checkout-api-7d8f9c6b4",
			Workload: "checkout-api", Labels: map[string]string{"app": "checkout-api", "team": "payments", "version": "1.18.2"},
			Containers: []kube.Container{
				{Name: "checkout-api", Image: "ghcr.io/acme/checkout-api:1.18.2", Ready: true, State: "Running", Ports: []kube.Port{{Name: "http", ContainerPort: 8080, Protocol: "TCP"}}},
				{Name: "istio-proxy", Image: "docker.io/istio/proxyv2:1.24.3", Ready: true, State: "Running"},
			},
			CPURequest: 250, CPULimit: 1000, MemoryRequest: 256 << 20, MemoryLimit: 512 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-prod", Namespace: "checkout", Name: "checkout-worker-6b9d7c8f5-q4m2",
			Phase: "Running", Ready: true, Restarts: 0, Node: "prod-node-2",
			StartTime: now.Add(-8*time.Hour - 4*time.Minute), OwnerKind: "ReplicaSet", OwnerName: "checkout-worker-6b9d7c8f5",
			Workload: "checkout-worker", Labels: map[string]string{"app": "checkout-worker", "team": "payments", "version": "1.18.2"},
			Containers: []kube.Container{{Name: "worker", Image: "ghcr.io/acme/checkout-worker:1.18.2", Ready: true, State: "Running"}},
			CPURequest: 180, CPULimit: 600, MemoryRequest: 192 << 20, MemoryLimit: 384 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-prod", Namespace: "payments", Name: "payments-api-5f6c9d8b7-n8p3",
			Phase: "Running", Ready: false, Restarts: 6, Node: "prod-node-1",
			StartTime: now.Add(-42 * time.Minute), OwnerKind: "ReplicaSet", OwnerName: "payments-api-5f6c9d8b7",
			Workload: "payments-api", Labels: map[string]string{"app": "payments-api", "team": "payments", "version": "2.4.0"},
			Containers: []kube.Container{
				{Name: "payments-api", Image: "ghcr.io/acme/payments-api:2.4.0", Ready: false, Restarts: 6, State: "Waiting", StateReason: "CrashLoopBackOff"},
			},
			CPURequest: 300, CPULimit: 1200, MemoryRequest: 384 << 20, MemoryLimit: 768 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-prod", Namespace: "payments", Name: "payments-db-0",
			Phase: "Running", Ready: true, Restarts: 0, Node: "prod-node-2",
			StartTime: now.Add(-72 * time.Hour), OwnerKind: "StatefulSet", OwnerName: "payments-db",
			Workload: "payments-db", Labels: map[string]string{"app": "payments-db", "team": "payments", "version": "16.3"},
			Containers: []kube.Container{{Name: "postgres", Image: "docker.io/postgres:16.3", Ready: true, State: "Running", Ports: []kube.Port{{ContainerPort: 5432, Protocol: "TCP"}}}},
			CPURequest: 1000, CPULimit: 2000, MemoryRequest: 2 << 30, MemoryLimit: 4 << 30, QoSClass: "Burstable",
		},
		{
			Context: "demo-prod", Namespace: "platform", Name: "auth-api-7c8d9e6f5-t7v4",
			Phase: "Running", Ready: true, Restarts: 1, Node: "prod-node-1",
			StartTime: now.Add(-14*time.Hour - 22*time.Minute), OwnerKind: "ReplicaSet", OwnerName: "auth-api-7c8d9e6f5",
			Workload: "auth-api", Labels: map[string]string{"app": "auth-api", "team": "platform", "version": "3.9.1"},
			Containers: []kube.Container{{Name: "auth-api", Image: "ghcr.io/acme/auth-api:3.9.1", Ready: true, State: "Running"}},
			CPURequest: 200, CPULimit: 800, MemoryRequest: 192 << 20, MemoryLimit: 384 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-prod", Namespace: "observability", Name: "prometheus-0",
			Phase: "Running", Ready: true, Restarts: 0, Node: "prod-node-2",
			StartTime: now.Add(-9 * 24 * time.Hour), OwnerKind: "StatefulSet", OwnerName: "prometheus",
			Workload: "prometheus", Labels: map[string]string{"app": "prometheus", "team": "platform", "version": "2.54.1"},
			Containers: []kube.Container{{Name: "prometheus", Image: "quay.io/prometheus/prometheus:v2.54.1", Ready: true, State: "Running", Ports: []kube.Port{{ContainerPort: 9090, Protocol: "TCP"}}}},
			CPURequest: 500, CPULimit: 2000, MemoryRequest: 1 << 30, MemoryLimit: 2 << 30, QoSClass: "Burstable",
		},
		{
			Context: "demo-prod", Namespace: "platform", Name: "image-sync-8b7c6d5e4-r9s5",
			Phase: "Pending", Ready: false, Restarts: 0, Node: "prod-node-1",
			StartTime: now.Add(-6 * time.Minute), OwnerKind: "ReplicaSet", OwnerName: "image-sync-8b7c6d5e4",
			Workload: "image-sync", Labels: map[string]string{"app": "image-sync", "team": "platform", "version": "0.12.4"},
			Containers: []kube.Container{{Name: "image-sync", Image: "ghcr.io/acme/image-sync:0.12.4", Ready: false, State: "Waiting", StateReason: "ContainerCreating"}},
			CPURequest: 100, CPULimit: 300, MemoryRequest: 128 << 20, MemoryLimit: 256 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-prod", Namespace: "checkout", Name: "checkout-api-6c7d8e9f0-b1c2",
			Phase: "Failed", Reason: "Evicted", Ready: false, Restarts: 2, Node: "prod-node-2",
			StartTime: now.Add(-2*time.Hour - 31*time.Minute), OwnerKind: "ReplicaSet", OwnerName: "checkout-api-6c7d8e9f0",
			Workload: "checkout-api", Labels: map[string]string{"app": "checkout-api", "team": "payments", "version": "1.18.1"},
			Containers: []kube.Container{{Name: "checkout-api", Image: "ghcr.io/acme/checkout-api:1.18.1", Ready: false, Restarts: 2, State: "Terminated", StateReason: "Evicted"}},
			CPURequest: 250, CPULimit: 1000, MemoryRequest: 256 << 20, MemoryLimit: 512 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-staging", Namespace: "checkout", Name: "checkout-api-6d7f8a9b0-k3l6",
			Phase: "Running", Ready: true, Restarts: 0, Node: "staging-node-1",
			StartTime: now.Add(-55 * time.Minute), OwnerKind: "ReplicaSet", OwnerName: "checkout-api-6d7f8a9b0",
			Workload: "checkout-api", Labels: map[string]string{"app": "checkout-api", "team": "payments", "version": "1.19.0-rc.1"},
			Containers: []kube.Container{{Name: "checkout-api", Image: "ghcr.io/acme/checkout-api:1.19.0-rc.1", Ready: true, State: "Running", Ports: []kube.Port{{ContainerPort: 8080, Protocol: "TCP"}}}},
			CPURequest: 150, CPULimit: 600, MemoryRequest: 192 << 20, MemoryLimit: 384 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-staging", Namespace: "payments", Name: "payments-api-5d6e7f8a9-m4n7",
			Phase: "Running", Ready: true, Restarts: 0, Node: "staging-node-1",
			StartTime: now.Add(-2*time.Hour - 8*time.Minute), OwnerKind: "ReplicaSet", OwnerName: "payments-api-5d6e7f8a9",
			Workload: "payments-api", Labels: map[string]string{"app": "payments-api", "team": "payments", "version": "2.5.0-rc.2"},
			Containers: []kube.Container{{Name: "payments-api", Image: "ghcr.io/acme/payments-api:2.5.0-rc.2", Ready: true, State: "Running"}},
			CPURequest: 180, CPULimit: 800, MemoryRequest: 256 << 20, MemoryLimit: 512 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-staging", Namespace: "platform", Name: "auth-api-7b8c9d0e1-p8q9",
			Phase: "Pending", Reason: "ImagePullBackOff", Ready: false, Restarts: 0, Node: "staging-node-1",
			StartTime: now.Add(-18 * time.Minute), OwnerKind: "ReplicaSet", OwnerName: "auth-api-7b8c9d0e1",
			Workload: "auth-api", Labels: map[string]string{"app": "auth-api", "team": "platform", "version": "3.10.0-rc.1"},
			Containers: []kube.Container{{Name: "auth-api", Image: "ghcr.io/acme/auth-api:3.10.0-rc.1", Ready: false, State: "Waiting", StateReason: "ImagePullBackOff"}},
			CPURequest: 150, CPULimit: 600, MemoryRequest: 192 << 20, MemoryLimit: 384 << 20, QoSClass: "Burstable",
		},
		{
			Context: "demo-staging", Namespace: "observability", Name: "grafana-0",
			Phase: "Running", Ready: true, Restarts: 0, Node: "staging-node-1",
			StartTime: now.Add(-4 * 24 * time.Hour), OwnerKind: "StatefulSet", OwnerName: "grafana",
			Workload: "grafana", Labels: map[string]string{"app": "grafana", "team": "platform", "version": "11.2.0"},
			Containers: []kube.Container{{Name: "grafana", Image: "docker.io/grafana/grafana:11.2.0", Ready: true, State: "Running", Ports: []kube.Port{{ContainerPort: 3000, Protocol: "TCP"}}}},
			CPURequest: 100, CPULimit: 500, MemoryRequest: 256 << 20, MemoryLimit: 512 << 20, QoSClass: "Burstable",
		},
	}
	for i := range pods {
		pod := &pods[i]
		pod.Cluster = pod.Context
		pod.ContainersCount = len(pod.Containers)
		for _, container := range pod.Containers {
			if container.Ready {
				pod.ReadyContainers++
			}
			pod.Images = append(pod.Images, container.Image)
		}
		if pod.PodIP == "" {
			pod.PodIP = fmt.Sprintf("10.244.%d.%d", 1+i%3, 2+i)
		}
	}
	return pods
}
