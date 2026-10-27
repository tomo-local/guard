package engine

import (
	"time"

	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/procexec"
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

	var name string
	var args []string
	if cc.Path != "" {
		name, args = cc.Path, cc.Args
	} else {
		name, args = "sh", append([]string{"-c", cc.Command}, cc.Args...)
	}

	run := procexec.Run(time.Duration(timeout)*time.Second, name, args, nil)

	result := CustomCheckResult{
		Ran:      true,
		ExitCode: run.ExitCode,
		TimedOut: run.TimedOut,
		RunErr:   run.RunErr,
		Stdout:   run.Stdout,
		Stderr:   run.Stderr,
	}
	if result.TimedOut || result.RunErr != nil {
		return result
	}

	for _, w := range cc.When {
		if w.Key != "exit" || w.Value != result.ExitCode {
			continue
		}
		result.Override = w.Action
		break
	}

	return result
}
