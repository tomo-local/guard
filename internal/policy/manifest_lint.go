package policy

import "fmt"

// Lint checks the manifest on its own: field constraints, commandGate
// patterns, and a warning for sections that are read but not enforced yet.
func (m *GuardManifest) Lint() []Issue {
	c := NewIssueCollector(m.SourcePath)

	validateStruct(c, m)

	if cg := m.Spec.CommandGate; cg != nil {
		for i, r := range cg.Rules {
			if r.To.Paths == nil {
				continue
			}
			for j, pat := range r.To.Paths.AnyOf {
				if pat != "" && !validPattern(pat) {
					c.Errorf("spec.commandGate.rules[%d].to.paths.anyOf[%d]: %q is not a valid path pattern", i, j, pat)
				}
			}
		}
	}

	if hasContent(m.Spec.RepositoryGate) {
		c.Warnf("spec.repositoryGate: not enforced yet; its content is ignored")
	}
	if hasContent(m.Spec.SQLGate) {
		c.Warnf("spec.sqlGate: not enforced yet; its content is ignored")
	}
	return c.Issues()
}

// lintManifestSet allows at most one manifest per project.
func lintManifestSet(policies []Policy) []Issue {
	var ms []*GuardManifest
	for _, p := range policies {
		if m, ok := p.(*GuardManifest); ok {
			ms = append(ms, m)
		}
	}
	if len(ms) < 2 {
		return nil
	}
	var issues []Issue
	for _, m := range ms[1:] {
		issues = append(issues, Issue{
			Severity: SeverityError,
			Path:     m.SourcePath,
			Message:  "more than one GuardManifest",
			Details: []Detail{
				{"also", ms[0].SourcePath},
				{"hint", fmt.Sprintf("keep a single %s", KindGuardManifest)},
			},
		})
	}
	return issues
}

// hasContent reports whether a decoded YAML value holds anything.
func hasContent(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	default:
		return true
	}
}
