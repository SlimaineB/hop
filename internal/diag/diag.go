package diag

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

type Finding struct {
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	Remedy   string   `json:"remedy,omitempty"`
}

type Report struct {
	Kind      string        `json:"kind"`
	Target    string        `json:"target"`
	Env       string        `json:"env,omitempty"`
	Verdict   Severity      `json:"verdict"`
	Summary   string        `json:"summary"`
	Findings  []Finding     `json:"findings"`
	Probes    []ProbeResult `json:"probes,omitempty"`
	Duration  time.Duration `json:"duration_ns,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

type ProbeResult struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Type     string        `json:"type"`
	Severity Severity      `json:"severity"`
	OK       bool          `json:"ok"`
	Summary  string        `json:"summary,omitempty"`
	Error    string        `json:"error,omitempty"`
	Evidence []string      `json:"evidence,omitempty"`
	Duration time.Duration `json:"duration_ns,omitempty"`
}

func NewReport(kind, target string) *Report {
	return &Report{
		Kind:      kind,
		Target:    target,
		Verdict:   SeverityInfo,
		Findings:  []Finding{},
		CheckedAt: time.Now(),
	}
}

func (r *Report) Add(finding Finding) {
	if finding.Severity == "" {
		finding.Severity = SeverityInfo
	}
	r.Findings = append(r.Findings, finding)
}

func (r *Report) Finish() *Report {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		left, right := severityRank(r.Findings[i].Severity), severityRank(r.Findings[j].Severity)
		if left != right {
			return left < right
		}
		return r.Findings[i].Title < r.Findings[j].Title
	})
	critical, warning := 0, 0
	for _, finding := range r.Findings {
		switch finding.Severity {
		case SeverityCritical:
			critical++
		case SeverityWarning:
			warning++
		}
	}
	switch {
	case critical == 1:
		r.Verdict = SeverityCritical
		r.Summary = "1 critical issue requires action."
	case critical > 1:
		r.Verdict = SeverityCritical
		r.Summary = fmt.Sprintf("%d critical issues require action.", critical)
	case warning > 0:
		r.Verdict = SeverityWarning
		r.Summary = fmt.Sprintf("%s to review.", plural(warning, "warning"))
	default:
		r.Verdict = SeverityInfo
		r.Summary = "No blocking issue detected."
	}
	return r
}

func DiagnosePod(detail kube.PodDetail, stats *kube.PodStats) *Report {
	pod := detail.Pod
	report := NewReport("pod", pod.Ref().String())

	diagnosePodPhase(report, pod)
	handledReasons := map[string]bool{}
	diagnosePodReason(report, pod.Reason, "pod", handledReasons)
	for _, container := range pod.Containers {
		if container.StateReason != "" && !handledReasons[container.StateReason] {
			diagnosePodReason(report, container.StateReason, "container "+container.Name, handledReasons)
		}
		if !container.Ready && pod.Ready {
			report.Add(Finding{
				ID:       "container-not-ready",
				Severity: SeverityWarning,
				Title:    "Container is not ready",
				Detail:   fmt.Sprintf("Container %s is not ready while the pod is reported ready.", container.Name),
				Evidence: containerEvidence(container),
				Remedy:   "Check the container readiness probe and recent events.",
			})
		}
	}
	diagnosePodRestarts(report, pod)
	diagnosePodConditions(report, detail.Conditions)
	diagnosePodNode(report, detail.Node, detail.NodeError)
	diagnosePodEvents(report, detail.Events)
	diagnosePodQoS(report, pod)
	diagnosePodTopology(report, detail.Topology)
	diagnosePodStats(report, pod, stats)

	return report.Finish()
}

func DiagnoseServer(server servers.Server, check servers.Check) *Report {
	report := NewReport("vm", server.Name)
	diagnoseMachine(report, check.Machine)
	diagnoseHTTP(report, "health", check.Health)
	diagnoseHTTP(report, "metrics", check.Metrics)
	diagnoseNetwork(report, check.Network, server)

	return report.Finish()
}

func diagnosePodPhase(report *Report, pod kube.Pod) {
	switch pod.Phase {
	case "Failed":
		report.Add(Finding{
			ID:       "pod-failed",
			Severity: SeverityCritical,
			Title:    "Pod is failed",
			Detail:   reasonDetail("The pod has entered the Failed phase", pod.Reason),
			Evidence: []string{"phase: Failed", "ready: " + boolText(pod.Ready)},
			Remedy:   "Inspect events and container logs, then recreate or roll back the workload.",
		})
	case "Pending":
		severity := SeverityWarning
		if !pod.StartTime.IsZero() && time.Since(pod.StartTime) >= 5*time.Minute {
			severity = SeverityCritical
		}
		report.Add(Finding{
			ID:       "pod-pending",
			Severity: severity,
			Title:    "Pod is pending",
			Detail:   reasonDetail("The pod has not started running yet", pod.Reason),
			Evidence: []string{"phase: Pending", "age: " + ageText(pod.StartTime)},
			Remedy:   "Check scheduling, image pulls, volume mounts and container creation events.",
		})
	case "Running":
		if !pod.Ready {
			report.Add(Finding{
				ID:       "pod-not-ready",
				Severity: SeverityWarning,
				Title:    "Pod is running but not ready",
				Detail:   reasonDetail("Readiness is false for a running pod", pod.Reason),
				Evidence: []string{
					fmt.Sprintf("ready containers: %d/%d", pod.ReadyContainers, pod.ContainersCount),
					"restarts: " + strconv.Itoa(int(pod.Restarts)),
				},
				Remedy: "Check readiness probes, recent events and container logs.",
			})
		}
	case "Succeeded":
		report.Add(Finding{
			ID:       "pod-succeeded",
			Severity: SeverityInfo,
			Title:    "Pod completed",
			Detail:   "The pod reached the Succeeded phase.",
		})
	case "":
		report.Add(Finding{
			ID:       "pod-phase-unknown",
			Severity: SeverityWarning,
			Title:    "Pod phase is unknown",
			Remedy:   "Refresh the pod and verify the Kubernetes API response.",
		})
	}
}

func diagnosePodReason(report *Report, reason, scope string, handled map[string]bool) {
	reason = strings.TrimSpace(reason)
	if reason == "" || handled[reason] {
		return
	}
	handled[reason] = true

	type reasonRule struct {
		severity Severity
		title    string
		remedy   string
	}
	rules := map[string]reasonRule{
		"CrashLoopBackOff":           {SeverityCritical, "Container is crash looping", "Read previous logs, then fix the crash or roll back the image."},
		"ImagePullBackOff":           {SeverityCritical, "Image cannot be pulled", "Verify the image reference, registry credentials and network access."},
		"ErrImagePull":               {SeverityCritical, "Image pull failed", "Verify the image reference, registry credentials and network access."},
		"InvalidImageName":           {SeverityCritical, "Invalid image name", "Fix the container image reference in the workload spec."},
		"CreateContainerConfigError": {SeverityCritical, "Container configuration is invalid", "Check referenced ConfigMaps, Secrets and env vars."},
		"CreateContainerError":       {SeverityCritical, "Container creation failed", "Inspect kubelet events and the container runtime."},
		"RunContainerError":          {SeverityCritical, "Container failed to start", "Inspect events and container runtime errors."},
		"OOMKilled":                  {SeverityCritical, "Container was OOM killed", "Raise the memory limit or reduce the workload memory usage."},
		"Evicted":                    {SeverityCritical, "Pod was evicted", "Relieve node pressure and review requests, limits and priority."},
		"FailedScheduling":           {SeverityCritical, "Pod scheduling failed", "Check node capacity, taints, affinity and tolerations."},
		"Unschedulable":              {SeverityCritical, "Pod is unschedulable", "Check node capacity, taints, affinity and tolerations."},
		"FailedMount":                {SeverityCritical, "Volume mount failed", "Check the PVC, Secret, ConfigMap and volume attachment events."},
		"FailedAttachVolume":         {SeverityCritical, "Volume attach failed", "Check the CSI driver and volume attachment events."},
		"OutOfcpu":                   {SeverityCritical, "Node is out of CPU", "Free CPU on the node or adjust pod requests."},
		"OutOfmemory":                {SeverityCritical, "Node is out of memory", "Free memory on the node or adjust pod limits."},
		"NodeLost":                   {SeverityCritical, "Pod node was lost", "Verify node health and reschedule the workload."},
		"Error":                      {SeverityCritical, "Container is in error state", "Inspect events and container logs."},
		"ContainerStatusUnknown":     {SeverityWarning, "Container status is unknown", "Check kubelet and container runtime health."},
		"ContainerCreating":          {SeverityWarning, "Container is still being created", "Wait or inspect volume, image and runtime events if it persists."},
		"PodInitializing":            {SeverityWarning, "Pod is initializing", "Check init containers and dependencies if it persists."},
		"Terminating":                {SeverityInfo, "Pod is terminating", "Wait for graceful termination or check finalizers."},
		"Completed":                  {SeverityInfo, "Container completed", ""},
		"CompletedWithError":         {SeverityWarning, "Container completed with an error", "Inspect the container logs and exit code."},
		"DeadlineExceeded":           {SeverityWarning, "Operation deadline exceeded", "Increase the timeout or inspect the dependency."},
	}
	rule, ok := rules[reason]
	if !ok {
		if strings.Contains(strings.ToLower(reason), "fail") || strings.Contains(strings.ToLower(reason), "error") {
			rule = reasonRule{SeverityWarning, "Container reports " + reason, "Inspect events and container logs for details."}
		} else {
			return
		}
	}
	report.Add(Finding{
		ID:       "reason-" + strings.ToLower(reason),
		Severity: rule.severity,
		Title:    rule.title,
		Detail:   fmt.Sprintf("%s reports reason %s.", scope, reason),
		Evidence: []string{"reason: " + reason, "scope: " + scope},
		Remedy:   rule.remedy,
	})
}

func diagnosePodRestarts(report *Report, pod kube.Pod) {
	severity := SeverityInfo
	switch {
	case pod.Restarts >= 10:
		severity = SeverityCritical
	case pod.Restarts >= 3:
		severity = SeverityWarning
	default:
		return
	}
	evidence := []string{fmt.Sprintf("total restarts: %d", pod.Restarts)}
	for _, container := range pod.Containers {
		if container.Restarts > 0 {
			evidence = append(evidence, fmt.Sprintf("%s: %d restarts", container.Name, container.Restarts))
		}
	}
	report.Add(Finding{
		ID:       "high-restarts",
		Severity: severity,
		Title:    "Pod restarts frequently",
		Detail:   fmt.Sprintf("The pod has restarted %d times.", pod.Restarts),
		Evidence: evidence,
		Remedy:   "Inspect previous logs and check probes, resource limits and dependencies.",
	})
}

func diagnosePodConditions(report *Report, conditions []kube.Condition) {
	for _, condition := range conditions {
		if !strings.EqualFold(condition.Status, "False") {
			continue
		}
		severity := SeverityWarning
		title := condition.Type + " is false"
		switch condition.Type {
		case "Ready":
			continue
		case "PodScheduled":
			severity = SeverityCritical
			title = "Pod is not scheduled"
		case "Initialized":
			title = "Pod initialization is incomplete"
		case "ContainersReady":
			title = "Containers are not ready"
		default:
			continue
		}
		evidence := []string{"condition: " + condition.Type + "=False"}
		if condition.Reason != "" {
			evidence = append(evidence, "reason: "+condition.Reason)
		}
		if condition.Message != "" {
			evidence = append(evidence, "message: "+condition.Message)
		}
		report.Add(Finding{
			ID:       "condition-" + strings.ToLower(condition.Type),
			Severity: severity,
			Title:    title,
			Evidence: evidence,
			Remedy:   "Inspect pod events and the condition message.",
		})
	}
}

func diagnosePodNode(report *Report, node *kube.NodeInfo, nodeError string) {
	if nodeError != "" {
		report.Add(Finding{
			ID:       "node-unavailable",
			Severity: SeverityWarning,
			Title:    "Node details are unavailable",
			Evidence: []string{nodeError},
			Remedy:   "Check permissions to read nodes and the node name.",
		})
	}
	if node == nil {
		return
	}
	if !node.Ready {
		report.Add(Finding{
			ID:       "node-not-ready",
			Severity: SeverityCritical,
			Title:    "Pod node is not ready",
			Detail:   "The node running this pod reports NotReady.",
			Evidence: []string{"node: " + node.Name},
			Remedy:   "Check node conditions, kubelet health and system resources.",
		})
	}
}

func diagnosePodEvents(report *Report, events []kube.Event) {
	var evidence []string
	total := 0
	for _, event := range events {
		if !strings.EqualFold(event.Type, "Warning") {
			continue
		}
		total++
		if len(evidence) >= 6 {
			continue
		}
		line := event.Reason + ": " + event.Message
		if event.Count > 1 {
			line += fmt.Sprintf(" (x%d)", event.Count)
		}
		evidence = append(evidence, line)
	}
	if total == 0 {
		return
	}
	report.Add(Finding{
		ID:       "warning-events",
		Severity: SeverityWarning,
		Title:    "Warning events are present",
		Detail:   fmt.Sprintf("%d warning event(s) were reported for this pod.", total),
		Evidence: evidence,
		Remedy:   "Inspect the full event list and correlate with logs and metrics.",
	})
}

func diagnosePodQoS(report *Report, pod kube.Pod) {
	switch pod.QoSClass {
	case "BestEffort":
		report.Add(Finding{
			ID:       "qos-besteffort",
			Severity: SeverityInfo,
			Title:    "Pod is BestEffort",
			Detail:   "No CPU or memory requests and limits are set.",
			Remedy:   "Set requests and limits if the workload needs predictable resources.",
		})
	case "Burstable":
		if pod.MemoryLimit == 0 {
			report.Add(Finding{
				ID:       "no-memory-limit",
				Severity: SeverityInfo,
				Title:    "Pod has no memory limit",
				Detail:   "The pod can grow until the node runs out of memory.",
				Remedy:   "Set a memory limit if the workload can be bounded.",
			})
		}
	}
}

func diagnosePodTopology(report *Report, topology *kube.Topology) {
	if topology == nil {
		return
	}
	if len(topology.Warnings) > 0 {
		report.Add(Finding{
			ID:       "topology-incomplete",
			Severity: SeverityWarning,
			Title:    "Related resources could not be loaded",
			Evidence: topology.Warnings,
			Remedy:   "Check RBAC permissions for services, endpointslices and ingresses.",
		})
	}
	services := map[string]bool{}
	sliceByService := map[string]string{}
	for _, node := range topology.Nodes {
		switch node.Kind {
		case "Service":
			services[node.Name] = true
		case "EndpointSlice":
			sliceByService[node.Subtitle] = node.Status
		case "PersistentVolumeClaim":
			if !strings.EqualFold(node.Status, "Bound") {
				report.Add(Finding{
					ID:       "pvc-not-bound-" + node.Name,
					Severity: SeverityCritical,
					Title:    "PersistentVolumeClaim is not bound",
					Detail:   fmt.Sprintf("PVC %s is in phase %s.", node.Name, firstNonEmpty(node.Status, "Unknown")),
					Evidence: []string{"pvc: " + node.Name, "status: " + firstNonEmpty(node.Status, "Unknown")},
					Remedy:   "Check the storage class, provisioner and volume binding events.",
				})
			}
		}
	}
	if len(services) == 0 {
		report.Add(Finding{
			ID:       "no-service",
			Severity: SeverityInfo,
			Title:    "No Service selects this pod",
			Detail:   "No service in the namespace selects this pod's labels.",
			Remedy:   "Add a service selector if the workload should receive traffic.",
		})
		return
	}
	for service := range services {
		status, ok := sliceByService[service]
		if !ok {
			report.Add(Finding{
				ID:       "service-no-endpointslice-" + service,
				Severity: SeverityWarning,
				Title:    "Service has no EndpointSlice",
				Detail:   fmt.Sprintf("Service %s selects the pod but no EndpointSlice was found.", service),
				Evidence: []string{"service: " + service},
				Remedy:   "Check the service selector and the endpoints controller.",
			})
			continue
		}
		if strings.HasPrefix(status, "0/") {
			report.Add(Finding{
				ID:       "service-no-ready-endpoints-" + service,
				Severity: SeverityWarning,
				Title:    "Service has no ready endpoints",
				Detail:   fmt.Sprintf("Service %s has no ready endpoint.", service),
				Evidence: []string{"service: " + service, "endpoints: " + status},
				Remedy:   "Fix pod readiness and verify the service targetPort.",
			})
		}
	}
}

func diagnosePodStats(report *Report, pod kube.Pod, stats *kube.PodStats) {
	if stats == nil {
		return
	}
	if pod.MemoryLimit > 0 && stats.WorkingSet > 0 {
		percent := float64(stats.WorkingSet) / float64(pod.MemoryLimit) * 100
		severity := SeverityInfo
		switch {
		case percent >= 95:
			severity = SeverityCritical
		case percent >= 85:
			severity = SeverityWarning
		}
		if severity != SeverityInfo {
			report.Add(Finding{
				ID:       "memory-near-limit",
				Severity: severity,
				Title:    "Pod memory is near its limit",
				Detail:   fmt.Sprintf("Working set is %.0f%% of the pod memory limit.", percent),
				Evidence: []string{
					fmt.Sprintf("working set: %s", servers.FormatBytes(uint64(stats.WorkingSet))),
					fmt.Sprintf("limit: %s", servers.FormatBytes(uint64(pod.MemoryLimit))),
				},
				Remedy: "Raise the memory limit or reduce the workload memory usage.",
			})
		}
	}
	if len(stats.Warnings) > 0 {
		report.Add(Finding{
			ID:       "metrics-incomplete",
			Severity: SeverityInfo,
			Title:    "Some pod metrics are unavailable",
			Evidence: stats.Warnings,
		})
	}
}

func diagnoseMachine(report *Report, machine servers.MachineInfo) {
	if machine.Error != "" && !machine.OK {
		if strings.Contains(strings.ToLower(machine.Error), "disabled") {
			report.Add(Finding{
				ID:       "ssh-checks-disabled",
				Severity: SeverityInfo,
				Title:    "SSH checks are disabled",
				Evidence: []string{machine.Error},
				Remedy:   "Enable SSH checks if machine diagnostics are required.",
			})
			return
		}
		report.Add(Finding{
			ID:       "machine-unreachable",
			Severity: SeverityCritical,
			Title:    "Machine snapshot failed",
			Detail:   "The server could not be inspected over SSH.",
			Evidence: []string{machine.Error},
			Remedy:   "Check SSH connectivity, credentials and the remote check command.",
		})
		return
	}
	if !machine.OK {
		report.Add(Finding{
			ID:       "machine-incomplete",
			Severity: SeverityWarning,
			Title:    "Machine snapshot is incomplete",
			Remedy:   "Verify SSH access and the diagnostic command output.",
		})
		return
	}
	if machine.CPUs > 0 {
		if load, ok := parseFirstFloat(machine.Load); ok {
			perCPU := load / float64(machine.CPUs)
			switch {
			case perCPU >= 2:
				report.Add(Finding{
					ID:       "load-critical",
					Severity: SeverityCritical,
					Title:    "CPU load is critical",
					Detail:   fmt.Sprintf("Load is %.2f per CPU across %d CPUs.", perCPU, machine.CPUs),
					Evidence: []string{"load: " + machine.Load},
					Remedy:   "Find the busy processes and reduce CPU pressure or add capacity.",
				})
			case perCPU >= 1:
				report.Add(Finding{
					ID:       "load-high",
					Severity: SeverityWarning,
					Title:    "CPU load is high",
					Detail:   fmt.Sprintf("Load is %.2f per CPU across %d CPUs.", perCPU, machine.CPUs),
					Evidence: []string{"load: " + machine.Load},
					Remedy:   "Check long-running processes and trends.",
				})
			}
		}
	}
	if machine.MemoryTotal > 0 {
		percent := float64(machine.MemoryUsed) / float64(machine.MemoryTotal) * 100
		switch {
		case percent >= 95:
			report.Add(Finding{
				ID:       "memory-critical",
				Severity: SeverityCritical,
				Title:    "Memory is almost exhausted",
				Detail:   fmt.Sprintf("Memory usage is %.0f%%.", percent),
				Evidence: []string{
					fmt.Sprintf("used: %s / %s", servers.FormatBytes(machine.MemoryUsed), servers.FormatBytes(machine.MemoryTotal)),
					fmt.Sprintf("available: %s", servers.FormatBytes(machine.MemoryAvailable)),
				},
				Remedy: "Free memory, restart leaking processes or add memory.",
			})
		case percent >= 85:
			report.Add(Finding{
				ID:       "memory-high",
				Severity: SeverityWarning,
				Title:    "Memory usage is high",
				Detail:   fmt.Sprintf("Memory usage is %.0f%%.", percent),
				Evidence: []string{
					fmt.Sprintf("used: %s / %s", servers.FormatBytes(machine.MemoryUsed), servers.FormatBytes(machine.MemoryTotal)),
					fmt.Sprintf("available: %s", servers.FormatBytes(machine.MemoryAvailable)),
				},
				Remedy: "Watch memory pressure and identify the largest consumers.",
			})
		}
	}
	if machine.DiskTotal > 0 {
		percent := float64(machine.DiskUsed) / float64(machine.DiskTotal) * 100
		if parsed, ok := parsePercent(machine.DiskPercent); ok {
			percent = parsed
		}
		switch {
		case percent >= 95:
			report.Add(Finding{
				ID:       "disk-critical",
				Severity: SeverityCritical,
				Title:    "Disk is almost full",
				Detail:   fmt.Sprintf("Filesystem / is %.0f%% full.", percent),
				Evidence: []string{
					fmt.Sprintf("used: %s / %s", servers.FormatBytes(machine.DiskUsed), servers.FormatBytes(machine.DiskTotal)),
					fmt.Sprintf("free: %s", servers.FormatBytes(machine.DiskFree)),
				},
				Remedy: "Free disk space or extend the filesystem.",
			})
		case percent >= 85:
			report.Add(Finding{
				ID:       "disk-high",
				Severity: SeverityWarning,
				Title:    "Disk usage is high",
				Detail:   fmt.Sprintf("Filesystem / is %.0f%% full.", percent),
				Evidence: []string{
					fmt.Sprintf("used: %s / %s", servers.FormatBytes(machine.DiskUsed), servers.FormatBytes(machine.DiskTotal)),
					fmt.Sprintf("free: %s", servers.FormatBytes(machine.DiskFree)),
				},
				Remedy: "Check logs, caches and old artifacts, then clean up space.",
			})
		}
	}
	if len(machine.FailedUnits) > 0 {
		severity := SeverityWarning
		if len(machine.FailedUnits) >= 3 {
			severity = SeverityCritical
		}
		report.Add(Finding{
			ID:       "failed-units",
			Severity: severity,
			Title:    "Systemd units have failed",
			Detail:   fmt.Sprintf("%d systemd unit(s) are failed.", len(machine.FailedUnits)),
			Evidence: machine.FailedUnits,
			Remedy:   "Inspect the units, then restart or disable them.",
		})
	}
}

func diagnoseHTTP(report *Report, kind string, check servers.HTTPCheck) {
	if strings.TrimSpace(check.URL) == "" {
		return
	}
	if !check.OK {
		severity := SeverityWarning
		title := "Metrics endpoint is failing"
		remedy := "Check the exporter, reverse proxy and network path."
		if kind == "health" {
			severity = SeverityCritical
			title = "Health endpoint is failing"
			remedy = "Check the service, its dependencies and the health endpoint."
		}
		evidence := []string{"url: " + check.URL}
		if check.Error != "" {
			evidence = append(evidence, "error: "+check.Error)
		}
		if check.StatusCode > 0 {
			evidence = append(evidence, fmt.Sprintf("status: %d", check.StatusCode))
		}
		report.Add(Finding{
			ID:       kind + "-down",
			Severity: severity,
			Title:    title,
			Evidence: evidence,
			Remedy:   remedy,
		})
		return
	}
	if check.LatencyMS >= 2000 {
		report.Add(Finding{
			ID:       kind + "-slow",
			Severity: SeverityWarning,
			Title:    "Endpoint response is slow",
			Detail:   fmt.Sprintf("%s responded in %dms.", strings.ToUpper(kind[:1])+kind[1:], check.LatencyMS),
			Evidence: []string{"url: " + check.URL, fmt.Sprintf("latency: %dms", check.LatencyMS)},
			Remedy:   "Check the endpoint and its dependencies for latency.",
		})
	}
}

func diagnoseNetwork(report *Report, network servers.NetworkInfo, server servers.Server) {
	if !network.Checked {
		return
	}
	if strings.TrimSpace(network.Target) == "" {
		report.Add(Finding{
			ID:       "network-target-missing",
			Severity: SeverityWarning,
			Title:    "No network target is configured",
			Remedy:   "Set the server host or name used for network checks.",
		})
		return
	}
	if !network.Reachable {
		evidence := []string{"target: " + network.Target}
		if network.Ping.Error != "" {
			evidence = append(evidence, "ping: "+network.Ping.Error)
		}
		report.Add(Finding{
			ID:       "network-unreachable",
			Severity: SeverityCritical,
			Title:    "Server is unreachable",
			Evidence: evidence,
			Remedy:   "Check the host, network path, firewall and SSH port.",
		})
		return
	}
	if !network.Ping.OK && network.Ping.Error != "" {
		report.Add(Finding{
			ID:       "icmp-blocked",
			Severity: SeverityInfo,
			Title:    "ICMP is blocked but TCP is reachable",
			Evidence: []string{"target: " + network.Target, "ping: " + network.Ping.Error},
		})
	}
	var closed []string
	for _, port := range network.Ports {
		if port.Open {
			continue
		}
		line := fmt.Sprintf("%d: %s", port.Port, firstNonEmpty(port.State, port.Error, "closed"))
		closed = append(closed, line)
	}
	if len(closed) > 0 {
		report.Add(Finding{
			ID:       "ports-closed",
			Severity: SeverityWarning,
			Title:    "Configured ports are closed",
			Detail:   fmt.Sprintf("%d configured port(s) did not accept a TCP connection.", len(closed)),
			Evidence: closed,
			Remedy:   "Verify the expected listening services and firewall rules.",
		})
	}
	if server.Port == 22 || server.Port == 0 {
		for _, port := range network.Ports {
			if port.Port == 22 && !port.Open {
				report.Add(Finding{
					ID:       "ssh-port-closed",
					Severity: SeverityWarning,
					Title:    "SSH port is closed",
					Evidence: []string{"port: 22", "state: " + firstNonEmpty(port.State, port.Error, "closed")},
					Remedy:   "Check the SSH service and firewall rules.",
				})
			}
		}
	}
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return 3
	}
}

func plural(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func reasonDetail(prefix, reason string) string {
	if reason == "" {
		return prefix + "."
	}
	return fmt.Sprintf("%s with reason %s.", prefix, reason)
}

func ageText(start time.Time) string {
	if start.IsZero() {
		return "unknown"
	}
	age := time.Since(start)
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func containerEvidence(container kube.Container) []string {
	evidence := []string{
		"container: " + container.Name,
		"state: " + firstNonEmpty(container.State, "unknown"),
		"ready: " + boolText(container.Ready),
	}
	if container.StateReason != "" {
		evidence = append(evidence, "reason: "+container.StateReason)
	}
	return evidence
}

func parseFirstFloat(value string) (float64, bool) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parsePercent(value string) (float64, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "%"))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
