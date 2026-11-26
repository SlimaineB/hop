package kube

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/SlimaineB/hop/internal/glob"
	"github.com/SlimaineB/hop/internal/version"
)

type Options struct {
	Kubeconfigs     []string
	IncludeContexts []string
	ExcludeContexts []string
	RequestTimeout  time.Duration
}

type ClientGoProvider struct {
	rules   *clientcmd.ClientConfigLoadingRules
	options Options

	mu             sync.Mutex
	contextsLoaded bool
	contexts       []Context
	contextsErr    error
	clients        map[string]*kubernetes.Clientset
}

func NewClientGo(opts Options) *ClientGoProvider {
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 10 * time.Second
	}
	return &ClientGoProvider{
		rules:   buildLoadingRules(opts.Kubeconfigs),
		options: opts,
		clients: map[string]*kubernetes.Clientset{},
	}
}

func buildLoadingRules(paths []string) *clientcmd.ClientConfigLoadingRules {
	switch len(paths) {
	case 0:
		return clientcmd.NewDefaultClientConfigLoadingRules()
	case 1:
		return &clientcmd.ClientConfigLoadingRules{ExplicitPath: paths[0]}
	default:
		return &clientcmd.ClientConfigLoadingRules{Precedence: paths}
	}
}

func (p *ClientGoProvider) Contexts(ctx context.Context) ([]Context, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.contextsLoaded {
		out := make([]Context, len(p.contexts))
		copy(out, p.contexts)
		return out, p.contextsErr
	}
	p.contextsLoaded = true

	raw, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		p.rules,
		&clientcmd.ConfigOverrides{},
	).RawConfig()
	if err != nil {
		p.contextsErr = fmt.Errorf("load kubeconfig: %w", err)
		return nil, p.contextsErr
	}

	contexts := make([]Context, 0, len(raw.Contexts))
	for name, kubeCtx := range raw.Contexts {
		if len(p.options.IncludeContexts) > 0 && !glob.MatchAny(p.options.IncludeContexts, name) {
			continue
		}
		if glob.MatchAny(p.options.ExcludeContexts, name) {
			continue
		}
		cluster := ""
		user := ""
		if kubeCtx != nil {
			cluster = kubeCtx.Cluster
			user = kubeCtx.AuthInfo
		}
		contexts = append(contexts, Context{Name: name, Cluster: cluster, User: user})
	}
	sort.Slice(contexts, func(i, j int) bool { return contexts[i].Name < contexts[j].Name })

	p.contexts = contexts
	if len(contexts) == 0 {
		p.contextsErr = fmt.Errorf("no Kubernetes context found (kubeconfig: %s)", p.rules.GetDefaultFilename())
	}
	return contexts, p.contextsErr
}

func (p *ClientGoProvider) client(name string) (*kubernetes.Clientset, error) {
	p.mu.Lock()
	if client, ok := p.clients[name]; ok {
		p.mu.Unlock()
		return client, nil
	}
	p.mu.Unlock()

	overrides := clientcmd.ConfigOverrides{CurrentContext: name}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(p.rules, &overrides)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("context %s: %w", name, err)
	}
	restConfig.Timeout = p.options.RequestTimeout
	restConfig.UserAgent = "hop/" + version.Version
	restConfig.QPS = 50
	restConfig.Burst = 100

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("context %s: %w", name, err)
	}

	p.mu.Lock()
	if existing, ok := p.clients[name]; ok {
		p.mu.Unlock()
		return existing, nil
	}
	p.clients[name] = client
	p.mu.Unlock()
	return client, nil
}

func (p *ClientGoProvider) ListPods(ctx context.Context, opts ListOptions) (*ListResult, error) {
	contexts, err := p.Contexts(ctx)
	if err != nil && len(contexts) == 0 {
		return nil, err
	}

	selected := contexts
	if len(opts.Contexts) > 0 {
		selected = selected[:0]
		for _, c := range contexts {
			if glob.MatchAny(opts.Contexts, c.Name) {
				selected = append(selected, c)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("no context matches %v", opts.Contexts)
		}
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 12
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = p.options.RequestTimeout
	}

	result := &ListResult{Contexts: contexts, FetchedAt: time.Now()}
	start := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)

	for _, clusterCtx := range selected {
		wg.Add(1)
		go func(c Context) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				result.Errors = append(result.Errors, ContextError{Context: c.Name, Error: ctx.Err().Error()})
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			requestCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			pods, err := p.listContext(requestCtx, c, opts)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, ContextError{Context: c.Name, Error: err.Error()})
				return
			}
			result.Pods = append(result.Pods, pods...)
		}(clusterCtx)
	}
	wg.Wait()

	result.Duration = time.Since(start)
	sort.Slice(result.Pods, func(i, j int) bool {
		a, b := result.Pods[i], result.Pods[j]
		if a.Context != b.Context {
			return a.Context < b.Context
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return result, nil
}

func (p *ClientGoProvider) listContext(ctx context.Context, c Context, opts ListOptions) ([]Pod, error) {
	client, err := p.client(c.Name)
	if err != nil {
		return nil, err
	}
	namespaces := opts.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}

	var pods []Pod
	for _, namespace := range namespaces {
		rawPods, err := listPodsInNamespace(ctx, client, namespace)
		if err != nil {
			return pods, fmt.Errorf("namespace %s: %w", namespace, err)
		}
		for i := range rawPods {
			raw := &rawPods[i]
			if glob.MatchAny(opts.ExcludeNamespaces, raw.Namespace) {
				continue
			}
			pods = append(pods, convertPod(c, raw))
		}
	}

	if opts.ResolveOwners {
		resolveOwners(ctx, client, pods)
	}
	return pods, nil
}

func listPodsInNamespace(ctx context.Context, client kubernetes.Interface, namespace string) ([]corev1.Pod, error) {
	var pods []corev1.Pod
	listOptions := metav1.ListOptions{Limit: 500}
	for {
		list, err := client.CoreV1().Pods(namespace).List(ctx, listOptions)
		if err != nil {
			return nil, err
		}
		pods = append(pods, list.Items...)
		if list.Continue == "" {
			break
		}
		listOptions.Continue = list.Continue
	}
	return pods, nil
}

func convertPod(c Context, pod *corev1.Pod) Pod {
	statuses := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
	readyContainers := 0
	var restarts int32
	for _, status := range pod.Status.ContainerStatuses {
		statuses[status.Name] = status
		if status.Ready {
			readyContainers++
		}
		restarts += status.RestartCount
	}

	containers := make([]Container, 0, len(pod.Spec.Containers))
	images := make([]string, 0, len(pod.Spec.Containers))
	seenImages := map[string]bool{}
	for _, spec := range pod.Spec.Containers {
		container := Container{Name: spec.Name, Image: spec.Image}
		for _, port := range spec.Ports {
			container.Ports = append(container.Ports, Port{
				Name:          port.Name,
				ContainerPort: port.ContainerPort,
				Protocol:      string(port.Protocol),
			})
		}
		if !seenImages[spec.Image] {
			seenImages[spec.Image] = true
			images = append(images, spec.Image)
		}
		if status, ok := statuses[spec.Name]; ok {
			container.Ready = status.Ready
			container.Restarts = status.RestartCount
			switch {
			case status.State.Running != nil:
				container.State = "running"
			case status.State.Waiting != nil:
				container.State = "waiting"
				container.StateReason = status.State.Waiting.Reason
			case status.State.Terminated != nil:
				container.State = "terminated"
				container.StateReason = status.State.Terminated.Reason
			}
		}
		containers = append(containers, container)
	}

	startTime := pod.CreationTimestamp.Time
	if pod.Status.StartTime != nil && !pod.Status.StartTime.IsZero() {
		startTime = pod.Status.StartTime.Time
	}

	ownerKind, ownerName, replicaSet := "", "", ""
	if len(pod.OwnerReferences) > 0 {
		owner := pod.OwnerReferences[0]
		ownerKind = owner.Kind
		ownerName = owner.Name
		if owner.Kind == "ReplicaSet" {
			replicaSet = owner.Name
		}
	}

	reason := pod.Status.Reason
	if reason == "" {
		for _, container := range containers {
			if container.State == "waiting" && container.StateReason != "" {
				reason = container.StateReason
				break
			}
		}
	}

	displayScalar := func(key string) string {
		if pod.Labels == nil {
			return ""
		}
		return pod.Labels[key]
	}

	return Pod{
		Context:         c.Name,
		Cluster:         c.Cluster,
		Namespace:       pod.Namespace,
		Name:            pod.Name,
		UID:             string(pod.UID),
		Phase:           string(pod.Status.Phase),
		Reason:          reason,
		Ready:           pod.Status.Phase == corev1.PodRunning && len(pod.Spec.Containers) > 0 && readyContainers == len(pod.Spec.Containers),
		ReadyContainers: readyContainers,
		ContainersCount: len(pod.Spec.Containers),
		Restarts:        restarts,
		Node:            pod.Spec.NodeName,
		PodIP:           pod.Status.PodIP,
		HostIP:          pod.Status.HostIP,
		QoSClass:        string(pod.Status.QOSClass),
		StartTime:       startTime,
		OwnerKind:       ownerKind,
		OwnerName:       ownerName,
		ReplicaSet:      replicaSet,
		Workload:        firstNonEmpty(displayScalar("app.kubernetes.io/name"), displayScalar("app"), displayScalar("k8s-app"), displayScalar("workload")),
		Labels:          copyMap(pod.Labels),
		Images:          images,
		Containers:      containers,
		ServiceAccount:  pod.Spec.ServiceAccountName,
	}
}

func resolveOwners(ctx context.Context, client kubernetes.Interface, pods []Pod) {
	needed := false
	for i := range pods {
		if pods[i].OwnerKind == "ReplicaSet" {
			needed = true
			break
		}
	}
	if !needed {
		return
	}
	list, err := client.AppsV1().ReplicaSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	owners := make(map[string]OwnerRef, len(list.Items))
	for i := range list.Items {
		rs := &list.Items[i]
		if len(rs.OwnerReferences) == 0 {
			continue
		}
		owner := rs.OwnerReferences[0]
		owners[rs.Namespace+"/"+rs.Name] = OwnerRef{Kind: owner.Kind, Name: owner.Name, UID: string(owner.UID)}
	}
	for i := range pods {
		pod := &pods[i]
		if pod.OwnerKind != "ReplicaSet" {
			continue
		}
		if owner, ok := owners[pod.Namespace+"/"+pod.OwnerName]; ok {
			pod.ReplicaSet = pod.OwnerName
			pod.OwnerKind = owner.Kind
			pod.OwnerName = owner.Name
		}
	}
}

func (p *ClientGoProvider) GetPod(ctx context.Context, ref PodRef) (*PodDetail, error) {
	client, err := p.client(ref.Context)
	if err != nil {
		return nil, err
	}
	raw, err := client.CoreV1().Pods(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod %s/%s on %s: %w", ref.Namespace, ref.Name, ref.Context, err)
	}
	contextInfo := Context{Name: ref.Context}
	if contexts, err := p.Contexts(ctx); err == nil {
		for _, c := range contexts {
			if c.Name == ref.Context {
				contextInfo = c
				break
			}
		}
	}
	base := convertPod(contextInfo, raw)
	return p.buildDetail(ctx, client, base, raw, ref), nil
}

func (p *ClientGoProvider) Close() error { return nil }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func copyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func eventFieldSelector(ref PodRef) string {
	return fields.OneTermEqualSelector("involvedObject.kind", "Pod").String() + "," +
		fields.OneTermEqualSelector("involvedObject.name", ref.Name).String() + "," +
		fields.OneTermEqualSelector("involvedObject.namespace", ref.Namespace).String()
}
