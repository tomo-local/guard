package cmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"ai-guard-tracker/internal/policy"
)

// loadPolicies loads every policy file under rulesDir plus the project
// manifest, when one exists. lenient (for `guard lint`) turns a file that
// can't be parsed into a reported issue instead of an error.
func loadPolicies(rulesDir, manifestPath string, lenient bool) ([]policy.Policy, error) {
	deps := policy.Deps{Runner: procRunner{}, Root: projectRoot(manifestPath)}
	deps.Cwd, _ = os.Getwd()

	load := policy.LoadDir
	if lenient {
		load = policy.LoadDirForLint
	}
	policies, err := load(rulesDir, deps)
	if err != nil {
		return nil, err
	}

	if _, statErr := os.Stat(manifestPath); statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return policies, nil
		}
		return nil, statErr
	}
	if lenient {
		return append(policies, policy.LoadFileForLint(manifestPath, deps)), nil
	}
	m, err := policy.LoadFile(manifestPath, deps)
	if err != nil {
		return nil, err
	}
	return append(policies, m), nil
}

// projectRoot is the directory that holds .guard/: the manifest's parent's
// parent, as an absolute path ("" when it can't be resolved).
func projectRoot(manifestPath string) string {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(abs))
}
