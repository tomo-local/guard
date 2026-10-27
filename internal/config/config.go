// Package config reads and writes ai-guard-tracker's own project-level
// settings file (.guard/config.yaml), as opposed to the Bash policy
// manifests in internal/policy. `guard init` is the only writer; every
// other command only ever reads it to pick a default.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultPath is where `guard init` writes, and every other command
// reads, this CLI's own settings (separate from GUARD_POLICIES_DIR/
// GUARD_LOG_PATH, which point at the Bash policies and audit logs).
const DefaultPath = ".guard/config.yaml"

// DefaultDuckDBPath is the database file guard init proposes (as the
// interactive prompt's default, and when -duckdb is set without
// -duckdb-path) when the user opts into DuckDB.
const DefaultDuckDBPath = ".guard/guard.duckdb"

// Config is this CLI's own settings file, written by `guard init` and
// safe to hand-edit afterwards.
type Config struct {
	DuckDB DuckDBConfig `yaml:"duckdb"`
}

// DuckDBConfig records whether `guard report --format duckdb` should be
// considered "set up" for this project, and where its database file
// lives by default.
type DuckDBConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path,omitempty"`
}

const fileHeader = "# ai-guard-tracker project settings, written by `guard init`.\n# Safe to edit by hand.\n\n"

// Load reads path. A missing file is not an error -- it just means
// nothing has been configured yet, so it returns the zero Config
// (DuckDB disabled).
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

// Save writes cfg to path as YAML, creating path's parent directory as
// needed. It always overwrites -- callers decide whether an existing
// file should be left alone (see cmd/init.go, which only calls Save
// when the file is missing or the user explicitly asked to change it).
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, append([]byte(fileHeader), data...), 0o644); err != nil {
		return fmt.Errorf("write config %q: %w", path, err)
	}
	return nil
}

// Exists reports whether a config file is already present at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
