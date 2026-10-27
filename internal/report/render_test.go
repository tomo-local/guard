package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sampleReport() Report {
	return Report{
		CheckEntries:  3,
		RecordEntries: 2,
		ActionCounts:  map[string]int{"ALLOW": 1, "DENY": 1, "CONFIRM": 1},
		ModeCounts:    map[string]int{"cli": 1, "hook": 2},
		DefaultCount:  1,
		ToolActionCounts: map[string]map[string]int{
			"git": {"DENY": 1, "CONFIRM": 1},
		},
		TopDenyConfirm: []ToolFunctionCount{
			{Tool: "git", Function: "push", DenyCount: 1, ConfirmCount: 1},
		},
		CustomCheckRuns:      1,
		CustomCheckOverrides: map[string]int{"ALLOW": 1},
		DailyTimeline: []DailyCount{
			{Date: "2026-10-20", Counts: map[string]int{"DENY": 1}},
		},
		RecordInterrupted: 1,
	}
}

func TestRenderMarkdown_ContainsKeyFigures(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	out := buf.String()

	if !strings.HasPrefix(out, "# guard report") {
		t.Fatalf("expected output to start with a top-level heading, got:\n%s", out[:min(100, len(out))])
	}
	for _, want := range []string{"ALLOW", "DENY", "CONFIRM", "git", "push", "2026-10-20", "| tool | function | deny | confirm |"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderMarkdown output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderMarkdown_EmptyReportSaysSo(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, Report{}); err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(buf.String(), "No check.jsonl entries") {
		t.Errorf("expected an empty-report message, got:\n%s", buf.String())
	}
}

func TestRenderJSON_RoundTripsKeyFigures(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("RenderJSON produced invalid JSON: %v\n%s", err, buf.String())
	}

	if decoded["check_entries"] != float64(3) {
		t.Errorf("check_entries = %v, want 3", decoded["check_entries"])
	}
	actionCounts, ok := decoded["action_counts"].(map[string]any)
	if !ok || actionCounts["DENY"] != float64(1) {
		t.Errorf("action_counts = %v, want DENY=1", decoded["action_counts"])
	}
	top, ok := decoded["top_deny_confirm"].([]any)
	if !ok || len(top) != 1 {
		t.Fatalf("top_deny_confirm = %v, want exactly 1 entry", decoded["top_deny_confirm"])
	}
}

func TestRenderJSON_ErrorFieldsBecomeStrings(t *testing.T) {
	r := sampleReport()
	r.CheckLogErr = errOf("boom")

	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"check_log_error": "boom"`) {
		t.Errorf("expected check_log_error to render as a plain string, got:\n%s", buf.String())
	}
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func errOf(msg string) error { return simpleErr(msg) }

func TestRenderHTML_ProducesWellFormedDocument(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	out := buf.String()

	if !strings.HasPrefix(out, "<!doctype html>") {
		t.Fatalf("expected output to start with <!doctype html>, got:\n%s", out[:min(100, len(out))])
	}
	for _, want := range []string{"<title>guard report</title>", "git", "push", "2026-10-20"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderHTML output missing %q", want)
		}
	}
	if strings.Contains(out, "cdn.") || strings.Contains(out, "https://") || strings.Contains(out, "http://") {
		t.Errorf("expected no external resource references in the offline HTML report")
	}
}

func TestRenderHTML_EscapesUntrustedFunctionNames(t *testing.T) {
	r := sampleReport()
	r.TopDenyConfirm = []ToolFunctionCount{
		{Tool: "git", Function: `<script>alert(1)</script>`, DenyCount: 1},
	}

	var buf bytes.Buffer
	if err := RenderHTML(&buf, r); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Fatalf("expected the function name to be HTML-escaped, got raw <script> tag in output:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("expected the escaped form &lt;script&gt; in output")
	}
}
