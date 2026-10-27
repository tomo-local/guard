package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-guard-tracker/internal/sidecar"
)

// writeJSONL writes lines as one dated check/record log file under dir
// (dir/YYYY-MM-DD.jsonl, UTC), matching the layout internal/sidecar
// produces. All lines land in one file regardless of their own
// timestamps -- callers that need entries split across multiple days
// pass distinct dates and call this once per date.
func writeJSONL(t *testing.T, dir string, date time.Time, lines []any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, date.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer f.Close()
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
}

func TestAnalyze_MissingDirs(t *testing.T) {
	dir := t.TempDir()
	r := Analyze(filepath.Join(dir, "check"), filepath.Join(dir, "record"))

	if r.CheckLogErr == nil {
		t.Errorf("expected CheckLogErr for a missing directory")
	}
	if r.RecordLogErr == nil {
		t.Errorf("expected RecordLogErr for a missing directory")
	}
	if r.CheckEntries != 0 || r.RecordEntries != 0 {
		t.Errorf("expected zero entries, got check=%d record=%d", r.CheckEntries, r.RecordEntries)
	}
	if len(r.ActionCounts) != 0 || len(r.TopDenyConfirm) != 0 {
		t.Errorf("expected empty aggregates, got %+v / %+v", r.ActionCounts, r.TopDenyConfirm)
	}
}

func TestAnalyze_EmptyDirIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	checkDir := filepath.Join(dir, "check")
	recordDir := filepath.Join(dir, "record")
	if err := os.MkdirAll(checkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(recordDir, 0o755); err != nil {
		t.Fatal(err)
	}

	r := Analyze(checkDir, recordDir)

	if r.CheckLogErr != nil || r.RecordLogErr != nil {
		t.Fatalf("an existing-but-empty dir must not be an error, got CheckLogErr=%v RecordLogErr=%v", r.CheckLogErr, r.RecordLogErr)
	}
	if r.CheckEntries != 0 || r.RecordEntries != 0 {
		t.Errorf("expected zero entries, got check=%d record=%d", r.CheckEntries, r.RecordEntries)
	}
}

func TestAnalyze_ChecksAggregateByActionModeAndTool(t *testing.T) {
	dir := t.TempDir()
	checkDir := filepath.Join(dir, "check")

	day1 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

	writeJSONL(t, checkDir, day1, []any{
		sidecar.CheckLogEntry{
			Timestamp: day1, Mode: "cli", Action: "ALLOW",
			Commands: []sidecar.CommandLog{{Tool: "git", Action: "ALLOW", Function: "log"}},
		},
		sidecar.CheckLogEntry{
			Timestamp: day1, Mode: "hook", Action: "DENY", Default: false,
			Commands: []sidecar.CommandLog{{Tool: "git", Action: "DENY", Function: "push"}},
		},
	})
	writeJSONL(t, checkDir, day2, []any{
		sidecar.CheckLogEntry{
			Timestamp: day2, Mode: "hook", Action: "CONFIRM", UsedFallbackParse: true,
			Commands: []sidecar.CommandLog{{Tool: "git", Action: "CONFIRM", Function: "push"}},
		},
		sidecar.CheckLogEntry{
			Timestamp: day2, Mode: "hook", Action: "CONFIRM", Default: true,
			Commands: []sidecar.CommandLog{{Tool: "curl", Action: "CONFIRM", Function: ""}},
		},
	})

	r := Analyze(checkDir, filepath.Join(dir, "record"))

	if r.CheckLogErr != nil {
		t.Fatalf("unexpected CheckLogErr: %v", r.CheckLogErr)
	}
	if r.CheckEntries != 4 {
		t.Fatalf("CheckEntries = %d, want 4", r.CheckEntries)
	}
	if r.ActionCounts["ALLOW"] != 1 || r.ActionCounts["DENY"] != 1 || r.ActionCounts["CONFIRM"] != 2 {
		t.Fatalf("ActionCounts = %+v, want ALLOW=1 DENY=1 CONFIRM=2", r.ActionCounts)
	}
	if r.ModeCounts["cli"] != 1 || r.ModeCounts["hook"] != 3 {
		t.Fatalf("ModeCounts = %+v, want cli=1 hook=3", r.ModeCounts)
	}
	if r.DefaultCount != 1 {
		t.Fatalf("DefaultCount = %d, want 1", r.DefaultCount)
	}
	if r.FallbackParseCount != 1 {
		t.Fatalf("FallbackParseCount = %d, want 1", r.FallbackParseCount)
	}
	if r.ToolActionCounts["git"]["ALLOW"] != 1 || r.ToolActionCounts["git"]["DENY"] != 1 || r.ToolActionCounts["git"]["CONFIRM"] != 1 {
		t.Fatalf("ToolActionCounts[git] = %+v", r.ToolActionCounts["git"])
	}
	if r.ToolActionCounts["curl"]["CONFIRM"] != 1 {
		t.Fatalf("ToolActionCounts[curl] = %+v", r.ToolActionCounts["curl"])
	}

	if len(r.TopDenyConfirm) != 1 {
		t.Fatalf("TopDenyConfirm = %+v, want exactly 1 row (curl has no function)", r.TopDenyConfirm)
	}
	top := r.TopDenyConfirm[0]
	if top.Tool != "git" || top.Function != "push" || top.DenyCount != 1 || top.ConfirmCount != 1 {
		t.Fatalf("TopDenyConfirm[0] = %+v, want git/push deny=1 confirm=1", top)
	}

	if len(r.DailyTimeline) != 2 {
		t.Fatalf("DailyTimeline = %+v, want 2 days", r.DailyTimeline)
	}
	if r.DailyTimeline[0].Date != "2026-10-20" || r.DailyTimeline[1].Date != "2026-10-21" {
		t.Fatalf("DailyTimeline dates = %q, %q, want 2026-10-20 then 2026-10-21", r.DailyTimeline[0].Date, r.DailyTimeline[1].Date)
	}
}

func TestAnalyze_CustomCheckAggregation(t *testing.T) {
	dir := t.TempDir()
	checkDir := filepath.Join(dir, "check")

	entries := []any{
		sidecar.CheckLogEntry{Action: "ALLOW", CustomCheck: &sidecar.CustomCheckLog{ExitCode: 0, Override: "ALLOW"}},
		sidecar.CheckLogEntry{Action: "DENY", CustomCheck: &sidecar.CustomCheckLog{ExitCode: 1, TimedOut: true}},
		sidecar.CheckLogEntry{Action: "CONFIRM"}, // no customCheck at all
	}
	writeJSONL(t, checkDir, time.Now(), entries)

	r := Analyze(checkDir, filepath.Join(dir, "record"))

	if r.CustomCheckRuns != 2 {
		t.Fatalf("CustomCheckRuns = %d, want 2", r.CustomCheckRuns)
	}
	if r.CustomCheckTimeouts != 1 {
		t.Fatalf("CustomCheckTimeouts = %d, want 1", r.CustomCheckTimeouts)
	}
	if r.CustomCheckOverrides["ALLOW"] != 1 {
		t.Fatalf("CustomCheckOverrides = %+v, want ALLOW=1", r.CustomCheckOverrides)
	}
}

func TestAnalyze_MalformedLineIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	checkDir := filepath.Join(dir, "check")

	if err := os.MkdirAll(checkDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	good, _ := json.Marshal(sidecar.CheckLogEntry{Action: "ALLOW"})
	content := string(good) + "\n" + "not json at all\n" + string(good) + "\n"
	if err := os.WriteFile(filepath.Join(checkDir, "2026-10-27.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := Analyze(checkDir, filepath.Join(dir, "record"))

	if r.CheckLogErr != nil {
		t.Fatalf("a malformed line should not surface as CheckLogErr: %v", r.CheckLogErr)
	}
	if r.CheckSkippedLines != 1 {
		t.Fatalf("CheckSkippedLines = %d, want 1", r.CheckSkippedLines)
	}
	if r.CheckEntries != 2 {
		t.Fatalf("CheckEntries = %d, want 2 (the malformed line must not count)", r.CheckEntries)
	}
	if r.ActionCounts["ALLOW"] != 2 {
		t.Fatalf("ActionCounts[ALLOW] = %d, want 2", r.ActionCounts["ALLOW"])
	}
}

func TestAnalyze_RecordInterrupted(t *testing.T) {
	dir := t.TempDir()
	recordDir := filepath.Join(dir, "record")

	entries := []any{
		sidecar.RecordLogEntry{Command: "sleep 100", Interrupted: true},
		sidecar.RecordLogEntry{Command: "echo hi", Interrupted: false},
	}
	writeJSONL(t, recordDir, time.Now(), entries)

	r := Analyze(filepath.Join(dir, "check"), recordDir)

	if r.RecordEntries != 2 {
		t.Fatalf("RecordEntries = %d, want 2", r.RecordEntries)
	}
	if r.RecordInterrupted != 1 {
		t.Fatalf("RecordInterrupted = %d, want 1", r.RecordInterrupted)
	}
}

func TestAnalyze_TopDenyConfirmIsCappedAndSorted(t *testing.T) {
	dir := t.TempDir()
	checkDir := filepath.Join(dir, "check")

	// 6 distinct tool+function combos, each with a different deny+confirm
	// total, to exercise both the topN=5 cap and the descending sort.
	funcs := []string{"a", "b", "c", "d", "e", "f"}
	var entries []any
	for i, fn := range funcs {
		count := i + 1 // "a"=1 ... "f"=6
		for j := 0; j < count; j++ {
			entries = append(entries, sidecar.CheckLogEntry{
				Action:   "DENY",
				Commands: []sidecar.CommandLog{{Tool: "git", Action: "DENY", Function: fn}},
			})
		}
	}
	writeJSONL(t, checkDir, time.Now(), entries)

	r := Analyze(checkDir, filepath.Join(dir, "record"))

	if len(r.TopDenyConfirm) != topN {
		t.Fatalf("TopDenyConfirm length = %d, want %d", len(r.TopDenyConfirm), topN)
	}
	// Highest count ("f"=6) must be first, "a" (count=1) must be excluded.
	if r.TopDenyConfirm[0].Function != "f" || r.TopDenyConfirm[0].DenyCount != 6 {
		t.Fatalf("TopDenyConfirm[0] = %+v, want function=f count=6", r.TopDenyConfirm[0])
	}
	for _, tf := range r.TopDenyConfirm {
		if tf.Function == "a" {
			t.Fatalf("expected the lowest-count function 'a' to be excluded from top %d, got %+v", topN, r.TopDenyConfirm)
		}
	}
}
