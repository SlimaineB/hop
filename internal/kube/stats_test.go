package kube

import "testing"

func TestFillPodStatsFromSummary(t *testing.T) {
	var pod rawPodStats
	pod.PodRef.Name = "api-1"
	pod.PodRef.Namespace = "apps"
	pod.CPU.UsageNanoCores = 250_000_000
	pod.Memory.UsageBytes = 512 * 1024 * 1024
	pod.Network.RxBytes = 1000
	pod.Network.TxBytes = 2000
	pod.EphemeralStorage.UsedBytes = 10
	pod.EphemeralStorage.CapacityBytes = 100
	pod.Containers = []rawContainerStats{{
		Name:   "api",
		CPU:    rawCPU{UsageNanoCores: 200_000_000},
		Memory: rawMemory{UsageBytes: 400 * 1024 * 1024},
	}}

	stats := &PodStats{}
	fillPodStats(stats, pod)
	if stats.CPU != 250 || stats.Memory != 512*1024*1024 {
		t.Fatalf("cpu/memory = %d/%d", stats.CPU, stats.Memory)
	}
	if stats.Network.RxBytes != 1000 || stats.Network.TxBytes != 2000 {
		t.Fatalf("network = %+v", stats.Network)
	}
	if stats.Disk.UsedBytes != 10 || stats.Disk.CapacityBytes != 100 {
		t.Fatalf("disk = %+v", stats.Disk)
	}
	if len(stats.Containers) != 1 || stats.Containers[0].CPU != 200 {
		t.Fatalf("containers = %+v", stats.Containers)
	}
}

func TestFindSummaryPod(t *testing.T) {
	pod := rawPodStats{}
	pod.PodRef.Name = "api"
	pod.PodRef.Namespace = "apps"
	found, ok := findSummaryPod([]rawPodStats{pod}, PodRef{Namespace: "apps", Name: "api"})
	if !ok || found.PodRef.Name != "api" {
		t.Fatalf("findSummaryPod = %+v/%v", found, ok)
	}
	if _, ok := findSummaryPod([]rawPodStats{pod}, PodRef{Namespace: "apps", Name: "other"}); ok {
		t.Fatal("unexpected pod match")
	}
}
