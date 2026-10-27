// Package procexec runs an external command with a timeout and captures
// its stdout/stderr. It exists because this CLI shells out to an
// external process in exactly two places -- cmd's customCheck
// runner (policy.CustomCheckRunner) and internal/sidecar's DuckDB sync --
// and both need the same "bounded by a timeout, never panics on a
// failing or timed-out command" behavior.
package procexec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// Result is the outcome of Run. ExitCode is only meaningful when RunErr
// is nil and TimedOut is false.
type Result struct {
	Stdout   string
	Stderr   string
	TimedOut bool
	ExitCode int
	// RunErr is non-nil only for a failure that isn't an ordinary
	// nonzero exit (e.g. the binary doesn't exist) or a timeout; nil on
	// success and on any ordinary exit code.
	RunErr error
}

// Run executes name with args, bounded by timeout. If stdin is non-nil,
// it's fed to the process's standard input. Stdout/Stderr are always
// captured into Result, even on a timeout or a non-exit error.
func Run(timeout time.Duration, name string, args []string, stdin io.Reader) Result {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}

	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.RunErr = ctx.Err()
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
	}
	return result
}
