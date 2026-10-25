package engine

import (
	"testing"

	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/shellcmd"
)

func gitPolicy(action policy.Action, label string, functions []string, sourcePath string) policy.Policy {
	return policy.Policy{
		APIVersion: policy.APIVersion,
		Kind:       policy.Kind,
		SourcePath: sourcePath,
		Spec: policy.Spec{
			Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
			Action: action,
			Rules: []policy.Rule{
				{To: policy.To{Label: label, Functions: functions}},
			},
		},
	}
}

func samplePolicies() []policy.Policy {
	return []policy.Policy{
		gitPolicy(policy.ActionAllow, "read", []string{"log", "status"}, "git/read-allow.yaml"),
		gitPolicy(policy.ActionConfirm, "write", []string{"add", "commit", "push"}, "git/write-confirm.yaml"),
		gitPolicy(policy.ActionDeny, "destructive", []string{"reset", "clean"}, "git/destructive-deny.yaml"),
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.Kind,
			SourcePath: "rm/deny.yaml",
			Spec: policy.Spec{
				Target: policy.Target{Module: policy.ModulePermission, Tool: "rm"},
				Action: policy.ActionDeny,
				Rules:  []policy.Rule{{To: policy.To{Label: "destructive"}}},
			},
		},
	}
}

func TestEvaluateCommand(t *testing.T) {
	e := NewEvaluator(samplePolicies())

	cases := []struct {
		name string
		cmd  shellcmd.SimpleCommand
		want policy.Action
	}{
		{"git read -> plain allow", shellcmd.SimpleCommand{Tool: "git", Args: []string{"log"}}, policy.ActionAllow},
		{"git write -> confirm", shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}}, policy.ActionConfirm},
		{"git destructive -> deny", shellcmd.SimpleCommand{Tool: "git", Args: []string{"reset", "--hard"}}, policy.ActionDeny},
		{"rm flat catch-all -> deny", shellcmd.SimpleCommand{Tool: "rm", Args: []string{"-rf", "/"}}, policy.ActionDeny},
		{"unknown tool defaults to confirm", shellcmd.SimpleCommand{Tool: "curl", Args: []string{"http://x"}}, policy.DefaultAction},
		{"unknown git subcommand falls to default", shellcmd.SimpleCommand{Tool: "git", Args: []string{"bisect"}}, policy.DefaultAction},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := e.EvaluateCommand(tc.cmd)
			if got.Action != tc.want {
				t.Fatalf("EvaluateCommand(%+v) = %s, want %s (reason=%q)", tc.cmd, got.Action, tc.want, got.Reason)
			}
		})
	}
}

func TestEvaluateCommand_ConfirmBeatsAllowWhenBothMatch(t *testing.T) {
	// Two policies target the same tool/function but declare different
	// actions. Since CONFIRM outranks ALLOW in severityRank, the CONFIRM
	// resource should win the strictest-wins comparison for the whole group.
	policies := []policy.Policy{
		gitPolicy(policy.ActionAllow, "write", []string{"commit"}, "git/write-allow.yaml"),
		gitPolicy(policy.ActionConfirm, "write", []string{"commit"}, "git/write-confirm.yaml"),
	}
	e := NewEvaluator(policies)

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"commit"}})
	if got.Action != policy.ActionConfirm {
		t.Fatalf("expected CONFIRM to win over a plain ALLOW when both match, got %s", got.Action)
	}
}

func TestEvaluateCommand_MessageCmdPlaceholder(t *testing.T) {
	policies := []policy.Policy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.Kind,
			SourcePath: "git/write-confirm.yaml",
			Spec: policy.Spec{
				Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
				Action: policy.ActionConfirm,
				Rules: []policy.Rule{
					{
						To: policy.To{Label: "write", Functions: []string{"add"}},
						Message: &policy.Message{
							Reason: "{cmd} is a write operation.",
							Next:   "Confirm {cmd} with the user.",
						},
					},
				},
			},
		},
	}

	e := NewEvaluator(policies)
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"add", "-A", "."}})

	wantReason := "git add -A . is a write operation."
	wantNext := "Confirm git add -A . with the user."
	if got.Reason != wantReason {
		t.Fatalf("Reason = %q, want %q", got.Reason, wantReason)
	}
	if got.Next != wantNext {
		t.Fatalf("Next = %q, want %q", got.Next, wantNext)
	}
}

func TestEvaluateCommand_LabelIsPurelyDescriptive(t *testing.T) {
	// To.Label plays no part in matching -- it's carried into Decision
	// (and from there into the check log) purely for readability.
	e := NewEvaluator(samplePolicies())

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Label != "write" {
		t.Fatalf("Label = %q, want %q", got.Label, "write")
	}

	// No matching rule at all -> no label, but the command still
	// evaluates to the default action rather than failing.
	got = e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "curl", Args: []string{"http://x"}})
	if got.Label != "" {
		t.Fatalf("Label = %q, want empty for an unmatched command", got.Label)
	}
}

func TestEvaluateCommand_DefaultReasonMentionsCommand(t *testing.T) {
	e := NewEvaluator(samplePolicies())
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "curl", Args: []string{"http://x"}})

	want := `No policy matched command "curl http://x".`
	if got.Reason != want {
		t.Fatalf("Reason = %q, want %q", got.Reason, want)
	}
}

func pushForceDenyPolicy() policy.Policy {
	return policy.Policy{
		APIVersion: policy.APIVersion,
		Kind:       policy.Kind,
		SourcePath: "git/push-force-deny.yaml",
		Spec: policy.Spec{
			Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
			Action: policy.ActionDeny,
			Rules: []policy.Rule{
				{To: policy.To{
					Label:     "destructive",
					Functions: []string{"push"},
					Args:      &policy.ArgsMatch{AnyOf: []string{"-f", "--force"}},
				}},
			},
		},
	}
}

func TestEvaluateCommand_ArgsMatchDistinguishesForcePush(t *testing.T) {
	policies := append(samplePolicies(), pushForceDenyPolicy())
	e := NewEvaluator(policies)

	cases := []struct {
		name string
		args []string
		want policy.Action
	}{
		{"plain push stays confirm", []string{"push"}, policy.ActionConfirm},
		{"push -f is denied", []string{"push", "-f"}, policy.ActionDeny},
		{"push --force is denied", []string{"push", "--force"}, policy.ActionDeny},
		{"push with unrelated flag stays confirm", []string{"push", "--dry-run"}, policy.ActionConfirm},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: tc.args})
			if got.Action != tc.want {
				t.Fatalf("EvaluateCommand(git %v) = %s, want %s", tc.args, got.Action, tc.want)
			}
		})
	}
}

func TestEvaluateCommand_FlagBeforeFunctionHeuristic(t *testing.T) {
	// Known v1 limitation: "-C <path>" eats the next arg, so "push" is never
	// seen as the function. Documented behavior, pinned by this test so a
	// future change to the heuristic is a deliberate decision.
	e := NewEvaluator(samplePolicies())
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"-C", "push"}})
	if got.Action != policy.DefaultAction {
		t.Fatalf("expected the flag-value heuristic limitation to produce the default action, got %s", got.Action)
	}
}

func TestEvaluateAll_CompoundCommandStrictestWins(t *testing.T) {
	e := NewEvaluator(samplePolicies())

	final, perCmd := e.EvaluateAll("git log && rm -rf /")
	if final.Action != policy.ActionDeny {
		t.Fatalf("expected DENY to win over ALLOW, got %s", final.Action)
	}
	if len(perCmd) != 2 {
		t.Fatalf("expected 2 decomposed commands, got %d", len(perCmd))
	}
	if perCmd[0].Action != policy.ActionAllow || perCmd[1].Action != policy.ActionDeny {
		t.Fatalf("unexpected per-command breakdown: %+v", perCmd)
	}
}

func TestEvaluateAll_DenyBeatsConfirm(t *testing.T) {
	e := NewEvaluator(samplePolicies())

	final, _ := e.EvaluateAll("git push; git reset --hard")
	if final.Action != policy.ActionDeny {
		t.Fatalf("expected DENY to win over CONFIRM, got %s", final.Action)
	}
}

func TestEvaluateCommand_CustomCheckOverridesAction(t *testing.T) {
	policies := []policy.Policy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.Kind,
			SourcePath: "git/push-custom-check.yaml",
			Spec: policy.Spec{
				Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
				Action: policy.ActionAllow,
				Rules: []policy.Rule{
					{
						To: policy.To{Label: "write", Functions: []string{"push"}},
						Options: &policy.Options{
							CustomCheck: &policy.CustomCheck{
								Command: "exit 1",
								When: []policy.When{
									{Key: "exit", Value: 0, Action: policy.ActionAllow},
									{Key: "exit", Value: 1, Action: policy.ActionDeny},
								},
							},
						},
					},
				},
			},
		},
	}

	e := NewEvaluator(policies)
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionDeny {
		t.Fatalf("expected customCheck exit=1 to override the ALLOW base to DENY, got %s", got.Action)
	}
	if got.CustomCheck == nil || got.CustomCheck.ExitCode != 1 {
		t.Fatalf("expected CustomCheckResult with ExitCode=1, got %+v", got.CustomCheck)
	}
}

func TestEvaluateCommand_CustomCheckWhenCanOverrideToConfirm(t *testing.T) {
	policies := []policy.Policy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.Kind,
			SourcePath: "git/push-custom-check.yaml",
			Spec: policy.Spec{
				Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
				Action: policy.ActionDeny,
				Rules: []policy.Rule{
					{
						To: policy.To{Label: "write", Functions: []string{"push"}},
						Options: &policy.Options{
							CustomCheck: &policy.CustomCheck{
								Command: "exit 0",
								When: []policy.When{
									{Key: "exit", Value: 0, Action: policy.ActionConfirm},
								},
							},
						},
					},
				},
			},
		},
	}

	e := NewEvaluator(policies)
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionConfirm {
		t.Fatalf("expected when[].action=CONFIRM to override the resource's own DENY, got %s", got.Action)
	}
}

func TestEvaluateCommand_CustomCheckOverrideBeatsBaseAction(t *testing.T) {
	// A matched customCheck `when` entry always overrides the resource's
	// plain Action, even downgrading it (here CONFIRM -> ALLOW).
	policies := []policy.Policy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.Kind,
			SourcePath: "git/push-custom-check.yaml",
			Spec: policy.Spec{
				Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
				Action: policy.ActionConfirm,
				Rules: []policy.Rule{
					{
						To: policy.To{Label: "write", Functions: []string{"push"}},
						Options: &policy.Options{
							CustomCheck: &policy.CustomCheck{
								Command: "exit 0",
								When: []policy.When{
									{Key: "exit", Value: 0, Action: policy.ActionAllow},
								},
							},
						},
					},
				},
			},
		},
	}

	e := NewEvaluator(policies)
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionAllow {
		t.Fatalf("expected customCheck's matched ALLOW to override the resource's own CONFIRM action, got %s", got.Action)
	}
}

func TestEvaluateCommand_CustomCheckNoWhenMatchKeepsBaseAction(t *testing.T) {
	policies := []policy.Policy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.Kind,
			SourcePath: "git/push-custom-check.yaml",
			Spec: policy.Spec{
				Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
				Action: policy.ActionConfirm,
				Rules: []policy.Rule{
					{
						To: policy.To{Label: "write", Functions: []string{"push"}},
						Options: &policy.Options{
							CustomCheck: &policy.CustomCheck{
								Command: "exit 9",
								When: []policy.When{
									{Key: "exit", Value: 0, Action: policy.ActionAllow},
								},
							},
						},
					},
				},
			},
		},
	}

	e := NewEvaluator(policies)
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionConfirm {
		t.Fatalf("expected the resource's own CONFIRM action to survive when no `when` entry matches, got %s", got.Action)
	}
}
