package cmd

import (
	"strings"
	"testing"

	"ai-guard-tracker/internal/policy"
)

func sampleIssues() []policy.Issue {
	return []policy.Issue{
		{Severity: policy.SeverityWarning, Path: "b.yaml", Message: "w1"},
		{Severity: policy.SeverityError, Path: "b.yaml", Message: "e1"},
		{Severity: policy.SeverityError, Path: "a.yaml", Message: "e2"},
		{Severity: policy.SeverityError, Path: "a.yaml", Message: "e3"},
	}
}

func TestFormatIssues_GroupsByFileAndSummarizes(t *testing.T) {
	got := formatIssues(sampleIssues(), painter{})

	want := `a.yaml
  error    e2
  error    e3

b.yaml
  error    e1
  warning  w1

3 errors, 1 warning in 2 files
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatIssues_ColorOnlyWhenEnabled(t *testing.T) {
	if strings.Contains(formatIssues(sampleIssues(), painter{}), "\x1b[") {
		t.Fatal("no ANSI codes expected when color is off")
	}
	colored := formatIssues(sampleIssues(), painter{on: true})
	if !strings.Contains(colored, "\x1b[31m") || !strings.Contains(colored, "\x1b[33m") {
		t.Fatalf("expected red and yellow codes, got %q", colored)
	}
}

func TestFormatIssues_RendersDetailsAsLabeledLines(t *testing.T) {
	issues := []policy.Issue{{
		Severity: policy.SeverityError,
		Path:     "a.yaml",
		Message:  "overlaps with b.yaml",
		Details: []policy.Detail{
			{Label: "example", Value: "`git push -f` matches both"},
			{Label: "this file", Value: "functions=[push]"},
		},
	}}

	got := formatIssues(issues, painter{})

	want := `a.yaml
  error    overlaps with b.yaml
      example     ` + "`git push -f`" + ` matches both
      this file   functions=[push]

1 error, 0 warnings in 1 file
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
