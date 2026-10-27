package policy

import (
	"fmt"
	"path"
	"strings"
)

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

	if rg := m.Spec.RepositoryGate; rg != nil {
		seen := map[string]bool{}
		for i, r := range rg.Repose {
			if r.Name != "" && !validRepoName(r.Name) {
				c.Errorf("spec.repositoryGate.repose[%d].name: %q must look like owner/name", i, r.Name)
			}
			if seen[r.Name] && r.Name != "" {
				c.Errorf("spec.repositoryGate.repose[%d].name: %q is listed more than once", i, r.Name)
			}
			seen[r.Name] = true
			for j, pat := range r.Protected {
				if pat != "" && !validBranchPattern(pat) {
					c.Errorf("spec.repositoryGate.repose[%d].protected[%d]: %q is not a valid branch pattern", i, j, pat)
				}
			}
		}
	}
	if sg := m.Spec.SQLGate; sg != nil {
		for i, cl := range sg.Target.Clients {
			if cl != "" && !contains(DBClients, cl) {
				c.Errorf("spec.sqlGate.target.clients[%d]: unknown client %q (supported: %s)", i, cl, strings.Join(DBClients, ", "))
			}
		}
		catchAll := false
		for i, r := range sg.Rules {
			if len(r.To.Functions) == 0 {
				if catchAll {
					c.Warnf("spec.sqlGate.rules[%d] is a second catch-all (no functions) rule; the earlier one makes it unreachable", i)
				}
				catchAll = true
			}
		}
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

// validRepoName accepts "owner/name" (and deeper paths such as GitLab
// subgroups): at least two non-empty segments, no spaces.
func validRepoName(name string) bool {
	if strings.ContainsAny(name, " \t") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

func validBranchPattern(pat string) bool {
	_, err := path.Match(pat, "")
	return err == nil
}
