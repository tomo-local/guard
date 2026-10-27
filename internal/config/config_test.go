package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_MissingFileReturnsZeroValue(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DuckDB.Enabled {
		t.Fatalf("expected DuckDB.Enabled=false for a missing config, got %+v", cfg)
	}
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	want := Config{DuckDB: DuckDBConfig{Enabled: true, Path: ".guard/guard.duckdb"}}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !Exists(path) {
		t.Fatalf("expected Exists=true after Save")
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestSave_WritesHumanReadableHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, Config{DuckDB: DuckDBConfig{Enabled: false}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), "guard init") {
		t.Errorf("expected a header mentioning `guard init`, got:\n%s", data)
	}
	if !strings.Contains(string(data), "enabled: false") {
		t.Errorf("expected enabled: false in the written YAML, got:\n%s", data)
	}
}

func TestExists_FalseForMissingPath(t *testing.T) {
	if Exists(filepath.Join(t.TempDir(), "nope.yaml")) {
		t.Fatalf("expected Exists=false for a path that was never saved to")
	}
}
