package engine

import (
	"testing"
	"time"

	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/procexec"
	"ai-guard-tracker/internal/shellcmd"
)

// shRunner is a CustomCheckRunner that really runs the command through sh,
// so customCheck tests keep exercising a real exit code.
type shRunner struct{}

func (shRunner) Run(timeout time.Duration, name string, args []string) policy.ProcResult {
	r := procexec.Run(timeout, name, args, nil)
	return policy.ProcResult{Stdout: r.Stdout, Stderr: r.Stderr, TimedOut: r.TimedOut, ExitCode: r.ExitCode, RunErr: r.RunErr}
}

func toPolicy(p policy.CommandGatePolicy) policy.Policy {
	return policy.NewCommandGate(p.SourcePath, p.Spec, policy.Deps{Runner: shRunner{}})
}

func toPolicies(ps []policy.CommandGatePolicy) []policy.Policy {
	out := make([]policy.Policy, len(ps))
	for i, p := range ps {
		out[i] = toPolicy(p)
	}
	return out
}

func gitPolicy(action policy.Action, label string, functions []string, sourcePath string) policy.CommandGatePolicy {
	return policy.CommandGatePolicy{
		APIVersion: policy.APIVersion,
		Kind:       policy.KindCommandGate,
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
	return toPolicies([]policy.CommandGatePolicy{
		gitPolicy(policy.ActionAllow, "read", []string{"log", "status"}, "git/read-allow.yaml"),
		gitPolicy(policy.ActionConfirm, "write", []string{"add", "commit", "push"}, "git/write-confirm.yaml"),
		gitPolicy(policy.ActionDeny, "destructive", []string{"reset", "clean"}, "git/destructive-deny.yaml"),
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.KindCommandGate,
			SourcePath: "rm/deny.yaml",
			Spec: policy.Spec{
				Target: policy.Target{Module: policy.ModulePermission, Tool: "rm"},
				Action: policy.ActionDeny,
				Rules:  []policy.Rule{{To: policy.To{Label: "destructive"}}},
			},
		},
	})
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
	policies := toPolicies([]policy.CommandGatePolicy{
		gitPolicy(policy.ActionAllow, "write", []string{"commit"}, "git/write-allow.yaml"),
		gitPolicy(policy.ActionConfirm, "write", []string{"commit"}, "git/write-confirm.yaml"),
	})
	e := NewEvaluator(policies)

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"commit"}})
	if got.Action != policy.ActionConfirm {
		t.Fatalf("expected CONFIRM to win over a plain ALLOW when both match, got %s", got.Action)
	}
}

func TestEvaluateCommand_MessageCmdPlaceholder(t *testing.T) {
	policies := toPolicies([]policy.CommandGatePolicy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.KindCommandGate,
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
	})

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

func TestEvaluateCommand_FunctionIsSet(t *testing.T) {
	// Function carries the firstFunction heuristic result into Decision
	// regardless of whether a policy matched, so guard report can
	// aggregate by tool+function.
	e := NewEvaluator(samplePolicies())

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push", "-f"}})
	if got.Function != "push" {
		t.Fatalf("Function = %q, want %q", got.Function, "push")
	}

	// Unmatched subcommand still reports the function on the default decision.
	got = e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"bisect"}})
	if got.Function != "bisect" {
		t.Fatalf("Function = %q, want %q on an unmatched default decision", got.Function, "bisect")
	}

	// Flat tool invoked with no non-flag argument -> no function (the
	// heuristic only ever returns the first arg that doesn't look like a
	// flag; "rm -rf /" itself does report "/" as its "function", which is
	// a known artifact of the heuristic, not of this new field).
	got = e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "rm", Args: []string{"-rf"}})
	if got.Function != "" {
		t.Fatalf("Function = %q, want empty when every arg looks like a flag", got.Function)
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

func pushForceDenyPolicy() policy.CommandGatePolicy {
	return policy.CommandGatePolicy{
		APIVersion: policy.APIVersion,
		Kind:       policy.KindCommandGate,
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
	policies := append(samplePolicies(), toPolicy(pushForceDenyPolicy()))
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
	policies := toPolicies([]policy.CommandGatePolicy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.KindCommandGate,
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
	})

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
	policies := toPolicies([]policy.CommandGatePolicy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.KindCommandGate,
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
	})

	e := NewEvaluator(policies)
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionConfirm {
		t.Fatalf("expected when[].action=CONFIRM to override the resource's own DENY, got %s", got.Action)
	}
}

func TestEvaluateCommand_CustomCheckOverrideBeatsBaseAction(t *testing.T) {
	// A matched customCheck `when` entry always overrides the resource's
	// plain Action, even downgrading it (here CONFIRM -> ALLOW).
	policies := toPolicies([]policy.CommandGatePolicy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.KindCommandGate,
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
	})

	e := NewEvaluator(policies)
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionAllow {
		t.Fatalf("expected customCheck's matched ALLOW to override the resource's own CONFIRM action, got %s", got.Action)
	}
}

func TestEvaluateCommand_CustomCheckNoWhenMatchKeepsBaseAction(t *testing.T) {
	policies := toPolicies([]policy.CommandGatePolicy{
		{
			APIVersion: policy.APIVersion,
			Kind:       policy.KindCommandGate,
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
	})

	e := NewEvaluator(policies)
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionConfirm {
		t.Fatalf("expected the resource's own CONFIRM action to survive when no `when` entry matches, got %s", got.Action)
	}
}

// fakeRunner returns a fixed exit code and counts how often it ran.
type fakeRunner struct {
	exit  int
	calls int
}

func (f *fakeRunner) Run(time.Duration, string, []string) policy.ProcResult {
	f.calls++
	return policy.ProcResult{ExitCode: f.exit}
}

func gateWithCheck(source string, action policy.Action, exit int, when policy.When, runner policy.CustomCheckRunner) policy.Policy {
	return policy.NewCommandGate(source, policy.Spec{
		Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
		Action: action,
		Rules: []policy.Rule{{
			To: policy.To{Label: "write", Functions: []string{"push"}},
			Options: &policy.Options{CustomCheck: &policy.CustomCheck{
				Command: "check",
				When:    []policy.When{when},
			}},
		}},
	}, policy.Deps{Runner: runner})
}

func plainGate(source string, action policy.Action) policy.Policy {
	return policy.NewCommandGate(source, policy.Spec{
		Target: policy.Target{Module: policy.ModulePermission, Tool: "git"},
		Action: action,
		Rules:  []policy.Rule{{To: policy.To{Label: "write", Functions: []string{"push"}}}},
	}, policy.Deps{})
}

// customCheck overrides only its own policy's action, and that verdict then
// competes under strictest-wins. A non-strictest policy's check now runs
// and can escalate the result (previously only the strictest policy's
// check ran, and it overrode the final decision).
func TestEvaluateCommand_CustomCheckOfLowerPolicyCanEscalate(t *testing.T) {
	runner := &fakeRunner{exit: 1}
	e := NewEvaluator([]policy.Policy{
		gateWithCheck("a.yaml", policy.ActionAllow, 1, policy.When{Key: "exit", Value: 1, Action: policy.ActionDeny}, runner),
		plainGate("b.yaml", policy.ActionConfirm),
	})

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionDeny {
		t.Fatalf("expected A's customCheck (ALLOW -> DENY) to beat B's CONFIRM, got %s", got.Action)
	}
	if runner.calls != 1 {
		t.Fatalf("expected the customCheck to run once, ran %d times", runner.calls)
	}
}

func TestEvaluateCommand_CustomCheckDowngradeStillLosesToStricterPolicy(t *testing.T) {
	runner := &fakeRunner{exit: 0}
	e := NewEvaluator([]policy.Policy{
		gateWithCheck("a.yaml", policy.ActionConfirm, 0, policy.When{Key: "exit", Value: 0, Action: policy.ActionAllow}, runner),
		plainGate("b.yaml", policy.ActionConfirm),
	})

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionConfirm {
		t.Fatalf("expected B's CONFIRM to win over A's downgraded ALLOW, got %s", got.Action)
	}
}

func TestEvaluateCommand_CustomCheckRunsEvenWhenAnotherPolicyDenies(t *testing.T) {
	runner := &fakeRunner{exit: 1}
	e := NewEvaluator([]policy.Policy{
		gateWithCheck("a.yaml", policy.ActionAllow, 1, policy.When{Key: "exit", Value: 1, Action: policy.ActionConfirm}, runner),
		plainGate("b.yaml", policy.ActionDeny),
	})

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionDeny {
		t.Fatalf("expected DENY, got %s", got.Action)
	}
	if runner.calls != 1 {
		t.Fatalf("pinning the side effect: A's customCheck runs even though B decides; ran %d times", runner.calls)
	}
}

func TestEvaluateCommand_NilRunnerSkipsCustomCheck(t *testing.T) {
	e := NewEvaluator([]policy.Policy{
		gateWithCheck("a.yaml", policy.ActionConfirm, 0, policy.When{Key: "exit", Value: 0, Action: policy.ActionAllow}, nil),
	})

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionConfirm || got.CustomCheck != nil {
		t.Fatalf("expected the policy's own CONFIRM and no check result without a Runner, got %+v", got)
	}
}

// laterPolicy is a command evaluator at a later priority level, standing
// in for a fallback kind such as a manifest.
type laterPolicy struct {
	action policy.Action
	calls  *int
}

func (laterPolicy) Meta() policy.Meta    { return policy.Meta{Kind: "Later", Source: "later.yaml"} }
func (laterPolicy) Lint() []policy.Issue { return nil }
func (laterPolicy) Priority() int        { return 100 }
func (l laterPolicy) Evaluate(shellcmd.SimpleCommand) (policy.Verdict, bool) {
	*l.calls++
	return policy.Verdict{Action: l.action, Source: "later.yaml"}, true
}

func TestEvaluateCommand_FirstPriorityLevelWithVerdictDecides(t *testing.T) {
	calls := 0
	later := laterPolicy{action: policy.ActionDeny, calls: &calls}

	// A level-0 policy matches: the later level must not be consulted,
	// even though it is stricter.
	e := NewEvaluator([]policy.Policy{later, plainGate("a.yaml", policy.ActionAllow)})
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"push"}})
	if got.Action != policy.ActionAllow || calls != 0 {
		t.Fatalf("expected level 0's ALLOW to decide without consulting level 100, got %s (later consulted %d times)", got.Action, calls)
	}

	// No level-0 policy matches: the later level is the fallback.
	got = e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "git", Args: []string{"status"}})
	if got.Action != policy.ActionDeny || got.Default {
		t.Fatalf("expected the level-100 fallback to decide DENY, got %+v", got)
	}
}
