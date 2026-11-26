package kube

import (
	"context"
	"time"
)

type Context struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Env       string `json:"env,omitempty"`
	Display   string `json:"display,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type Port struct {
	Name          string `json:"name,omitempty"`
	ContainerPort int32  `json:"container_port"`
	Protocol      string `json:"protocol,omitempty"`
}

type Container struct {
	Name        string `json:"name"`
	Image       string `json:"image"`
	Ready       bool   `json:"ready"`
	Restarts    int32  `json:"restarts"`
	State       string `json:"state"`
	StateReason string `json:"state_reason,omitempty"`
	Ports       []Port `json:"ports,omitempty"`
}

type Pod struct {
	Context         string            `json:"context"`
	Cluster         string            `json:"cluster"`
	Env             string            `json:"env,omitempty"`
	Display         string            `json:"display,omitempty"`
	Namespace       string            `json:"namespace"`
	Name            string            `json:"name"`
	UID             string            `json:"uid,omitempty"`
	Phase           string            `json:"phase"`
	Reason          string            `json:"reason,omitempty"`
	Ready           bool              `json:"ready"`
	ReadyContainers int               `json:"ready_containers"`
	ContainersCount int               `json:"containers_count"`
	Restarts        int32             `json:"restarts"`
	Node            string            `json:"node,omitempty"`
	PodIP           string            `json:"pod_ip,omitempty"`
	HostIP          string            `json:"host_ip,omitempty"`
	QoSClass        string            `json:"qos_class,omitempty"`
	StartTime       time.Time         `json:"start_time"`
	OwnerKind       string            `json:"owner_kind,omitempty"`
	OwnerName       string            `json:"owner_name,omitempty"`
	ReplicaSet      string            `json:"replica_set,omitempty"`
	Workload        string            `json:"workload,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Images          []string          `json:"images,omitempty"`
	Containers      []Container       `json:"containers,omitempty"`
	ServiceAccount  string            `json:"service_account,omitempty"`
}

func (p Pod) Age() time.Duration {
	if p.StartTime.IsZero() {
		return 0
	}
	return time.Since(p.StartTime)
}

func (p Pod) Ref() PodRef {
	return PodRef{Context: p.Context, Namespace: p.Namespace, Name: p.Name}
}

type PodRef struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func (r PodRef) String() string {
	return r.Context + "/" + r.Namespace + "/" + r.Name
}

type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"last_transition_time"`
}

type Event struct {
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Source    string    `json:"source,omitempty"`
	Count     int32     `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

type NodeCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastHeartbeatTime  time.Time `json:"last_heartbeat_time"`
	LastTransitionTime time.Time `json:"last_transition_time"`
}

type NodeTaint struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Effect string `json:"effect"`
}

type NodeInfo struct {
	Name              string            `json:"name"`
	UID               string            `json:"uid,omitempty"`
	ProviderID        string            `json:"provider_id,omitempty"`
	Ready             bool              `json:"ready"`
	InternalIP        string            `json:"internal_ip,omitempty"`
	ExternalIP        string            `json:"external_ip,omitempty"`
	Hostname          string            `json:"hostname,omitempty"`
	PodCIDR           string            `json:"pod_cidr,omitempty"`
	PodCIDRs          []string          `json:"pod_cidrs,omitempty"`
	OperatingSystem   string            `json:"operating_system,omitempty"`
	Architecture      string            `json:"architecture,omitempty"`
	OSImage           string            `json:"os_image,omitempty"`
	KernelVersion     string            `json:"kernel_version,omitempty"`
	ContainerRuntime  string            `json:"container_runtime,omitempty"`
	KubeletVersion    string            `json:"kubelet_version,omitempty"`
	Capacity          map[string]string `json:"capacity,omitempty"`
	Allocatable       map[string]string `json:"allocatable,omitempty"`
	CapacityCPU       string            `json:"capacity_cpu,omitempty"`
	CapacityMemory    string            `json:"capacity_memory,omitempty"`
	CapacityPods      string            `json:"capacity_pods,omitempty"`
	AllocatableCPU    string            `json:"allocatable_cpu,omitempty"`
	AllocatableMemory string            `json:"allocatable_memory,omitempty"`
	AllocatablePods   string            `json:"allocatable_pods,omitempty"`
	Conditions        []NodeCondition   `json:"conditions,omitempty"`
	Taints            []NodeTaint       `json:"taints,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	CreationTimestamp time.Time         `json:"creation_timestamp"`
}

type PodDetail struct {
	Pod               Pod               `json:"pod"`
	Node              *NodeInfo         `json:"node,omitempty"`
	NodeError         string            `json:"node_error,omitempty"`
	YAML              string            `json:"yaml,omitempty"`
	Describe          string            `json:"describe,omitempty"`
	Conditions        []Condition       `json:"conditions,omitempty"`
	Events            []Event           `json:"events,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	NodeSelectors     map[string]string `json:"node_selectors,omitempty"`
	OwnerChain        []OwnerRef        `json:"owner_chain,omitempty"`
	Volumes           []Volume          `json:"volumes,omitempty"`
	Topology          *Topology         `json:"topology,omitempty"`
	ServiceAccount    string            `json:"service_account,omitempty"`
	PriorityClassName string            `json:"priority_class_name,omitempty"`
	QoSClass          string            `json:"qos_class,omitempty"`
	HostIP            string            `json:"host_ip,omitempty"`
	PodIP             string            `json:"pod_ip,omitempty"`
	PodIPs            []string          `json:"pod_ips,omitempty"`
	FetchedAt         time.Time         `json:"fetched_at"`
}

type OwnerRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	UID  string `json:"uid,omitempty"`
}

type Volume struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Topology struct {
	Nodes    []TopologyNode `json:"nodes,omitempty"`
	Edges    []TopologyEdge `json:"edges,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
}

type TopologyNode struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	Subtitle string         `json:"subtitle,omitempty"`
	Status   string         `json:"status,omitempty"`
	Command  string         `json:"command,omitempty"`
	Facts    []TopologyFact `json:"facts,omitempty"`
}

type TopologyFact struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type TopologyEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

type ContextError struct {
	Context string `json:"context"`
	Error   string `json:"error"`
}

type ListResult struct {
	Pods      []Pod          `json:"pods"`
	Contexts  []Context      `json:"contexts"`
	Errors    []ContextError `json:"errors,omitempty"`
	FetchedAt time.Time      `json:"fetched_at"`
	Duration  time.Duration  `json:"duration"`
}

type ListOptions struct {
	Contexts          []string
	Namespaces        []string
	ExcludeNamespaces []string
	Concurrency       int
	Timeout           time.Duration
	ResolveOwners     bool
}

type Provider interface {
	Contexts(ctx context.Context) ([]Context, error)
	ListPods(ctx context.Context, opts ListOptions) (*ListResult, error)
	GetPod(ctx context.Context, ref PodRef) (*PodDetail, error)
	Close() error
}
