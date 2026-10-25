package engine

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"

	"ai-guard-tracker/internal/policy"
)

// CustomCheckResult records what happened when a rule's options.customCheck
// was executed, for both decision-making and later audit logging.
type CustomCheckResult struct {
	Ran      bool
	ExitCode int
	TimedOut bool
	RunErr   error
	Override policy.Action // empty when no `when` entry matched
	Stdout   string
	Stderr   string
}

// RunCustomCheck executes the script or inline command described by cc and
// evaluates its `when` list against the resulting exit code. A timeout
// (default policy.DefaultCustomCheckTimeoutSeconds) always applies so a
// hanging script can never block guard check indefinitely; on timeout the
// caller should fall back to the rule's own action (Override is left empty).
func RunCustomCheck(cc *policy.CustomCheck) CustomCheckResult {
	timeout := cc.TimeoutSeconds
	if timeout <= 0 {
		timeout = policy.DefaultCustomCheckTimeoutSeconds
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if cc.Path != "" {
		cmd = exec.CommandContext(ctx, cc.Path, cc.Args...)
	} else {
		args := append([]string{"-c", cc.Command}, cc.Args...)
		cmd = exec.CommandContext(ctx, "sh", args...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result := CustomCheckResult{Ran: true}
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.RunErr = ctx.Err()
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		return result
	}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		result.ExitCode = 0
	case errors.As(err, &exitErr):
		result.ExitCode = exitErr.ExitCode()
	default:
		result.RunErr = err
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		return result
	}

	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	for _, w := range cc.When {
		if w.Key != "exit" || w.Value != result.ExitCode {
			continue
		}
		result.Override = w.Action
		break
	}

	return result
}
