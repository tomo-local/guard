package cmd

import (
	"flag"
	"fmt"

	"ai-guard-tracker/internal/engine"
	"ai-guard-tracker/internal/policy"
)

const lintUsage = `Usage:
  guard lint [-rules <dir>]

Options:
  -rules string   policy directory to lint (default ".guard/policies", env GUARD_POLICIES_DIR)

Precedence: -flag > env var > built-in default.

Exit codes: 0=no errors (warnings may still be printed), 1=at least one error.
`

// RunLint validates every policy manifest under the given directory and
// prints each issue found.
func (c *Cmd) RunLint(args []string) error {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	rulesDir := fs.String("rules", defaultPoliciesDir(), "policy directory to lint (env GUARD_POLICIES_DIR)")
	fs.Usage = func() { fmt.Fprint(c.Stderr, lintUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	policies, err := policy.LoadDir(*rulesDir, policy.Deps{})
	if err != nil {
		return fmt.Errorf("load policies: %w", err)
	}

	issues := engine.Lint(policies)
	for _, issue := range issues {
		fmt.Fprintln(c.Stdout, issue.String())
	}

	if len(issues) == 0 {
		fmt.Fprintf(c.Stdout, "ok: %d policy file(s), no issues\n", len(policies))
		return nil
	}

	if policy.HasErrors(issues) {
		return &ExitError{Code: 1}
	}
	return nil
}
