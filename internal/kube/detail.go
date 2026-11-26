package kube

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

func (p *ClientGoProvider) buildDetail(ctx context.Context, client kubernetes.Interface, base Pod, raw *corev1.Pod, ref PodRef) *PodDetail {
	detail := &PodDetail{
		Pod:               base,
		Labels:            copyMap(raw.Labels),
		Annotations:       copyMap(raw.Annotations),
		NodeSelectors:     copyMap(raw.Spec.NodeSelector),
		ServiceAccount:    raw.Spec.ServiceAccountName,
		PriorityClassName: raw.Spec.PriorityClassName,
		QoSClass:          string(raw.Status.QOSClass),
		HostIP:            raw.Status.HostIP,
		PodIP:             raw.Status.PodIP,
		FetchedAt:         time.Now(),
	}
	for _, condition := range raw.Status.Conditions {
		detail.Conditions = append(detail.Conditions, Condition{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time,
		})
	}
	for _, ip := range raw.Status.PodIPs {
		detail.PodIPs = append(detail.PodIPs, ip.IP)
	}
	for _, ref := range raw.OwnerReferences {
		detail.OwnerChain = append(detail.OwnerChain, OwnerRef{Kind: ref.Kind, Name: ref.Name, UID: string(ref.UID)})
	}
	if len(raw.OwnerReferences) > 0 && raw.OwnerReferences[0].Kind == "ReplicaSet" {
		if rs, err := client.AppsV1().ReplicaSets(ref.Namespace).Get(ctx, raw.OwnerReferences[0].Name, metav1.GetOptions{}); err == nil {
			for _, ref := range rs.OwnerReferences {
				detail.OwnerChain = append(detail.OwnerChain, OwnerRef{Kind: ref.Kind, Name: ref.Name, UID: string(ref.UID)})
			}
		}
	}
	for _, volume := range raw.Spec.Volumes {
		detail.Volumes = append(detail.Volumes, Volume{Name: volume.Name, Type: volumeType(volume)})
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		events, err := client.CoreV1().Events(ref.Namespace).List(ctx, metav1.ListOptions{
			FieldSelector: eventFieldSelector(ref),
		})
		if err == nil {
			detail.Events = convertEvents(events.Items)
		}
	}()
	if raw.Spec.NodeName != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			node, err := fetchNode(ctx, client, raw.Spec.NodeName)
			if err != nil {
				detail.NodeError = err.Error()
				return
			}
			detail.Node = node
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		detail.Topology = buildTopology(ctx, client, base, raw, detail)
	}()
	wg.Wait()

	if data, err := yaml.Marshal(raw); err == nil {
		detail.YAML = string(data)
	}
	detail.Describe = buildDescribe(base, raw, detail.Events)
	return detail
}

func convertEvents(items []corev1.Event) []Event {
	events := make([]Event, 0, len(items))
	for i := range items {
		e := &items[i]
		first := e.FirstTimestamp.Time
		if first.IsZero() {
			first = e.EventTime.Time
		}
		if first.IsZero() {
			first = e.CreationTimestamp.Time
		}
		last := e.LastTimestamp.Time
		if last.IsZero() {
			last = e.EventTime.Time
		}
		if last.IsZero() {
			last = first
		}
		count := e.Count
		if e.Series != nil {
			if count == 0 {
				count = e.Series.Count
			}
			if !e.Series.LastObservedTime.IsZero() {
				last = e.Series.LastObservedTime.Time
			}
		}
		source := e.Source.Component
		if source == "" {
			source = e.ReportingController
		}
		if source == "" {
			source = e.Source.Host
		}
		events = append(events, Event{
			Type:      e.Type,
			Reason:    e.Reason,
			Message:   e.Message,
			Source:    source,
			Count:     count,
			FirstSeen: first,
			LastSeen:  last,
		})
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].LastSeen.After(events[j].LastSeen)
	})
	if len(events) > 50 {
		events = events[:50]
	}
	return events
}

func volumeType(volume corev1.Volume) string {
	value := reflect.ValueOf(volume.VolumeSource)
	typ := value.Type()
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.Kind() == reflect.Ptr && !field.IsNil() {
			return typ.Field(i).Name
		}
		if field.Kind() == reflect.Map && !field.IsNil() {
			return typ.Field(i).Name
		}
	}
	return "unknown"
}
