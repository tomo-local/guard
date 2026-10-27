package policy

import (
	"strings"
	"testing"
)

func validPolicy() *CommandGatePolicy {
	return &CommandGatePolicy{
		APIVersion: APIVersion,
		Kind:       KindCommandGate,
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
	issues := lintAll(validPolicy())
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestLint_FieldErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(p *CommandGatePolicy)
		wantSub string
	}{
		{"apiVersion", func(p *CommandGatePolicy) { p.APIVersion = "v2" }, "apiVersion"},
		{"kind", func(p *CommandGatePolicy) { p.Kind = "Typo" }, "kind"},
		{"metadata.name", func(p *CommandGatePolicy) { p.Metadata.Name = "" }, "metadata.name"},
		{"target.module", func(p *CommandGatePolicy) { p.Spec.Target.Module = "network" }, "spec.target.module"},
		{"target.tool", func(p *CommandGatePolicy) { p.Spec.Target.Tool = "" }, "spec.target.tool"},
		{"action", func(p *CommandGatePolicy) { p.Spec.Action = "MAYBE" }, "spec.action"},
		{"rules empty", func(p *CommandGatePolicy) { p.Spec.Rules = nil }, "spec.rules"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPolicy()
			tc.mutate(p)
			issues := lintAll(p)
			if !hasErrorContaining(issues, tc.wantSub) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantSub, issues)
			}
		})
	}
}

func TestLint_ArgsMatchEmptyAnyOfIsAnError(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].To.Args = &ArgsMatch{}

	issues := lintAll(p)
	if !hasErrorContaining(issues, "to.args.anyOf") && !hasErrorContaining(issues, "args.anyOf") {
		t.Fatalf("expected an args.anyOf error for an empty AnyOf, got %v", issues)
	}
}

func TestLint_ArgsMatchEmptyElementIsAnError(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].To.Args = &ArgsMatch{AnyOf: []string{"-f", ""}}

	issues := lintAll(p)
	if len(issues) == 0 {
		t.Fatalf("expected an error for an empty element in args.anyOf, got none")
	}
}

func hasOverlapError(issues []Issue) bool {
	return hasErrorContaining(issues, "overlaps with")
}

func TestLint_AnyOfNarrowingStillOverlaps(t *testing.T) {
	p1 := validPolicy()
	p1.SourcePath = "a.yaml"
	p1.Spec.Rules[0].To.Args = &ArgsMatch{AnyOf: []string{"-f"}}

	p2 := validPolicy()
	p2.SourcePath = "b.yaml"
	// p1 only matches a subset of what p2 matches, but "git push -f"
	// satisfies both, so this is an overlap.
	p2.Spec.Rules[0].To.Args = nil

	if !hasOverlapError(lintAll(p1, p2)) {
		t.Fatal("expected an overlap error when one rule narrows the other")
	}
}

func TestLint_NoneOfSeparatesPolicies(t *testing.T) {
	// The shape of the shipped git policies: write-confirm steps aside
	// for the -f/--force that push-force-deny owns.
	p1 := validPolicy()
	p1.SourcePath = "write-confirm.yaml"
	p1.Spec.Rules = []Rule{{To: To{Functions: []string{"push"}, Args: &ArgsMatch{NoneOf: []string{"-f", "--force"}}}}}

	p2 := validPolicy()
	p2.SourcePath = "push-force-deny.yaml"
	p2.Spec.Action = ActionDeny
	p2.Spec.Rules = []Rule{{To: To{Functions: []string{"push"}, Args: &ArgsMatch{AnyOf: []string{"-f", "--force"}}}}}

	if issues := lintAll(p1, p2); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestLint_NoneOfPartiallyCoveringStillOverlaps(t *testing.T) {
	p1 := validPolicy()
	p1.SourcePath = "a.yaml"
	p1.Spec.Rules = []Rule{{To: To{Functions: []string{"push"}, Args: &ArgsMatch{NoneOf: []string{"-f"}}}}}

	p2 := validPolicy()
	p2.SourcePath = "b.yaml"
	p2.Spec.Rules = []Rule{{To: To{Functions: []string{"push"}, Args: &ArgsMatch{AnyOf: []string{"-f", "--force"}}}}}

	// "git push --force" matches both: a excludes only -f.
	if !hasOverlapError(lintAll(p1, p2)) {
		t.Fatal("expected an overlap error")
	}
}

func TestLint_OverlapCases(t *testing.T) {
	cases := []struct {
		name     string
		a, b     To
		overlaps bool
	}{
		{"disjoint functions", To{Functions: []string{"add"}}, To{Functions: []string{"push"}}, false},
		{"shared function", To{Functions: []string{"add", "push"}}, To{Functions: []string{"push"}}, true},
		{"catch-all vs function", To{}, To{Functions: []string{"push"}}, true},
		{"catch-all vs catch-all", To{}, To{}, true},
		{"anyOf groups disjoint tokens both satisfiable", To{Args: &ArgsMatch{AnyOf: []string{"-a"}}}, To{Args: &ArgsMatch{AnyOf: []string{"-b"}}}, true},
		{"anyOf vs noneOf of same tokens", To{Args: &ArgsMatch{AnyOf: []string{"-f"}}}, To{Args: &ArgsMatch{NoneOf: []string{"-f"}}}, false},
		{"noneOf vs noneOf", To{Args: &ArgsMatch{NoneOf: []string{"-f"}}}, To{Args: &ArgsMatch{NoneOf: []string{"-g"}}}, true},
		{"anyOf fully excluded by own noneOf elsewhere", To{Functions: []string{"push"}, Args: &ArgsMatch{AnyOf: []string{"-f"}, NoneOf: []string{"-x"}}}, To{Functions: []string{"push"}, Args: &ArgsMatch{NoneOf: []string{"-f"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := conditionsOverlap(tc.a, tc.b); got != tc.overlaps {
				t.Fatalf("conditionsOverlap = %v, want %v", got, tc.overlaps)
			}
			if got := conditionsOverlap(tc.b, tc.a); got != tc.overlaps {
				t.Fatalf("conditionsOverlap (swapped) = %v, want %v", got, tc.overlaps)
			}
		})
	}
}

func TestLint_DifferentToolsNeverOverlap(t *testing.T) {
	p1 := validPolicy()
	p1.SourcePath = "a.yaml"
	p2 := validPolicy()
	p2.SourcePath = "b.yaml"
	p2.Spec.Target.Tool = "docker"

	if issues := lintAll(p1, p2); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestLint_LabelDoesNotAvoidOverlap(t *testing.T) {
	// Label plays no part in matching, so differing labels don't separate
	// two policies.
	p1 := validPolicy()
	p1.SourcePath = "a.yaml"
	p1.Spec.Rules[0].To.Label = "one label"

	p2 := validPolicy()
	p2.SourcePath = "b.yaml"
	p2.Spec.Rules[0].To.Label = "a completely different label"

	if !hasOverlapError(lintAll(p1, p2)) {
		t.Fatal("expected an overlap error despite differing labels")
	}
}

func TestLint_RulesInsideOnePolicyAreNotCompared(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules = []Rule{
		{To: To{Functions: []string{"push"}}},
		{To: To{Functions: []string{"push"}, Args: &ArgsMatch{AnyOf: []string{"-f"}}}},
	}
	if hasOverlapError(lintAll(p)) {
		t.Fatal("rule precedence inside a policy is fixed; no overlap error expected")
	}
}

func TestLint_ArgsNoneOfOnlyIsValid(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].To.Args = &ArgsMatch{NoneOf: []string{"-f"}}
	if issues := lintAll(p); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestLint_ArgsNoneOfEmptyElementIsAnError(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].To.Args = &ArgsMatch{NoneOf: []string{""}}
	if len(lintAll(p)) == 0 {
		t.Fatal("expected an error for an empty element in args.noneOf")
	}
}

func TestLint_ArgsSameTokenInAnyOfAndNoneOfIsAnError(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].To.Args = &ArgsMatch{AnyOf: []string{"-f"}, NoneOf: []string{"-f"}}
	if !hasErrorContaining(lintAll(p), "can never match") {
		t.Fatalf("expected a can-never-match error, got %v", lintAll(p))
	}
}

func TestArgsMatch(t *testing.T) {
	cases := []struct {
		name string
		m    *ArgsMatch
		args []string
		want bool
	}{
		{"nil matches", nil, []string{"x"}, true},
		{"anyOf hit", &ArgsMatch{AnyOf: []string{"-f"}}, []string{"push", "-f"}, true},
		{"anyOf miss", &ArgsMatch{AnyOf: []string{"-f"}}, []string{"push"}, false},
		{"noneOf clear", &ArgsMatch{NoneOf: []string{"-f"}}, []string{"push"}, true},
		{"noneOf hit", &ArgsMatch{NoneOf: []string{"-f"}}, []string{"push", "-f"}, false},
		{"anyOf hit but noneOf hit", &ArgsMatch{AnyOf: []string{"-a"}, NoneOf: []string{"-b"}}, []string{"-a", "-b"}, false},
		{"anyOf hit and noneOf clear", &ArgsMatch{AnyOf: []string{"-a"}, NoneOf: []string{"-b"}}, []string{"-a"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := argsMatch(tc.m, tc.args); got != tc.want {
				t.Fatalf("argsMatch = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLint_CustomCheckExclusivity(t *testing.T) {
	p := validPolicy()
	p.Spec.Rules[0].Options = &Options{CustomCheck: &CustomCheck{}}

	issues := lintAll(p)
	if !hasErrorContaining(issues, "customCheck") {
		t.Fatalf("expected a customCheck error when neither path nor command is set, got %v", issues)
	}

	p.Spec.Rules[0].Options.CustomCheck.Path = "./check.sh"
	p.Spec.Rules[0].Options.CustomCheck.Command = "echo ok"
	issues = lintAll(p)
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

	issues := lintAll(p)
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

	issues := lintAll(p)
	if !hasWarningContaining(issues, "catch-all") {
		t.Fatalf("expected a catch-all warning, got %v", issues)
	}
}

func TestLint_CrossPolicyOverlapIsAnError(t *testing.T) {
	p1 := validPolicy()
	p1.SourcePath = "a.yaml"
	p2 := validPolicy()
	p2.SourcePath = "b.yaml"

	issues := lintAll(p1, p2)
	if !hasOverlapError(issues) {
		t.Fatalf("expected an overlap error, got %v", issues)
	}
	if !HasErrors(issues) {
		t.Fatal("an overlap must fail lint")
	}
}

// lintAll mirrors engine.Lint for a set of command-gate policies: each
// policy on its own, then the set across policies.
func lintAll(ps ...*CommandGatePolicy) []Issue {
	var issues []Issue
	set := make([]Policy, len(ps))
	for i, p := range ps {
		issues = append(issues, p.Lint()...)
		set[i] = p
	}
	return append(issues, lintCommandGateSet(set)...)
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

func TestLint_OverlapMessageNamesBothFilesAndAnExampleCommand(t *testing.T) {
	p1 := validPolicy()
	p1.SourcePath = "git/write-confirm.yaml"
	p1.Spec.Rules = []Rule{{To: To{Functions: []string{"push"}, Args: &ArgsMatch{NoneOf: []string{"-f"}}}}}

	p2 := validPolicy()
	p2.SourcePath = "git/push-force-deny.yaml"
	p2.Spec.Rules = []Rule{{To: To{Functions: []string{"push"}, Args: &ArgsMatch{AnyOf: []string{"-f", "--force"}}}}}

	issues := lintAll(p1, p2)
	var overlap []Issue
	for _, i := range issues {
		if strings.Contains(i.Message, "overlaps with") {
			overlap = append(overlap, i)
		}
	}
	if len(overlap) != 1 {
		t.Fatalf("expected one overlap issue per pair, got %v", issues)
	}
	i := overlap[0]
	if i.Path != "git/push-force-deny.yaml" {
		t.Fatalf("expected the issue under the file that sorts first, got %q", i.Path)
	}
	if i.Message != "overlaps with git/write-confirm.yaml" {
		t.Fatalf("unexpected summary %q", i.Message)
	}
	want := map[string]string{
		"example":    "`git push --force` matches both",
		"this file":  "functions=[push] anyOf=[-f,--force]",
		"other file": "functions=[push] noneOf=[-f]",
	}
	got := map[string]string{}
	for _, d := range i.Details {
		got[d.Label] = d.Value
	}
	for label, value := range want {
		if got[label] != value {
			t.Fatalf("detail %q = %q, want %q", label, got[label], value)
		}
	}
	if got["hint"] == "" {
		t.Fatal("expected a hint detail")
	}
}
