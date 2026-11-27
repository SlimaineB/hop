package plugins

// Info describes a built-in source for `hop config sources`.
type Info struct {
	ID          string
	Title       string
	Kind        string
	Description string
	Selectors   []string
}

func Catalog() []Info {
	return []Info{
		{ID: "pods", Title: "Pods", Kind: "pods", Description: "Kubernetes pods from every kubeconfig context"},
		{ID: "servers", Title: "VMs", Kind: "servers", Description: "SSH and configured VM/server inventory", Selectors: []string{"vm", "vms"}},
		{ID: "containers", Title: "Containers", Kind: "containers", Description: "Docker and Podman containers", Selectors: []string{"container", "containers", "ctr", "docker", "podman"}},
		{ID: "repos", Title: "Repositories", Kind: "repos", Description: "Local Git repositories with GitHub/GitLab links", Selectors: []string{"git", "repos"}},
		{ID: "packages", Title: "Packages", Kind: "packages", Description: "Container images and tags from GHCR, GitLab, and registries", Selectors: []string{"pkg", "packages", "registry"}},
		{ID: "tickets", Title: "Tickets", Kind: "tickets", Description: "Issue tracker tickets (network, opt-in per search)", Selectors: []string{"ticket", "tickets", "issue", "issues"}},
		{ID: "shortcuts", Title: "Shortcuts", Kind: "shortcuts", Description: "Browser shortcuts from the hop config", Selectors: []string{"link", "links", "shortcut", "shortcuts"}},
	}
}
