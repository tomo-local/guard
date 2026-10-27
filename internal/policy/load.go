package policy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadDir reads every *.yaml/*.yml file anywhere under dir (recursively)
// and builds one Policy per file, using the kind named in the file to pick
// the implementation from Kinds. This lets policies for the same tool be
// grouped into a subdirectory (e.g. .guard/policies/git/*.yaml) instead of
// all living flat in one directory. Files are returned in sorted path
// order so results are deterministic.
//
// A file whose kind is missing or unknown still loads, as a placeholder
// that only reports itself in Lint: it never takes part in evaluation, and
// one stray manifest does not stop the rest of the directory from working.
func LoadDir(dir string, deps Deps) ([]Policy, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read policies dir %q: %w", dir, err)
	}
	sort.Strings(paths)

	policies := make([]Policy, 0, len(paths))
	for _, p := range paths {
		pol, err := LoadFile(p, deps)
		if err != nil {
			return nil, err
		}
		policies = append(policies, pol)
	}
	return policies, nil
}

// LoadFile builds the Policy described by a single manifest file.
func LoadFile(path string, deps Deps) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file %q: %w", path, err)
	}

	var head struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("parse policy file %q: %w", path, err)
	}

	kind, ok := Kinds[head.Kind]
	if !ok {
		return &unknownKindPolicy{source: path, kind: head.Kind}, nil
	}
	pol, err := kind.New(data, path, deps)
	if err != nil {
		return nil, fmt.Errorf("parse policy file %q: %w", path, err)
	}
	return pol, nil
}

// unknownKindPolicy stands in for a manifest whose kind isn't registered.
// It implements Policy but not CommandEvaluator, so nothing evaluates it.
type unknownKindPolicy struct {
	source string
	kind   string
}

func (p *unknownKindPolicy) Meta() Meta {
	return Meta{Kind: p.kind, Source: p.source}
}

func (p *unknownKindPolicy) Lint() []Issue {
	c := NewIssueCollector(p.source)
	if p.kind == "" {
		c.Errorf("kind: must not be empty (known kinds: %s)", strings.Join(KindNames(), ", "))
	} else {
		c.Errorf("kind: unknown kind %q (known kinds: %s)", p.kind, strings.Join(KindNames(), ", "))
	}
	return c.Issues()
}
