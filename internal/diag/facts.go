package diag

import (
	"time"

	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

func PodFacts(detail kube.PodDetail, stats *kube.PodStats) map[string]any {
	pod := detail.Pod
	labels := make(map[string]any, len(detail.Labels))
	for key, value := range detail.Labels {
		labels[key] = value
	}
	annotations := make(map[string]any, len(detail.Annotations))
	for key, value := range detail.Annotations {
		annotations[key] = value
	}
	conditions := make(map[string]any, len(detail.Conditions))
	for _, condition := range detail.Conditions {
		conditions[condition.Type] = map[string]any{
			"status":  condition.Status,
			"reason":  condition.Reason,
			"message": condition.Message,
		}
	}
	containers := make([]any, 0, len(pod.Containers))
	for _, container := range pod.Containers {
		ports := make([]any, 0, len(container.Ports))
		for _, port := range container.Ports {
			ports = append(ports, map[string]any{
				"name":           port.Name,
				"container_port": port.ContainerPort,
				"protocol":       port.Protocol,
			})
		}
		containers = append(containers, map[string]any{
			"name":         container.Name,
			"image":        container.Image,
			"ready":        container.Ready,
			"restarts":     container.Restarts,
			"state":        container.State,
			"state_reason": container.StateReason,
			"ports":        ports,
		})
	}
	images := make([]any, 0, len(pod.Images))
	for _, image := range pod.Images {
		images = append(images, image)
	}
	facts := map[string]any{
		"pod.context":          pod.Context,
		"pod.cluster":          pod.Cluster,
		"pod.env":              pod.Env,
		"pod.display":          pod.Display,
		"pod.namespace":        pod.Namespace,
		"pod.name":             pod.Name,
		"pod.uid":              pod.UID,
		"pod.phase":            pod.Phase,
		"pod.reason":           pod.Reason,
		"pod.ready":            pod.Ready,
		"pod.ready_containers": pod.ReadyContainers,
		"pod.containers_count": pod.ContainersCount,
		"pod.restarts":         pod.Restarts,
		"pod.node":             pod.Node,
		"pod.pod_ip":           pod.PodIP,
		"pod.host_ip":          pod.HostIP,
		"pod.qos_class":        pod.QoSClass,
		"pod.owner_kind":       pod.OwnerKind,
		"pod.owner_name":       pod.OwnerName,
		"pod.workload":         pod.Workload,
		"pod.service_account":  pod.ServiceAccount,
		"pod.cpu_request":      pod.CPURequest,
		"pod.cpu_limit":        pod.CPULimit,
		"pod.memory_request":   pod.MemoryRequest,
		"pod.memory_limit":     pod.MemoryLimit,
		"pod.start_time":       pod.StartTime,
		"pod.age_seconds":      ageSeconds(pod.StartTime),
		"pod.labels":           labels,
		"pod.annotations":      annotations,
		"pod.conditions":       conditions,
		"pod.containers":       containers,
		"pod.images":           images,
	}
	if len(pod.Images) > 0 {
		facts["pod.image"] = pod.Images[0]
	}
	if stats != nil {
		facts["stats.cpu_millicores"] = stats.CPU
		facts["stats.memory_bytes"] = stats.Memory
		facts["stats.working_set_bytes"] = stats.WorkingSet
		facts["stats.window"] = stats.Window
		facts["stats.source"] = stats.Source
		facts["stats.warnings"] = toAnyStrings(stats.Warnings)
		if pod.MemoryLimit > 0 && stats.WorkingSet > 0 {
			facts["stats.memory_limit_percent"] = float64(stats.WorkingSet) / float64(pod.MemoryLimit) * 100
		}
		if stats.Node != nil {
			facts["stats.node"] = map[string]any{
				"name":             stats.Node.Name,
				"ready":            stats.Node.Ready,
				"cpu_millicores":   stats.Node.CPU,
				"cpu_capacity":     stats.Node.CPUCapacity,
				"memory_bytes":     stats.Node.Memory,
				"memory_capacity":  stats.Node.MemoryCapacity,
				"memory_available": stats.Node.MemoryAvailable,
				"working_set":      stats.Node.WorkingSetBytes,
				"pod_capacity":     stats.Node.PodCapacity,
			}
		}
	}
	if detail.Node != nil {
		facts["pod.node_info"] = map[string]any{
			"name":        detail.Node.Name,
			"ready":       detail.Node.Ready,
			"hostname":    detail.Node.Hostname,
			"internal_ip": detail.Node.InternalIP,
			"os":          detail.Node.OperatingSystem,
			"kernel":      detail.Node.KernelVersion,
			"runtime":     detail.Node.ContainerRuntime,
		}
	}
	if detail.Topology != nil {
		services := []any{}
		ingresses := []any{}
		pvcs := map[string]any{}
		endpoints := map[string]any{}
		for _, node := range detail.Topology.Nodes {
			switch node.Kind {
			case "Service":
				services = append(services, node.Name)
			case "Ingress":
				ingresses = append(ingresses, node.Name)
			case "PersistentVolumeClaim":
				pvcs[node.Name] = node.Status
			case "EndpointSlice":
				endpoints[node.Subtitle] = node.Status
			}
		}
		facts["pod.services"] = services
		facts["pod.ingresses"] = ingresses
		facts["pod.pvc_statuses"] = pvcs
		facts["pod.endpoint_slices"] = endpoints
		facts["pod.topology_warnings"] = toAnyStrings(detail.Topology.Warnings)
	}
	warningCount := 0
	reasons := []any{}
	seenReasons := map[string]bool{}
	for _, event := range detail.Events {
		if event.Type != "Warning" {
			continue
		}
		warningCount++
		if event.Reason != "" && !seenReasons[event.Reason] {
			seenReasons[event.Reason] = true
			reasons = append(reasons, event.Reason)
		}
	}
	facts["pod.warning_event_count"] = warningCount
	facts["pod.warning_event_reasons"] = reasons
	return facts
}

func ServerFacts(server servers.Server, check servers.Check) map[string]any {
	tags := make([]any, 0, len(server.Tags))
	for _, tag := range server.Tags {
		tags = append(tags, tag)
	}
	ports := make([]any, 0, len(server.CheckPorts))
	for _, port := range server.CheckPorts {
		ports = append(ports, port)
	}
	failedUnits := toAnyStrings(check.Machine.FailedUnits)
	networkPorts := make([]any, 0, len(check.Network.Ports))
	openPorts := []any{}
	closedPorts := []any{}
	for _, port := range check.Network.Ports {
		networkPorts = append(networkPorts, map[string]any{
			"port":       port.Port,
			"open":       port.Open,
			"state":      port.State,
			"latency_ms": port.LatencyMS,
			"error":      port.Error,
		})
		if port.Open {
			openPorts = append(openPorts, port.Port)
		} else {
			closedPorts = append(closedPorts, port.Port)
		}
	}
	machine := map[string]any{
		"ok":               check.Machine.OK,
		"hostname":         check.Machine.Hostname,
		"os":               check.Machine.OS,
		"kernel":           check.Machine.Kernel,
		"uptime":           check.Machine.Uptime,
		"load":             check.Machine.Load,
		"cpus":             check.Machine.CPUs,
		"memory_total":     check.Machine.MemoryTotal,
		"memory_used":      check.Machine.MemoryUsed,
		"memory_available": check.Machine.MemoryAvailable,
		"disk_total":       check.Machine.DiskTotal,
		"disk_used":        check.Machine.DiskUsed,
		"disk_free":        check.Machine.DiskFree,
		"disk_percent":     check.Machine.DiskPercent,
		"failed_units":     failedUnits,
		"error":            check.Machine.Error,
	}
	facts := map[string]any{
		"vm.name":           server.Name,
		"vm.ssh":            server.SSH,
		"vm.host":           server.Host,
		"vm.user":           server.User,
		"vm.port":           server.Port,
		"vm.proxy_jump":     server.ProxyJump,
		"vm.env":            server.Env,
		"vm.tags":           tags,
		"vm.source":         server.Source,
		"vm.ssh_checks":     server.SSHChecks,
		"vm.network_checks": server.NetworkChecks,
		"vm.check_ports":    ports,
		"vm.machine":        machine,
		"vm.health": map[string]any{
			"url":        check.Health.URL,
			"ok":         check.Health.OK,
			"status":     check.Health.StatusCode,
			"latency_ms": check.Health.LatencyMS,
			"error":      check.Health.Error,
		},
		"vm.metrics": map[string]any{
			"url":        check.Metrics.URL,
			"ok":         check.Metrics.OK,
			"status":     check.Metrics.StatusCode,
			"latency_ms": check.Metrics.LatencyMS,
			"error":      check.Metrics.Error,
		},
		"vm.network": map[string]any{
			"target":       check.Network.Target,
			"checked":      check.Network.Checked,
			"reachable":    check.Network.Reachable,
			"ping_ok":      check.Network.Ping.OK,
			"ping_latency": check.Network.Ping.LatencyMS,
			"ping_error":   check.Network.Ping.Error,
			"ports":        networkPorts,
			"open_ports":   openPorts,
			"closed_ports": closedPorts,
		},
		"vm.failed_unit_count": len(check.Machine.FailedUnits),
	}
	if check.Machine.MemoryTotal > 0 {
		facts["vm.memory_percent"] = float64(check.Machine.MemoryUsed) / float64(check.Machine.MemoryTotal) * 100
	}
	if check.Machine.DiskTotal > 0 {
		percent := float64(check.Machine.DiskUsed) / float64(check.Machine.DiskTotal) * 100
		if parsed, ok := parsePercent(check.Machine.DiskPercent); ok {
			percent = parsed
		}
		facts["vm.disk_percent"] = percent
	}
	if check.Machine.CPUs > 0 {
		if load, ok := parseFirstFloat(check.Machine.Load); ok {
			facts["vm.load_per_cpu"] = load / float64(check.Machine.CPUs)
		}
	}
	return facts
}

func ageSeconds(start time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	age := time.Since(start)
	if age < 0 {
		return 0
	}
	return int64(age.Seconds())
}

func toAnyStrings(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
