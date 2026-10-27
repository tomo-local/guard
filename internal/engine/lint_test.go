package engine

import (
	"strings"
	"testing"

	"ai-guard-tracker/internal/policy"
)

func TestLint_RunsPerPolicyAndPerKindChecks(t *testing.T) {
	policies := []policy.Policy{
		plainGate("a.yaml", policy.ActionConfirm),
		plainGate("b.yaml", policy.ActionConfirm),
	}

	issues := Lint(policies)

	var dup bool
	for _, i := range issues {
		if strings.Contains(i.Message, "duplicate match condition") {
			dup = true
		}
	}
	if !dup {
		t.Fatalf("expected the command-gate set lint to flag the duplicate match condition, got %v", issues)
	}
}
