package actions

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/SlimaineB/hop/internal/kube"
)

type Options struct {
	Kubeconfigs []string
	From        time.Time
	To          time.Time
}

func (o Options) HasTimeRange() bool {
	return !o.From.IsZero() || !o.To.IsZero()
}

type Command struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
}

func Commands(pod kube.Pod, opts Options) []Command {
	commands := []Command{
		{
			ID:          "k9s",
			Label:       "Open in k9s",
			Command:     K9sCommand(pod, opts),
			Description: "Open k9s filtered to this pod",
		},
		{
			ID:          "describe",
			Label:       "Describe",
			Command:     kubectlCommand(pod, opts, "describe", "pod", shellQuote(pod.Name)),
			Description: "Full pod details",
		},
		{
			ID:          "logs",
			Label:       "Logs",
			Command:     logsCommand(pod, opts),
			Description: logsDescription(opts),
		},
		{
			ID:          "exec",
			Label:       "Shell",
			Command:     kubectlCommand(pod, opts, "exec", "-it", shellQuote(pod.Name), "--", "sh"),
			Description: "Open a shell in the first container",
		},
		{
			ID:          "events",
			Label:       "Events",
			Command:     kubectlCommand(pod, opts, "get", "events", "--field-selector", "involvedObject.name="+shellQuote(pod.Name), "--sort-by=.lastTimestamp"),
			Description: "Recent pod events",
		},
	}
	if port := firstPort(pod); port > 0 {
		commands = append(commands, Command{
			ID:          "port-forward",
			Label:       "Port-forward",
			Command:     kubectlCommand(pod, opts, "port-forward", shellQuote(pod.Name), fmt.Sprintf("%d:%d", port, port)),
			Description: fmt.Sprintf("Expose port %d locally", port),
		})
	} else {
		commands = append(commands, Command{
			ID:          "port-forward",
			Label:       "Port-forward",
			Command:     kubectlCommand(pod, opts, "port-forward", shellQuote(pod.Name), "8080:<remote-port>"),
			Description: "Adapt to the actual container port",
		})
	}
	if pod.Node != "" {
		commands = append(commands, Command{
			ID:          "node",
			Label:       "Describe node",
			Command:     kubectlClusterCommand(pod, opts, "describe", "node", shellQuote(pod.Node)),
			Description: "Details for the node hosting the pod",
		})
	}
	if pod.OwnerKind == "Deployment" && pod.OwnerName != "" {
		commands = append(commands, Command{
			ID:          "rollout-restart",
			Label:       "Restart deployment",
			Command:     kubectlCommand(pod, opts, "rollout", "restart", "deployment/"+shellQuote(pod.OwnerName)),
			Description: "Restart the owning Deployment",
		})
	}
	return commands
}

func logsCommand(pod kube.Pod, opts Options) string {
	args := []string{"logs", "-f", "--all-containers=true", "--prefix"}
	if !opts.From.IsZero() {
		args = append(args, "--since-time", shellQuote(opts.From.UTC().Format(time.RFC3339)))
	}
	args = append(args, shellQuote(pod.Name))
	return kubectlCommand(pod, opts, args...)
}

func logsDescription(opts Options) string {
	if !opts.HasTimeRange() {
		return "Follow logs from all containers"
	}
	if !opts.From.IsZero() && !opts.To.IsZero() {
		return "Logs since " + opts.From.Format("15:04") + " (stream stays open)"
	}
	return "Logs from the requested range (stream stays open)"
}

func kubectlCommand(pod kube.Pod, opts Options, args ...string) string {
	parts := make([]string, 0, len(args)+8)
	if prefix := kubeconfigPrefix(opts); prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, "kubectl", "--context", shellQuote(pod.Context), "-n", shellQuote(pod.Namespace))
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

func kubectlClusterCommand(pod kube.Pod, opts Options, args ...string) string {
	parts := make([]string, 0, len(args)+6)
	if prefix := kubeconfigPrefix(opts); prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, "kubectl", "--context", shellQuote(pod.Context))
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

func GetCommand(pod kube.Pod, opts Options) string {
	return kubectlCommand(pod, opts, "get", "pod", shellQuote(pod.Name), "-o", "yaml")
}

func K9sCommand(pod kube.Pod, opts Options) string {
	parts := make([]string, 0, 10)
	if prefix := kubeconfigPrefix(opts); prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts,
		"k9s",
		"--context", shellQuote(pod.Context),
		"-n", shellQuote(pod.Namespace),
		"-c", shellQuote("pod /^"+regexp.QuoteMeta(pod.Name)+"$"),
	)
	return strings.Join(parts, " ")
}

func kubeconfigPrefix(opts Options) string {
	if len(opts.Kubeconfigs) == 0 {
		return ""
	}
	return "KUBECONFIG=" + shellQuote(strings.Join(opts.Kubeconfigs, string(os.PathListSeparator)))
}

func K9s(pod kube.Pod, opts Options) *exec.Cmd {
	args := []string{
		"--context", pod.Context,
		"-n", pod.Namespace,
		"-c", "pod /^" + regexp.QuoteMeta(pod.Name) + "$",
	}
	cmd := exec.Command("k9s", args...)
	applyKubeconfigEnv(cmd, opts)
	return cmd
}

func Kubectl(pod kube.Pod, opts Options, args ...string) *exec.Cmd {
	full := []string{"--context", pod.Context, "-n", pod.Namespace}
	full = append(full, args...)
	cmd := exec.Command("kubectl", full...)
	applyKubeconfigEnv(cmd, opts)
	return cmd
}

func Logs(pod kube.Pod, opts Options) *exec.Cmd {
	args := []string{"logs", "-f", "--all-containers=true", "--prefix"}
	if !opts.From.IsZero() {
		args = append(args, "--since-time", opts.From.UTC().Format(time.RFC3339))
	}
	args = append(args, pod.Name)
	return Kubectl(pod, opts, args...)
}

func Describe(pod kube.Pod, opts Options) *exec.Cmd {
	return Kubectl(pod, opts, "describe", "pod", pod.Name)
}

func DescribePaged(pod kube.Pod, opts Options) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return Describe(pod, opts)
	}
	pager := strings.TrimSpace(os.Getenv("PAGER"))
	if pager == "" {
		if !Available("less") {
			return Describe(pod, opts)
		}
		pager = "less -R"
	}
	line := kubectlCommand(pod, opts, "describe", "pod", shellQuote(pod.Name)) + " | " + pager
	cmd := exec.Command("sh", "-c", line)
	applyKubeconfigEnv(cmd, opts)
	return cmd
}

func applyKubeconfigEnv(cmd *exec.Cmd, opts Options) {
	if len(opts.Kubeconfigs) == 0 {
		return
	}
	env := os.Environ()
	env = append(env, "KUBECONFIG="+strings.Join(opts.Kubeconfigs, string(os.PathListSeparator)))
	cmd.Env = env
}

type browserCandidate struct {
	name string
	args []string
}

func OpenBrowser(url string) error {
	if url == "" {
		return fmt.Errorf("empty URL")
	}
	return openBrowser(browserCandidates(url), 1500*time.Millisecond)
}

func browserCandidates(url string) []browserCandidate {
	switch runtime.GOOS {
	case "darwin":
		return []browserCandidate{{"open", []string{url}}}
	case "windows":
		return []browserCandidate{{"cmd", []string{"/c", "start", "", escapeCmdArg(url)}}}
	default:
		wsl := os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != ""
		if wsl {
			return []browserCandidate{
				{"wslview", []string{url}},
				{"cmd.exe", []string{"/c", "start", "", escapeCmdArg(url)}},
				{"xdg-open", []string{url}},
				{"sensible-browser", []string{url}},
			}
		}
		return []browserCandidate{
			{"xdg-open", []string{url}},
			{"sensible-browser", []string{url}},
		}
	}
}

var cmdArgEscaper = strings.NewReplacer(
	"^", "^^",
	"&", "^&",
	"|", "^|",
	"<", "^<",
	">", "^>",
	"(", "^(",
	")", "^)",
	"%", "^%",
)

func escapeCmdArg(value string) string {
	return cmdArgEscaper.Replace(value)
}

func openBrowser(candidates []browserCandidate, timeout time.Duration) error {
	var lastErr error
	var names []string
	for _, c := range candidates {
		names = append(names, c.name)
		if _, err := exec.LookPath(c.name); err != nil {
			continue
		}
		cmd := exec.Command(c.name, c.args...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			lastErr = fmt.Errorf("%s: %w", c.name, err)
			continue
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				return nil
			}
			lastErr = fmt.Errorf("%s: %w", c.name, err)
		case <-time.After(timeout):
			return nil
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no browser opener found (tried %s)", strings.Join(names, ", "))
}

func Copy(text string) error {
	type clipboard struct {
		name string
		args []string
	}
	candidates := []clipboard{
		{"wl-copy", nil},
		{"xclip", []string{"-selection", "clipboard"}},
		{"xsel", []string{"--clipboard", "--input"}},
		{"pbcopy", nil},
		{"clip.exe", nil},
	}
	var lastErr error
	for _, c := range candidates {
		if _, err := exec.LookPath(c.name); err != nil {
			continue
		}
		cmd := exec.Command(c.name, c.args...)
		cmd.Stdin = bytes.NewBufferString(text)
		cmd.Stdout = nil
		if err := cmd.Run(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no clipboard tool found (install wl-clipboard, xclip or xsel)")
}

func Available(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func firstPort(pod kube.Pod) int32 {
	for _, container := range pod.Containers {
		for _, port := range container.Ports {
			if port.ContainerPort > 0 {
				return port.ContainerPort
			}
		}
	}
	return 0
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '@' || r == '%' || r == '+' || r == '=')
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
