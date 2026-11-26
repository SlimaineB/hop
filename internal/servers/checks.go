package servers

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
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

type Check struct {
	Server    string      `json:"server"`
	CheckedAt time.Time   `json:"checked_at"`
	Metrics   HTTPCheck   `json:"metrics"`
	Health    HTTPCheck   `json:"health"`
	Machine   MachineInfo `json:"machine"`
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
	check.Metrics = checkHTTP(ctx, client, server.MetricsURL)
	check.Health = checkHTTP(ctx, client, server.HealthURL)
	check.Machine = s.checkMachine(ctx, server)
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

func (s *Service) checkMachine(ctx context.Context, server Server) MachineInfo {
	info := MachineInfo{}
	if !server.SSHChecks {
		info.Error = "SSH checks disabled"
		return info
	}
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "LogLevel=ERROR"}
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
