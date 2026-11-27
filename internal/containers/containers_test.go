package containers

import "testing"

func TestParseDockerList(t *testing.T) {
	output := []byte(`{"Command":"\"nginx -g\"","CreatedAt":"2026-11-26 10:00:00 +0100 CET","ID":"abc123","Image":"nginx:alpine","Labels":"com.docker.compose.project=lab,com.docker.compose.service=web","Names":"lab-web-1","Ports":"0.0.0.0:8080->80/tcp","State":"running","Status":"Up 2 hours"}` + "\n" +
		`{"Command":"\"/entrypoint\"","ID":"def456","Image":"redis:7","Names":"cache","State":"exited","Status":"Exited (0) 1 hour ago"}`)
	items := parseList(output, Engine{Name: "docker", Type: "docker"})
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Name != "lab-web-1" || items[0].State != "running" ||
		items[0].Image != "nginx:alpine" || items[0].Project != "lab" || items[0].Service != "web" {
		t.Fatalf("docker item = %+v", items[0])
	}
	if items[0].Ports != "0.0.0.0:8080->80/tcp" || items[0].Engine != "docker" {
		t.Fatalf("docker ports/engine = %+v", items[0])
	}
	if items[1].Name != "cache" || items[1].State != "exited" {
		t.Fatalf("second item = %+v", items[1])
	}
}

func TestParsePodmanArray(t *testing.T) {
	output := []byte(`[{"Id":"0123456789abcdef","Names":["podman-web"],"Image":"docker.io/library/nginx:latest","State":"running","Status":"Up 5 minutes","Labels":{"com.docker.compose.project":"lab"},"Ports":[{"host_port":8081}]}]`)
	items := parseList(output, Engine{Name: "podman", Type: "podman"})
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	item := items[0]
	if item.ID != "0123456789abcdef" || item.Name != "podman-web" || item.Project != "lab" {
		t.Fatalf("item = %+v", item)
	}
	if item.Engine != "podman" || item.EngineType != "podman" {
		t.Fatalf("engine = %+v", item)
	}
}

func TestRankContainers(t *testing.T) {
	service := &Service{minScore: 0.5}
	items := []Container{
		{ID: "1", Name: "web", Image: "nginx:latest", State: "running", Engine: "docker", Project: "lab", Service: "web", Labels: map[string]string{"tier": "front"}},
		{ID: "2", Name: "db", Image: "postgres:16", State: "exited", Engine: "podman"},
	}
	all := service.rank(items, "", 10)
	if len(all) != 2 {
		t.Fatalf("all = %+v", all)
	}
	running := service.rank(items, "status:running", 10)
	if len(running) != 1 || running[0].Container.Name != "web" {
		t.Fatalf("running = %+v", running)
	}
	label := service.rank(items, "label:tier=front", 10)
	if len(label) != 1 || label[0].Container.Name != "web" {
		t.Fatalf("label = %+v", label)
	}
	if hits := service.rank(items, "missing", 10); len(hits) != 0 {
		t.Fatalf("missing = %+v", hits)
	}
}
