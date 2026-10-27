// Package engine matches decomposed shell commands against loaded policies
// and produces an ALLOW/DENY/CONFIRM decision, aggregating "strictest wins"
// both across policies that match a single command and across the multiple
// commands a compound command line decomposes into.
package engine

import (
	"fmt"
	"strings"

	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/shellcmd"
)

// Decision is the outcome of evaluating one (possibly compound) command
// line against the loaded policy set.
type Decision struct {
	Action            policy.Action
	Reason            string
	Next              string
	Label             string   // deduplicated To.Label values from the matched rule(s), joined with ", "; purely descriptive, see policy.To.Label
	MatchedPolicies   []string // source paths that contributed to Action
	CustomCheck       *CustomCheckResult
	UsedFallbackParse bool
	Default           bool   // true when no policy matched and DefaultAction was used
	Tool              string // set by EvaluateCommand; blank on the aggregated EvaluateAll result
	Function          string // set by EvaluateCommand (firstFunction heuristic); blank on the aggregated EvaluateAll result or when the tool has no subcommand
}

const defaultNext = "Review the command and confirm with the user before proceeding."

func severityRank(a policy.Action) int {
	switch a {
	case policy.ActionDeny:
		return 3
	case policy.ActionConfirm:
		return 2
	case policy.ActionAllow:
		return 1
	default:
		return 0
	}
}

// Evaluator holds the policy set loaded for a run of `guard check` and
// evaluates commands against it. It exists (rather than threading
// []policy.Policy through every call) because evaluation genuinely has
// state to hold onto -- the loaded policies -- for the lifetime of the
// check.
type Evaluator struct {
	Policies []policy.Policy
}

// NewEvaluator returns an Evaluator bound to policies.
func NewEvaluator(policies []policy.Policy) *Evaluator {
	return &Evaluator{Policies: policies}
}

// EvaluateAll decomposes raw and evaluates every simple command it
// contains, returning the aggregated final decision plus the per-command
// breakdown (useful for audit logging).
func (e *Evaluator) EvaluateAll(raw string) (Decision, []Decision) {
	cmds, usedFallback := shellcmd.Decompose(raw)
	if len(cmds) == 0 {
		d := defaultDecision(raw)
		d.UsedFallbackParse = usedFallback
		return d, nil
	}

	perCommand := make([]Decision, len(cmds))
	for i, c := range cmds {
		perCommand[i] = e.EvaluateCommand(c)
	}

	final := aggregateStrictest(perCommand)
	final.UsedFallbackParse = usedFallback
	return final, perCommand
}

// EvaluateCommand evaluates a single simple command against every policy
// that targets its tool.
//
// When a rule sets options.customCheck, its result (when its `when` list
// matches) always overrides the resource's plain Action -- even downgrading
// a CONFIRM or DENY to a plain ALLOW. The override only doesn't apply when
// customCheck didn't run, has no `when` match, or wasn't present at all.
func (e *Evaluator) EvaluateCommand(cmd shellcmd.SimpleCommand) Decision {
	type match struct {
		pol  policy.Policy
		rule policy.Rule
	}

	var matches []match
	for _, p := range e.Policies {
		if p.Spec.Target.Tool != cmd.Tool {
			continue
		}
		if rule, ok := matchRule(p.Spec.Rules, cmd.Args); ok {
			matches = append(matches, match{pol: p, rule: rule})
		}
	}

	function := firstFunction(cmd.Args)

	if len(matches) == 0 {
		d := defaultDecision(commandString(cmd))
		d.Tool = cmd.Tool
		d.Function = function
		return d
	}

	strictest := matches[0].pol.Spec.Action
	for _, m := range matches[1:] {
		if severityRank(m.pol.Spec.Action) > severityRank(strictest) {
			strictest = m.pol.Spec.Action
		}
	}

	var reasons, nexts, labels, paths []string
	var customCheck *policy.CustomCheck
	for _, m := range matches {
		if m.pol.Spec.Action != strictest {
			continue
		}
		paths = append(paths, m.pol.SourcePath)
		if m.rule.Message != nil {
			reasons = appendUnique(reasons, renderMessageTemplate(m.rule.Message.Reason, cmd))
			nexts = appendUnique(nexts, renderMessageTemplate(m.rule.Message.Next, cmd))
		}
		labels = appendUnique(labels, m.rule.To.Label)
		if customCheck == nil && m.rule.Options != nil && m.rule.Options.CustomCheck != nil {
			customCheck = m.rule.Options.CustomCheck
		}
	}

	decision := Decision{
		Action:          strictest,
		Reason:          strings.Join(reasons, "\n"),
		Next:            strings.Join(nexts, "\n"),
		Label:           strings.Join(labels, ", "),
		MatchedPolicies: paths,
		Tool:            cmd.Tool,
		Function:        function,
	}

	if customCheck != nil {
		result := RunCustomCheck(customCheck)
		decision.CustomCheck = &result
		if result.Override != "" {
			decision.Action = result.Override
		}
	}

	return decision
}

// defaultDecision is the built-in fallback used when no policy matches at
// all. cmdStr, when known, is embedded in Reason for context; pass "" when
// there's nothing meaningful to show (e.g. aggregateStrictest's empty case).
func defaultDecision(cmdStr string) Decision {
	reason := "No policy matched this command."
	if cmdStr != "" {
		reason = fmt.Sprintf("No policy matched command %q.", cmdStr)
	}
	return Decision{
		Action:  policy.DefaultAction,
		Reason:  reason,
		Next:    defaultNext,
		Default: true,
	}
}

// commandString renders a decomposed SimpleCommand back into a plain
// "<tool> <args...>" string, used both for the {cmd} placeholder in
// policy-authored messages and for the default-fallback Reason text.
func commandString(cmd shellcmd.SimpleCommand) string {
	if len(cmd.Args) == 0 {
		return cmd.Tool
	}
	return cmd.Tool + " " + strings.Join(cmd.Args, " ")
}

// renderMessageTemplate replaces the {cmd} placeholder -- the only one
// supported in v1 -- in a policy-authored Message.Reason/Message.Next with
// the exact command that matched the rule (e.g. "git add -A").
func renderMessageTemplate(s string, cmd shellcmd.SimpleCommand) string {
	if s == "" {
		return s
	}
	return strings.ReplaceAll(s, "{cmd}", commandString(cmd))
}

// aggregateStrictest combines several decisions (one per decomposed simple
// command) into the final decision for the whole command line.
func aggregateStrictest(decisions []Decision) Decision {
	if len(decisions) == 0 {
		return defaultDecision("")
	}

	strictest := decisions[0].Action
	for _, d := range decisions[1:] {
		if severityRank(d.Action) > severityRank(strictest) {
			strictest = d.Action
		}
	}

	var reasons, nexts, labels, paths []string
	allDefault := true
	for _, d := range decisions {
		if d.Action != strictest {
			continue
		}
		if d.Reason != "" {
			reasons = appendUnique(reasons, d.Reason)
		}
		if d.Next != "" {
			nexts = appendUnique(nexts, d.Next)
		}
		labels = appendUnique(labels, d.Label)
		paths = append(paths, d.MatchedPolicies...)
		if !d.Default {
			allDefault = false
		}
	}

	return Decision{
		Action:          strictest,
		Reason:          strings.Join(reasons, "\n"),
		Next:            strings.Join(nexts, "\n"),
		Label:           strings.Join(labels, ", "),
		MatchedPolicies: uniqueStrings(paths),
		Default:         allDefault,
	}
}

// matchRule finds the rule (if any) that applies to args: a rule naming
// specific functions wins if the command's subcommand is listed in it,
// otherwise a catch-all rule (no functions) applies. Either kind is
// further restricted by To.Args when the rule sets it (e.g. only matching
// "push" invocations that also carry "-f"/"--force").
func matchRule(rules []policy.Rule, args []string) (policy.Rule, bool) {
	function := firstFunction(args)

	if function != "" {
		for _, r := range rules {
			if len(r.To.Functions) == 0 {
				continue
			}
			if !argsMatch(r.To.Args, args) {
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
		if !argsMatch(r.To.Args, args) {
			continue
		}
		return r, true
	}

	return policy.Rule{}, false
}

// argsMatch reports whether args contains at least one of m.AnyOf as a
// literal token. A nil/empty m always matches (the rule applies
// regardless of flags).
func argsMatch(m *policy.ArgsMatch, args []string) bool {
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

// firstFunction treats the first argument that doesn't look like a flag as
// the subcommand name. This is a heuristic: it misclassifies flags that
// take a separate value (e.g. "git -C <path> push" sees "<path>" as the
// function), a known v1 limitation documented in the README.
func firstFunction(args []string) string {
	for _, a := range args {
		if a != "" && !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func appendUnique(list []string, s string) []string {
	if s == "" {
		return list
	}
	for _, existing := range list {
		if existing == s {
			return list
		}
	}
	return append(list, s)
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
