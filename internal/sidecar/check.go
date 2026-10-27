// Package sidecar appends structured audit records to .guard/logs/ as a
// side effect of guard check, independent of the decision logic itself.
package sidecar

import (
	"time"
)

// CheckLogEntry is one JSONL record in the check log: what was evaluated
// and what guard check decided.
type CheckLogEntry struct {
	Timestamp         time.Time       `json:"timestamp"`
	Mode              string          `json:"mode"` // "hook" | "cli"
	SessionID         string          `json:"session_id,omitempty"`
	RawCommand        string          `json:"raw_command"`
	Action            string          `json:"action"`
	Reason            string          `json:"reason,omitempty"`
	Next              string          `json:"next,omitempty"`
	MatchedPolicies   []string        `json:"matched_policies,omitempty"`
	Default           bool            `json:"default"`
	UsedFallbackParse bool            `json:"used_fallback_parse"`
	CustomCheck       *CustomCheckLog `json:"custom_check,omitempty"`
	Commands          []CommandLog    `json:"commands,omitempty"`
}

// CommandLog is the verdict for one simple command decomposed out of
// RawCommand (e.g. the two sides of "a && b").
type CommandLog struct {
	Tool   string `json:"tool"`
	Action string `json:"action"`
	// Label carries the matched rule's policy.To.Label (free-text,
	// purely descriptive -- see its doc comment), when set.
	Label string `json:"label,omitempty"`
	// Function is the engine's firstFunction heuristic (e.g. "push" for
	// "git push -f"), empty for flat tools with no subcommand. Added for
	// guard report's tool+function aggregation -- entries logged before
	// this field existed simply omit it.
	Function string `json:"function,omitempty"`
}

type CustomCheckLog struct {
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
	Override string `json:"override,omitempty"`
}

// AppendCheckLog appends entry as one JSON line to path. See
// appendJSONLine for why JSON Lines (rather than a rewritten JSON array)
// is safe under concurrent `guard check` processes.
func AppendCheckLog(path string, entry CheckLogEntry) error {
	return appendJSONLine(path, entry)
}
