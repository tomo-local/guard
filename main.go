package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"os"

	"ai-guard-tracker/cmd"
)

// starterPolicies ships this repo's own proven sample manifests inside the
// binary, so `guard init` can seed a fresh project even when guard is
// installed standalone elsewhere (not run from inside this repo).
//
//go:embed .guard/policies
var starterPolicies embed.FS

func main() {
	args := os.Args[1:]

	var command string
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}

	c := cmd.New(os.Stdin, os.Stdout, os.Stderr, starterPolicies)

	var err error
	switch command {
	case "check":
		err = c.RunCheck(args)
	case "lint":
		err = c.RunLint(args)
	case "init":
		err = c.RunInit(args)
	case "record":
		err = c.RunRecord(args)
	case "report":
		err = c.RunReport(args)
	case "", "-h", "--help", "help":
		showHelp()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\nRun 'guard --help' for usage.\n", command)
		os.Exit(1)
	}

	if err != nil {
		var exitErr *cmd.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		if errors.Is(err, flag.ErrHelp) {
			// Usage was already printed by the subcommand's FlagSet.
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func showHelp() {
	fmt.Fprint(os.Stderr, `guard: AI agent command guardrails

Usage: guard <command> [options]

Commands:
  init     Set up .guard/policies and .guard/logs for a new project
  check    Evaluate a Bash command against .guard/policies (CLI arg or PreToolUse hook on stdin)
  record   Log a Bash command's result (PostToolUse hook on stdin only)
  lint     Validate .guard/policies manifests
  report   Summarize .guard/logs/*.jsonl as md/json/html (default html) or a DuckDB database
  help     Show this help message
  -h, --help   Show this help message

Run 'guard <command> -h' for details and options of each command.

Examples:
  guard init
  guard check "git push --force"
  echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | guard check
  guard lint
  guard report
  guard report --format md
  guard report --format json --out .guard/report.json
  guard report --format duckdb --out .guard/guard.duckdb
`)
}
