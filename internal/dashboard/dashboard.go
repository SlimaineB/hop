// Package dashboard aggregates pods, metrics-server samples and server checks
// into a single infrastructure overview.
package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SlimaineB/hop/internal/app"
	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

type PodSource interface {
	Snapshot() *app.Snapshot
	Warm(ctx context.Context, req app.WarmRequest) (*app.Snapshot, error)
	Metrics(ctx context.Context, req app.MetricsRequest) (*kube.MetricsResult, error)
}

type ServerSource interface {
	List() ([]servers.Server, error)
	CheckServer(ctx context.Context, server servers.Server) servers.Check
}

type Service struct {
	pods    PodSource
	servers ServerSource
	cfg     *config.Config

	mu          sync.Mutex
	checkMu     sync.Mutex
	serverCards []ServerCard
	serverAt    time.Time
}

func New(pods PodSource, serverSource ServerSource, cfg *config.Config) *Service {
	return &Service{pods: pods, servers: serverSource, cfg: cfg}
}

type Totals struct {
	Pods             int   `json:"pods"`
	Ready            int   `json:"ready"`
	NotReady         int   `json:"not_ready"`
	Running          int   `json:"running"`
	Pending          int   `json:"pending"`
	Failed           int   `json:"failed"`
	Completed        int   `json:"completed"`
	Restarts         int   `json:"restarts"`
	Contexts         int   `json:"contexts"`
	Namespaces       int   `json:"namespaces"`
	Apps             int   `json:"apps"`
	Nodes            int   `json:"nodes"`
	Servers          int   `json:"servers"`
	ServersDown      int   `json:"servers_down"`
	ServersWarn      int   `json:"servers_warn"`
	PodsEstimated    int64 `json:"pods_estimated,omitempty"`
	CPU              int64 `json:"cpu_millicores"`
	Memory           int64 `json:"memory_bytes"`
	CPUCapacity      int64 `json:"cpu_capacity_millicores"`
	MemoryCapacity   int64 `json:"memory_capacity_bytes"`
	MetricsAvailable bool  `json:"metrics_available"`
}

type Bucket struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Pods       int    `json:"pods"`
	Ready      int    `json:"ready"`
	NotReady   int    `json:"not_ready"`
	Running    int    `json:"running"`
	Pending    int    `json:"pending"`
	Failed     int    `json:"failed"`
	Completed  int    `json:"completed"`
	Restarts   int    `json:"restarts"`
	Contexts   int    `json:"contexts"`
	Namespaces int    `json:"namespaces"`
	Apps       int    `json:"apps"`
	CPU        int64  `json:"cpu_millicores"`
	Memory     int64  `json:"memory_bytes"`
}

type NodeCard struct {
	Context        string `json:"context"`
	Name           string `json:"name"`
	Env            string `json:"env,omitempty"`
	Ready          bool   `json:"ready"`
	Pods           int    `json:"pods"`
	CPU            int64  `json:"cpu_millicores"`
	Memory         int64  `json:"memory_bytes"`
	CPUCapacity    int64  `json:"cpu_capacity_millicores"`
	MemoryCapacity int64  `json:"memory_capacity_bytes"`
}

type ServerCard struct {
	Name             string              `json:"name"`
	Env              string              `json:"env,omitempty"`
	Host             string              `json:"host,omitempty"`
	User             string              `json:"user,omitempty"`
	MetricsURL       string              `json:"metrics_url,omitempty"`
	HealthURL        string              `json:"health_url,omitempty"`
	SSHChecks        bool                `json:"ssh_checks"`
	NetworkChecks    bool                `json:"network_checks"`
	CheckPorts       []int               `json:"check_ports,omitempty"`
	Source           string              `json:"source,omitempty"`
	Tags             []string            `json:"tags,omitempty"`
	Status           string              `json:"status"`
	Checked          bool                `json:"checked"`
	CheckedAt        time.Time           `json:"checked_at,omitempty"`
	MetricsOK        bool                `json:"metrics_ok"`
	HealthOK         bool                `json:"health_ok"`
	MachineOK        bool                `json:"machine_ok"`
	NetworkChecked   bool                `json:"network_checked"`
	NetworkReachable bool                `json:"network_reachable"`
	PingOK           bool                `json:"ping_ok"`
	PingMS           int64               `json:"ping_ms,omitempty"`
	NetworkError     string              `json:"network_error,omitempty"`
	OpenPorts        int                 `json:"open_ports"`
	Ports            []servers.PortCheck `json:"ports,omitempty"`
	CPUs             int                 `json:"cpus,omitempty"`
	Load             string              `json:"load,omitempty"`
	Uptime           string              `json:"uptime,omitempty"`
	OS               string              `json:"os,omitempty"`
	MemoryUsed       uint64              `json:"memory_used,omitempty"`
	MemoryTotal      uint64              `json:"memory_total,omitempty"`
	DiskUsed         uint64              `json:"disk_used,omitempty"`
	DiskTotal        uint64              `json:"disk_total,omitempty"`
	DiskPercent      string              `json:"disk_percent,omitempty"`
	FailedUnits      []string            `json:"failed_units,omitempty"`
	Error            string              `json:"error,omitempty"`
}

type PodIssue struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Env       string `json:"env,omitempty"`
	App       string `json:"app,omitempty"`
	Node      string `json:"node,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Ready     bool   `json:"ready"`
	Restarts  int32  `json:"restarts"`
	Age       string `json:"age,omitempty"`
	CPU       int64  `json:"cpu_millicores"`
	Memory    int64  `json:"memory_bytes"`
	Severity  int    `json:"severity"`
}

type Overview struct {
	FetchedAt      time.Time           `json:"fetched_at"`
	Duration       time.Duration       `json:"duration"`
	Truncated      bool                `json:"truncated,omitempty"`
	EstimatedTotal int64               `json:"estimated_total,omitempty"`
	Totals         Totals              `json:"totals"`
	Groups         map[string][]Bucket `json:"groups"`
	Nodes          []NodeCard          `json:"nodes"`
	Servers        []ServerCard        `json:"servers"`
	Issues         []PodIssue          `json:"issues"`
	IssueTotal     int                 `json:"issue_total"`
	Errors         []string            `json:"errors,omitempty"`
}

func (s *Service) Overview(ctx context.Context, force bool) (*Overview, error) {
	start := time.Now()
	servers := s.inventoryServers()
	overview := &Overview{
		FetchedAt: time.Now(),
		Groups:    map[string][]Bucket{},
		Servers:   servers,
		Totals:    Totals{Servers: len(servers)},
	}
	for _, server := range servers {
		switch server.Status {
		case "down":
			overview.Totals.ServersDown++
		case "warn":
			overview.Totals.ServersWarn++
		}
	}
	if s.pods == nil {
		overview.Duration = time.Since(start)
		return overview, nil
	}
	snapshot, err := s.pods.Warm(ctx, app.WarmRequest{ForceRefresh: force})
	if err != nil && snapshot == nil {
		return nil, err
	}
	if snapshot == nil {
		snapshot = s.pods.Snapshot()
	}
	metrics, metricErr := s.pods.Metrics(ctx, app.MetricsRequest{ForceRefresh: force})
	if metricErr != nil {
		overview.Errors = append(overview.Errors, "metrics: "+metricErr.Error())
	}
	if snapshot != nil {
		overview.Errors = append(overview.Errors, contextErrors(snapshot.Errors)...)
		overview.Truncated = snapshot.Truncated
		overview.EstimatedTotal = snapshot.EstimatedTotal
		overview.Totals.PodsEstimated = snapshot.EstimatedTotal
		if snapshot.Truncated {
			overview.Errors = append(overview.Errors, fmt.Sprintf("pod list truncated to %d (estimated ~%d); raise kube.max_pods / kube.max_pods_per_context", len(snapshot.Pods), snapshot.EstimatedTotal))
		}
		s.aggregate(overview, snapshot, metrics)
	}
	overview.Duration = time.Since(start)
	return overview, nil
}

func (s *Service) aggregate(overview *Overview, snapshot *app.Snapshot, metrics *kube.MetricsResult) {
	usage := map[string]kube.PodMetrics{}
	if metrics != nil {
		for _, item := range metrics.Pods {
			usage[podKey(item.Context, item.Namespace, item.Name)] = item
		}
	}

	type bucketAggregate struct {
		Bucket
		contexts   map[string]bool
		namespaces map[string]bool
		apps       map[string]bool
	}
	groups := map[string]map[string]*bucketAggregate{
		"env":       {},
		"app":       {},
		"cluster":   {},
		"namespace": {},
	}
	nodes := map[string]*NodeCard{}
	contexts := map[string]bool{}
	namespaces := map[string]bool{}
	apps := map[string]bool{}
	var issues []PodIssue

	totals := overview.Totals
	for _, pod := range snapshot.Pods {
		env := s.envFor(pod.Context, pod.Cluster)
		if pod.Env != "" {
			env = pod.Env
		}
		appName := podApp(pod)
		usageItem := usage[podKey(pod.Context, pod.Namespace, pod.Name)]

		contexts[pod.Context] = true
		namespaces[pod.Namespace] = true
		apps[appName] = true

		for kind, key := range map[string]string{
			"env":       env,
			"app":       appName,
			"cluster":   pod.Context,
			"namespace": pod.Namespace,
		} {
			if groups[kind][key] == nil {
				groups[kind][key] = &bucketAggregate{
					Bucket:     Bucket{Key: key, Title: bucketTitle(kind, key), Kind: kind},
					contexts:   map[string]bool{},
					namespaces: map[string]bool{},
					apps:       map[string]bool{},
				}
			}
			bucket := groups[kind][key]
			applyPod(&bucket.Bucket, pod, usageItem)
			bucket.contexts[pod.Context] = true
			bucket.namespaces[pod.Namespace] = true
			bucket.apps[appName] = true
		}

		totals.Pods++
		totals.Restarts += int(pod.Restarts)
		totals.CPU += usageItem.CPU
		totals.Memory += usageItem.Memory
		if pod.Ready {
			totals.Ready++
		}
		switch pod.Phase {
		case "Running":
			totals.Running++
		case "Pending":
			totals.Pending++
		case "Failed":
			totals.Failed++
		case "Succeeded":
			totals.Completed++
		}
		if !pod.Ready && pod.Phase == "Running" {
			totals.NotReady++
		}

		nodeKey := pod.Context + "\x00" + pod.Node
		if pod.Node != "" {
			node := nodes[nodeKey]
			if node == nil {
				node = &NodeCard{Context: pod.Context, Name: pod.Node, Env: env}
				nodes[nodeKey] = node
			}
			node.Pods++
		}

		hardIssue := pod.Phase != "Succeeded" && (!pod.Ready || (pod.Phase != "Running" && pod.Phase != "Succeeded"))
		restartIssue := pod.Phase != "Succeeded" && pod.Restarts >= 10
		if hardIssue || restartIssue {
			severity := 0
			if !pod.Ready && pod.Phase != "Succeeded" {
				severity += 100
			}
			if pod.Phase == "Failed" {
				severity += 120
			}
			if pod.Phase == "Pending" {
				severity += 60
			}
			if restartIssue {
				restartSeverity := int(pod.Restarts) * 3
				if restartSeverity > 90 {
					restartSeverity = 90
				}
				if restartSeverity > severity {
					severity = restartSeverity
				}
			}
			issues = append(issues, PodIssue{
				Context: pod.Context, Namespace: pod.Namespace, Name: pod.Name,
				Env: env, App: appName, Node: pod.Node, Phase: pod.Phase,
				Reason: pod.Reason, Ready: pod.Ready, Restarts: pod.Restarts,
				Age: ageLabel(pod.Age()), CPU: usageItem.CPU, Memory: usageItem.Memory,
				Severity: severity,
			})
		}
	}

	if metrics != nil {
		totals.MetricsAvailable = len(metrics.Pods) > 0
		for _, item := range metrics.Nodes {
			nodeKey := item.Context + "\x00" + item.Name
			node := nodes[nodeKey]
			if node == nil {
				node = &NodeCard{Context: item.Context, Name: item.Name, Env: s.envFor(item.Context, "")}
				nodes[nodeKey] = node
			}
			node.CPU = item.CPU
			node.Memory = item.Memory
			node.CPUCapacity = item.CPUCapacity
			node.MemoryCapacity = item.MemoryCapacity
			node.Ready = item.Ready
			totals.CPUCapacity += item.CPUCapacity
			totals.MemoryCapacity += item.MemoryCapacity
		}
	}

	totals.Contexts = len(contexts)
	totals.Namespaces = len(namespaces)
	totals.Apps = len(apps)
	totals.Nodes = len(nodes)
	overview.Totals = totals

	for kind, bucketMap := range groups {
		buckets := make([]Bucket, 0, len(bucketMap))
		for _, aggregate := range bucketMap {
			aggregate.Contexts = len(aggregate.contexts)
			aggregate.Namespaces = len(aggregate.namespaces)
			aggregate.Apps = len(aggregate.apps)
			buckets = append(buckets, aggregate.Bucket)
		}
		sortBuckets(buckets)
		overview.Groups[kind] = buckets
	}

	overview.Nodes = make([]NodeCard, 0, len(nodes))
	for _, node := range nodes {
		overview.Nodes = append(overview.Nodes, *node)
	}
	sort.Slice(overview.Nodes, func(i, j int) bool {
		if overview.Nodes[i].Context != overview.Nodes[j].Context {
			return overview.Nodes[i].Context < overview.Nodes[j].Context
		}
		return overview.Nodes[i].Name < overview.Nodes[j].Name
	})

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Severity != issues[j].Severity {
			return issues[i].Severity > issues[j].Severity
		}
		if issues[i].Restarts != issues[j].Restarts {
			return issues[i].Restarts > issues[j].Restarts
		}
		return issues[i].Name < issues[j].Name
	})
	overview.IssueTotal = len(issues)
	if len(issues) > 12 {
		issues = issues[:12]
	}
	overview.Issues = issues
}

func applyPod(bucket *Bucket, pod kube.Pod, usage kube.PodMetrics) {
	bucket.Pods++
	bucket.Restarts += int(pod.Restarts)
	bucket.CPU += usage.CPU
	bucket.Memory += usage.Memory
	switch pod.Phase {
	case "Running":
		bucket.Running++
	case "Pending":
		bucket.Pending++
	case "Failed":
		bucket.Failed++
	case "Succeeded":
		bucket.Completed++
	}
	if pod.Ready {
		bucket.Ready++
	}
	if !pod.Ready && pod.Phase == "Running" {
		bucket.NotReady++
	}
}

func sortBuckets(buckets []Bucket) {
	sort.SliceStable(buckets, func(i, j int) bool {
		a, b := buckets[i], buckets[j]
		badA := a.NotReady + a.Failed + a.Pending
		badB := b.NotReady + b.Failed + b.Pending
		if badA != badB {
			return badA > badB
		}
		if a.Restarts != b.Restarts {
			return a.Restarts > b.Restarts
		}
		if a.Pods != b.Pods {
			return a.Pods > b.Pods
		}
		return strings.ToLower(a.Key) < strings.ToLower(b.Key)
	})
}

func podApp(pod kube.Pod) string {
	for _, key := range []string{"app.kubernetes.io/name", "app", "k8s-app", "k8s-app-name", "service"} {
		if value := strings.TrimSpace(pod.Labels[key]); value != "" {
			return value
		}
	}
	if pod.Workload != "" {
		return pod.Workload
	}
	if pod.OwnerName != "" {
		return pod.OwnerName
	}
	return "unassigned"
}

func podKey(context, namespace, name string) string {
	return context + "/" + namespace + "/" + name
}

func bucketTitle(kind, key string) string {
	switch kind {
	case "env":
		if key == "" || key == "unknown" {
			return "Unassigned"
		}
		return strings.ToUpper(key)
	default:
		return key
	}
}

func (s *Service) envFor(context, cluster string) string {
	if s.cfg == nil {
		return ""
	}
	return s.cfg.Resolve(context, cluster).Env
}

func contextErrors(items []kube.ContextError) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Context+": "+item.Error)
	}
	return out
}

func (s *Service) inventoryServers() []ServerCard {
	if s.servers == nil {
		return nil
	}
	list, err := s.servers.List()
	if err != nil {
		return nil
	}
	out := make([]ServerCard, 0, len(list))
	for _, server := range list {
		card := ServerCard{
			Name: server.Name, Env: server.Env, Host: server.Host, User: server.User,
			MetricsURL: server.MetricsURL, HealthURL: server.HealthURL, SSHChecks: server.SSHChecks,
			NetworkChecks: server.NetworkChecks, CheckPorts: server.CheckPorts,
			Source: server.Source, Tags: server.Tags, Status: "unknown",
		}
		s.applyCachedCheck(&card)
		out = append(out, card)
	}
	sortServers(out)
	return out
}

func (s *Service) applyCachedCheck(card *ServerCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cached := range s.serverCards {
		if cached.Name == card.Name {
			*card = cached
			return
		}
	}
}

func (s *Service) ServerChecks(ctx context.Context, force bool) ([]ServerCard, error) {
	if s.servers == nil {
		return nil, nil
	}
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	s.mu.Lock()
	if !force && len(s.serverCards) > 0 && time.Since(s.serverAt) < 20*time.Second {
		cards := append([]ServerCard(nil), s.serverCards...)
		s.mu.Unlock()
		return cards, nil
	}
	s.mu.Unlock()

	list, err := s.servers.List()
	if err != nil {
		return nil, err
	}
	cards := make([]ServerCard, len(list))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range list {
		wg.Add(1)
		go func(index int, server servers.Server) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			check := s.servers.CheckServer(ctx, server)
			card := cardForCheck(server, check)
			mu.Lock()
			cards[index] = card
			mu.Unlock()
		}(i, list[i])
	}
	wg.Wait()

	sortServers(cards)
	s.mu.Lock()
	s.serverCards = cards
	s.serverAt = time.Now()
	s.mu.Unlock()
	return cards, nil
}

func cardForCheck(server servers.Server, check servers.Check) ServerCard {
	card := ServerCard{
		Name: server.Name, Env: server.Env, Host: server.Host, User: server.User,
		MetricsURL: server.MetricsURL, HealthURL: server.HealthURL, SSHChecks: server.SSHChecks,
		NetworkChecks: server.NetworkChecks, CheckPorts: server.CheckPorts,
		Source: server.Source, Tags: server.Tags, Status: "unknown",
		Checked: true, CheckedAt: check.CheckedAt,
		MetricsOK: check.Metrics.OK, HealthOK: check.Health.OK, MachineOK: check.Machine.OK,
		NetworkChecked: check.Network.Checked, NetworkReachable: check.Network.Reachable,
		PingOK: check.Network.Ping.OK, PingMS: check.Network.Ping.LatencyMS,
		Ports: check.Network.Ports,
		CPUs:  check.Machine.CPUs, Load: check.Machine.Load, Uptime: check.Machine.Uptime,
		OS: check.Machine.OS, MemoryUsed: check.Machine.MemoryUsed,
		MemoryTotal: check.Machine.MemoryTotal, DiskUsed: check.Machine.DiskUsed,
		DiskTotal: check.Machine.DiskTotal, DiskPercent: check.Machine.DiskPercent,
		FailedUnits: check.Machine.FailedUnits,
	}
	if check.Network.Ping.Error != "" && !check.Network.Ping.OK {
		card.NetworkError = check.Network.Ping.Error
	}
	for _, port := range check.Network.Ports {
		if port.Open {
			card.OpenPorts++
		}
	}
	if check.Machine.Error != "" && !check.Machine.OK {
		card.Error = check.Machine.Error
	}
	machineError := check.Machine.Error != "" && check.Machine.Error != "SSH checks disabled"
	switch {
	case check.Health.URL != "" && !check.Health.OK:
		card.Status = "down"
	case check.Metrics.URL != "" && !check.Metrics.OK:
		card.Status = "down"
	case len(check.Machine.FailedUnits) > 0:
		card.Status = "warn"
	case check.Machine.OK:
		card.Status = "ok"
	case check.Network.Checked && !check.Network.Reachable:
		card.Status = "down"
	case machineError:
		card.Status = "warn"
	case check.Network.Reachable || check.Health.OK || check.Metrics.OK:
		card.Status = "ok"
	}
	return card
}

func sortServers(cards []ServerCard) {
	rank := map[string]int{"down": 0, "warn": 1, "unknown": 2, "ok": 3}
	sort.SliceStable(cards, func(i, j int) bool {
		if rank[cards[i].Status] != rank[cards[j].Status] {
			return rank[cards[i].Status] < rank[cards[j].Status]
		}
		if cards[i].Env != cards[j].Env {
			return cards[i].Env < cards[j].Env
		}
		return strings.ToLower(cards[i].Name) < strings.ToLower(cards[j].Name)
	})
}

func ageLabel(age time.Duration) string {
	if age <= 0 {
		return ""
	}
	days := int(age.Hours() / 24)
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	hours := int(age.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", int(age.Minutes()))
}
