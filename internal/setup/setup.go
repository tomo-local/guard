// Package setup implements `guard init`'s bootstrap logic: creating the
// logs directory and seeding the policies directory with starter policies,
// without ever overwriting anything already there.
package setup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"ai-guard-tracker/internal/policy"
)

// starterRoot is the path under which main.go's go:embed directive
// ("//go:embed .guard/policies") places the starter manifests inside the
// fs.FS passed to Run.
const starterRoot = ".guard/policies"

// HookSettingsSnippet is printed after Run so the user can wire guard
// check into Claude Code themselves. Run never edits .claude/settings.json
// directly, to avoid clobbering unrelated existing hook configuration.
const HookSettingsSnippet = `
Add this to .claude/settings.json to run "guard check" before every Bash command:

{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [{ "type": "command", "command": "guard check" }] }
    ]
  }
}
`

// Result summarizes what Run did, for the caller to report to the user.
type Result struct {
	CheckLogDir  string
	RecordLogDir string
	PoliciesDir  string
	// ExistingPolicyCount is how many policies were already present in
	// PoliciesDir before Run looked at it. When > 0, SeededPolicyCount is
	// always 0 -- an existing policies directory is left untouched.
	ExistingPolicyCount int
	SeededPolicyCount   int
}

// Run creates the check/record log directories and seeds policiesDir
// with the starter policies found under starterRoot in starterFS, but
// only when policiesDir has no policy manifests yet.
func Run(policiesDir, checkLogDir, recordLogDir string, starterFS fs.FS) (Result, error) {
	res := Result{PoliciesDir: policiesDir, CheckLogDir: checkLogDir, RecordLogDir: recordLogDir}

	if err := mkdirAllLogDir(res.CheckLogDir); err != nil {
		return res, fmt.Errorf("create check log dir: %w", err)
	}
	if err := mkdirAllLogDir(res.RecordLogDir); err != nil {
		return res, fmt.Errorf("create record log dir: %w", err)
	}

	existing, err := policy.LoadDir(policiesDir)
	if err != nil {
		return res, fmt.Errorf("check existing policies in %q: %w", policiesDir, err)
	}
	res.ExistingPolicyCount = len(existing)
	if res.ExistingPolicyCount > 0 {
		return res, nil
	}

	written, err := writeStarterPolicies(starterFS, policiesDir)
	if err != nil {
		return res, fmt.Errorf("seed starter policies into %q: %w", policiesDir, err)
	}
	res.SeededPolicyCount = written
	return res, nil
}

// mkdirAllLogDir is os.MkdirAll with a clearer error when dir already
// exists as a regular file -- the shape a GUARD_LOG_PATH/
// GUARD_RECORD_LOG_PATH left over from before logs were split by day
// (pointing at a single check.jsonl/record.jsonl file, not a directory)
// would produce.
func mkdirAllLogDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if info, statErr := os.Stat(dir); statErr == nil && !info.IsDir() {
			return fmt.Errorf("%q already exists as a file, not a directory -- if GUARD_LOG_PATH/GUARD_RECORD_LOG_PATH (or -log/-record-log) is still set to a pre-split check.jsonl/record.jsonl path, point it at a directory instead (e.g. \".guard/logs/check\"): %w", dir, err)
		}
		return err
	}
	return nil
}

// writeStarterPolicies copies every embedded starter manifest into dir,
// preserving the per-tool subdirectory layout (git/allow.yaml, ...).
// Skips (doesn't overwrite) any file that already exists -- a second
// line of defense even though Run only calls this when dir is empty.
func writeStarterPolicies(starterFS fs.FS, dir string) (int, error) {
	written := 0
	err := fs.WalkDir(starterFS, starterRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel := strings.TrimPrefix(path, starterRoot+"/")
		dest := filepath.Join(dir, rel)

		if _, err := os.Stat(dest); err == nil {
			return nil // already present, don't overwrite
		}

		content, err := fs.ReadFile(starterFS, path)
		if err != nil {
			return fmt.Errorf("read starter policy %q: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create dir for %q: %w", dest, err)
		}
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			return fmt.Errorf("write %q: %w", dest, err)
		}
		written++
		return nil
	})
	return written, err
}
