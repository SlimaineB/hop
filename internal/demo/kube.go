package demo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/glob"
	"github.com/SlimaineB/hop/internal/kube"
)

var contexts = []kube.Context{
	{Name: "demo-prod", Cluster: "demo-prod", User: "hop"},
	{Name: "demo-staging", Cluster: "demo-staging", User: "hop"},
}

type Provider struct {
	pods  []kube.Pod
	byRef map[string]kube.Pod
}

func NewProvider() *Provider {
	pods := demoPods(time.Now())
	byRef := make(map[string]kube.Pod, len(pods))
	for _, pod := range pods {
		byRef[pod.Ref().String()] = pod
	}
	return &Provider{pods: pods, byRef: byRef}
}

func (p *Provider) Contexts(context.Context) ([]kube.Context, error) {
	return append([]kube.Context(nil), contexts...), nil
}

func (p *Provider) ListPods(_ context.Context, opts kube.ListOptions) (*kube.ListResult, error) {
	start := time.Now()
	pods := make([]kube.Pod, 0, len(p.pods))
	for _, pod := range p.pods {
		if !matches(pod.Context, pod.Namespace, opts) {
			continue
		}
		pods = append(pods, pod)
	}
	sort.SliceStable(pods, func(i, j int) bool {
		return pods[i].StartTime.After(pods[j].StartTime)
	})
	return &kube.ListResult{
		Pods:      pods,
		Contexts:  append([]kube.Context(nil), contexts...),
		FetchedAt: time.Now(),
		Duration:  time.Since(start),
	}, nil
}

func (p *Provider) ListMetrics(_ context.Context, opts kube.ListOptions) (*kube.MetricsResult, error) {
	start := time.Now()
	pods := make([]kube.PodMetrics, 0, len(p.pods))
	for _, pod := range p.pods {
		if !matches(pod.Context, pod.Namespace, opts) {
			continue
		}
		pods = append(pods, podMetrics(pod))
	}
	return &kube.MetricsResult{
		Pods:      pods,
		Nodes:     nodeMetrics(),
		FetchedAt: time.Now(),
		Duration:  time.Since(start),
	}, nil
}

func (p *Provider) GetPod(_ context.Context, ref kube.PodRef) (*kube.PodDetail, error) {
	pod, ok := p.byRef[ref.String()]
	if !ok {
		return nil, fmt.Errorf("pod %s not found", ref.String())
	}
	node := nodeFor(pod)
	ready := "False"
	if pod.Ready {
		ready = "True"
	}
	state := "Ready"
	if !pod.Ready {
		state = "NotReady"
	}
	events := []kube.Event{
		{Type: "Normal", Reason: "Scheduled", Message: "Successfully assigned " + pod.Namespace + "/" + pod.Name + " to " + pod.Node, Source: "default-scheduler", Count: 1, LastSeen: pod.StartTime.Add(2 * time.Second)},
		{Type: "Normal", Reason: "Pulled", Message: "Container image already present on machine", Source: "kubelet", Count: 1, LastSeen: pod.StartTime.Add(8 * time.Second)},
		{Type: "Normal", Reason: "Started", Message: "Started container", Source: "kubelet", Count: 1, LastSeen: pod.StartTime.Add(10 * time.Second)},
	}
	if !pod.Ready {
		events = append(events, kube.Event{
			Type: "Warning", Reason: "Unhealthy", Message: "Readiness probe failed: HTTP probe failed with statuscode: 503",
			Source: "kubelet", Count: 7, LastSeen: time.Now().Add(-2 * time.Minute),
		})
	}
	owner := make([]kube.OwnerRef, 0, 2)
	if pod.OwnerKind != "" && pod.OwnerName != "" {
		owner = append(owner, kube.OwnerRef{Kind: pod.OwnerKind, Name: pod.OwnerName})
		if pod.OwnerKind == "ReplicaSet" || pod.OwnerKind == "StatefulSet" {
			owner = append(owner, kube.OwnerRef{Kind: "Deployment", Name: pod.OwnerName})
		}
	}
	return &kube.PodDetail{
		Pod:      pod,
		Node:     node,
		YAML:     podYAML(pod),
		Describe: podDescribe(pod),
		Conditions: []kube.Condition{
			{Type: "PodScheduled", Status: "True", LastTransitionTime: pod.StartTime},
			{Type: "Initialized", Status: "True", LastTransitionTime: pod.StartTime},
			{Type: "ContainersReady", Status: ready, Reason: state, LastTransitionTime: pod.StartTime},
			{Type: "Ready", Status: ready, Reason: state, LastTransitionTime: pod.StartTime},
		},
		Events:         events,
		Labels:         pod.Labels,
		Annotations:    map[string]string{"demo.hop.dev/workload": pod.Workload, "demo.hop.dev/owner": "platform"},
		OwnerChain:     owner,
		Volumes:        []kube.Volume{{Name: "config", Type: "ConfigMap"}, {Name: "tmp", Type: "EmptyDir"}},
		ServiceAccount: "default",
		QoSClass:       pod.QoSClass,
		HostIP:         strings.TrimSuffix(pod.Node, "-1") + ".10",
		PodIP:          pod.PodIP,
		PodIPs:         []string{pod.PodIP},
		FetchedAt:      time.Now(),
	}, nil
}

func (p *Provider) GetPodStats(_ context.Context, ref kube.PodRef, node string) (*kube.PodStats, error) {
	pod, ok := p.byRef[ref.String()]
	if !ok {
		return nil, fmt.Errorf("pod %s not found", ref.String())
	}
	cpu, memory := usageFor(pod)
	containers := make([]kube.ContainerStats, 0, len(pod.Containers))
	for i, container := range pod.Containers {
		share := int64(i + 1)
		containers = append(containers, kube.ContainerStats{
			Name:   container.Name,
			CPU:    cpu / share,
			Memory: memory / share,
			Rootfs: kube.DiskStats{UsedBytes: uint64(120+i) << 20, CapacityBytes: 10 << 30, AvailableBytes: 9 << 30},
			Logs:   kube.DiskStats{UsedBytes: uint64(8+i) << 20, CapacityBytes: 1 << 30, AvailableBytes: 1000 << 20},
		})
	}
	if node == "" {
		node = pod.Node
	}
	return &kube.PodStats{
		Context:    ref.Context,
		Namespace:  ref.Namespace,
		Name:       ref.Name,
		FetchedAt:  time.Now(),
		Source:     "demo",
		Window:     "30s",
		CPU:        cpu,
		Memory:     memory,
		WorkingSet: memory - 12<<20,
		Network:    kube.NetworkStats{RxBytes: uint64(cpu) * 4200, TxBytes: uint64(cpu) * 2600},
		Disk:       kube.DiskStats{UsedBytes: 220 << 20, CapacityBytes: 10 << 30, AvailableBytes: 9 << 30},
		Containers: containers,
		Node: &kube.NodeUsage{
			Name:            node,
			Ready:           true,
			ReadyKnown:      true,
			CPU:             cpu * 7,
			Memory:          memory * 5,
			CPUCapacity:     8000,
			MemoryCapacity:  32 << 30,
			PodCapacity:     110,
			Disk:            kube.DiskStats{UsedBytes: 44 << 30, CapacityBytes: 120 << 30, AvailableBytes: 76 << 30},
			Network:         kube.NetworkStats{RxBytes: 82 << 30, TxBytes: 39 << 30},
			WorkingSetBytes: memory * 4,
			MemoryAvailable: 16 << 30,
		},
	}, nil
}

func (p *Provider) Close() error {
	return nil
}

func matches(ctx, namespace string, opts kube.ListOptions) bool {
	if len(opts.Contexts) > 0 && !glob.MatchAny(opts.Contexts, ctx) {
		return false
	}
	if len(opts.Namespaces) > 0 && !glob.MatchAny(opts.Namespaces, namespace) {
		return false
	}
	for _, excluded := range opts.ExcludeNamespaces {
		if glob.Match(excluded, namespace) {
			return false
		}
	}
	return true
}

func nodeFor(pod kube.Pod) *kube.NodeInfo {
	capacity := map[string]string{"cpu": "8", "memory": "32Gi", "pods": "110"}
	return &kube.NodeInfo{
		Name:              pod.Node,
		Ready:             true,
		InternalIP:        strings.TrimSuffix(pod.Node, "-1") + ".10",
		Hostname:          pod.Node,
		PodCIDR:           "10.244.0.0/24",
		OperatingSystem:   "linux",
		Architecture:      "amd64",
		OSImage:           "Ubuntu 24.04.2 LTS",
		KernelVersion:     "6.8.0-60-generic",
		ContainerRuntime:  "containerd://2.0.4",
		KubeletVersion:    "v1.33.2",
		Capacity:          capacity,
		Allocatable:       capacity,
		CapacityCPU:       "8",
		CapacityMemory:    "32Gi",
		CapacityPods:      "110",
		CreationTimestamp: time.Now().Add(-96 * time.Hour),
	}
}

func podMetrics(pod kube.Pod) kube.PodMetrics {
	cpu, memory := usageFor(pod)
	containers := make([]kube.ContainerMetrics, 0, len(pod.Containers))
	for i, container := range pod.Containers {
		share := int64(i + 1)
		containers = append(containers, kube.ContainerMetrics{Name: container.Name, CPU: cpu / share, Memory: memory / share})
	}
	return kube.PodMetrics{
		Context:    pod.Context,
		Namespace:  pod.Namespace,
		Name:       pod.Name,
		CPU:        cpu,
		Memory:     memory,
		Window:     "30s",
		Timestamp:  time.Now(),
		Containers: containers,
	}
}

func usageFor(pod kube.Pod) (int64, int64) {
	cpu := pod.CPURequest / 3
	if cpu <= 0 {
		cpu = 18
	}
	memory := pod.MemoryRequest / 2
	if memory <= 0 {
		memory = 96 << 20
	}
	return cpu, memory
}

func nodeMetrics() []kube.NodeMetrics {
	return []kube.NodeMetrics{
		{Context: "demo-prod", Name: "prod-node-1", CPU: 2140, Memory: 11 << 30, CPUCapacity: 8000, MemoryCapacity: 32 << 30, PodCapacity: 110, Ready: true},
		{Context: "demo-prod", Name: "prod-node-2", CPU: 1730, Memory: 9 << 30, CPUCapacity: 8000, MemoryCapacity: 32 << 30, PodCapacity: 110, Ready: true},
		{Context: "demo-staging", Name: "staging-node-1", CPU: 860, Memory: 4 << 30, CPUCapacity: 4000, MemoryCapacity: 16 << 30, PodCapacity: 80, Ready: true},
	}
}

func podYAML(pod kube.Pod) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  nodeName: %s
  serviceAccountName: default
status:
  phase: %s
  podIP: %s
`, pod.Name, pod.Namespace, pod.Labels["app"], pod.Node, pod.Phase, pod.PodIP)
}

func podDescribe(pod kube.Pod) string {
	return fmt.Sprintf("Name: %s\nNamespace: %s\nNode: %s\nPhase: %s\nReady: %t\nRestarts: %d\n",
		pod.Name, pod.Namespace, pod.Node, pod.Phase, pod.Ready, pod.Restarts)
}
