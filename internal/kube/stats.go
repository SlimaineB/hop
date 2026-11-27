package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
)

// StatsProvider is implemented by providers that can return richer pod usage
// than metrics-server alone (kubelet summary adds network and ephemeral disk).
type StatsProvider interface {
	GetPodStats(ctx context.Context, ref PodRef, node string) (*PodStats, error)
}

type NetworkStats struct {
	RxBytes  uint64 `json:"rx_bytes"`
	TxBytes  uint64 `json:"tx_bytes"`
	RxErrors uint64 `json:"rx_errors,omitempty"`
	TxErrors uint64 `json:"tx_errors,omitempty"`
}

type DiskStats struct {
	UsedBytes      uint64 `json:"used_bytes"`
	CapacityBytes  uint64 `json:"capacity_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	InodesUsed     uint64 `json:"inodes_used,omitempty"`
	InodesFree     uint64 `json:"inodes_free,omitempty"`
	Inodes         uint64 `json:"inodes,omitempty"`
}

type ContainerStats struct {
	Name   string    `json:"name"`
	CPU    int64     `json:"cpu_millicores"`
	Memory int64     `json:"memory_bytes"`
	Rootfs DiskStats `json:"rootfs,omitempty"`
	Logs   DiskStats `json:"logs,omitempty"`
}

type NodeUsage struct {
	Name             string       `json:"name"`
	Ready            bool         `json:"ready"`
	ReadyKnown       bool         `json:"ready_known"`
	CPU              int64        `json:"cpu_millicores"`
	Memory           int64        `json:"memory_bytes"`
	CPUCapacity      int64        `json:"cpu_capacity_millicores"`
	MemoryCapacity   int64        `json:"memory_capacity_bytes"`
	PodCapacity      int64        `json:"pod_capacity"`
	Disk             DiskStats    `json:"disk"`
	Network          NetworkStats `json:"network"`
	WorkingSetBytes  int64        `json:"working_set_bytes,omitempty"`
	MemoryAvailable  int64        `json:"memory_available_bytes,omitempty"`
	EphemeralStorage DiskStats    `json:"ephemeral_storage,omitempty"`
}

type PodStats struct {
	Context    string           `json:"context"`
	Namespace  string           `json:"namespace"`
	Name       string           `json:"name"`
	FetchedAt  time.Time        `json:"fetched_at"`
	Source     string           `json:"source"`
	Window     string           `json:"window,omitempty"`
	CPU        int64            `json:"cpu_millicores"`
	Memory     int64            `json:"memory_bytes"`
	WorkingSet int64            `json:"working_set_bytes,omitempty"`
	Network    NetworkStats     `json:"network"`
	Disk       DiskStats        `json:"disk"`
	Containers []ContainerStats `json:"containers,omitempty"`
	Node       *NodeUsage       `json:"node,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
}

type rawCPU struct {
	UsageNanoCores       uint64 `json:"usageNanoCores"`
	UsageCoreNanoSeconds uint64 `json:"usageCoreNanoSeconds"`
}

type rawMemory struct {
	UsageBytes      uint64 `json:"usageBytes"`
	WorkingSetBytes uint64 `json:"workingSetBytes"`
	AvailableBytes  uint64 `json:"availableBytes"`
}

type rawNetwork struct {
	Name     string `json:"name"`
	RxBytes  uint64 `json:"rxBytes"`
	RxErrors uint64 `json:"rxErrors"`
	TxBytes  uint64 `json:"txBytes"`
	TxErrors uint64 `json:"txErrors"`
}

type rawFs struct {
	UsedBytes      uint64 `json:"usedBytes"`
	CapacityBytes  uint64 `json:"capacityBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	InodesUsed     uint64 `json:"inodesUsed"`
	InodesFree     uint64 `json:"inodesFree"`
	Inodes         uint64 `json:"inodes"`
}

type rawContainerStats struct {
	Name   string    `json:"name"`
	CPU    rawCPU    `json:"cpu"`
	Memory rawMemory `json:"memory"`
	Rootfs rawFs     `json:"rootfs"`
	Logs   rawFs     `json:"logs"`
}

type rawPodStats struct {
	PodRef struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
	} `json:"podRef"`
	CPU              rawCPU              `json:"cpu"`
	Memory           rawMemory           `json:"memory"`
	Network          rawNetwork          `json:"network"`
	EphemeralStorage rawFs               `json:"ephemeral-storage"`
	Containers       []rawContainerStats `json:"containers"`
}

type rawNodeStats struct {
	NodeName string     `json:"nodeName"`
	CPU      rawCPU     `json:"cpu"`
	Memory   rawMemory  `json:"memory"`
	Network  rawNetwork `json:"network"`
	Fs       rawFs      `json:"fs"`
}

type rawSummary struct {
	Node rawNodeStats  `json:"node"`
	Pods []rawPodStats `json:"pods"`
}

func (p *ClientGoProvider) GetPodStats(ctx context.Context, ref PodRef, node string) (*PodStats, error) {
	client, err := p.client(ctx, ref.Context)
	if err != nil {
		return nil, err
	}
	stats := &PodStats{
		Context:   ref.Context,
		Namespace: ref.Namespace,
		Name:      ref.Name,
		FetchedAt: time.Now(),
	}

	var summaryErr error
	if node != "" {
		raw, err := client.CoreV1().RESTClient().Get().
			AbsPath("/api/v1/nodes", node, "proxy", "stats", "summary").
			DoRaw(ctx)
		if err == nil {
			var summary rawSummary
			if err := json.Unmarshal(raw, &summary); err != nil {
				summaryErr = fmt.Errorf("kubelet summary: %w", err)
			} else if pod, ok := findSummaryPod(summary.Pods, ref); ok {
				fillPodStats(stats, pod)
				stats.Source = "kubelet"
				stats.Node = nodeUsage(summary.Node)
				enrichNodeCapacity(ctx, client, node, stats.Node)
				return stats, nil
			} else {
				summaryErr = fmt.Errorf("kubelet summary: pod not found")
			}
		} else {
			summaryErr = fmt.Errorf("kubelet summary: %w", err)
		}
	} else {
		summaryErr = fmt.Errorf("pod has no node")
	}
	if summaryErr != nil {
		stats.Warnings = append(stats.Warnings, summaryErr.Error())
	}

	raw, err := client.CoreV1().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", ref.Namespace, "pods", ref.Name).
		DoRaw(ctx)
	if err != nil {
		if summaryErr != nil {
			return nil, fmt.Errorf("%v; metrics-server: %w", summaryErr, err)
		}
		return nil, fmt.Errorf("metrics-server: %w", err)
	}
	var item rawMetricsItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("metrics-server: %w", err)
	}
	stats.Source = "metrics-server"
	stats.CPU = quantityMilli(item.Usage.CPU)
	stats.Memory = quantityBytes(item.Usage.Memory)
	stats.Window = item.Window
	for _, container := range item.Containers {
		stats.Containers = append(stats.Containers, ContainerStats{
			Name:   container.Name,
			CPU:    quantityMilli(container.Usage.CPU),
			Memory: quantityBytes(container.Usage.Memory),
		})
	}
	if node != "" {
		if info, err := fetchNode(ctx, client, node); err == nil {
			stats.Node = &NodeUsage{
				Name:           info.Name,
				Ready:          info.Ready,
				ReadyKnown:     true,
				CPUCapacity:    quantityMilli(info.AllocatableCPU),
				MemoryCapacity: quantityBytes(info.AllocatableMemory),
				PodCapacity:    quantityValue(info.AllocatablePods),
			}
		}
	}
	stats.Warnings = append(stats.Warnings, "network/disk need kubelet summary access (nodes/proxy)")
	return stats, nil
}

func findSummaryPod(pods []rawPodStats, ref PodRef) (rawPodStats, bool) {
	for _, pod := range pods {
		if pod.PodRef.Name == ref.Name && pod.PodRef.Namespace == ref.Namespace {
			return pod, true
		}
	}
	return rawPodStats{}, false
}

func fillPodStats(stats *PodStats, pod rawPodStats) {
	stats.CPU = int64(pod.CPU.UsageNanoCores / 1_000_000)
	stats.Memory = int64(pod.Memory.UsageBytes)
	stats.WorkingSet = int64(pod.Memory.WorkingSetBytes)
	stats.Network = NetworkStats{
		RxBytes:  pod.Network.RxBytes,
		TxBytes:  pod.Network.TxBytes,
		RxErrors: pod.Network.RxErrors,
		TxErrors: pod.Network.TxErrors,
	}
	stats.Disk = diskStats(pod.EphemeralStorage)
	for _, container := range pod.Containers {
		stats.Containers = append(stats.Containers, ContainerStats{
			Name:   container.Name,
			CPU:    int64(container.CPU.UsageNanoCores / 1_000_000),
			Memory: int64(container.Memory.UsageBytes),
			Rootfs: diskStats(container.Rootfs),
			Logs:   diskStats(container.Logs),
		})
	}
}

func nodeUsage(raw rawNodeStats) *NodeUsage {
	usage := &NodeUsage{
		Name:             raw.NodeName,
		CPU:              int64(raw.CPU.UsageNanoCores / 1_000_000),
		Memory:           int64(raw.Memory.UsageBytes),
		WorkingSetBytes:  int64(raw.Memory.WorkingSetBytes),
		MemoryAvailable:  int64(raw.Memory.AvailableBytes),
		Disk:             diskStats(raw.Fs),
		EphemeralStorage: diskStats(raw.Fs),
		Network: NetworkStats{
			RxBytes:  raw.Network.RxBytes,
			TxBytes:  raw.Network.TxBytes,
			RxErrors: raw.Network.RxErrors,
			TxErrors: raw.Network.TxErrors,
		},
	}
	return usage
}

func diskStats(raw rawFs) DiskStats {
	return DiskStats{
		UsedBytes:      raw.UsedBytes,
		CapacityBytes:  raw.CapacityBytes,
		AvailableBytes: raw.AvailableBytes,
		InodesUsed:     raw.InodesUsed,
		InodesFree:     raw.InodesFree,
		Inodes:         raw.Inodes,
	}
}

func enrichNodeCapacity(ctx context.Context, client kubernetes.Interface, node string, usage *NodeUsage) {
	info, err := fetchNode(ctx, client, node)
	if err != nil {
		return
	}
	usage.Name = info.Name
	usage.Ready = info.Ready
	usage.ReadyKnown = true
	usage.CPUCapacity = quantityMilli(info.AllocatableCPU)
	usage.MemoryCapacity = quantityBytes(info.AllocatableMemory)
	usage.PodCapacity = quantityValue(info.AllocatablePods)
}

func quantityValue(raw string) int64 {
	if raw == "" {
		return 0
	}
	quantity, err := resource.ParseQuantity(raw)
	if err != nil {
		return 0
	}
	return quantity.Value()
}
