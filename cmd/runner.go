package cmd

import (
	"time"

	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/procexec"
)

// procRunner runs a rule's options.customCheck as an external process. It
// is the outside implementation of the port policy.CustomCheckRunner, and
// lives here because this package is where policies are wired to the world.
type procRunner struct{}

func (procRunner) Run(timeout time.Duration, name string, args []string) policy.ProcResult {
	r := procexec.Run(timeout, name, args, nil)
	return policy.ProcResult{
		Stdout:   r.Stdout,
		Stderr:   r.Stderr,
		TimedOut: r.TimedOut,
		ExitCode: r.ExitCode,
		RunErr:   r.RunErr,
	}
}
