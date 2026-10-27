package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"

	"ai-guard-tracker/internal/config"
	"ai-guard-tracker/internal/report"
	"ai-guard-tracker/internal/sidecar"
)

// The "self-managed state" (JSONL + in-memory aggregation) and DuckDB
// backends are deliberately kept behind two separate interfaces in two
// separate packages -- report.StateStore (internal/report, the
// rendering side) and sidecar.DuckDBStore (internal/sidecar, the
// persistence side) -- rather than one shared shape. RunReport talks to
// these interface values, not the concrete types, so a future backend
// only has to satisfy the one shape it actually matches.
var (
	stateStore  report.StateStore   = report.GoStateStore{}
	duckDBStore sidecar.DuckDBStore = sidecar.DuckDBCLIStore{}
)

// renderers are the "rendered document" formats: they all take the same
// Report and just differ in how they write it out, so -out (write to a
// file) vs. stdout is handled once, uniformly, for all of them. "duckdb"
// isn't one of these -- it persists to a real database rather than
// rendering a document, so it's handled separately in RunReport.
var renderers = map[string]func(io.Writer, report.Report) error{
	"md":   report.RenderMarkdown,
	"json": report.RenderJSON,
	"html": report.RenderHTML,
}

const reportUsage = `Usage:
  guard report [-log <path>] [-record-log <path>] [-format md|json|html|duckdb] [-out <path>]

Options:
  -log string         check decision log path (default ".guard/logs/check.jsonl", env GUARD_LOG_PATH)
  -record-log string  record log path (default ".guard/logs/record.jsonl", env GUARD_RECORD_LOG_PATH)
  -config string       project settings file path (default ".guard/config.yaml", env GUARD_CONFIG_PATH)
  -format string        "md", "json", "html" (default), or "duckdb"
  -out string            output file path. For md/json/html, printed to stdout when
                          omitted. For duckdb, falls back to the path saved by "guard
                          init" in -config when omitted; required if that's also unset.

-format duckdb loads check.jsonl/record.jsonl into a DuckDB database file
(full reload every run) and prints a month x action count summary. Requires
the duckdb CLI on PATH (e.g. "brew install duckdb"); the database file is
yours to open afterwards for ad-hoc SQL: duckdb <path>

Exit codes: 0=success (an empty or missing log is not an error), 1=usage/runtime error.
`

// RunReport aggregates .guard/logs/check.jsonl and record.jsonl into a
// summary, in one of four formats: a Markdown document, JSON, a
// self-contained HTML page (default), or loaded into a DuckDB database
// with a monthly summary printed. md/json/html print to stdout unless
// -out names a file to write instead; duckdb always needs a database
// path, from -out or from -config.
func (c *Cmd) RunReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	logPath := fs.String("log", defaultLogPath(), "check decision log path (env GUARD_LOG_PATH)")
	recordLogPath := fs.String("record-log", defaultRecordLogPath(), "record log path (env GUARD_RECORD_LOG_PATH)")
	configPath := fs.String("config", defaultConfigPath(), "project settings file path (env GUARD_CONFIG_PATH)")
	format := fs.String("format", "html", `output format: "md", "json", "html", or "duckdb"`)
	out := fs.String("out", "", "output file path (printed to stdout when omitted, except -format duckdb)")
	fs.Usage = func() { fmt.Fprint(c.Stderr, reportUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *format == "duckdb" {
		return c.runReportDuckDB(*logPath, *recordLogPath, *configPath, *out)
	}

	render, ok := renderers[*format]
	if !ok {
		fmt.Fprintf(c.Stderr, "unknown -format %q (want \"md\", \"json\", \"html\", or \"duckdb\")\n", *format)
		return &ExitError{Code: 1}
	}

	rpt := stateStore.Analyze(*logPath, *recordLogPath)

	if *out == "" {
		return render(c.Stdout, rpt)
	}
	f, err := os.Create(*out)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()
	if err := render(f, rpt); err != nil {
		return fmt.Errorf("render report: %w", err)
	}
	fmt.Fprintf(c.Stdout, "written to %s\n", *out)
	return nil
}

func (c *Cmd) runReportDuckDB(logPath, recordLogPath, configPath, out string) error {
	dbPath := out
	if dbPath == "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		dbPath = cfg.DuckDB.Path
	}
	if dbPath == "" {
		fmt.Fprint(c.Stderr, reportUsage)
		return &ExitError{Code: 1}
	}

	result, err := duckDBStore.Sync(logPath, recordLogPath, dbPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.Stdout, "database written to %s\n", dbPath)
	if !result.ChecksLoaded {
		fmt.Fprintln(c.Stdout, "check.jsonl is missing or empty -- nothing to summarize yet.")
	} else {
		fmt.Fprintln(c.Stdout, "\nMonthly summary (action counts):")
		fmt.Fprint(c.Stdout, result.MonthlySummary)
	}
	fmt.Fprintf(c.Stdout, "\nOpen it yourself for ad-hoc SQL: duckdb %s\n", dbPath)
	return nil
}
