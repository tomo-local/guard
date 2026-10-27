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
	if err := mkdirAllLogDir(dir); err != nil {
		return err
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

// mkdirAllLogDir is os.MkdirAll with a clearer error when dir already
// exists as a regular file -- the shape a GUARD_LOG_PATH/
// GUARD_RECORD_LOG_PATH left over from before logs were split by day
// (pointing at a single check.jsonl/record.jsonl file, not a directory)
// would produce.
func mkdirAllLogDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if info, statErr := os.Stat(dir); statErr == nil && !info.IsDir() {
			return fmt.Errorf("%q already exists as a file, not a directory -- if GUARD_LOG_PATH/GUARD_RECORD_LOG_PATH (or -log/-record-log) is still set to a pre-split check.jsonl/record.jsonl path, point it at a directory instead (e.g. \".guard/logs/check\"): %w", dir, err)
		}
		return fmt.Errorf("create log dir %q: %w", dir, err)
	}
	return nil
}
