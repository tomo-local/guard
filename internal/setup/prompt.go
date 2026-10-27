package setup

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"ai-guard-tracker/internal/config"
)

// PromptDuckDB interactively asks (via stdin/stdout) whether this
// project should use `guard report --format duckdb`, and if so, where
// its database file should live. It's a thin, deliberately
// stdin/stdout-only function so it can be driven by a real terminal or
// (in tests) by a strings.Reader/bytes.Buffer -- it never touches a
// file itself, see cmd/init.go for how the answer gets persisted.
//
// An empty/EOF answer to the yes/no question defaults to "no" (DuckDB
// stays disabled) -- safe for a non-interactive stdin (e.g. redirected
// from /dev/null) to not hang or accidentally opt in.
func PromptDuckDB(stdin io.Reader, stdout io.Writer) config.DuckDBConfig {
	reader := bufio.NewReader(stdin)

	fmt.Fprint(stdout, "Use DuckDB for `guard report --format duckdb` (requires the duckdb CLI)? [y/N]: ")
	answer := readLine(reader)
	if !isYes(answer) {
		return config.DuckDBConfig{Enabled: false}
	}

	fmt.Fprintf(stdout, "DuckDB database file path [%s]: ", config.DefaultDuckDBPath)
	path := strings.TrimSpace(readLine(reader))
	if path == "" {
		path = config.DefaultDuckDBPath
	}
	return config.DuckDBConfig{Enabled: true, Path: path}
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n') // ignore io.EOF: an empty/partial line is a valid (default) answer
	return strings.TrimSpace(line)
}

func isYes(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
