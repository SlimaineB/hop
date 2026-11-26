package kube

import (
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	utilduration "k8s.io/apimachinery/pkg/util/duration"
)

func buildDescribe(pod Pod, raw *corev1.Pod, events []Event) string {
	var b strings.Builder
	line := func(label, value string) {
		fmt.Fprintf(&b, "%-22s%s\n", label+":", value)
	}
	lineIf := func(label, value string) {
		if value != "" {
			line(label, value)
		}
	}

	line("Name", pod.Name)
	line("Namespace", pod.Namespace)
	lineIf("Priority", raw.Spec.PriorityClassName)
	line("Node", pod.Node)
	line("Start Time", formatTime(pod.StartTime))
	if len(pod.Labels) > 0 {
		writeMap(&b, "Labels", pod.Labels)
	}
	if annotations := filteredAnnotations(raw.Annotations); len(annotations) > 0 {
		writeMap(&b, "Annotations", annotations)
	}
	line("Status", pod.Phase)
	lineIf("Reason", pod.Reason)
	lineIf("IP", pod.PodIP)
	lineIf("Host IP", pod.HostIP)
	if len(raw.Status.PodIPs) > 1 {
		ips := make([]string, 0, len(raw.Status.PodIPs))
		for _, ip := range raw.Status.PodIPs {
			ips = append(ips, ip.IP)
		}
		line("IPs", strings.Join(ips, ", "))
	}
	if len(raw.OwnerReferences) > 0 {
		owners := make([]string, 0, len(raw.OwnerReferences))
		for _, owner := range raw.OwnerReferences {
			owners = append(owners, owner.Kind+"/"+owner.Name)
		}
		line("Controlled By", strings.Join(owners, ", "))
	}
	lineIf("Service Account", pod.ServiceAccount)
	lineIf("QoS Class", pod.QoSClass)

	b.WriteString("Containers:\n")
	for _, container := range raw.Spec.Containers {
		status := containerStatus(raw, container.Name)
		fmt.Fprintf(&b, "  %s:\n", container.Name)
		if status != nil {
			fmt.Fprintf(&b, "    Container ID:  %s\n", status.ContainerID)
		}
		fmt.Fprintf(&b, "    Image:         %s\n", container.Image)
		if status != nil {
			fmt.Fprintf(&b, "    Image ID:      %s\n", status.ImageID)
		}
		for _, port := range container.Ports {
			fmt.Fprintf(&b, "    Port:          %d/%s\n", port.ContainerPort, port.Protocol)
			fmt.Fprintf(&b, "    Host Port:     %d/%s\n", port.HostPort, port.Protocol)
		}
		if status != nil {
			writeContainerState(&b, status)
			fmt.Fprintf(&b, "    Ready:         %t\n", status.Ready)
			fmt.Fprintf(&b, "    Restart Count: %d\n", status.RestartCount)
		}
		if len(container.Env) == 0 {
			fmt.Fprintf(&b, "    Environment:   <none>\n")
		} else {
			fmt.Fprintf(&b, "    Environment:\n")
			for _, env := range container.Env {
				value := env.Value
				if env.ValueFrom != nil {
					value = "<valueFrom>"
				}
				fmt.Fprintf(&b, "      %s:  %s\n", env.Name, value)
			}
		}
		if len(container.VolumeMounts) == 0 {
			fmt.Fprintf(&b, "    Mounts:        <none>\n")
		} else {
			fmt.Fprintf(&b, "    Mounts:\n")
			for _, mount := range container.VolumeMounts {
				fmt.Fprintf(&b, "      %s from %s (%s)\n", mount.MountPath, mount.Name, mountSubPath(mount))
			}
		}
	}

	if len(raw.Status.Conditions) > 0 {
		b.WriteString("Conditions:\n")
		fmt.Fprintf(&b, "  %-20s %-8s %-24s %s\n", "Type", "Status", "Reason", "Message")
		for _, condition := range raw.Status.Conditions {
			message := condition.Message
			if len(message) > 120 {
				message = message[:117] + "..."
			}
			fmt.Fprintf(&b, "  %-20s %-8s %-24s %s\n", condition.Type, condition.Status, condition.Reason, message)
		}
	}

	if len(raw.Spec.Volumes) > 0 {
		b.WriteString("Volumes:\n")
		for _, volume := range raw.Spec.Volumes {
			fmt.Fprintf(&b, "  %s:\n", volume.Name)
			fmt.Fprintf(&b, "    Type:  %s\n", volumeType(volume))
		}
	}

	if len(raw.Spec.NodeSelector) > 0 {
		writeMap(&b, "Node-Selectors", raw.Spec.NodeSelector)
	}
	if len(raw.Spec.Tolerations) > 0 {
		b.WriteString("Tolerations:\n")
		for _, toleration := range raw.Spec.Tolerations {
			fmt.Fprintf(&b, "  %s%s:%s\n", toleration.Key, toleration.Operator, toleration.Effect)
		}
	}

	b.WriteString("Events:\n")
	if len(events) == 0 {
		b.WriteString("  <none>\n")
	} else {
		fmt.Fprintf(&b, "  %-8s %-22s %-10s %-24s %s\n", "Type", "Reason", "Age", "From", "Message")
		fmt.Fprintf(&b, "  %-8s %-22s %-10s %-24s %s\n", "----", "------", "---", "----", "-------")
		for _, event := range events {
			message := event.Message
			if len(message) > 160 {
				message = message[:157] + "..."
			}
			source := event.Source
			if len(source) > 24 {
				source = source[:21] + "..."
			}
			fmt.Fprintf(&b, "  %-8s %-22s %-10s %-24s %s\n",
				event.Type, event.Reason, eventAge(event.LastSeen), source, message)
		}
	}
	return b.String()
}

func writeMap(b *strings.Builder, label string, values map[string]string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		if i == 0 {
			fmt.Fprintf(b, "%-21s %s=%s\n", label+":", key, values[key])
			continue
		}
		fmt.Fprintf(b, "%-21s %s=%s\n", "", key, values[key])
	}
}

func filteredAnnotations(annotations map[string]string) map[string]string {
	if len(annotations) == 0 {
		return nil
	}
	out := make(map[string]string, len(annotations))
	for key, value := range annotations {
		if key == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		}
		if len(value) > 1000 {
			value = value[:997] + "..."
		}
		out[key] = value
	}
	return out
}

func writeContainerState(b *strings.Builder, status *corev1.ContainerStatus) {
	switch {
	case status.State.Running != nil:
		fmt.Fprintf(b, "    State:         Running\n")
		fmt.Fprintf(b, "      Started:     %s\n", formatTime(status.State.Running.StartedAt.Time))
	case status.State.Waiting != nil:
		fmt.Fprintf(b, "    State:         Waiting\n")
		fmt.Fprintf(b, "      Reason:      %s\n", status.State.Waiting.Reason)
		if status.State.Waiting.Message != "" {
			fmt.Fprintf(b, "      Message:     %s\n", status.State.Waiting.Message)
		}
	case status.State.Terminated != nil:
		fmt.Fprintf(b, "    State:         Terminated\n")
		fmt.Fprintf(b, "      Reason:      %s\n", status.State.Terminated.Reason)
		fmt.Fprintf(b, "      Exit Code:   %d\n", status.State.Terminated.ExitCode)
		fmt.Fprintf(b, "      Started:     %s\n", formatTime(status.State.Terminated.StartedAt.Time))
		fmt.Fprintf(b, "      Finished:    %s\n", formatTime(status.State.Terminated.FinishedAt.Time))
	default:
		fmt.Fprintf(b, "    State:         <unknown>\n")
	}
}

func containerStatus(pod *corev1.Pod, name string) *corev1.ContainerStatus {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == name {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
}

func mountSubPath(mount corev1.VolumeMount) string {
	if mount.SubPath == "" {
		return "rw"
	}
	return "rw, subPath=" + mount.SubPath
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	return t.Local().Format("2006-01-02 15:04:05 -0700")
}

func eventAge(t time.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	return utilduration.HumanDuration(time.Since(t))
}
