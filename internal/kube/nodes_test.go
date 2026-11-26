package kube

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConvertNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node-1",
			UID:               "uid-1",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)),
			Labels:            map[string]string{"topology.kubernetes.io/zone": "eu-west-1a"},
		},
		Spec: corev1.NodeSpec{
			ProviderID: "aws:///eu-west-1a/i-123",
			PodCIDR:    "10.0.0.0/24",
			PodCIDRs:   []string{"10.0.0.0/24", "fd00::/64"},
			Taints: []corev1.Taint{
				{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
			},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("7500m"),
				corev1.ResourceMemory: resource.MustParse("30Gi"),
				corev1.ResourcePods:   resource.MustParse("100"),
			},
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.1.2"},
				{Type: corev1.NodeHostName, Address: "node-1.internal"},
			},
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             corev1.ConditionTrue,
					Reason:             "KubeletReady",
					LastHeartbeatTime:  metav1.NewTime(time.Date(2026, 11, 25, 16, 0, 0, 0, time.UTC)),
					LastTransitionTime: metav1.NewTime(time.Date(2026, 11, 20, 10, 1, 0, 0, time.UTC)),
				},
			},
			NodeInfo: corev1.NodeSystemInfo{
				OperatingSystem:         "linux",
				Architecture:            "amd64",
				OSImage:                 "Ubuntu 24.04",
				KernelVersion:           "6.8.0",
				ContainerRuntimeVersion: "containerd://1.7.0",
				KubeletVersion:          "v1.30.1",
			},
		},
	}

	info := convertNode(node)
	if info.Name != "node-1" || !info.Ready {
		t.Fatalf("unexpected node info: %+v", info)
	}
	if info.InternalIP != "10.0.1.2" || info.Hostname != "node-1.internal" {
		t.Fatalf("addresses: %+v", info)
	}
	if info.AllocatableCPU != "7500m" || info.CapacityMemory != "32Gi" {
		t.Fatalf("resources: %+v", info)
	}
	if info.PodCIDR != "10.0.0.0/24" || len(info.PodCIDRs) != 2 {
		t.Fatalf("cidrs: %+v", info)
	}
	if len(info.Conditions) != 1 || info.Conditions[0].Type != "Ready" {
		t.Fatalf("conditions: %+v", info.Conditions)
	}
	if len(info.Taints) != 1 || info.Taints[0].Key != "dedicated" {
		t.Fatalf("taints: %+v", info.Taints)
	}
	if info.Labels["topology.kubernetes.io/zone"] != "eu-west-1a" {
		t.Fatalf("labels: %+v", info.Labels)
	}
}
