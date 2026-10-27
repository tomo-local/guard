package policy

import "sort"

// Kind describes how one policy kind is built from YAML and linted as a
// set. It is the registration point for new kinds: write the kind's file in
// this package and add one entry to Kinds.
type Kind struct {
	// New decodes the whole manifest in data into a policy of this kind.
	New func(data []byte, source string, deps Deps) (Policy, error)
	// LintSet reports problems that span several policies of this kind
	// (e.g. duplicate match conditions). May be nil.
	LintSet func(policies []Policy) []Issue
}

// Kinds maps a manifest's `kind:` value to its Kind.
var Kinds = map[string]Kind{
	KindCommandGate: {New: newCommandGate, LintSet: lintCommandGateSet},
}

// KindNames returns the registered kind names in sorted order.
func KindNames() []string {
	names := make([]string, 0, len(Kinds))
	for n := range Kinds {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
