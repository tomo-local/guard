package setup

import (
	"bytes"
	"strings"
	"testing"
)

func TestPromptDuckDB_YesWithCustomPath(t *testing.T) {
	stdin := strings.NewReader("y\n.guard/custom.duckdb\n")
	var stdout bytes.Buffer

	got := PromptDuckDB(stdin, &stdout)

	if !got.Enabled {
		t.Fatalf("expected Enabled=true, got %+v", got)
	}
	if got.Path != ".guard/custom.duckdb" {
		t.Fatalf("Path = %q, want %q", got.Path, ".guard/custom.duckdb")
	}
	if !strings.Contains(stdout.String(), "Use DuckDB") {
		t.Errorf("expected the yes/no question to be printed, got:\n%s", stdout.String())
	}
}

func TestPromptDuckDB_YesWithDefaultPath(t *testing.T) {
	stdin := strings.NewReader("yes\n\n") // empty second line -> default path
	var stdout bytes.Buffer

	got := PromptDuckDB(stdin, &stdout)

	if !got.Enabled {
		t.Fatalf("expected Enabled=true, got %+v", got)
	}
	if got.Path == "" {
		t.Fatalf("expected a default path to be filled in, got empty")
	}
}

func TestPromptDuckDB_NoAnswerDisables(t *testing.T) {
	stdin := strings.NewReader("n\n")
	var stdout bytes.Buffer

	got := PromptDuckDB(stdin, &stdout)

	if got.Enabled {
		t.Fatalf("expected Enabled=false, got %+v", got)
	}
	if got.Path != "" {
		t.Fatalf("expected empty Path when disabled, got %q", got.Path)
	}
}

func TestPromptDuckDB_EmptyAnswerDefaultsToNo(t *testing.T) {
	// Simulates a non-interactive/EOF stdin (e.g. redirected from
	// /dev/null): must not hang and must default to the safe "disabled"
	// answer rather than accidentally opting in.
	stdin := strings.NewReader("")
	var stdout bytes.Buffer

	got := PromptDuckDB(stdin, &stdout)

	if got.Enabled {
		t.Fatalf("expected Enabled=false on an empty/EOF stdin, got %+v", got)
	}
}

func TestPromptDuckDB_UppercaseYesIsAccepted(t *testing.T) {
	stdin := strings.NewReader("Y\n")
	var stdout bytes.Buffer

	got := PromptDuckDB(stdin, &stdout)

	if !got.Enabled {
		t.Fatalf("expected Enabled=true for an uppercase Y, got %+v", got)
	}
}
