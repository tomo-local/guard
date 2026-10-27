package cmd

import (
	"flag"
	"fmt"
	"os/exec"

	"ai-guard-tracker/internal/config"
	"ai-guard-tracker/internal/setup"
)

const initUsage = `Usage:
  guard init [-rules <dir>] [-log <path>] [-config <path>] [-duckdb] [-duckdb-path <path>]

Options:
  -rules string        policy directory to set up (default ".guard/policies", env GUARD_POLICIES_DIR)
  -log string           check decision log path, used only to derive the logs
                          directory to create (default ".guard/logs/check.jsonl", env GUARD_LOG_PATH)
  -config string         project settings file path (default ".guard/config.yaml", env GUARD_CONFIG_PATH)
  -duckdb               enable/disable "guard report --format duckdb" (true/false)
  -duckdb-path string    DuckDB database file path when -duckdb is enabled (default ".guard/guard.duckdb")

Precedence for -rules/-log/-config: -flag > env var > built-in default.

Creates the logs directory, and seeds the policies directory with this
CLI's own starter policies -- but only if it has no policy manifests
yet; an existing policies directory is left untouched. Never modifies
.claude/settings.json -- prints the hook snippet to add there instead.

DuckDB setup: if -duckdb and/or -duckdb-path is passed, that answer is
saved to -config without asking anything (useful for scripted/non-
interactive setup). Otherwise, if -config doesn't already exist yet,
guard init asks interactively on stdin/stdout whether to use DuckDB. If
-config already exists and neither flag was passed, it's left untouched.
`

// RunInit sets up .guard/policies (seeded with starter policies if empty),
// the logs directory, and this project's .guard/config.yaml (asking
// interactively whether to use DuckDB, unless -duckdb/-duckdb-path were
// passed or a config file already exists), then prints the Claude Code
// hook snippet to add manually. The actual bootstrap logic lives in
// internal/setup; this is just flag parsing and reporting the result.
func (c *Cmd) RunInit(args []string) error {
	flagSet := flag.NewFlagSet("init", flag.ContinueOnError)
	flagSet.SetOutput(c.Stderr)
	rulesDir := flagSet.String("rules", defaultPoliciesDir(), "policy directory to set up (env GUARD_POLICIES_DIR)")
	logPath := flagSet.String("log", defaultLogPath(), "check decision log path, used to derive the logs directory (env GUARD_LOG_PATH)")
	configPath := flagSet.String("config", defaultConfigPath(), "project settings file path (env GUARD_CONFIG_PATH)")
	duckdbFlag := flagSet.Bool("duckdb", false, `enable/disable "guard report --format duckdb"`)
	duckdbPathFlag := flagSet.String("duckdb-path", "", "DuckDB database file path when -duckdb is enabled")
	flagSet.Usage = func() { fmt.Fprint(c.Stderr, initUsage) }
	if err := flagSet.Parse(args); err != nil {
		return err
	}

	var duckdbFlagSet, duckdbPathFlagSet bool
	flagSet.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "duckdb":
			duckdbFlagSet = true
		case "duckdb-path":
			duckdbPathFlagSet = true
		}
	})

	res, err := setup.Run(*rulesDir, *logPath, c.StarterPolicies)
	if err != nil {
		return err
	}

	fmt.Fprintf(c.Stdout, "logs directory ready: %s\n", res.LogsDir)
	if res.ExistingPolicyCount > 0 {
		fmt.Fprintf(c.Stdout, "policies directory already has %d polic%s, left untouched: %s\n", res.ExistingPolicyCount, plural(res.ExistingPolicyCount), res.PoliciesDir)
	} else {
		fmt.Fprintf(c.Stdout, "policies directory seeded with %d starter polic%s: %s\n", res.SeededPolicyCount, plural(res.SeededPolicyCount), res.PoliciesDir)
	}

	// Printed regardless of setupDuckDBConfig's outcome below: core setup
	// (policies/logs) already succeeded by this point, and the hook
	// snippet is the one step guard init can't do for the user, so a
	// DuckDB config-save failure must never cause it to go unprinted.
	duckdbErr := c.setupDuckDBConfig(*configPath, duckdbFlagSet, *duckdbFlag, duckdbPathFlagSet, *duckdbPathFlag)

	fmt.Fprint(c.Stdout, setup.HookSettingsSnippet)

	return duckdbErr
}

// setupDuckDBConfig decides the project's DuckDB setting and, unless it's
// already decided and the caller didn't ask to change it, persists it to
// configPath -- non-interactively when -duckdb/-duckdb-path were passed
// on the command line, interactively (via c.Stdin/c.Stdout) otherwise.
func (c *Cmd) setupDuckDBConfig(configPath string, duckdbFlagSet, duckdbFlag, duckdbPathFlagSet bool, duckdbPathFlag string) error {
	explicit := duckdbFlagSet || duckdbPathFlagSet

	if !explicit && config.Exists(configPath) {
		fmt.Fprintf(c.Stdout, "config already exists, left untouched: %s\n", configPath)
		return nil
	}

	var duckdb config.DuckDBConfig
	if explicit {
		duckdb.Enabled = !duckdbFlagSet || duckdbFlag // a bare -duckdb-path implies enabling
		if duckdb.Enabled {
			duckdb.Path = duckdbPathFlag
			if duckdb.Path == "" {
				duckdb.Path = config.DefaultDuckDBPath
			}
		}
	} else {
		duckdb = setup.PromptDuckDB(c.Stdin, c.Stdout)
	}

	if err := config.Save(configPath, config.Config{DuckDB: duckdb}); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if duckdb.Enabled {
		fmt.Fprintf(c.Stdout, "config written: %s (duckdb enabled, path=%s)\n", configPath, duckdb.Path)
		// Checked here, not just left to guard report's own LookPath
		// check, so a missing binary surfaces immediately at setup time
		// instead of silently waiting until the first "guard report
		// --format duckdb" run. Not fatal -- the CLI may well be
		// installed later, so the config is still saved as asked.
		if _, err := exec.LookPath("duckdb"); err != nil {
			fmt.Fprintln(c.Stderr, "warning: duckdb CLI not found on PATH -- install it before running `guard report --format duckdb` (e.g. `brew install duckdb`)")
		}
	} else {
		fmt.Fprintf(c.Stdout, "config written: %s (duckdb disabled)\n", configPath)
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
