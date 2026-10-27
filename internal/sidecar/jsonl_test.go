package sidecar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendCheckLog_SplitsByUTCDay(t *testing.T) {
	dir := t.TempDir()

	day1 := time.Date(2026, 10, 20, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 21, 0, 0, 1, 0, time.UTC)

	if err := AppendCheckLog(dir, CheckLogEntry{Timestamp: day1, Action: "ALLOW"}); err != nil {
		t.Fatalf("AppendCheckLog: %v", err)
	}
	if err := AppendCheckLog(dir, CheckLogEntry{Timestamp: day2, Action: "DENY"}); err != nil {
		t.Fatalf("AppendCheckLog: %v", err)
	}

	for _, want := range []string{"2026-10-20.jsonl", "2026-10-21.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Fatalf("expected %s to exist: %v", want, err)
		}
	}
}

func TestAppendCheckLog_SameDayAppendsToOneFile(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	if err := AppendCheckLog(dir, CheckLogEntry{Timestamp: day, Action: "ALLOW"}); err != nil {
		t.Fatalf("AppendCheckLog: %v", err)
	}
	if err := AppendCheckLog(dir, CheckLogEntry{Timestamp: day.Add(time.Hour), Action: "DENY"}); err != nil {
		t.Fatalf("AppendCheckLog: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-10-20.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := len(splitLines(data)); got != 2 {
		t.Fatalf("expected 2 lines in the day's file, got %d:\n%s", got, data)
	}
}

func TestAppendCheckLog_DateBoundaryIsUTCNotLocal(t *testing.T) {
	dir := t.TempDir()
	// 2026-10-20T23:30:00-05:00 is 2026-10-21T04:30:00Z -- the file must
	// be named after the UTC day, not the date as written in the offset.
	ts, err := time.Parse(time.RFC3339, "2026-10-20T23:30:00-05:00")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := AppendRecordLog(dir, RecordLogEntry{Timestamp: ts, Command: "echo hi"}); err != nil {
		t.Fatalf("AppendRecordLog: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "2026-10-21.jsonl")); err != nil {
		t.Fatalf("expected the UTC-day file 2026-10-21.jsonl to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-10-20.jsonl")); err == nil {
		t.Fatalf("did not expect a 2026-10-20.jsonl file (that's the local, not UTC, day)")
	}
}

func TestAppendCheckLog_OldFilePathGivesClearError(t *testing.T) {
	dir := t.TempDir()
	// Simulate a pre-split GUARD_LOG_PATH still pointing at the old
	// single-file layout (.guard/logs/check.jsonl) -- the log "dir" is
	// actually an existing regular file.
	oldFilePath := filepath.Join(dir, "check.jsonl")
	if err := os.WriteFile(oldFilePath, []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := AppendCheckLog(oldFilePath, CheckLogEntry{Timestamp: time.Now(), Action: "ALLOW"})
	if err == nil {
		t.Fatal("expected an error when the log dir is actually an existing file")
	}
	if !strings.Contains(err.Error(), "already exists as a file") {
		t.Fatalf("expected a hint about the old file-based layout, got: %v", err)
	}
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, string(data[start:i]))
			}
			start = i + 1
		}
	}
	return lines
}
