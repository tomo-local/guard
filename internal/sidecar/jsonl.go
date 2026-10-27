package sidecar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// appendJSONLine marshals v and appends it as one line to dir's log file
// for date's day (UTC), creating the directory and file as needed. JSON
// Lines keeps this a single atomic O_APPEND write regardless of how many
// guard processes run concurrently (e.g. parallel Bash tool calls) -- no
// read-modify-write cycle, no locking needed, and each day's file stays
// streamable/grep-able. Splitting by day (rather than one ever-growing
// file) keeps individual files small as usage accumulates. Shared by
// AppendCheckLog and AppendRecordLog.
func appendJSONLine(dir string, date time.Time, v any) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	path := filepath.Join(dir, date.UTC().Format("2006-01-02")+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", path, err)
	}
	defer f.Close()

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}
	data = append(data, '\n')

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write log entry: %w", err)
	}
	return nil
}
