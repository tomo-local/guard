package cmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"ai-guard-tracker/internal/policy"
)

// loadPolicies loads every policy file under rulesDir, the project manifest
// and the database files under dbDir (connections.yml, policy.yml), when they
// exist. lenient (for `guard lint`) turns a file that can't be parsed into a
// reported issue instead of an error.
func loadPolicies(rulesDir, manifestPath, dbDir string, lenient bool) ([]policy.Policy, error) {
	deps := policy.Deps{Runner: procRunner{}, Root: projectRoot(manifestPath)}
	deps.Cwd, _ = os.Getwd()

	// The manifest and the database connections are read first: they label
	// branches and environments, which the policies loaded next consult
	// through deps.
	manifest, err := loadOptional(manifestPath, deps, lenient)
	if err != nil {
		return nil, err
	}
	if gm, ok := manifest.(*policy.GuardManifest); ok {
		deps.Repo = &gitRepo{cwd: deps.Cwd, manifest: gm}
	}

	connections, err := loadOptional(dbFile(dbDir, "connections"), deps, lenient)
	if err != nil {
		return nil, err
	}
	if gc, ok := connections.(*policy.GuardDBConnections); ok {
		deps.DB = gc
	}

	dbPolicy, err := loadOptional(dbFile(dbDir, "policy"), deps, lenient)
	if err != nil {
		return nil, err
	}

	load := policy.LoadDir
	if lenient {
		load = policy.LoadDirForLint
	}
	policies, err := load(rulesDir, deps)
	if err != nil {
		return nil, err
	}
	for _, p := range []policy.Policy{manifest, connections, dbPolicy} {
		if p != nil {
			policies = append(policies, p)
		}
	}
	return policies, nil
}

// dbFile is dbDir/name.yml, or name.yaml when only that exists.
func dbFile(dbDir, name string) string {
	yml := filepath.Join(dbDir, name+".yml")
	if _, err := os.Stat(yml); err != nil {
		if yaml := filepath.Join(dbDir, name+".yaml"); fileExists(yaml) {
			return yaml
		}
	}
	return yml
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// loadOptional loads one policy file; nil (and no error) when it doesn't exist.
func loadOptional(path string, deps policy.Deps, lenient bool) (policy.Policy, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if lenient {
		return policy.LoadFileForLint(path, deps), nil
	}
	return policy.LoadFile(path, deps)
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
