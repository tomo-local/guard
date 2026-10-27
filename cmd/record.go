package cmd

import (
	"flag"
	"fmt"
	"io"

	"ai-guard-tracker/internal/sidecar"
)

const recordUsage = `Usage:
  <hook-json> | guard record    # PostToolUse hook payload on stdin

Options:
  -log string   record log directory, one file per UTC day (default ".guard/logs/record", env GUARD_RECORD_LOG_PATH)

Precedence: -flag > env var > built-in default.

guard record only reads a Claude Code PostToolUse hook payload from
stdin -- there is no CLI mode. tool_name != "Bash" is a silent no-op.
Always exits 0: recording must never block or alter Claude's flow, and a
log-write failure is only a warning on stderr.
`

// RunRecord parses a PostToolUse hook payload from stdin and appends what
// Bash actually ran (and its raw output) to the record log. The parsing
// and log-entry construction live in internal/sidecar; this is just flag
// handling and wiring.
func (c *Cmd) RunRecord(args []string) error {
	flagSet := flag.NewFlagSet("record", flag.ContinueOnError)
	flagSet.SetOutput(c.Stderr)
	logDir := flagSet.String("log", defaultRecordLogDir(), "record log directory (env GUARD_RECORD_LOG_PATH)")
	flagSet.Usage = func() { fmt.Fprint(c.Stderr, recordUsage) }
	if err := flagSet.Parse(args); err != nil {
		return err
	}

	data, err := io.ReadAll(c.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	entry, ok := sidecar.BuildRecordLogEntry(data)
	if !ok {
		return nil
	}

	if err := sidecar.AppendRecordLog(*logDir, entry); err != nil {
		fmt.Fprintf(c.Stderr, "warning: failed to write record log: %v\n", err)
	}
	return nil
}
