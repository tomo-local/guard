package sidecar

import (
	"encoding/json"
	"time"

	"ai-guard-tracker/internal/hookproto"
)

// RecordLogEntry is one JSONL record in the record log: the Bash command
// Claude Code actually ran and its raw result, as reported by the
// PostToolUse hook. Stdout/Stderr are stored verbatim, not summarized --
// this is the raw record; any summarizing belongs in `guard report`.
type RecordLogEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	SessionID   string    `json:"session_id,omitempty"`
	ToolUseID   string    `json:"tool_use_id,omitempty"`
	Command     string    `json:"command"`
	DurationMs  *int      `json:"duration_ms,omitempty"`
	Interrupted bool      `json:"interrupted,omitempty"`
	Stdout      string    `json:"stdout,omitempty"`
	Stderr      string    `json:"stderr,omitempty"`
}

// BuildRecordLogEntry parses raw as a Claude Code PostToolUse hook
// payload and builds the entry to record. ok is false when there's
// nothing to record: the payload isn't recognizable, or ToolName isn't
// "Bash" (guard's scope is Bash only, per the PreToolUse side too).
func BuildRecordLogEntry(raw []byte) (entry RecordLogEntry, ok bool) {
	var in hookproto.PostToolUseInput
	if err := json.Unmarshal(raw, &in); err != nil || in.ToolName == "" {
		return RecordLogEntry{}, false
	}
	if in.ToolName != "Bash" {
		return RecordLogEntry{}, false
	}

	var bashInput hookproto.BashToolInput
	_ = json.Unmarshal(in.ToolInput, &bashInput)

	var resp hookproto.BashToolResponse
	_ = json.Unmarshal(in.ToolResponse, &resp)

	entry = RecordLogEntry{
		Timestamp:   time.Now().UTC(),
		SessionID:   in.SessionID,
		ToolUseID:   in.ToolUseID,
		Command:     bashInput.Command,
		DurationMs:  in.DurationMs,
		Interrupted: resp.Interrupted,
		Stdout:      resp.Stdout,
		Stderr:      resp.Stderr,
	}
	return entry, true
}

// AppendRecordLog appends entry as one JSON line to dir's log file for
// entry.Timestamp's day (UTC).
func AppendRecordLog(dir string, entry RecordLogEntry) error {
	return appendJSONLine(dir, entry.Timestamp, entry)
}
