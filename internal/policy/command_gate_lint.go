package policy

import (
	"strings"
)

// Lint checks this one policy: field presence/enum constraints via
// validateStruct, plus structural checks below.
func (p *CommandGatePolicy) Lint() []Issue {
	c := NewIssueCollector(p.SourcePath)

	validateStruct(c, p)

	for i, r := range p.Spec.Rules {
		m := r.To.Args
		if m == nil {
			continue
		}
		if len(m.AnyOf) == 0 && len(m.NoneOf) == 0 {
			c.Errorf("spec.rules[%d].to.args.anyOf: set anyOf or noneOf with at least 1 item, or remove args", i)
		}
		for _, tok := range m.AnyOf {
			if contains(m.NoneOf, tok) {
				c.Errorf("spec.rules[%d].to.args: %q is in both anyOf and noneOf, so the rule can never match", i, tok)
			}
		}
	}

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

// lintCommandGateSet enforces "one command, one policy": two policies for
// the same tool must not both match any command. Every overlapping pair is an
// error, reported once per pair. (The engine still resolves an overlap with
// strictest-wins so a policy set that ignores lint stays well defined.)
//
// Rules are compared by their match conditions only; To.Label plays no part
// in matching. Rules inside one policy are not compared: their precedence is
// fixed (see matchRule).
func lintCommandGateSet(policies []Policy) []Issue {
	var gates []*CommandGatePolicy
	for _, pol := range policies {
		if p, ok := pol.(*CommandGatePolicy); ok {
			gates = append(gates, p)
		}
	}

	var issues []Issue
	for i := 0; i < len(gates); i++ {
		for j := i + 1; j < len(gates); j++ {
			a, b := gates[i], gates[j]
			if a.Spec.Target.Tool != b.Spec.Target.Tool {
				continue
			}
			ra, rb, ok := firstOverlap(a.Spec.Rules, b.Spec.Rules)
			if !ok {
				continue
			}
			// Report under the file that sorts first, naming the other, so
			// the pair shows up once and each side's condition is clear.
			if b.SourcePath < a.SourcePath {
				a, b = b, a
				ra, rb = rb, ra
			}
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     a.SourcePath,
				Message:  "overlaps with " + b.SourcePath,
				Details: []Detail{
					{"example", witnessCommand(a.Spec.Target.Tool, ra.To, rb.To) + " matches both"},
					{"this file", describeCondition(ra.To)},
					{"other file", describeCondition(rb.To)},
					{"hint", "one command must be decided by exactly one policy; split the conditions, e.g. with args.noneOf"},
				},
			})
		}
	}
	return issues
}

// witnessCommand builds an example command that satisfies both conditions
// (which must overlap), e.g. "git push -f".
func witnessCommand(tool string, a, b To) string {
	parts := []string{tool}
	switch {
	case len(a.Functions) > 0 && len(b.Functions) > 0:
		for _, f := range a.Functions {
			if contains(b.Functions, f) {
				parts = append(parts, f)
				break
			}
		}
	case len(a.Functions) > 0:
		parts = append(parts, a.Functions[0])
	case len(b.Functions) > 0:
		parts = append(parts, b.Functions[0])
	}

	var none []string
	for _, t := range []To{a, b} {
		if t.Args != nil {
			none = append(none, t.Args.NoneOf...)
		}
	}
	for _, t := range []To{a, b} {
		if t.Args == nil {
			continue
		}
		for _, tok := range t.Args.AnyOf {
			if !contains(none, tok) {
				if !contains(parts, tok) {
					parts = append(parts, tok)
				}
				break
			}
		}
	}
	return "`" + strings.Join(parts, " ") + "`"
}

// firstOverlap returns the first pair of rules (one from each side) that
// can match the same command.
func firstOverlap(as, bs []Rule) (Rule, Rule, bool) {
	for _, a := range as {
		for _, b := range bs {
			if conditionsOverlap(a.To, b.To) {
				return a, b, true
			}
		}
	}
	return Rule{}, Rule{}, false
}

// conditionsOverlap reports whether some command satisfies both a and b.
// A rule is (F=functions, A=anyOf, N=noneOf); empty F is a catch-all.
func conditionsOverlap(a, b To) bool {
	if len(a.Functions) > 0 && len(b.Functions) > 0 && !intersects(a.Functions, b.Functions) {
		return false
	}

	var aAny, bAny, none []string
	if a.Args != nil {
		aAny = a.Args.AnyOf
		none = append(none, a.Args.NoneOf...)
	}
	if b.Args != nil {
		bAny = b.Args.AnyOf
		none = append(none, b.Args.NoneOf...)
	}
	// An argv can hit each anyOf group while avoiding every noneOf token
	// exactly when each group has a token outside the combined noneOf.
	return hasOutside(aAny, none) && hasOutside(bAny, none)
}

// hasOutside reports whether group is empty (no requirement) or has a token
// that isn't excluded.
func hasOutside(group, excluded []string) bool {
	if len(group) == 0 {
		return true
	}
	for _, t := range group {
		if !contains(excluded, t) {
			return true
		}
	}
	return false
}

func intersects(a, b []string) bool {
	for _, x := range a {
		if contains(b, x) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func describeCondition(t To) string {
	s := "functions=[" + strings.Join(t.Functions, ",") + "]"
	if t.Args != nil {
		if len(t.Args.AnyOf) > 0 {
			s += " anyOf=[" + strings.Join(t.Args.AnyOf, ",") + "]"
		}
		if len(t.Args.NoneOf) > 0 {
			s += " noneOf=[" + strings.Join(t.Args.NoneOf, ",") + "]"
		}
	}
	return s
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
