---
name: guard-reports
description: Summarize ai-guard-tracker's check/record logs for the user and generate a shareable HTML report via `guard report`. Use when the user asks for a guard report, wants to see guard activity/stats, or runs /guard-reports.
---

# guard-reports

Do not improvise a summary by hand-parsing `.guard/logs/*.jsonl` yourself --
`guard report` already does the aggregation correctly (the strictest-wins
and customCheck accounting in particular), so hand-parsing only duplicates
that work and risks being misleading.

This skill's job is:

1. Run `guard report --format md` and relay its summary to the user in
   chat -- action/tool/mode counts, which tools are triggering
   deny/confirm most often, and anything about customCheck executions
   (exit codes, timeouts, which `when` overrides fired). Use `md`
   specifically here (not the default `html`, which isn't meant to be
   pasted into chat, and not `json`, which isn't meant to be read by a
   person).
2. Generate the shareable artifact:
   ```bash
   guard report --format html --out .guard/report.html
   ```
   and tell the user the path (`.guard/report.html`). Don't print the raw
   HTML into chat.
3. If the user wants the data for scripting rather than reading, `guard
   report --format json` is the one meant for that.
4. If the logs are empty or very small, say so plainly rather than treating
   a thin report as if it were meaningful.
5. If the user asks for a specific slice (a date range, one tool, only
   denies), check `guard report -h` for supported flags before assuming
   none exist, and otherwise say that filtering isn't supported yet.
