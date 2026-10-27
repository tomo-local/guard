package sidecar

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// duckDBTimeout bounds how long the external duckdb CLI may run for one
// sync. Not flag-configurable in v1 -- same convention as
// options.customCheck's fixed default timeout: add a flag later if a
// large enough log ever needs it.
const duckDBTimeout = 30 * time.Second

// SyncDuckDBResult reports what a DuckDBStore's Sync actually did.
type SyncDuckDBResult struct {
	ChecksLoaded   bool
	RecordsLoaded  bool
	MonthlySummary string // the database engine's own rendered table; empty when ChecksLoaded is false
}

// DuckDBStore persists check.jsonl/record.jsonl into a real SQL
// database file (a full reload on every call, not an incremental sync)
// and reports what it did. It lives in this package, not internal/report,
// because sidecar is where this CLI's data gets persisted -- JSONL via
// AppendCheckLog/AppendRecordLog today, optionally DuckDB too -- while
// internal/report's job is only to turn persisted data into a rendered
// report, not to own how or where that data is stored.
type DuckDBStore interface {
	Sync(checkLogPath, recordLogPath, dbPath string) (SyncDuckDBResult, error)
}

// DuckDBCLIStore is the DuckDBStore backed by shelling out to the
// external duckdb CLI rather than an embedded Go driver: DuckDB's own Go
// driver requires CGO, which would complicate this CLI's
// cross-compilation and Homebrew formula build, so this mirrors the
// existing options.customCheck pattern of delegating to an external
// process instead.
type DuckDBCLIStore struct{}

// Sync (re)builds dbPath's `checks`/`records` tables from
// checkLogPath/recordLogPath. JSON parsing itself is left entirely to
// DuckDB's read_json_auto, which infers the schema (including the
// nested commands[] struct array) straight from the JSONL files -- Go
// never touches the JSON here, so this schema can't drift from
// CheckLogEntry/RecordLogEntry without also being visible the next time
// someone runs this.
func (DuckDBCLIStore) Sync(checkLogPath, recordLogPath, dbPath string) (SyncDuckDBResult, error) {
	if _, err := exec.LookPath("duckdb"); err != nil {
		return SyncDuckDBResult{}, fmt.Errorf("duckdb CLI not found on PATH -- install it first (e.g. `brew install duckdb`; see https://duckdb.org/docs/installation/)")
	}

	var result SyncDuckDBResult
	var script strings.Builder

	// read_json_auto errors out on a missing file and infers a useless
	// single-JSON-column schema on an empty one, so a table is only
	// created when there's real data behind it -- a log with nothing in
	// it yet is a normal state (see internal/report/analyze.go's same
	// graceful-degradation stance), not an error.
	result.ChecksLoaded = nonEmptyFile(checkLogPath)
	if result.ChecksLoaded {
		fmt.Fprintf(&script, "CREATE OR REPLACE TABLE checks AS SELECT * FROM read_json_auto(%s);\n", sqlQuote(checkLogPath))
	}
	result.RecordsLoaded = nonEmptyFile(recordLogPath)
	if result.RecordsLoaded {
		fmt.Fprintf(&script, "CREATE OR REPLACE TABLE records AS SELECT * FROM read_json_auto(%s);\n", sqlQuote(recordLogPath))
	}

	if result.ChecksLoaded {
		fmt.Fprintln(&script, `CREATE OR REPLACE VIEW monthly_summary AS
SELECT strftime(timestamp, '%Y-%m') AS month, action, count(*) AS count
FROM checks
GROUP BY month, action
ORDER BY month, action;`)
		// -no-agent (passed as a CLI arg below) plus an explicit .mode
		// pins the output format regardless of the environment duckdb
		// runs in -- its "agent mode" auto-detects AI_AGENT/CLAUDECODE
		// and otherwise falls back to a box-drawing table when stdout
		// isn't a terminal, so without both of these the exact same
		// command would render differently depending on who/what calls
		// `guard report`.
		fmt.Fprintln(&script, ".mode markdown")
		fmt.Fprintln(&script, "SELECT * FROM monthly_summary;")
	}

	ctx, cancel := context.WithTimeout(context.Background(), duckDBTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "duckdb", "-no-agent", dbPath)
	cmd.Stdin = strings.NewReader(script.String())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return SyncDuckDBResult{}, fmt.Errorf("duckdb sync failed: %w\n%s", err, stderr.String())
	}

	result.MonthlySummary = stdout.String()
	return result, nil
}

func nonEmptyFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
