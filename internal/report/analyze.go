// Package report aggregates ai-guard-tracker's .guard/logs/check/*.jsonl
// and .guard/logs/record/*.jsonl files (one per UTC day) into summary
// statistics for `guard report`. It only reads and counts -- it never
// re-evaluates policies, so it has no dependency on internal/policy or
// internal/engine.
package report

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"ai-guard-tracker/internal/sidecar"
)

// topN is the fixed size of Report.TopDenyConfirm. Not exposed as a flag
// in v1 -- see PLAN.md for the reasoning.
const topN = 5

// Report is the aggregated result of analyzing the check and record log
// directories. A missing or unreadable log is not a fatal error: it is
// recorded in CheckLogErr/RecordLogErr and the rest of the report is
// still produced from whatever could be read.
type Report struct {
	GeneratedAt time.Time

	CheckEntries       int
	CheckSkippedLines  int
	CheckLogErr        error
	RecordEntries      int
	RecordSkippedLines int
	RecordLogErr       error

	ActionCounts       map[string]int // "ALLOW"/"DENY"/"CONFIRM" -> count, by final decision
	ModeCounts         map[string]int // "cli"/"hook" -> count
	DefaultCount       int
	FallbackParseCount int

	ToolActionCounts map[string]map[string]int // tool -> action -> count (per decomposed command)
	TopDenyConfirm   []ToolFunctionCount

	CustomCheckRuns      int
	CustomCheckTimeouts  int
	CustomCheckOverrides map[string]int // override action -> count (empty override excluded)

	DailyTimeline []DailyCount

	RecordInterrupted int
}

// ToolFunctionCount is one row of Report.TopDenyConfirm: how often a
// given tool+function combination was decided DENY or CONFIRM. JSON
// tags keep RenderJSON's output snake_case throughout, matching
// jsonReport's own fields.
type ToolFunctionCount struct {
	Tool         string `json:"tool"`
	Function     string `json:"function"`
	DenyCount    int    `json:"deny_count"`
	ConfirmCount int    `json:"confirm_count"`
}

// DailyCount is one day's worth of check log entries, broken down by
// final decision action.
type DailyCount struct {
	Date   string         `json:"date"` // YYYY-MM-DD, UTC
	Counts map[string]int `json:"counts"`
}

// Analyze reads every *.jsonl file under checkLogDir and recordLogDir
// and builds a Report. Neither directory needs to exist; Analyze
// degrades gracefully rather than returning an error, since a report
// should still be produced (even if mostly empty) when nothing has been
// logged yet.
func Analyze(checkLogDir, recordLogDir string) Report {
	r := Report{
		GeneratedAt:          time.Now().UTC(),
		ActionCounts:         map[string]int{},
		ModeCounts:           map[string]int{},
		ToolActionCounts:     map[string]map[string]int{},
		CustomCheckOverrides: map[string]int{},
	}

	denyConfirm := map[[2]string]*ToolFunctionCount{} // [tool, function] -> counts
	daily := map[string]map[string]int{}

	var checkTotalLines int
	checkTotalLines, r.CheckLogErr = forEachLineInDir(checkLogDir, func(line []byte) {
		var entry sidecar.CheckLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			r.CheckSkippedLines++
			return
		}

		r.ActionCounts[entry.Action]++
		if entry.Mode != "" {
			r.ModeCounts[entry.Mode]++
		}
		if entry.Default {
			r.DefaultCount++
		}
		if entry.UsedFallbackParse {
			r.FallbackParseCount++
		}

		if entry.CustomCheck != nil {
			r.CustomCheckRuns++
			if entry.CustomCheck.TimedOut {
				r.CustomCheckTimeouts++
			}
			if entry.CustomCheck.Override != "" {
				r.CustomCheckOverrides[entry.CustomCheck.Override]++
			}
		}

		date := entry.Timestamp.UTC().Format("2006-01-02")
		if daily[date] == nil {
			daily[date] = map[string]int{}
		}
		daily[date][entry.Action]++

		for _, cmd := range entry.Commands {
			if cmd.Tool == "" {
				continue
			}
			if r.ToolActionCounts[cmd.Tool] == nil {
				r.ToolActionCounts[cmd.Tool] = map[string]int{}
			}
			r.ToolActionCounts[cmd.Tool][cmd.Action]++

			if cmd.Function == "" {
				continue
			}
			if cmd.Action != "DENY" && cmd.Action != "CONFIRM" {
				continue
			}
			key := [2]string{cmd.Tool, cmd.Function}
			tf := denyConfirm[key]
			if tf == nil {
				tf = &ToolFunctionCount{Tool: cmd.Tool, Function: cmd.Function}
				denyConfirm[key] = tf
			}
			if cmd.Action == "DENY" {
				tf.DenyCount++
			} else {
				tf.ConfirmCount++
			}
		}
	})
	r.CheckEntries = checkTotalLines - r.CheckSkippedLines

	var recordTotalLines int
	recordTotalLines, r.RecordLogErr = forEachLineInDir(recordLogDir, func(line []byte) {
		var entry sidecar.RecordLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			r.RecordSkippedLines++
			return
		}
		if entry.Interrupted {
			r.RecordInterrupted++
		}
	})
	r.RecordEntries = recordTotalLines - r.RecordSkippedLines

	for _, tf := range denyConfirm {
		r.TopDenyConfirm = append(r.TopDenyConfirm, *tf)
	}
	sort.Slice(r.TopDenyConfirm, func(i, j int) bool {
		a, b := r.TopDenyConfirm[i], r.TopDenyConfirm[j]
		at, bt := a.DenyCount+a.ConfirmCount, b.DenyCount+b.ConfirmCount
		if at != bt {
			return at > bt
		}
		if a.Tool != b.Tool {
			return a.Tool < b.Tool
		}
		return a.Function < b.Function
	})
	if len(r.TopDenyConfirm) > topN {
		r.TopDenyConfirm = r.TopDenyConfirm[:topN]
	}

	dates := make([]string, 0, len(daily))
	for d := range daily {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	for _, d := range dates {
		r.DailyTimeline = append(r.DailyTimeline, DailyCount{Date: d, Counts: daily[d]})
	}

	return r
}

// forEachLineInDir reads every *.jsonl file directly under dir, in
// filename order (which is also chronological order given the
// YYYY-MM-DD.jsonl naming from internal/sidecar), and calls fn with each
// non-empty line's raw bytes. It returns the total number of such lines
// seen across all files (successfully parsed or not -- fn itself is
// responsible for counting its own parse failures) and a non-nil error
// only when dir itself couldn't be read (including "does not exist").
// An existing-but-empty dir (e.g. right after `guard init`, before
// anything has been logged yet) is not an error.
func forEachLineInDir(dir string, fn func(line []byte)) (totalLines int, err error) {
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		return 0, readErr
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".jsonl" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		n, err := forEachLine(filepath.Join(dir, name), fn)
		totalLines += n
		if err != nil {
			return totalLines, err
		}
	}
	return totalLines, nil
}

// forEachLine opens path and calls fn with each non-empty line's raw
// bytes, returning the number of such lines seen (successfully parsed or
// not -- fn itself is responsible for counting its own parse failures)
// and a non-nil error only when the file couldn't be opened at all.
func forEachLine(path string, fn func(line []byte)) (totalLines int, err error) {
	f, openErr := os.Open(path)
	if openErr != nil {
		return 0, openErr
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		fn(line)
		totalLines++
	}
	return totalLines, scanner.Err()
}
