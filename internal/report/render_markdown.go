package report

import (
	"fmt"
	"io"
	"sort"
)

// RenderMarkdown writes r as a GitHub-flavored Markdown summary to w --
// headings and tables, so the output reads well both raw and rendered
// (e.g. pasted into a PR description or a chat message). Section order
// mirrors RenderHTML: overview, action/mode breakdown, default/fallback
// rates, per-tool breakdown, top deny/confirm tool+function, customCheck
// aggregation, daily timeline, then record.jsonl's execution facts.
func RenderMarkdown(w io.Writer, r Report) error {
	fmt.Fprintln(w, "# guard report")
	fmt.Fprintf(w, "\n_generated %s_\n", r.GeneratedAt.Format("2006-01-02 15:04:05 UTC"))

	fmt.Fprintln(w, "\n## Overview")
	fmt.Fprintln(w, "\n| log | entries | skipped lines | status |")
	fmt.Fprintln(w, "|---|---|---|---|")
	writeLogStatusRow(w, "check.jsonl", r.CheckEntries, r.CheckSkippedLines, r.CheckLogErr)
	writeLogStatusRow(w, "record.jsonl", r.RecordEntries, r.RecordSkippedLines, r.RecordLogErr)

	if r.CheckEntries == 0 {
		fmt.Fprintln(w, "\nNo check.jsonl entries to summarize yet.")
		return nil
	}

	fmt.Fprintln(w, "\n## Actions (final decision)")
	writeCountTableMD(w, "action", r.ActionCounts)

	fmt.Fprintln(w, "\n## Mode")
	writeCountTableMD(w, "mode", r.ModeCounts)

	fmt.Fprintln(w, "\n## Default / fallback rates")
	fmt.Fprintln(w, "\n| metric | count | total |")
	fmt.Fprintln(w, "|---|---|---|")
	fmt.Fprintf(w, "| no policy matched (default action used) | %d | %d |\n", r.DefaultCount, r.CheckEntries)
	fmt.Fprintf(w, "| shell parse fell back to whitespace split | %d | %d |\n", r.FallbackParseCount, r.CheckEntries)

	fmt.Fprintln(w, "\n## Per-tool action breakdown")
	if len(r.ToolActionCounts) == 0 {
		fmt.Fprintln(w, "\n(none)")
	} else {
		fmt.Fprintln(w, "\n| tool | action | count |")
		fmt.Fprintln(w, "|---|---|---|")
		tools := make([]string, 0, len(r.ToolActionCounts))
		for t := range r.ToolActionCounts {
			tools = append(tools, t)
		}
		sort.Strings(tools)
		for _, t := range tools {
			actions := make([]string, 0, len(r.ToolActionCounts[t]))
			for a := range r.ToolActionCounts[t] {
				actions = append(actions, a)
			}
			sort.Strings(actions)
			for _, a := range actions {
				fmt.Fprintf(w, "| %s | %s | %d |\n", t, a, r.ToolActionCounts[t][a])
			}
		}
	}

	fmt.Fprintln(w, "\n## Top DENY/CONFIRM by tool+function")
	if len(r.TopDenyConfirm) == 0 {
		fmt.Fprintln(w, "\n(none)")
	} else {
		fmt.Fprintln(w, "\n| tool | function | deny | confirm |")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, tf := range r.TopDenyConfirm {
			fmt.Fprintf(w, "| %s | %s | %d | %d |\n", tf.Tool, tf.Function, tf.DenyCount, tf.ConfirmCount)
		}
	}

	fmt.Fprintln(w, "\n## customCheck")
	fmt.Fprintf(w, "\nruns: %d, timeouts: %d\n", r.CustomCheckRuns, r.CustomCheckTimeouts)
	if len(r.CustomCheckOverrides) > 0 {
		fmt.Fprintln(w, "\n**overrides**")
		writeCountTableMD(w, "override action", r.CustomCheckOverrides)
	}

	fmt.Fprintln(w, "\n## Daily timeline (UTC)")
	if len(r.DailyTimeline) == 0 {
		fmt.Fprintln(w, "\n(none)")
	} else {
		fmt.Fprintln(w, "\n| date | breakdown |")
		fmt.Fprintln(w, "|---|---|")
		for _, d := range r.DailyTimeline {
			actions := make([]string, 0, len(d.Counts))
			for a := range d.Counts {
				actions = append(actions, a)
			}
			sort.Strings(actions)
			var breakdown string
			for i, a := range actions {
				if i > 0 {
					breakdown += ", "
				}
				breakdown += fmt.Sprintf("%s=%d", a, d.Counts[a])
			}
			fmt.Fprintf(w, "| %s | %s |\n", d.Date, breakdown)
		}
	}

	fmt.Fprintln(w, "\n## record.jsonl (actual execution)")
	fmt.Fprintf(w, "\nentries: %d, interrupted: %d\n", r.RecordEntries, r.RecordInterrupted)

	return nil
}

func writeLogStatusRow(w io.Writer, name string, entries, skipped int, err error) {
	switch {
	case err != nil:
		fmt.Fprintf(w, "| %s | %d | %d | unreadable (%v) |\n", name, entries, skipped, err)
	case skipped > 0:
		fmt.Fprintf(w, "| %s | %d | %d | ok (some lines skipped) |\n", name, entries, skipped)
	default:
		fmt.Fprintf(w, "| %s | %d | %d | ok |\n", name, entries, skipped)
	}
}

func writeCountTableMD(w io.Writer, keyHeader string, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "\n| %s | count |\n", keyHeader)
	fmt.Fprintln(w, "|---|---|")
	for _, k := range keys {
		fmt.Fprintf(w, "| %s | %d |\n", k, counts[k])
	}
}
