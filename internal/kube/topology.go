package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func buildTopology(ctx context.Context, client kubernetes.Interface, base Pod, raw *corev1.Pod, detail *PodDetail) *Topology {
	topology := &Topology{}
	nodes := map[string]bool{}
	edges := map[string]bool{}
	namespace := base.Namespace
	contextName := base.Context

	addNode := func(node TopologyNode) string {
		if node.ID == "" {
			return ""
		}
		if !nodes[node.ID] {
			nodes[node.ID] = true
			topology.Nodes = append(topology.Nodes, node)
		}
		return node.ID
	}
	addEdge := func(from, to, label string) {
		if from == "" || to == "" || from == to {
			return
		}
		key := from + "\x00" + to + "\x00" + label
		if edges[key] {
			return
		}
		edges[key] = true
		topology.Edges = append(topology.Edges, TopologyEdge{From: from, To: to, Label: label})
	}
	addWarning := func(format string, args ...any) {
		topology.Warnings = append(topology.Warnings, fmt.Sprintf(format, args...))
	}

	podID := addNode(TopologyNode{
		ID:       "pod/" + namespace + "/" + raw.Name,
		Kind:     "Pod",
		Name:     raw.Name,
		Subtitle: strings.TrimSpace(base.OwnerKind + " " + base.OwnerName),
		Status:   firstNonEmpty(base.Phase, base.Reason, "Unknown"),
		Command:  commandFor(contextName, namespace, "pod", raw.Name, "get", "-o", "yaml"),
		Facts: []TopologyFact{
			{Key: "Namespace", Value: namespace},
			{Key: "IP", Value: firstNonEmpty(base.PodIP, "—")},
			{Key: "Node", Value: firstNonEmpty(base.Node, "—")},
			{Key: "Restarts", Value: fmt.Sprintf("%d", base.Restarts)},
			{Key: "QoS", Value: firstNonEmpty(detail.QoSClass, "—")},
		},
	})

	// Controller chain, closest owner first.
	parentID := podID
	for _, owner := range detail.OwnerChain {
		if owner.Kind == "" || owner.Name == "" {
			continue
		}
		ownerID := addNode(TopologyNode{
			ID:       "owner/" + strings.ToLower(owner.Kind) + "/" + namespace + "/" + owner.Name,
			Kind:     owner.Kind,
			Name:     owner.Name,
			Subtitle: "owner",
			Status:   "owner",
			Command:  commandFor(contextName, namespace, strings.ToLower(owner.Kind), owner.Name, "describe"),
			Facts: []TopologyFact{
				{Key: "Kind", Value: owner.Kind},
				{Key: "Namespace", Value: namespace},
				{Key: "UID", Value: firstNonEmpty(owner.UID, "—")},
			},
		})
		addEdge(ownerID, parentID, "owns")
		parentID = ownerID
	}

	// PersistentVolumeClaims mounted by the pod.
	claimNames := map[string]bool{}
	for _, volume := range raw.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName != "" {
			claimNames[volume.PersistentVolumeClaim.ClaimName] = true
		}
	}
	if len(claimNames) > 0 {
		claims, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			addWarning("pvc: %v", err)
		} else {
			for i := range claims.Items {
				claim := &claims.Items[i]
				if !claimNames[claim.Name] {
					continue
				}
				capacity := ""
				if quantity, ok := claim.Status.Capacity[corev1.ResourceStorage]; ok {
					capacity = quantity.String()
				}
				claimID := addNode(TopologyNode{
					ID:       "pvc/" + namespace + "/" + claim.Name,
					Kind:     "PersistentVolumeClaim",
					Name:     claim.Name,
					Subtitle: firstNonEmpty(stringValue(claim.Spec.StorageClassName), claim.Spec.VolumeName, "claim"),
					Status:   firstNonEmpty(string(claim.Status.Phase), "Unknown"),
					Command:  commandFor(contextName, namespace, "persistentvolumeclaim", claim.Name, "describe"),
					Facts: []TopologyFact{
						{Key: "Storage class", Value: firstNonEmpty(stringValue(claim.Spec.StorageClassName), "—")},
						{Key: "Capacity", Value: firstNonEmpty(capacity, "—")},
						{Key: "Volume", Value: firstNonEmpty(claim.Spec.VolumeName, "—")},
						{Key: "Access modes", Value: accessModes(claim.Spec.AccessModes)},
					},
				})
				addEdge(claimID, podID, "mounts")
			}
		}
	}

	// Services that select this pod.
	serviceIDs := map[string]string{}
	serviceSliced := map[string]bool{}
	services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		addWarning("services: %v", err)
	} else {
		for i := range services.Items {
			service := &services.Items[i]
			if !selectorMatches(service.Spec.Selector, raw.Labels) {
				continue
			}
			serviceID := addNode(TopologyNode{
				ID:       "service/" + namespace + "/" + service.Name,
				Kind:     "Service",
				Name:     service.Name,
				Subtitle: string(service.Spec.Type),
				Status:   string(service.Spec.Type),
				Command:  commandFor(contextName, namespace, "service", service.Name, "describe"),
				Facts: []TopologyFact{
					{Key: "Cluster IP", Value: firstNonEmpty(service.Spec.ClusterIP, "—")},
					{Key: "Ports", Value: servicePorts(service.Spec.Ports)},
					{Key: "Selector", Value: selectorText(service.Spec.Selector)},
				},
			})
			serviceIDs[service.Name] = serviceID
		}
	}

	// EndpointSlices for selected services.
	if len(serviceIDs) > 0 {
		slices, err := client.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			addWarning("endpointslices: %v", err)
		} else {
			for i := range slices.Items {
				slice := &slices.Items[i]
				serviceName := slice.Labels["kubernetes.io/service-name"]
				serviceID, ok := serviceIDs[serviceName]
				if !ok {
					continue
				}
				sliceID := addNode(TopologyNode{
					ID:       "endpointslice/" + namespace + "/" + slice.Name,
					Kind:     "EndpointSlice",
					Name:     slice.Name,
					Subtitle: serviceName,
					Status:   endpointSliceStatus(slice),
					Command:  commandFor(contextName, namespace, "endpointslice", slice.Name, "describe"),
					Facts: []TopologyFact{
						{Key: "Service", Value: serviceName},
						{Key: "Address type", Value: firstNonEmpty(string(slice.AddressType), "—")},
						{Key: "Ports", Value: endpointPorts(slice.Ports)},
						{Key: "Endpoints", Value: endpointText(slice.Endpoints)},
					},
				})
				addEdge(serviceID, sliceID, "routes")
				serviceSliced[serviceName] = true
				for _, endpoint := range slice.Endpoints {
					if endpointReferencesPod(endpoint, raw, base) {
						label := "endpoint"
						if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
							label = "ready"
						}
						addEdge(sliceID, podID, label)
					}
				}
			}
		}
		for name, serviceID := range serviceIDs {
			if !serviceSliced[name] {
				addEdge(serviceID, podID, "selects")
			}
		}
	}

	// Ingresses routing to selected services.
	if len(serviceIDs) > 0 {
		ingresses, err := client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			addWarning("ingresses: %v", err)
		} else {
			for i := range ingresses.Items {
				ingress := &ingresses.Items[i]
				backends := ingressServices(ingress)
				var linked []string
				for _, serviceName := range backends {
					if serviceID, ok := serviceIDs[serviceName]; ok {
						addEdge("ingress/"+namespace+"/"+ingress.Name, serviceID, "routes")
						linked = append(linked, serviceName)
					}
				}
				if len(linked) == 0 {
					continue
				}
				addNode(TopologyNode{
					ID:       "ingress/" + namespace + "/" + ingress.Name,
					Kind:     "Ingress",
					Name:     ingress.Name,
					Subtitle: firstNonEmpty(ingressClassName(ingress), "ingress"),
					Status:   "routes",
					Command:  commandFor(contextName, namespace, "ingress", ingress.Name, "describe"),
					Facts: []TopologyFact{
						{Key: "Class", Value: firstNonEmpty(ingressClassName(ingress), "—")},
						{Key: "Hosts", Value: ingressHosts(ingress)},
						{Key: "Services", Value: strings.Join(linked, ", ")},
					},
				})
			}
		}
	}

	if detail.Node != nil {
		nodeID := addNode(TopologyNode{
			ID:       "node/" + detail.Node.Name,
			Kind:     "Node",
			Name:     detail.Node.Name,
			Subtitle: firstNonEmpty(detail.Node.Hostname, detail.Node.InternalIP),
			Status:   map[bool]string{true: "Ready", false: "NotReady"}[detail.Node.Ready],
			Command:  commandFor(contextName, "", "node", detail.Node.Name, "describe"),
			Facts: []TopologyFact{
				{Key: "Internal IP", Value: firstNonEmpty(detail.Node.InternalIP, "—")},
				{Key: "OS", Value: firstNonEmpty(detail.Node.OperatingSystem, "—")},
				{Key: "Kernel", Value: firstNonEmpty(detail.Node.KernelVersion, "—")},
				{Key: "Runtime", Value: firstNonEmpty(detail.Node.ContainerRuntime, "—")},
			},
		})
		addEdge(podID, nodeID, "runs on")
	}

	sort.SliceStable(topology.Nodes, func(i, j int) bool {
		if topology.Nodes[i].Kind == topology.Nodes[j].Kind {
			return topology.Nodes[i].Name < topology.Nodes[j].Name
		}
		return topology.Nodes[i].Kind < topology.Nodes[j].Kind
	})
	if len(topology.Nodes) <= 1 {
		return nil
	}
	return topology
}

func selectorMatches(selector map[string]string, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func selectorText(selector map[string]string) string {
	if len(selector) == 0 {
		return "—"
	}
	keys := make([]string, 0, len(selector))
	for key := range selector {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+selector[key])
	}
	return strings.Join(parts, ", ")
}

func servicePorts(ports []corev1.ServicePort) string {
	if len(ports) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		value := fmt.Sprintf("%d", port.Port)
		if port.Name != "" {
			value = port.Name + ":" + value
		}
		if port.Protocol != "" {
			value += "/" + string(port.Protocol)
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, ", ")
}

func endpointPorts(ports []discoveryv1.EndpointPort) string {
	if len(ports) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		if port.Port == nil {
			continue
		}
		value := fmt.Sprintf("%d", *port.Port)
		if port.Name != nil && *port.Name != "" {
			value = *port.Name + ":" + value
		}
		if port.Protocol != nil {
			value += "/" + string(*port.Protocol)
		}
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

func endpointText(endpoints []discoveryv1.Endpoint) string {
	if len(endpoints) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		ready := "?"
		if endpoint.Conditions.Ready != nil {
			ready = map[bool]string{true: "ready", false: "not ready"}[*endpoint.Conditions.Ready]
		}
		parts = append(parts, strings.Join(endpoint.Addresses, ",")+" ("+ready+")")
	}
	return strings.Join(parts, ", ")
}

func endpointSliceStatus(slice *discoveryv1.EndpointSlice) string {
	total := len(slice.Endpoints)
	ready := 0
	for _, endpoint := range slice.Endpoints {
		if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
			ready++
		}
	}
	return fmt.Sprintf("%d/%d ready", ready, total)
}

func endpointReferencesPod(endpoint discoveryv1.Endpoint, raw *corev1.Pod, base Pod) bool {
	if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" && endpoint.TargetRef.Name == raw.Name {
		return true
	}
	ips := map[string]bool{base.PodIP: true}
	for _, ip := range raw.Status.PodIPs {
		ips[ip.IP] = true
	}
	for _, address := range endpoint.Addresses {
		if ips[address] {
			return true
		}
	}
	return false
}

func ingressServices(ingress *networkingv1.Ingress) []string {
	set := map[string]bool{}
	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
		set[ingress.Spec.DefaultBackend.Service.Name] = true
	}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				set[path.Backend.Service.Name] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func ingressClassName(ingress *networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName != nil {
		return *ingress.Spec.IngressClassName
	}
	return ""
}

func ingressHosts(ingress *networkingv1.Ingress) string {
	var hosts []string
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	if len(hosts) == 0 {
		return "—"
	}
	return strings.Join(hosts, ", ")
}

func accessModes(modes []corev1.PersistentVolumeAccessMode) string {
	if len(modes) == 0 {
		return "—"
	}
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		out = append(out, string(mode))
	}
	return strings.Join(out, ", ")
}

func commandFor(contextName, namespace, kind, name string, verbs ...string) string {
	parts := []string{"kubectl"}
	if contextName != "" {
		parts = append(parts, "--context", shellQuoteKube(contextName))
	}
	if namespace != "" {
		parts = append(parts, "-n", shellQuoteKube(namespace))
	}
	parts = append(parts, verbs...)
	parts = append(parts, kind, shellQuoteKube(name))
	return strings.Join(parts, " ")
}

func shellQuoteKube(value string) string {
	if value == "" {
		return "''"
	}
	safe := true
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune("-_./:@%+=,", r) {
			safe = false
			break
		}
	}
	if safe {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
