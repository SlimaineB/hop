package kube

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func fetchNode(ctx context.Context, client kubernetes.Interface, name string) (*NodeInfo, error) {
	node, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return convertNode(node), nil
}

func convertNode(node *corev1.Node) *NodeInfo {
	info := &NodeInfo{
		Name:              node.Name,
		UID:               string(node.UID),
		ProviderID:        node.Spec.ProviderID,
		PodCIDR:           node.Spec.PodCIDR,
		PodCIDRs:          append([]string(nil), node.Spec.PodCIDRs...),
		OperatingSystem:   node.Status.NodeInfo.OperatingSystem,
		Architecture:      node.Status.NodeInfo.Architecture,
		OSImage:           node.Status.NodeInfo.OSImage,
		KernelVersion:     node.Status.NodeInfo.KernelVersion,
		ContainerRuntime:  node.Status.NodeInfo.ContainerRuntimeVersion,
		KubeletVersion:    node.Status.NodeInfo.KubeletVersion,
		Capacity:          resourceMap(node.Status.Capacity),
		Allocatable:       resourceMap(node.Status.Allocatable),
		Labels:            copyMap(node.Labels),
		CreationTimestamp: node.CreationTimestamp.Time,
	}
	if info.PodCIDR == "" && len(info.PodCIDRs) > 0 {
		info.PodCIDR = info.PodCIDRs[0]
	}
	info.CapacityCPU = info.Capacity[string(corev1.ResourceCPU)]
	info.CapacityMemory = info.Capacity[string(corev1.ResourceMemory)]
	info.CapacityPods = info.Capacity[string(corev1.ResourcePods)]
	info.AllocatableCPU = info.Allocatable[string(corev1.ResourceCPU)]
	info.AllocatableMemory = info.Allocatable[string(corev1.ResourceMemory)]
	info.AllocatablePods = info.Allocatable[string(corev1.ResourcePods)]

	for _, address := range node.Status.Addresses {
		switch address.Type {
		case corev1.NodeInternalIP:
			if info.InternalIP == "" {
				info.InternalIP = address.Address
			}
		case corev1.NodeExternalIP:
			if info.ExternalIP == "" {
				info.ExternalIP = address.Address
			}
		case corev1.NodeHostName:
			if info.Hostname == "" {
				info.Hostname = address.Address
			}
		}
	}
	for _, condition := range node.Status.Conditions {
		info.Conditions = append(info.Conditions, NodeCondition{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastHeartbeatTime:  condition.LastHeartbeatTime.Time,
			LastTransitionTime: condition.LastTransitionTime.Time,
		})
		if condition.Type == corev1.NodeReady {
			info.Ready = condition.Status == corev1.ConditionTrue
		}
	}
	for _, taint := range node.Spec.Taints {
		info.Taints = append(info.Taints, NodeTaint{
			Key:    taint.Key,
			Value:  taint.Value,
			Effect: string(taint.Effect),
		})
	}
	return info
}

func resourceMap(list corev1.ResourceList) map[string]string {
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]string, len(list))
	for name, quantity := range list {
		out[string(name)] = quantity.String()
	}
	return out
}
