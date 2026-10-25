package policy

import (
	"strings"
	"testing"
)

func validPolicy() Policy {
	return Policy{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "git-write-confirm"},
		SourcePath: "git-write-confirm.yaml",
		Spec: Spec{
			Target: Target{Module: ModulePermission, Tool: "git"},
			Action: ActionConfirm,
			Rules: []Rule{
				{
					To: To{Label: "write", Functions: []string{"commit", "push"}},
					Message: &Message{
						Reason: "git への書き込み操作です。",
						Next:   "実行前にユーザーに確認してください。",
					},
				},
			},
		},
	}
}

func TestLint_Valid(t *testing.T) {
	issues := Lint([]Policy{validPolicy()})
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestLint_FieldErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(p *Policy)
		wantSub string
	}{
		{"apiVersion", func(p *Policy) { p.APIVersion = "v2" }, "apiVersion"},
		{"kind", func(p *Policy) { p.Kind = "Typo" }, "kind"},
		{"metadata.name", func(p *Policy) { p.Metadata.Name = "" }, "metadata.name"},
		{"target.module", func(p *Policy) { p.Spec.Target.Module = "network" }, "spec.target.module"},
		{"target.tool", func(p *Policy) { p.Spec.Target.Tool = "" }, "spec.target.tool"},
		{"action", func(p *Policy) { p.Spec.Action = "MAYBE" }, "spec.action"},
		{"rules empty", func(p *Policy) { p.Spec.Rules = nil }, "spec.rules"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPolicy()
			tc.mutate(&p)
			issues := Lint([]Policy{p})
			if !hasErrorContaining(issues, tc.wantSub) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantSub, issues)
			}
		})
	}
}

func TestLint_ArgsMatchEmptyAnyOfIsAnError(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].To.Args = &ArgsMatch{}

	issues := Lint([]Policy{p})
	if !hasErrorContaining(issues, "to.args.anyOf") && !hasErrorContaining(issues, "args.anyOf") {
		t.Fatalf("expected an args.anyOf error for an empty AnyOf, got %v", issues)
	}
}

func TestLint_ArgsMatchEmptyElementIsAnError(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].To.Args = &ArgsMatch{AnyOf: []string{"-f", ""}}

	issues := Lint([]Policy{p})
	if len(issues) == 0 {
		t.Fatalf("expected an error for an empty element in args.anyOf, got none")
	}
}

func TestLint_ArgsDifferenceAvoidsFalseCrossPolicyDuplicate(t *testing.T) {
	p1 := validPolicy()
	p1.SourcePath = "a.yaml"
	p1.Spec.Rules[0].To.Args = &ArgsMatch{AnyOf: []string{"-f"}}

	p2 := validPolicy()
	p2.SourcePath = "b.yaml"
	// Same tool/functions as p1 but no args condition -- these should
	// NOT be flagged as a duplicate match condition, since args narrows p1
	// to a strict subset of what p2 matches.
	p2.Spec.Rules[0].To.Args = nil

	issues := Lint([]Policy{p1, p2})
	if hasWarningContaining(issues, "duplicate match condition") {
		t.Fatalf("did not expect a duplicate warning when args differ, got %v", issues)
	}
}

func TestLint_LabelDifferenceDoesNotAvoidCrossPolicyDuplicate(t *testing.T) {
	// Unlike Args, Label plays no part in matching (it's purely
	// descriptive -- see To.Label's doc comment), so two rules differing
	// only in Label are still a genuine duplicate match condition.
	p1 := validPolicy()
	p1.SourcePath = "a.yaml"
	p1.Spec.Rules[0].To.Label = "one label"

	p2 := validPolicy()
	p2.SourcePath = "b.yaml"
	p2.Spec.Rules[0].To.Label = "a completely different label"

	issues := Lint([]Policy{p1, p2})
	if !hasWarningContaining(issues, "duplicate match condition") {
		t.Fatalf("expected a duplicate match condition warning despite differing labels, got %v", issues)
	}
}

func TestLint_CustomCheckExclusivity(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].Options = &Options{CustomCheck: &CustomCheck{}}

	issues := Lint([]Policy{p})
	if !hasErrorContaining(issues, "customCheck") {
		t.Fatalf("expected a customCheck error when neither path nor command is set, got %v", issues)
	}

	p.Spec.Rules[0].Options.CustomCheck.Path = "./check.sh"
	p.Spec.Rules[0].Options.CustomCheck.Command = "echo ok"
	issues = Lint([]Policy{p})
	if !hasErrorContaining(issues, "customCheck") {
		t.Fatalf("expected a customCheck error when both path and command are set, got %v", issues)
	}
}

func TestLint_CustomCheckWhenKey(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].Options = &Options{CustomCheck: &CustomCheck{
		Path: "./check.sh",
		When: []When{{Key: "status", Value: 0, Action: ActionAllow}},
	}}

	issues := Lint([]Policy{p})
	if !hasErrorContaining(issues, "when[0].key") {
		t.Fatalf("expected a when[0].key error, got %v", issues)
	}
}

func TestLint_DuplicateCatchAllWarning(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules = []Rule{
		{To: To{Label: "write"}},
		{To: To{Label: "destructive"}},
	}

	issues := Lint([]Policy{p})
	if !hasWarningContaining(issues, "catch-all") {
		t.Fatalf("expected a catch-all warning, got %v", issues)
	}
}

func TestLint_CrossPolicyDuplicateWarning(t *testing.T) {
	p1 := validPolicy()
	p1.SourcePath = "a.yaml"
	p2 := validPolicy()
	p2.SourcePath = "b.yaml"

	issues := Lint([]Policy{p1, p2})
	if !hasWarningContaining(issues, "duplicate match condition") {
		t.Fatalf("expected a duplicate match condition warning, got %v", issues)
	}
}

func hasErrorContaining(issues []Issue, substr string) bool {
	for _, i := range issues {
		if i.Severity == SeverityError && strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}

func hasWarningContaining(issues []Issue, substr string) bool {
	for _, i := range issues {
		if i.Severity == SeverityWarning && strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}
