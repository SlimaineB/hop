package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/SlimaineB/hop/internal/glob"
)

const metricsPodsPath = "/apis/metrics.k8s.io/v1beta1/pods"
const metricsNodesPath = "/apis/metrics.k8s.io/v1beta1/nodes"

type ContainerMetrics struct {
	Name   string `json:"name"`
	CPU    int64  `json:"cpu_millicores"`
	Memory int64  `json:"memory_bytes"`
}

type PodMetrics struct {
	Context    string             `json:"context"`
	Namespace  string             `json:"namespace"`
	Name       string             `json:"name"`
	CPU        int64              `json:"cpu_millicores"`
	Memory     int64              `json:"memory_bytes"`
	Window     string             `json:"window,omitempty"`
	Timestamp  time.Time          `json:"timestamp,omitempty"`
	Containers []ContainerMetrics `json:"containers,omitempty"`
}

type NodeMetrics struct {
	Context        string `json:"context"`
	Name           string `json:"name"`
	CPU            int64  `json:"cpu_millicores"`
	Memory         int64  `json:"memory_bytes"`
	CPUCapacity    int64  `json:"cpu_capacity_millicores"`
	MemoryCapacity int64  `json:"memory_capacity_bytes"`
	PodCapacity    int64  `json:"pod_capacity"`
	Ready          bool   `json:"ready"`
}

type MetricsResult struct {
	Pods      []PodMetrics   `json:"pods"`
	Nodes     []NodeMetrics  `json:"nodes"`
	Errors    []ContextError `json:"errors,omitempty"`
	FetchedAt time.Time      `json:"fetched_at"`
	Duration  time.Duration  `json:"duration"`
}

type rawMetricsList struct {
	Items []rawMetricsItem `json:"items"`
}

type rawMetricsItem struct {
	Metadata   rawMetricsMeta `json:"metadata"`
	Timestamp  string         `json:"timestamp"`
	Window     string         `json:"window"`
	Usage      rawUsage       `json:"usage"`
	Containers []rawContainer `json:"containers"`
}

type rawContainer struct {
	Name  string   `json:"name"`
	Usage rawUsage `json:"usage"`
}

type rawMetricsMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type rawUsage struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

func (p *ClientGoProvider) ListMetrics(ctx context.Context, opts ListOptions) (*MetricsResult, error) {
	contexts, err := p.Contexts(ctx)
	if err != nil && len(contexts) == 0 {
		return nil, err
	}

	selected := contexts
	if len(opts.Contexts) > 0 {
		selected = make([]Context, 0, len(contexts))
		for _, c := range contexts {
			if glob.MatchAny(opts.Contexts, c.Name) {
				selected = append(selected, c)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("no context matches %v", opts.Contexts)
		}
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 6
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = p.options.RequestTimeout
	}

	result := &MetricsResult{FetchedAt: time.Now()}
	start := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)

	for _, clusterCtx := range selected {
		wg.Add(1)
		go func(c Context) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				result.Errors = append(result.Errors, ContextError{Context: c.Name, Error: ctx.Err().Error()})
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			requestCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			pods, nodes, err := p.contextMetrics(requestCtx, c, opts)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, ContextError{Context: c.Name, Error: err.Error()})
				return
			}
			result.Pods = append(result.Pods, pods...)
			result.Nodes = append(result.Nodes, nodes...)
		}(clusterCtx)
	}
	wg.Wait()

	result.Duration = time.Since(start)
	sort.Slice(result.Pods, func(i, j int) bool {
		a, b := result.Pods[i], result.Pods[j]
		if a.Context != b.Context {
			return a.Context < b.Context
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	sort.Slice(result.Nodes, func(i, j int) bool {
		if result.Nodes[i].Context != result.Nodes[j].Context {
			return result.Nodes[i].Context < result.Nodes[j].Context
		}
		return result.Nodes[i].Name < result.Nodes[j].Name
	})
	return result, nil
}

func (p *ClientGoProvider) contextMetrics(ctx context.Context, c Context, opts ListOptions) ([]PodMetrics, []NodeMetrics, error) {
	client, err := p.client(c.Name)
	if err != nil {
		return nil, nil, err
	}
	rest := client.CoreV1().RESTClient()

	podsRaw, err := rest.Get().AbsPath(metricsPodsPath).Do(ctx).Raw()
	if err != nil {
		return nil, nil, fmt.Errorf("pod metrics: %w", err)
	}
	var podList rawMetricsList
	if err := json.Unmarshal(podsRaw, &podList); err != nil {
		return nil, nil, fmt.Errorf("pod metrics: %w", err)
	}

	nodesRaw, err := rest.Get().AbsPath(metricsNodesPath).Do(ctx).Raw()
	if err != nil {
		return nil, nil, fmt.Errorf("node metrics: %w", err)
	}
	var nodeList rawMetricsList
	if err := json.Unmarshal(nodesRaw, &nodeList); err != nil {
		return nil, nil, fmt.Errorf("node metrics: %w", err)
	}

	capacity := p.nodeCapacity(ctx, client)

	pods := make([]PodMetrics, 0, len(podList.Items))
	for _, item := range podList.Items {
		if glob.MatchAny(opts.ExcludeNamespaces, item.Metadata.Namespace) {
			continue
		}
		if !allowsNamespace(opts.Namespaces, item.Metadata.Namespace) {
			continue
		}
		pod := PodMetrics{
			Context:   c.Name,
			Namespace: item.Metadata.Namespace,
			Name:      item.Metadata.Name,
			Window:    item.Window,
		}
		pod.Timestamp, _ = time.Parse(time.RFC3339, item.Timestamp)
		for _, container := range item.Containers {
			metrics := ContainerMetrics{
				Name:   container.Name,
				CPU:    quantityMilli(container.Usage.CPU),
				Memory: quantityBytes(container.Usage.Memory),
			}
			pod.CPU += metrics.CPU
			pod.Memory += metrics.Memory
			pod.Containers = append(pod.Containers, metrics)
		}
		pods = append(pods, pod)
	}

	nodes := make([]NodeMetrics, 0, len(nodeList.Items))
	for _, item := range nodeList.Items {
		node := NodeMetrics{
			Context: c.Name,
			Name:    item.Metadata.Name,
			CPU:     quantityMilli(item.Usage.CPU),
			Memory:  quantityBytes(item.Usage.Memory),
		}
		if info, ok := capacity[item.Metadata.Name]; ok {
			node.CPUCapacity = info.cpu
			node.MemoryCapacity = info.memory
			node.PodCapacity = info.pods
			node.Ready = info.ready
		}
		nodes = append(nodes, node)
	}
	return pods, nodes, nil
}

type nodeCapacity struct {
	cpu    int64
	memory int64
	pods   int64
	ready  bool
}

func (p *ClientGoProvider) nodeCapacity(ctx context.Context, client kubernetes.Interface) map[string]nodeCapacity {
	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	out := make(map[string]nodeCapacity, len(list.Items))
	for _, node := range list.Items {
		cap := nodeCapacity{}
		if quantity, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
			cap.cpu = quantity.MilliValue()
		}
		if quantity, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
			cap.memory = quantity.Value()
		}
		if quantity, ok := node.Status.Allocatable[corev1.ResourcePods]; ok {
			cap.pods = quantity.Value()
		}
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady {
				cap.ready = condition.Status == corev1.ConditionTrue
				break
			}
		}
		out[node.Name] = cap
	}
	return out
}

func allowsNamespace(patterns []string, namespace string) bool {
	if len(patterns) == 0 {
		return true
	}
	return glob.MatchAny(patterns, namespace)
}

func quantityMilli(raw string) int64 {
	if raw == "" {
		return 0
	}
	quantity, err := resource.ParseQuantity(raw)
	if err != nil {
		return 0
	}
	return quantity.MilliValue()
}

func quantityBytes(raw string) int64 {
	if raw == "" {
		return 0
	}
	quantity, err := resource.ParseQuantity(raw)
	if err != nil {
		return 0
	}
	return quantity.Value()
}
