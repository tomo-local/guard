package cmd

// env.go holds the environment variables that override this CLI's
// defaults (GUARD_POLICIES_DIR, GUARD_LOG_PATH, GUARD_RECORD_LOG_PATH,
// GUARD_CONFIG_PATH), following the same <APPNAME>_<SETTING> convention
// as made/habit-tracker's HABIT_CONFIG_DIR. Precedence is CLI flag > env
// var > hardcoded default: each flag's *default* is computed from the
// env var, so an explicit flag still wins.

import (
	"os"

	"ai-guard-tracker/internal/config"
)

const (
	envPoliciesDir   = "GUARD_POLICIES_DIR"
	envLogPath       = "GUARD_LOG_PATH"
	envRecordLogPath = "GUARD_RECORD_LOG_PATH"
	envConfigPath    = "GUARD_CONFIG_PATH"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func defaultPoliciesDir() string {
	return envOrDefault(envPoliciesDir, ".guard/policies")
}

func defaultLogDir() string {
	return envOrDefault(envLogPath, ".guard/logs/check")
}

func defaultRecordLogDir() string {
	return envOrDefault(envRecordLogPath, ".guard/logs/record")
}

func defaultConfigPath() string {
	return envOrDefault(envConfigPath, config.DefaultPath)
}
