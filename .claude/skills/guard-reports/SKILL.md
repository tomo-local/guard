---
name: guard-reports
description: Summarize ai-guard-tracker's check/record logs for the user as an insight report, and generate a shareable plain HTML file via `guard report` (or a session-correlated variant written with the Write tool). Use when the user asks for a guard report, wants to see guard activity/stats, or runs /guard-reports.
---

# guard-reports

Do not improvise a summary by hand-parsing `.guard/logs/*.jsonl` yourself --
`guard report` already does the aggregation correctly (the strictest-wins
and customCheck accounting in particular), so hand-parsing only duplicates
that work and risks being misleading.

This skill's job is:

1. Run `guard report --format json` and parse its output. This is the data
   source for the insight report below -- use `json`, not `md`/`html`
   (those are pre-rendered documents meant for relaying/sharing as-is, not
   for you to analyze).
2. Write an **insight report** in chat from that JSON -- not a mechanical
   restating of every field. Look at the data and call out whatever is
   actually notable, with the numbers that back it up. Things worth
   checking:
   - `top_deny_confirm`: which tool+function combos dominate DENY/CONFIRM,
     and whether one or two account for most of it (a concentration worth
     naming) versus being spread thin.
   - `default_count` / `fallback_parse_count` relative to `check_entries`:
     a high ratio means policies aren't covering real usage well, or the
     shell parser is frequently falling back -- both are gaps worth
     flagging, not just numbers to restate.
   - `daily_timeline`: look for a trend or a spike on a specific day
     rather than just reading off the per-day counts.
   - `custom_check_*` fields: frequent overrides to ALLOW can mean the
     base policy is stricter than it needs to be; frequent timeouts can
     mean the check script itself is unreliable.
   - `record_interrupted` relative to `record_entries`: a high interrupt
     rate is worth surfacing on its own.
   Prefer 2-4 concrete, specific observations over a generic "everything
   looks fine" -- if nothing stands out, say that plainly instead of
   manufacturing insight from thin data.
3. Generate the shareable report as a **plain HTML file on disk** --
   never via the Artifact tool / claude.ai artifacts. This is a local
   dev-tool report, not something to publish:
   ```bash
   guard report --format html --out .guard/report.html
   ```
   and tell the user the path (`.guard/report.html`). Don't print the raw
   HTML into chat, and don't call the Artifact tool for it.
4. If the logs are empty or very small (check `check_entries`/
   `record_entries` in the JSON), say so plainly rather than treating a
   thin report as if it were meaningful.
5. If the user asks for a specific slice (a date range, one tool, only
   denies), check `guard report -h` for supported flags before assuming
   none exist, and otherwise say that filtering isn't supported yet.

## Session-correlated insights report

If the user asks for something closer to Claude Code's own `/insights`
report -- e.g. explaining *why* DENY/CONFIRM happened by cross-referencing
their own past Claude Code sessions, over a given date range -- this is a
different, heavier report. Still **never use the Artifact tool** for it;
always write a plain `.html` file to disk with the Write tool.

1. Read the raw `.guard/logs/check.jsonl` entries for the requested period
   directly (this is fine here -- unlike step 1 above, the goal is per-event
   timestamps/commands for correlation, not re-deriving `guard report`'s
   aggregation).
2. Find session transcripts under `~/.claude/projects/*/*.jsonl` whose
   timestamp range overlaps the period, and read the messages immediately
   around each DENY/CONFIRM cluster's timestamps to explain what task was
   actually in progress (e.g. "this DENY was a manual sanity-check while
   refactoring policy X", not a real blocked action). Call this out plainly
   when the log turns out to be dev/test activity rather than real usage --
   don't let a misleading tool-level stat (e.g. "git dominates DENY") stand
   without that context.
3. Write the result as a **plain static HTML file** via the Write tool
   (e.g. `.guard/insights-report.html`), not the Artifact tool. Style it
   like a lightweight analytics report: an at-a-glance summary box, stat
   tiles, bar charts for tool/day breakdowns, and short cards describing
   each session cluster with the commands involved. Tell the user the file
   path when done; don't publish it anywhere.
