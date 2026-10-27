package engine

import (
	"strings"
	"testing"

	"ai-guard-tracker/internal/policy"
)

func TestLint_RunsPerPolicyAndPerKindChecks(t *testing.T) {
	policies := []policy.Policy{
		namedGate("a.yaml", policy.ActionConfirm),
		namedGate("b.yaml", policy.ActionConfirm),
	}

	issues := Lint(policies)

	var dup bool
	for _, i := range issues {
		if strings.Contains(i.Message, "overlaps with") {
			dup = true
		}
	}
	if !dup {
		t.Fatalf("expected the command-gate set lint to flag the overlap, got %v", issues)
	}
}

func namedGate(source string, action policy.Action) policy.Policy {
	return plainGate(source, action)
}
