// Package engine combines policies: it matches decomposed shell commands against
// loaded policies by asking each one for its verdict
// and produces an ALLOW/DENY/CONFIRM decision, aggregating "strictest wins"
// both across policies that match a single command and across the multiple
// commands a compound command line decomposes into.
package engine

import (
	"fmt"
	"sort"
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
	Label             string   // deduplicated labels from the matched verdict(s), joined with ", "; purely descriptive, see policy.To.Label
	MatchedPolicies   []string // source paths that contributed to Action
	CustomCheck       *policy.CheckResult
	UsedFallbackParse bool
	Default           bool   // true when no policy matched and DefaultAction was used
	Tool              string // set by EvaluateCommand; blank on the aggregated EvaluateAll result
	Function          string // set by EvaluateCommand (shellcmd.SimpleCommand.Function heuristic); blank on the aggregated EvaluateAll result or when the tool has no subcommand
}

const defaultNext = "Review the command and confirm with the user before proceeding."

func severityRank(a policy.Action) int { return policy.ActionRank(a) }

// Evaluator holds the command-evaluating policies loaded for a run of
// `guard check`, ordered by priority, and evaluates commands against them.
// It knows nothing about policy kinds: it only combines the verdicts that
// each policy returns.
type Evaluator struct {
	policies []policy.CommandEvaluator
}

// NewEvaluator returns an Evaluator over the policies that can evaluate
// commands; the rest (e.g. kinds that cover other ground) are ignored.
func NewEvaluator(policies []policy.Policy) *Evaluator {
	var ce []policy.CommandEvaluator
	for _, p := range policies {
		if e, ok := p.(policy.CommandEvaluator); ok {
			ce = append(ce, e)
		}
	}
	sort.SliceStable(ce, func(i, j int) bool { return ce[i].Priority() < ce[j].Priority() })
	return &Evaluator{policies: ce}
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

// EvaluateCommand evaluates a single simple command. Policies are consulted
// one priority level at a time; the first level where any policy returns a
// verdict decides (strictest verdict wins within the level), and lower
// levels are not consulted. With no verdict at all, the default applies.
func (e *Evaluator) EvaluateCommand(cmd shellcmd.SimpleCommand) Decision {
	function := cmd.Function()

	for i := 0; i < len(e.policies); {
		level := e.policies[i].Priority()
		var verdicts []policy.Verdict
		for ; i < len(e.policies) && e.policies[i].Priority() == level; i++ {
			if v, ok := e.policies[i].Evaluate(cmd); ok {
				verdicts = append(verdicts, v)
			}
		}
		if len(verdicts) == 0 {
			continue
		}
		d := strictestVerdict(verdicts)
		d.Tool = cmd.Tool
		d.Function = function
		return d
	}

	d := defaultDecision(cmd.String())
	d.Tool = cmd.Tool
	d.Function = function
	return d
}

// strictestVerdict combines the verdicts of one priority level: the
// strictest action wins, and the messages, labels and sources of the
// verdicts at that action are merged.
func strictestVerdict(verdicts []policy.Verdict) Decision {
	strictest := verdicts[0].Action
	for _, v := range verdicts[1:] {
		if severityRank(v.Action) > severityRank(strictest) {
			strictest = v.Action
		}
	}

	var reasons, nexts, labels, paths []string
	var customCheck *policy.CheckResult
	for _, v := range verdicts {
		if v.Action != strictest {
			continue
		}
		paths = append(paths, v.Source)
		reasons = appendUnique(reasons, v.Reason)
		nexts = appendUnique(nexts, v.Next)
		labels = appendUnique(labels, v.Label)
		if customCheck == nil && v.CustomCheck != nil {
			customCheck = v.CustomCheck
		}
	}

	return Decision{
		Action:          strictest,
		Reason:          strings.Join(reasons, "\n"),
		Next:            strings.Join(nexts, "\n"),
		Label:           strings.Join(labels, ", "),
		MatchedPolicies: paths,
		CustomCheck:     customCheck,
	}
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
