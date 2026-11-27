package servers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HTTPCheck struct {
	URL        string `json:"url"`
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code,omitempty"`
	LatencyMS  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

type MachineInfo struct {
	OK              bool     `json:"ok"`
	Hostname        string   `json:"hostname,omitempty"`
	OS              string   `json:"os,omitempty"`
	Kernel          string   `json:"kernel,omitempty"`
	Uptime          string   `json:"uptime,omitempty"`
	Load            string   `json:"load,omitempty"`
	CPUs            int      `json:"cpus,omitempty"`
	MemoryTotal     uint64   `json:"memory_total,omitempty"`
	MemoryUsed      uint64   `json:"memory_used,omitempty"`
	MemoryAvailable uint64   `json:"memory_available,omitempty"`
	DiskTotal       uint64   `json:"disk_total,omitempty"`
	DiskUsed        uint64   `json:"disk_used,omitempty"`
	DiskFree        uint64   `json:"disk_free,omitempty"`
	DiskPercent     string   `json:"disk_percent,omitempty"`
	FailedUnits     []string `json:"failed_units,omitempty"`
	Error           string   `json:"error,omitempty"`
}

type PingCheck struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

type PortCheck struct {
	Port      int    `json:"port"`
	Open      bool   `json:"open"`
	State     string `json:"state"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

type NetworkInfo struct {
	Target    string      `json:"target,omitempty"`
	Checked   bool        `json:"checked"`
	Reachable bool        `json:"reachable"`
	Ping      PingCheck   `json:"ping"`
	Ports     []PortCheck `json:"ports,omitempty"`
}

type Check struct {
	Server    string      `json:"server"`
	CheckedAt time.Time   `json:"checked_at"`
	Metrics   HTTPCheck   `json:"metrics"`
	Health    HTTPCheck   `json:"health"`
	Machine   MachineInfo `json:"machine"`
	Network   NetworkInfo `json:"network"`
}

func (s *Service) Check(ctx context.Context, name string) (Check, error) {
	server, err := s.Find(name)
	if err != nil {
		return Check{}, err
	}
	return s.CheckServer(ctx, *server), nil
}

func (s *Service) CheckServer(ctx context.Context, server Server) Check {
	check := Check{Server: server.Name, CheckedAt: time.Now()}
	client := &http.Client{Timeout: 6 * time.Second}
	var metrics, health HTTPCheck
	var machine MachineInfo
	var network NetworkInfo
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		metrics = checkHTTP(ctx, client, server.MetricsURL)
	}()
	go func() {
		defer wg.Done()
		health = checkHTTP(ctx, client, server.HealthURL)
	}()
	go func() {
		defer wg.Done()
		machine = s.checkMachine(ctx, server)
	}()
	go func() {
		defer wg.Done()
		network = s.checkNetwork(ctx, server)
	}()
	wg.Wait()
	check.Metrics = metrics
	check.Health = health
	check.Machine = machine
	check.Network = network
	return check
}

func checkHTTP(ctx context.Context, client *http.Client, url string) HTTPCheck {
	check := HTTPCheck{URL: url}
	if strings.TrimSpace(url) == "" {
		check.Error = "not configured"
		return check
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	start := time.Now()
	response, err := client.Do(req)
	check.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		check.Error = err.Error()
		return check
	}
	_ = response.Body.Close()
	check.StatusCode = response.StatusCode
	check.OK = response.StatusCode >= 200 && response.StatusCode < 400
	if !check.OK {
		check.Error = response.Status
	}
	return check
}

func (s *Service) checkNetwork(ctx context.Context, server Server) NetworkInfo {
	target := networkTarget(server)
	info := NetworkInfo{Target: target, Checked: server.NetworkChecks}
	if !server.NetworkChecks {
		info.Ping.Error = "network checks disabled"
		return info
	}
	if target == "" {
		info.Ping.Error = "no host configured"
		return info
	}
	info.Ping = checkPing(ctx, target)
	info.Reachable = info.Ping.OK
	for _, port := range networkPorts(server) {
		result := checkTCP(ctx, target, port)
		info.Ports = append(info.Ports, result)
		if result.Open {
			info.Reachable = true
		}
	}
	return info
}

func networkTarget(server Server) string {
	host := strings.TrimSpace(server.Host)
	if host == "" {
		host = strings.TrimSpace(server.Name)
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host
}

func networkPorts(server Server) []int {
	ports := normalizePorts(server.CheckPorts)
	if server.Port > 0 {
		ports = append(ports, server.Port)
	}
	ports = normalizePorts(ports)
	if len(ports) == 0 {
		ports = []int{22}
	}
	if len(ports) > 16 {
		ports = ports[:16]
	}
	return ports
}

func checkPing(ctx context.Context, host string) PingCheck {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := pingCommand(pingCtx, host)
	output, err := cmd.CombinedOutput()
	text := string(output)
	if latency, ok := parsePingLatency(text); ok {
		return PingCheck{OK: true, LatencyMS: latency}
	}
	if err == nil {
		return PingCheck{OK: true}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return PingCheck{Error: "ping unavailable"}
	}
	return PingCheck{Error: commandSummary(err, text)}
}

func pingCommand(ctx context.Context, host string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1000", host)
	case "windows":
		return exec.CommandContext(ctx, "ping", "-n", "1", "-w", "1000", host)
	default:
		return exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", host)
	}
}

func parsePingLatency(output string) (int64, bool) {
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		index := strings.Index(lower, "time=")
		if index < 0 {
			index = strings.Index(lower, "time<")
		}
		if index < 0 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line[index+5:]))
		if len(fields) == 0 {
			continue
		}
		value := strings.TrimSuffix(strings.TrimSuffix(fields[0], "ms"), "MS")
		ms, err := strconv.ParseFloat(value, 64)
		if err != nil || ms < 0 {
			continue
		}
		latency := int64(ms + 0.5)
		if latency < 1 {
			latency = 1
		}
		return latency, true
	}
	return 0, false
}

func checkTCP(ctx context.Context, host string, port int) PortCheck {
	result := PortCheck{Port: port, State: "error"}
	dialCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	result.LatencyMS = time.Since(start).Milliseconds()
	if err == nil {
		_ = conn.Close()
		result.Open = true
		result.State = "open"
		return result
	}
	result.Error = err.Error()
	switch {
	case isTimeout(err):
		result.State = "filtered"
	case strings.Contains(strings.ToLower(err.Error()), "connection refused"):
		result.State = "closed"
	}
	return result
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func commandSummary(err error, output string) string {
	text := strings.TrimSpace(output)
	if text == "" {
		if err != nil {
			return err.Error()
		}
		return "no response"
	}
	if len(text) > 180 {
		text = text[:179] + "…"
	}
	if err != nil {
		return err.Error() + ": " + text
	}
	return text
}

func (s *Service) checkMachine(ctx context.Context, server Server) MachineInfo {
	info := MachineInfo{}
	if !server.SSHChecks {
		info.Error = "SSH checks disabled"
		return info
	}
	args := append([]string{}, s.sshConfigArgs()...)
	args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "LogLevel=ERROR")
	args = append(args, sshTargetArgs(server)...)
	commandCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "ssh", args...)
	cmd.Stdin = strings.NewReader(machineScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		info.Error = strings.TrimSpace(string(output))
		if info.Error == "" {
			info.Error = err.Error()
		} else {
			info.Error = err.Error() + ": " + info.Error
		}
		return info
	}
	info = parseMachineOutput(string(output))
	info.OK = true
	return info
}

const machineScript = `set -u
echo @@HOSTNAME
hostname 2>/dev/null || uname -n
echo @@OS
. /etc/os-release 2>/dev/null
printf '%s\n' "${PRETTY_NAME:-Linux}"
echo @@KERNEL
uname -r 2>/dev/null || true
echo @@UPTIME
uptime -p 2>/dev/null || uptime 2>/dev/null || true
echo @@LOAD
cut -d' ' -f1-3 /proc/loadavg 2>/dev/null || true
echo @@CPU
nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || echo 0
echo @@MEM
free -b 2>/dev/null | awk '/^Mem:/{print $2, $3, $7}'
echo @@DISK
df -P -B1 / 2>/dev/null | awk 'NR==2{print $2, $3, $4, $5}'
echo @@FAILED
systemctl --failed --no-legend --plain 2>/dev/null | head -20
`

func parseMachineOutput(output string) MachineInfo {
	sections := map[string][]string{}
	current := ""
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "@@") {
			current = strings.TrimPrefix(line, "@@")
			continue
		}
		if current == "" {
			continue
		}
		sections[current] = append(sections[current], line)
	}
	value := func(key string) string {
		return strings.TrimSpace(strings.Join(sections[key], "\n"))
	}
	first := func(key string) string {
		lines := sections[key]
		if len(lines) == 0 {
			return ""
		}
		return strings.TrimSpace(lines[0])
	}
	info := MachineInfo{
		Hostname: first("HOSTNAME"),
		OS:       first("OS"),
		Kernel:   first("KERNEL"),
		Uptime:   value("UPTIME"),
		Load:     first("LOAD"),
	}
	info.CPUs, _ = strconv.Atoi(first("CPU"))
	if fields := strings.Fields(first("MEM")); len(fields) >= 3 {
		info.MemoryTotal, _ = strconv.ParseUint(fields[0], 10, 64)
		info.MemoryUsed, _ = strconv.ParseUint(fields[1], 10, 64)
		info.MemoryAvailable, _ = strconv.ParseUint(fields[2], 10, 64)
	}
	if fields := strings.Fields(first("DISK")); len(fields) >= 4 {
		info.DiskTotal, _ = strconv.ParseUint(fields[0], 10, 64)
		info.DiskUsed, _ = strconv.ParseUint(fields[1], 10, 64)
		info.DiskFree, _ = strconv.ParseUint(fields[2], 10, 64)
		info.DiskPercent = strings.TrimSuffix(fields[3], "%")
	}
	for _, line := range sections["FAILED"] {
		line = strings.TrimSpace(strings.TrimLeft(line, "●* "))
		if line == "" {
			continue
		}
		info.FailedUnits = append(info.FailedUnits, line)
	}
	return info
}

func FormatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
