package report

import (
	"encoding/json"
	"io"
	"time"
)

// jsonReport mirrors Report for JSON output, except CheckLogErr/
// RecordLogErr (plain error values, which encoding/json can't render
// usefully -- most concrete error types have no exported fields and
// would just marshal to "{}") become plain strings, empty when nil.
type jsonReport struct {
	GeneratedAt time.Time `json:"generated_at"`

	CheckEntries      int    `json:"check_entries"`
	CheckSkippedLines int    `json:"check_skipped_lines"`
	CheckLogError     string `json:"check_log_error,omitempty"`

	RecordEntries      int    `json:"record_entries"`
	RecordSkippedLines int    `json:"record_skipped_lines"`
	RecordLogError     string `json:"record_log_error,omitempty"`

	ActionCounts       map[string]int `json:"action_counts"`
	ModeCounts         map[string]int `json:"mode_counts"`
	DefaultCount       int            `json:"default_count"`
	FallbackParseCount int            `json:"fallback_parse_count"`

	ToolActionCounts map[string]map[string]int `json:"tool_action_counts"`
	TopDenyConfirm   []ToolFunctionCount       `json:"top_deny_confirm"`

	CustomCheckRuns      int            `json:"custom_check_runs"`
	CustomCheckTimeouts  int            `json:"custom_check_timeouts"`
	CustomCheckOverrides map[string]int `json:"custom_check_overrides"`

	DailyTimeline []DailyCount `json:"daily_timeline"`

	RecordInterrupted int `json:"record_interrupted"`
}

// RenderJSON writes r as indented JSON to w -- the same data
// RenderMarkdown/RenderHTML render, for callers that want to parse the
// report programmatically instead.
func RenderJSON(w io.Writer, r Report) error {
	jr := jsonReport{
		GeneratedAt: r.GeneratedAt,

		CheckEntries:      r.CheckEntries,
		CheckSkippedLines: r.CheckSkippedLines,
		CheckLogError:     errString(r.CheckLogErr),

		RecordEntries:      r.RecordEntries,
		RecordSkippedLines: r.RecordSkippedLines,
		RecordLogError:     errString(r.RecordLogErr),

		ActionCounts:       r.ActionCounts,
		ModeCounts:         r.ModeCounts,
		DefaultCount:       r.DefaultCount,
		FallbackParseCount: r.FallbackParseCount,

		ToolActionCounts: r.ToolActionCounts,
		TopDenyConfirm:   r.TopDenyConfirm,

		CustomCheckRuns:      r.CustomCheckRuns,
		CustomCheckTimeouts:  r.CustomCheckTimeouts,
		CustomCheckOverrides: r.CustomCheckOverrides,

		DailyTimeline: r.DailyTimeline,

		RecordInterrupted: r.RecordInterrupted,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jr)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
