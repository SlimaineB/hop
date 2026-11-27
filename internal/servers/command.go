package servers

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type CommandResult struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
}

func (s *Service) RunCommand(ctx context.Context, server Server, command string) (CommandResult, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return CommandResult{}, errors.New("empty command")
	}
	if !server.SSHChecks {
		return CommandResult{}, errors.New("SSH checks disabled")
	}
	args := append([]string{}, s.sshConfigArgs()...)
	args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "LogLevel=ERROR")
	args = append(args, sshTargetArgs(server)...)
	args = append(args, command)
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		if result.ExitCode < 0 {
			result.ExitCode = 1
		}
		return result, nil
	}
	if commandCtx.Err() != nil {
		return result, commandCtx.Err()
	}
	return result, err
}
