package policy

import (
	"strings"
	"time"

	"ai-guard-tracker/internal/shellcmd"
)

// Evaluate returns this policy's verdict for cmd: the policy's Action when
// one of its rules matches the command's tool/function/args.
//
// When the matched rule sets options.customCheck, its result (when its
// `when` list matches) overrides the policy's plain Action -- even
// downgrading a CONFIRM or DENY to a plain ALLOW. The override doesn't
// apply when customCheck didn't run, has no `when` match, or wasn't
// present at all.
func (p *CommandGatePolicy) Evaluate(cmd shellcmd.SimpleCommand) (Verdict, bool) {
	if p.Spec.Target.Tool != cmd.Tool {
		return Verdict{}, false
	}
	rule, ok := matchRule(p.Spec.Rules, cmd)
	if !ok {
		return Verdict{}, false
	}

	v := Verdict{
		Action: p.Spec.Action,
		Label:  rule.To.Label,
		Source: p.SourcePath,
	}
	if rule.Message != nil {
		v.Reason = renderMessageTemplate(rule.Message.Reason, cmd)
		v.Next = renderMessageTemplate(rule.Message.Next, cmd)
	}

	if rule.Options != nil && rule.Options.CustomCheck != nil && p.deps.Runner != nil {
		result := runCustomCheck(p.deps.Runner, rule.Options.CustomCheck)
		v.CustomCheck = &result
		if result.Override != "" {
			v.Action = result.Override
		}
	}
	return v, true
}

// runCustomCheck executes the script or inline command described by cc and
// evaluates its `when` list against the resulting exit code. A timeout
// (default DefaultCustomCheckTimeoutSeconds) always applies so a hanging
// script can never block guard check indefinitely; on timeout the caller
// falls back to the policy's own action (Override is left empty).
func runCustomCheck(runner CustomCheckRunner, cc *CustomCheck) CheckResult {
	timeout := cc.TimeoutSeconds
	if timeout <= 0 {
		timeout = DefaultCustomCheckTimeoutSeconds
	}

	var name string
	var args []string
	if cc.Path != "" {
		name, args = cc.Path, cc.Args
	} else {
		name, args = "sh", append([]string{"-c", cc.Command}, cc.Args...)
	}

	run := runner.Run(time.Duration(timeout)*time.Second, name, args)

	result := CheckResult{
		Ran:      true,
		ExitCode: run.ExitCode,
		TimedOut: run.TimedOut,
		RunErr:   run.RunErr,
		Stdout:   run.Stdout,
		Stderr:   run.Stderr,
	}
	if result.TimedOut || result.RunErr != nil {
		return result
	}

	for _, w := range cc.When {
		if w.Key != "exit" || w.Value != result.ExitCode {
			continue
		}
		result.Override = w.Action
		break
	}

	return result
}

// renderMessageTemplate replaces the {cmd} placeholder -- the only one
// supported in v1 -- in a policy-authored Message.Reason/Message.Next with
// the exact command that matched the rule (e.g. "git add -A").
func renderMessageTemplate(s string, cmd shellcmd.SimpleCommand) string {
	if s == "" {
		return s
	}
	return strings.ReplaceAll(s, "{cmd}", cmd.String())
}

// matchRule finds the rule (if any) that applies to cmd: a rule naming
// specific functions wins if the command's subcommand is listed in it,
// otherwise a catch-all rule (no functions) applies. Either kind is
// further restricted by To.Args when the rule sets it (e.g. only matching
// "push" invocations that also carry "-f"/"--force").
func matchRule(rules []Rule, cmd shellcmd.SimpleCommand) (Rule, bool) {
	function := cmd.Function()

	if function != "" {
		for _, r := range rules {
			if len(r.To.Functions) == 0 {
				continue
			}
			if !argsMatch(r.To.Args, cmd.Args) {
				continue
			}
			for _, f := range r.To.Functions {
				if f == function {
					return r, true
				}
			}
		}
	}

	for _, r := range rules {
		if len(r.To.Functions) != 0 {
			continue
		}
		if !argsMatch(r.To.Args, cmd.Args) {
			continue
		}
		return r, true
	}

	return Rule{}, false
}

// argsMatch reports whether args contains at least one of m.AnyOf as a
// literal token. A nil/empty m always matches (the rule applies
// regardless of flags).
func argsMatch(m *ArgsMatch, args []string) bool {
	if m == nil || len(m.AnyOf) == 0 {
		return true
	}
	for _, a := range args {
		for _, want := range m.AnyOf {
			if a == want {
				return true
			}
		}
	}
	return false
}
