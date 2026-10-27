package procexec

import (
	"strings"
	"testing"
	"time"
)

func TestRun_SuccessCapturesStdout(t *testing.T) {
	r := Run(5*time.Second, "sh", []string{"-c", "echo hi"}, nil)
	if r.RunErr != nil || r.TimedOut {
		t.Fatalf("unexpected failure: %+v", r)
	}
	if r.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", r.ExitCode)
	}
	if strings.TrimSpace(r.Stdout) != "hi" {
		t.Fatalf("Stdout = %q, want %q", r.Stdout, "hi")
	}
}

func TestRun_NonzeroExitIsNotRunErr(t *testing.T) {
	r := Run(5*time.Second, "sh", []string{"-c", "exit 7"}, nil)
	if r.RunErr != nil {
		t.Fatalf("expected RunErr=nil for an ordinary nonzero exit, got %v", r.RunErr)
	}
	if r.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", r.ExitCode)
	}
}

func TestRun_StdinIsFedToProcess(t *testing.T) {
	r := Run(5*time.Second, "cat", nil, strings.NewReader("from stdin"))
	if r.RunErr != nil {
		t.Fatalf("unexpected RunErr: %v", r.RunErr)
	}
	if r.Stdout != "from stdin" {
		t.Fatalf("Stdout = %q, want %q", r.Stdout, "from stdin")
	}
}

func TestRun_Timeout(t *testing.T) {
	r := Run(50*time.Millisecond, "sleep", []string{"5"}, nil)
	if !r.TimedOut {
		t.Fatalf("expected TimedOut=true, got %+v", r)
	}
	if r.RunErr == nil {
		t.Fatalf("expected RunErr to be set on timeout")
	}
}

func TestRun_CommandNotFoundSetsRunErr(t *testing.T) {
	r := Run(5*time.Second, "this-binary-does-not-exist-xyz", nil, nil)
	if r.RunErr == nil {
		t.Fatalf("expected RunErr for a nonexistent binary, got %+v", r)
	}
	if r.TimedOut {
		t.Fatalf("expected TimedOut=false for a command-not-found failure")
	}
}

func TestRun_StderrIsCaptured(t *testing.T) {
	r := Run(5*time.Second, "sh", []string{"-c", "echo oops >&2"}, nil)
	if strings.TrimSpace(r.Stderr) != "oops" {
		t.Fatalf("Stderr = %q, want %q", r.Stderr, "oops")
	}
}
