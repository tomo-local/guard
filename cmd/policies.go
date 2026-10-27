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

	// The manifest is read first: it labels branches, which the policies
	// loaded next consult through deps.Repo.
	manifest, err := loadManifest(manifestPath, deps, lenient)
	if err != nil {
		return nil, err
	}
	if gm, ok := manifest.(*policy.GuardManifest); ok {
		deps.Repo = &gitRepo{cwd: deps.Cwd, manifest: gm}
	}

	load := policy.LoadDir
	if lenient {
		load = policy.LoadDirForLint
	}
	policies, err := load(rulesDir, deps)
	if err != nil {
		return nil, err
	}
	if manifest != nil {
		policies = append(policies, manifest)
	}
	return policies, nil
}

// loadManifest loads the project manifest; nil (and no error) when the file
// doesn't exist.
func loadManifest(manifestPath string, deps policy.Deps, lenient bool) (policy.Policy, error) {
	if _, err := os.Stat(manifestPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if lenient {
		return policy.LoadFileForLint(manifestPath, deps), nil
	}
	return policy.LoadFile(manifestPath, deps)
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
