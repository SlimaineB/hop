package diag

import (
	"testing"
	"time"

	"github.com/SlimaineB/hop/internal/kube"
	"github.com/SlimaineB/hop/internal/servers"
)

func TestDiagnosePodCrashLoop(t *testing.T) {
	pod := kube.PodDetail{
		Pod: kube.Pod{
			Context:         "prod",
			Namespace:       "payments",
			Name:            "api-7f9c",
			Phase:           "Running",
			Reason:          "CrashLoopBackOff",
			Ready:           false,
			ContainersCount: 1,
			Restarts:        12,
			StartTime:       time.Now().Add(-20 * time.Minute),
			QoSClass:        "Burstable",
			Containers: []kube.Container{
				{Name: "api", Ready: false, Restarts: 12, State: "waiting", StateReason: "CrashLoopBackOff"},
			},
		},
		Conditions: []kube.Condition{
			{Type: "Ready", Status: "False", Reason: "ContainersNotReady"},
		},
		Events: []kube.Event{
			{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting failed container", Count: 4},
		},
	}

	report := DiagnosePod(pod, nil)

	if report.Verdict != SeverityCritical {
		t.Fatalf("verdict = %s, want critical", report.Verdict)
	}
	assertFinding(t, report, "reason-crashloopbackoff")
	assertFinding(t, report, "high-restarts")
	assertFinding(t, report, "pod-not-ready")
}

func TestDiagnosePodHealthy(t *testing.T) {
	pod := kube.PodDetail{
		Pod: kube.Pod{
			Context:         "prod",
			Namespace:       "payments",
			Name:            "api-7f9c",
			Phase:           "Running",
			Ready:           true,
			ContainersCount: 1,
			StartTime:       time.Now().Add(-2 * time.Hour),
			QoSClass:        "Guaranteed",
			MemoryLimit:     512 * 1024 * 1024,
			Containers: []kube.Container{
				{Name: "api", Ready: true, State: "running"},
			},
		},
		Conditions: []kube.Condition{
			{Type: "Ready", Status: "True"},
		},
		Topology: &kube.Topology{
			Nodes: []kube.TopologyNode{
				{Kind: "Service", Name: "api", Status: "ClusterIP"},
				{Kind: "EndpointSlice", Name: "api-abc", Subtitle: "api", Status: "1/1 ready"},
			},
		},
	}

	report := DiagnosePod(pod, nil)

	if report.Verdict != SeverityInfo {
		t.Fatalf("verdict = %s, want info, findings=%+v", report.Verdict, report.Findings)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", report.Findings)
	}
}

func TestDiagnosePodMemoryNearLimit(t *testing.T) {
	detail := kube.PodDetail{
		Pod: kube.Pod{
			Context:     "prod",
			Namespace:   "payments",
			Name:        "api",
			Phase:       "Running",
			Ready:       true,
			MemoryLimit: 100 * 1024 * 1024,
		},
	}
	stats := &kube.PodStats{WorkingSet: 98 * 1024 * 1024}

	report := DiagnosePod(detail, stats)

	if report.Verdict != SeverityCritical {
		t.Fatalf("verdict = %s, want critical", report.Verdict)
	}
	assertFinding(t, report, "memory-near-limit")
}

func TestDiagnosePodUnboundPVC(t *testing.T) {
	detail := kube.PodDetail{
		Pod: kube.Pod{
			Context:   "prod",
			Namespace: "payments",
			Name:      "api",
			Phase:     "Running",
			Ready:     true,
		},
		Topology: &kube.Topology{
			Nodes: []kube.TopologyNode{
				{Kind: "Service", Name: "api"},
				{Kind: "EndpointSlice", Subtitle: "api", Status: "1/1 ready"},
				{Kind: "PersistentVolumeClaim", Name: "data", Status: "Pending"},
			},
		},
	}

	report := DiagnosePod(detail, nil)

	if report.Verdict != SeverityCritical {
		t.Fatalf("verdict = %s, want critical", report.Verdict)
	}
	assertFinding(t, report, "pvc-not-bound-data")
}

func TestDiagnoseServerUnhealthy(t *testing.T) {
	server := servers.Server{Name: "api-01", Host: "10.0.0.10", Port: 22, NetworkChecks: true}
	check := servers.Check{
		Server: "api-01",
		Machine: servers.MachineInfo{
			OK:          true,
			CPUs:        4,
			Load:        "9.50 8.10 7.20",
			MemoryTotal: 1000,
			MemoryUsed:  970,
			DiskTotal:   1000,
			DiskUsed:    980,
			DiskPercent: "98",
			FailedUnits: []string{"nginx.service"},
		},
		Health: servers.HTTPCheck{URL: "https://api.example/health", OK: false, StatusCode: 503, Error: "503 Service Unavailable"},
		Network: servers.NetworkInfo{
			Target:    "10.0.0.10",
			Checked:   true,
			Reachable: true,
			Ping:      servers.PingCheck{OK: true, LatencyMS: 1},
			Ports: []servers.PortCheck{
				{Port: 22, Open: true},
				{Port: 443, Open: false, State: "closed"},
			},
		},
	}

	report := DiagnoseServer(server, check)

	if report.Verdict != SeverityCritical {
		t.Fatalf("verdict = %s, want critical", report.Verdict)
	}
	assertFinding(t, report, "load-critical")
	assertFinding(t, report, "memory-critical")
	assertFinding(t, report, "disk-critical")
	assertFinding(t, report, "health-down")
	assertFinding(t, report, "ports-closed")
}

func TestDiagnoseServerUnreachable(t *testing.T) {
	server := servers.Server{Name: "api-01", Host: "10.0.0.10", NetworkChecks: true}
	check := servers.Check{
		Server:  "api-01",
		Machine: servers.MachineInfo{Error: "ssh: connect to host 10.0.0.10 port 22: connection refused"},
		Network: servers.NetworkInfo{
			Target:    "10.0.0.10",
			Checked:   true,
			Reachable: false,
			Ping:      servers.PingCheck{Error: "timeout"},
		},
	}

	report := DiagnoseServer(server, check)

	if report.Verdict != SeverityCritical {
		t.Fatalf("verdict = %s, want critical", report.Verdict)
	}
	assertFinding(t, report, "machine-unreachable")
	assertFinding(t, report, "network-unreachable")
}

func TestDiagnoseServerHealthy(t *testing.T) {
	server := servers.Server{Name: "api-01", Host: "10.0.0.10", Port: 22, NetworkChecks: true}
	check := servers.Check{
		Server: "api-01",
		Machine: servers.MachineInfo{
			OK:          true,
			CPUs:        4,
			Load:        "0.40 0.30 0.20",
			MemoryTotal: 8 * 1024 * 1024 * 1024,
			MemoryUsed:  2 * 1024 * 1024 * 1024,
			DiskTotal:   50 * 1024 * 1024 * 1024,
			DiskUsed:    10 * 1024 * 1024 * 1024,
			DiskPercent: "20",
		},
		Health: servers.HTTPCheck{URL: "https://api.example/health", OK: true, StatusCode: 200, LatencyMS: 25},
		Network: servers.NetworkInfo{
			Target:    "10.0.0.10",
			Checked:   true,
			Reachable: true,
			Ping:      servers.PingCheck{OK: true, LatencyMS: 1},
			Ports:     []servers.PortCheck{{Port: 22, Open: true}},
		},
	}

	report := DiagnoseServer(server, check)

	if report.Verdict != SeverityInfo {
		t.Fatalf("verdict = %s, want info, findings=%+v", report.Verdict, report.Findings)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", report.Findings)
	}
}

func assertFinding(t *testing.T, report *Report, id string) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.ID == id {
			return
		}
	}
	t.Fatalf("finding %q not found in %+v", id, report.Findings)
}
