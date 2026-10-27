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
  -manifest string  project manifest to lint (default ".guard/manifest.yaml", env GUARD_MANIFEST_PATH)
  -color string   colorize output: auto, always, never (default "auto"; auto honors NO_COLOR)

Precedence: -flag > env var > built-in default.

Exit codes: 0=no errors (warnings may still be printed), 1=at least one error.
`

// RunLint validates every policy manifest under the given directory and
// prints every issue found, grouped by file. A file that fails to parse is
// reported like any other issue instead of stopping the run.
func (c *Cmd) RunLint(args []string) error {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	rulesDir := fs.String("rules", defaultPoliciesDir(), "policy directory to lint (env GUARD_POLICIES_DIR)")
	manifest := fs.String("manifest", defaultManifestPath(), "project manifest to lint (env GUARD_MANIFEST_PATH)")
	color := fs.String("color", "auto", "colorize output: auto, always, never (auto honors NO_COLOR)")
	fs.Usage = func() { fmt.Fprint(c.Stderr, lintUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	policies, err := loadPolicies(*rulesDir, *manifest, true)
	if err != nil {
		return fmt.Errorf("load policies: %w", err)
	}

	issues := engine.Lint(policies)
	painter := newPainter(c.Stdout, *color)
	if len(issues) == 0 {
		fmt.Fprintf(c.Stdout, "%s %d policy file(s), no issues\n", painter.green("ok:"), len(policies))
		return nil
	}

	fmt.Fprint(c.Stdout, formatIssues(issues, painter))

	if policy.HasErrors(issues) {
		return &ExitError{Code: 1}
	}
	return nil
}
