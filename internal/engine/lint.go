package engine

import (
	"ai-guard-tracker/internal/policy"
)

// Lint checks every loaded policy on its own, then each kind's policies
// together for problems that only show up across files (e.g. duplicate
// match conditions).
func Lint(policies []policy.Policy) []policy.Issue {
	var issues []policy.Issue
	for _, p := range policies {
		issues = append(issues, p.Lint()...)
	}

	byKind := map[string][]policy.Policy{}
	for _, p := range policies {
		k := p.Meta().Kind
		byKind[k] = append(byKind[k], p)
	}
	for _, name := range policy.KindNames() {
		kind := policy.Kinds[name]
		if kind.LintSet == nil || len(byKind[name]) == 0 {
			continue
		}
		issues = append(issues, kind.LintSet(byKind[name])...)
	}
	return issues
}
