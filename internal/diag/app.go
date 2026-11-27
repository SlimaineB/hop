package diag

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SlimaineB/hop/internal/config"
	"github.com/SlimaineB/hop/internal/servers"
)

const (
	logTailBytes         = 256 * 1024
	maxEvidence          = 5
	defaultAppLogPattern = `(?i)\b(error|exception|fatal|panic|traceback)\b`
)

func (e *Engine) DiagnoseApp(ctx context.Context, app config.AppConfig) *Report {
	start := time.Now()
	report := NewReport("app", app.Name)
	report.Env = app.Env
	probes := appDiagnosticProbes(app)
	results := make([]ProbeResult, len(probes))
	var wg sync.WaitGroup
	wg.Add(len(probes))
	for index, probe := range probes {
		go func(index int, probe config.AppProbeConfig) {
			defer wg.Done()
			results[index] = runAppProbe(ctx, app, probe)
		}(index, probe)
	}
	wg.Wait()
	report.Probes = results
	for _, result := range results {
		if result.OK {
			continue
		}
		detail := result.Error
		if detail == "" {
			detail = "Application probe failed."
		}
		evidence := []string{"app: " + app.Name, "probe: " + result.ID + " (" + result.Type + ")"}
		evidence = append(evidence, result.Evidence...)
		report.Add(Finding{
			ID:       "probe-" + result.ID,
			Severity: result.Severity,
			Title:    result.Title,
			Detail:   detail,
			Evidence: evidence,
			Remedy:   remedyForProbe(app, result.ID),
		})
	}
	e.apply(ctx, report, Target{
		Kind:  "app",
		Name:  app.Name,
		Facts: AppFacts(app, results),
		App:   &app,
	}, nil)
	report.Duration = time.Since(start)
	report = e.finish(report)
	if len(report.Probes) > 0 {
		okCount := 0
		for _, probe := range report.Probes {
			if probe.OK {
				okCount++
			}
		}
		report.Summary = fmt.Sprintf("%d/%d probes passed · %s", okCount, len(report.Probes), report.Summary)
		sort.SliceStable(report.Probes, func(i, j int) bool {
			left, right := severityRank(report.Probes[i].Severity), severityRank(report.Probes[j].Severity)
			if report.Probes[i].OK != report.Probes[j].OK {
				return !report.Probes[i].OK
			}
			if left != right {
				return left < right
			}
			return report.Probes[i].ID < report.Probes[j].ID
		})
	}
	return report
}

func (e *Engine) DiagnoseApps(ctx context.Context, apps []config.AppConfig) []*Report {
	reports := make([]*Report, len(apps))
	var wg sync.WaitGroup
	wg.Add(len(apps))
	for index, app := range apps {
		go func(index int, app config.AppConfig) {
			defer wg.Done()
			reports[index] = e.DiagnoseApp(ctx, app)
		}(index, app)
	}
	wg.Wait()
	return reports
}

func AppFacts(app config.AppConfig, probes []ProbeResult) map[string]any {
	tags := make([]any, 0, len(app.Tags))
	for _, tag := range app.Tags {
		tags = append(tags, tag)
	}
	facts := map[string]any{
		"app.name":        app.Name,
		"app.title":       app.Title,
		"app.description": app.Description,
		"app.env":         app.Env,
		"app.host":        app.Host,
		"app.port":        app.Port,
		"app.health_url":  app.HealthURL,
		"app.metrics_url": app.MetricsURL,
		"app.log_path":    app.LogPath,
		"app.log_pattern": app.LogPattern,
		"app.tags":        tags,
		"app.probe_count": len(probes),
	}
	probeMap := map[string]any{}
	failed := []any{}
	ok := []any{}
	critical, warning := 0, 0
	for _, probe := range probes {
		probeMap[probe.ID] = map[string]any{
			"id":          probe.ID,
			"title":       probe.Title,
			"type":        probe.Type,
			"ok":          probe.OK,
			"severity":    string(probe.Severity),
			"summary":     probe.Summary,
			"error":       probe.Error,
			"duration_ms": float64(probe.Duration) / float64(time.Millisecond),
		}
		if probe.OK {
			ok = append(ok, probe.ID)
			continue
		}
		failed = append(failed, probe.ID)
		switch probe.Severity {
		case SeverityCritical:
			critical++
		case SeverityWarning:
			warning++
		}
	}
	facts["app.probes"] = probeMap
	facts["app.failed_probes"] = failed
	facts["app.ok_probes"] = ok
	facts["app.failed_probe_count"] = len(failed)
	facts["app.critical_probe_count"] = critical
	facts["app.warning_probe_count"] = warning
	return facts
}

func appDiagnosticProbes(app config.AppConfig) []config.AppProbeConfig {
	probes := append([]config.AppProbeConfig(nil), app.Probes...)
	existing := map[string]bool{}
	for _, probe := range probes {
		existing[strings.ToLower(probe.ID)] = true
	}
	add := func(probe config.AppProbeConfig) {
		if probe.ID == "" || existing[strings.ToLower(probe.ID)] {
			return
		}
		existing[strings.ToLower(probe.ID)] = true
		probes = append(probes, probe)
	}
	timeout := app.Timeout
	if timeout.Get() <= 0 {
		timeout = config.Duration(3 * time.Second)
	}
	if app.HealthURL != "" {
		add(config.AppProbeConfig{
			ID:       "health",
			Title:    "Health endpoint",
			Type:     "http",
			Severity: "critical",
			URL:      app.HealthURL,
			Timeout:  timeout,
			Remedy:   "Check the service process, its dependencies and the health endpoint logs.",
		})
	}
	if app.MetricsURL != "" {
		add(config.AppProbeConfig{
			ID:       "metrics",
			Title:    "Metrics endpoint",
			Type:     "http",
			Severity: "warning",
			URL:      app.MetricsURL,
			Timeout:  timeout,
			Remedy:   "Check the metrics exporter or instrumentation endpoint.",
		})
	}
	if app.Port > 0 {
		add(config.AppProbeConfig{
			ID:       "port",
			Title:    fmt.Sprintf("TCP port %d", app.Port),
			Type:     "tcp",
			Severity: "critical",
			Host:     app.Host,
			Port:     app.Port,
			Timeout:  timeout,
			Remedy:   "Check that the application is listening and that no firewall blocks the port.",
		})
	}
	if app.LogPath != "" {
		add(config.AppProbeConfig{
			ID:       "logs",
			Title:    "Application logs",
			Type:     "log",
			Severity: "warning",
			Path:     app.LogPath,
			Pattern:  app.LogPattern,
			Since:    app.LogSince,
			Timeout:  timeout,
			Remedy:   "Inspect the matching log lines and fix the underlying errors.",
		})
	}
	return probes
}

func runAppProbe(ctx context.Context, app config.AppConfig, probe config.AppProbeConfig) ProbeResult {
	start := time.Now()
	result := ProbeResult{
		ID:       probe.ID,
		Title:    probe.Title,
		Type:     probe.Type,
		Severity: severityFromConfig(probe.Severity),
	}
	timeout := probe.Timeout.Get()
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch probe.Type {
	case "http", "https":
		runHTTPProbe(probeCtx, probe, &result)
	case "tcp":
		runTCPProbe(probeCtx, probe, &result)
	case "log":
		runLogProbe(probeCtx, probe, &result)
	case "command":
		runCommandProbe(probeCtx, probe, &result)
	default:
		result.Error = "unsupported probe type " + probe.Type
	}
	result.Duration = time.Since(start)
	if result.Error != "" {
		result.OK = false
	}
	return result
}

func runHTTPProbe(ctx context.Context, probe config.AppProbeConfig, result *ProbeResult) {
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.URL, nil)
	if err != nil {
		result.Error = err.Error()
		return
	}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		result.Error = err.Error()
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	result.Summary = fmt.Sprintf("HTTP %d in %s", resp.StatusCode, latency.Round(time.Millisecond))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		result.Error = fmt.Sprintf("unexpected HTTP status %d", resp.StatusCode)
		if text := strings.TrimSpace(string(body)); text != "" {
			result.Evidence = append(result.Evidence, "body: "+truncate(text, 400))
		}
		return
	}
	result.OK = true
}

func runTCPProbe(ctx context.Context, probe config.AppProbeConfig, result *ProbeResult) {
	host := probe.Host
	if host == "" {
		host = "127.0.0.1"
	}
	address := net.JoinHostPort(host, strconv.Itoa(probe.Port))
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	latency := time.Since(start)
	if err != nil {
		result.Error = err.Error()
		return
	}
	_ = conn.Close()
	result.OK = true
	result.Summary = fmt.Sprintf("TCP %s open in %s", address, latency.Round(time.Millisecond))
}

func runLogProbe(ctx context.Context, probe config.AppProbeConfig, result *ProbeResult) {
	if probe.Path == "" {
		result.Error = "log path is empty"
		return
	}
	pattern := probe.Pattern
	if pattern == "" {
		pattern = defaultAppLogPattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		result.Error = "invalid log pattern: " + err.Error()
		return
	}
	fatalRe := regexp.MustCompile(`(?i)\b(fatal|panic|segfault|out of memory|oom)\b`)
	file, err := os.Open(probe.Path)
	if err != nil {
		result.Error = err.Error()
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		result.Error = err.Error()
		return
	}
	offset := info.Size() - logTailBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		result.Error = err.Error()
		return
	}
	cutoff := time.Time{}
	if since := probe.Since.Get(); since > 0 {
		cutoff = time.Now().Add(-since)
	}
	matches := []string{}
	matchCount := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			result.Error = err.Error()
			return
		}
		line := scanner.Text()
		if !cutoff.IsZero() {
			if stamp, ok := logLineTime(line); ok && stamp.Before(cutoff) {
				continue
			}
		}
		if !re.MatchString(line) {
			continue
		}
		matchCount++
		if len(matches) < maxEvidence {
			matches = append(matches, truncate(strings.TrimSpace(line), 400))
		}
		if fatalRe.MatchString(line) {
			result.Severity = SeverityCritical
		}
	}
	if err := scanner.Err(); err != nil {
		result.Error = err.Error()
		return
	}
	result.Summary = fmt.Sprintf("%d matching log lines", matchCount)
	if matchCount == 0 {
		result.OK = true
		result.Summary = "no matching log line"
		return
	}
	result.Error = fmt.Sprintf("%d matching log lines", matchCount)
	result.Evidence = matches
}

func runCommandProbe(ctx context.Context, probe config.AppProbeConfig, result *ProbeResult) {
	cmd := exec.CommandContext(ctx, "sh", "-c", probe.Command)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	latency := time.Since(start)
	commandResult := servers.CommandResult{Stdout: strings.TrimSpace(stdout.String()), Stderr: strings.TrimSpace(stderr.String())}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			commandResult.ExitCode = exitErr.ExitCode()
			if commandResult.ExitCode < 0 {
				commandResult.ExitCode = 1
			}
		} else {
			result.Error = err.Error()
			return
		}
	}
	var stdoutMatch *regexp.Regexp
	if probe.Expect.StdoutMatches != "" {
		re, err := regexp.Compile(probe.Expect.StdoutMatches)
		if err != nil {
			result.Error = "invalid stdout_matches: " + err.Error()
			return
		}
		stdoutMatch = re
	}
	failures := commandFailures(probe.Expect, commandResult, stdoutMatch)
	result.Summary = fmt.Sprintf("command exit %d in %s", commandResult.ExitCode, latency.Round(time.Millisecond))
	if len(failures) > 0 {
		result.Error = strings.Join(failures, "; ")
		result.Evidence = append(result.Evidence, commandEvidence(commandResult)...)
		return
	}
	result.OK = true
}

func logLineTime(line string) (time.Time, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return time.Time{}, false
	}
	candidates := []string{strings.Trim(fields[0], "[]")}
	if len(fields) > 1 {
		candidates = append(candidates, fields[0]+" "+fields[1])
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
		"2006/01/02 15:04:05",
	}
	for _, candidate := range candidates {
		for _, layout := range layouts {
			if stamp, err := time.Parse(layout, candidate); err == nil {
				return stamp, true
			}
		}
	}
	return time.Time{}, false
}

func remedyForProbe(app config.AppConfig, id string) string {
	for _, probe := range app.Probes {
		if strings.EqualFold(probe.ID, id) {
			return probe.Remedy
		}
	}
	return ""
}
