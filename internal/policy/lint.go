package policy

import (
	"fmt"
	"sort"
	"strings"
)

// Lint validates a set of loaded policies and returns every issue found.
// Policies are checked both individually (field presence/enum constraints
// via validateStruct, plus structural checks below) and against each
// other (e.g. for duplicate match conditions across files).
func Lint(policies []Policy) []Issue {
	var issues []Issue

	for _, p := range policies {
		issues = append(issues, lintOne(p)...)
	}

	issues = append(issues, lintCrossPolicy(policies)...)

	return issues
}

func lintOne(p Policy) []Issue {
	c := NewIssueCollector(p.SourcePath)

	validateStruct(c, p)

	// Not expressible as a single field constraint: a second catch-all
	// (no functions) rule in the same resource is reachable-but-pointless,
	// since the first one already matches everything. Warning, not an
	// error -- strictest-wins still resolves it deterministically.
	catchAllSeen := false
	for i, r := range p.Spec.Rules {
		if len(r.To.Functions) == 0 {
			if catchAllSeen {
				c.Warnf("spec.rules[%d] is a second catch-all (no functions) rule for this tool; the earlier one makes it unreachable", i)
			}
			catchAllSeen = true
		}
	}

	return c.Issues()
}

// lintCrossPolicy flags resources that declare the exact same match
// condition for the same tool. This is not an error -- strictest-wins
// resolves it deterministically -- but it is almost always unintentional,
// so it spans multiple source paths and can't go through a single-file
// IssueCollector.
//
// The key deliberately excludes To.Label: Label plays no part in matching
// (see To.Label's doc comment), so two rules differing only in Label are
// still a genuine duplicate match condition.
func lintCrossPolicy(policies []Policy) []Issue {
	type key struct {
		tool      string
		functions string
		args      string
	}
	seen := map[key][]string{} // key -> source paths that declare it

	for _, p := range policies {
		for _, r := range p.Spec.Rules {
			fns := append([]string{}, r.To.Functions...)
			sort.Strings(fns)
			var argsAnyOf []string
			if r.To.Args != nil {
				argsAnyOf = append([]string{}, r.To.Args.AnyOf...)
				sort.Strings(argsAnyOf)
			}
			k := key{
				tool:      p.Spec.Target.Tool,
				functions: strings.Join(fns, ","),
				args:      strings.Join(argsAnyOf, ","),
			}
			seen[k] = append(seen[k], p.SourcePath)
		}
	}

	var issues []Issue
	for k, paths := range seen {
		unique := uniqueStrings(paths)
		if len(unique) < 2 {
			continue
		}
		sort.Strings(unique)
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Path:     strings.Join(unique, ", "),
			Message:  fmt.Sprintf("duplicate match condition tool=%q functions=[%s] args=[%s] declared in multiple policies; strictest action wins automatically", k.tool, k.functions, k.args),
		})
	}
	return issues
}

func uniqueStrings(in []string) []string {
	m := map[string]struct{}{}
	for _, s := range in {
		m[s] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	return out
}
