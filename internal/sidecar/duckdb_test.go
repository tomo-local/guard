package sidecar

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeJSONL(t *testing.T, path string, lines []any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
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

// requireDuckDB skips the test when the external duckdb CLI isn't on
// PATH -- DuckDBCLIStore deliberately shells out to it rather than
// embedding a CGO driver (see duckdb.go), so these tests are
// integration tests against a real binary, not something every
// dev/CI machine is guaranteed to have installed.
func requireDuckDB(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("duckdb"); err != nil {
		t.Skip("duckdb CLI not found on PATH, skipping (install it to run this test, e.g. `brew install duckdb`)")
	}
}

func TestDuckDBCLIStore_NoDuckDBOnPath(t *testing.T) {
	if _, err := exec.LookPath("duckdb"); err == nil {
		t.Skip("duckdb CLI is on PATH, can't exercise the not-found path")
	}
	_, err := DuckDBCLIStore{}.Sync("check.jsonl", "record.jsonl", filepath.Join(t.TempDir(), "x.duckdb"))
	if err == nil {
		t.Fatal("expected an error when the duckdb CLI isn't on PATH")
	}
}

func TestDuckDBCLIStore_MissingLogsStillCreatesDatabase(t *testing.T) {
	requireDuckDB(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "guard.duckdb")

	result, err := DuckDBCLIStore{}.Sync(filepath.Join(dir, "check.jsonl"), filepath.Join(dir, "record.jsonl"), dbPath)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.ChecksLoaded || result.RecordsLoaded {
		t.Fatalf("expected ChecksLoaded/RecordsLoaded=false for missing logs, got %+v", result)
	}
	if result.MonthlySummary != "" {
		t.Fatalf("expected empty MonthlySummary when checks weren't loaded, got %q", result.MonthlySummary)
	}
	if !nonEmptyFile(dbPath) {
		t.Fatalf("expected a database file to be created at %s even with no logs", dbPath)
	}
}

func TestDuckDBCLIStore_LoadsChecksAndSummarizesByMonth(t *testing.T) {
	requireDuckDB(t)
	dir := t.TempDir()
	checkPath := filepath.Join(dir, "check.jsonl")
	dbPath := filepath.Join(dir, "guard.duckdb")

	writeJSONL(t, checkPath, []any{
		CheckLogEntry{Timestamp: mustParseTime(t, "2026-08-05T10:00:00Z"), Action: "DENY"},
		CheckLogEntry{Timestamp: mustParseTime(t, "2026-09-15T10:00:00Z"), Action: "ALLOW"},
		CheckLogEntry{Timestamp: mustParseTime(t, "2026-09-20T10:00:00Z"), Action: "ALLOW"},
	})

	result, err := DuckDBCLIStore{}.Sync(checkPath, filepath.Join(dir, "record.jsonl"), dbPath)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !result.ChecksLoaded {
		t.Fatal("expected ChecksLoaded=true")
	}
	if result.RecordsLoaded {
		t.Fatal("expected RecordsLoaded=false (no record.jsonl)")
	}

	for _, want := range []string{"2026-08", "2026-09", "DENY", "ALLOW"} {
		if !strings.Contains(result.MonthlySummary, want) {
			t.Errorf("MonthlySummary missing %q:\n%s", want, result.MonthlySummary)
		}
	}
	// August has 1 DENY, September has 2 ALLOW -- spot-check the counts
	// landed in the right row, not just that the keywords appear somewhere.
	if !strings.Contains(result.MonthlySummary, "2026-09") || !countAppearsNear(result.MonthlySummary, "2026-09", "2") {
		t.Errorf("expected September's ALLOW count to be 2:\n%s", result.MonthlySummary)
	}

	if !nonEmptyFile(dbPath) {
		t.Fatalf("expected a database file at %s", dbPath)
	}
}

func TestDuckDBCLIStore_PathWithSingleQuoteIsEscaped(t *testing.T) {
	requireDuckDB(t)
	dir := t.TempDir()
	// A log path containing a single quote must not break out of the
	// 'string literal' read_json_auto(...) is built from.
	tricky := filepath.Join(dir, "check's.jsonl")
	writeJSONL(t, tricky, []any{CheckLogEntry{Action: "ALLOW"}})

	result, err := DuckDBCLIStore{}.Sync(tricky, filepath.Join(dir, "record.jsonl"), filepath.Join(dir, "guard.duckdb"))
	if err != nil {
		t.Fatalf("Sync with a quote in the path should still work (properly escaped), got error: %v", err)
	}
	if !result.ChecksLoaded {
		t.Fatalf("expected the quote-containing path to still load successfully")
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return parsed
}

// countAppearsNear is a loose sanity check that substring b appears
// somewhere after substring a in s (i.e. on the same or a later line),
// without parsing duckdb's markdown table structure in detail.
func countAppearsNear(s, a, b string) bool {
	i := strings.Index(s, a)
	if i < 0 {
		return false
	}
	return strings.Contains(s[i:], b)
}
