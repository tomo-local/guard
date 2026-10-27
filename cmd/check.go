package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"ai-guard-tracker/internal/engine"
	"ai-guard-tracker/internal/hookproto"
	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/sidecar"
)

const checkUsage = `Usage:
  guard check "<command>"      # CLI mode: evaluate a command directly
  <hook-json> | guard check    # hook mode: Claude Code PreToolUse payload on stdin

Options:
  -rules string   policy directory to load (default ".guard/policies", env GUARD_POLICIES_DIR)
  -log string     check decision log directory, one file per UTC day (default ".guard/logs/check", env GUARD_LOG_PATH)
  -manifest string  project manifest, used when no policy matches (default ".guard/manifest.yaml", env GUARD_MANIFEST_PATH)

Precedence for both: -flag > env var > built-in default.

Exit codes (CLI mode only): 0=ALLOW, 1=DENY, 2=CONFIRM. Hook mode always exits 0
and communicates the decision via hookSpecificOutput.permissionDecision.
`

// RunCheck evaluates a Bash command against the loaded policies, either
// taken from a positional argument (CLI mode) or read as a Claude Code
// PreToolUse hook payload from stdin (hook mode).
func (c *Cmd) RunCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	rulesDir := fs.String("rules", defaultPoliciesDir(), "policy directory to load (env GUARD_POLICIES_DIR)")
	logDir := fs.String("log", defaultLogDir(), "check decision log directory (env GUARD_LOG_PATH)")
	manifest := fs.String("manifest", defaultManifestPath(), "project manifest (env GUARD_MANIFEST_PATH)")
	fs.Usage = func() { fmt.Fprint(c.Stderr, checkUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	policies, loadErr := loadPolicies(*rulesDir, *manifest, false)
	evaluator := engine.NewEvaluator(policies)

	if rest := fs.Args(); len(rest) > 0 {
		return c.runCheckCLI(evaluator, strings.Join(rest, " "), loadErr, *logDir)
	}
	return c.runCheckHook(evaluator, loadErr, *logDir)
}

func (c *Cmd) runCheckCLI(evaluator *engine.Evaluator, raw string, loadErr error, logDir string) error {
	if loadErr != nil {
		fmt.Fprintf(c.Stderr, "warning: failed to load policies: %v (defaulting to CONFIRM)\n", loadErr)
	}

	final, perCmd := evaluator.EvaluateAll(raw)

	if err := sidecar.AppendCheckLog(logDir, buildLogEntry("cli", "", raw, final, perCmd)); err != nil {
		fmt.Fprintf(c.Stderr, "warning: failed to write check log: %v\n", err)
	}

	fmt.Fprintf(c.Stdout, "%s %s\n", final.Action, raw)
	if final.Reason != "" {
		fmt.Fprintf(c.Stdout, "reason: %s\n", final.Reason)
	}
	if final.Next != "" {
		fmt.Fprintf(c.Stdout, "next: %s\n", final.Next)
	}

	switch final.Action {
	case policy.ActionDeny:
		return &ExitError{Code: 1}
	case policy.ActionConfirm:
		return &ExitError{Code: 2}
	default:
		return nil
	}
}

func (c *Cmd) runCheckHook(evaluator *engine.Evaluator, loadErr error, logDir string) error {
	data, err := io.ReadAll(c.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	var in hookproto.PreToolUseInput
	if err := json.Unmarshal(data, &in); err != nil || in.ToolName == "" {
		return fmt.Errorf("no command given and stdin is not a recognizable PreToolUse hook payload\n%s", checkUsage)
	}

	if in.ToolName != "Bash" {
		// Out of scope: no opinion, let Claude Code's normal permission
		// flow decide. Not logged since guard never evaluated anything.
		fmt.Fprint(c.Stdout, "{}")
		return nil
	}

	var bashInput hookproto.BashToolInput
	if err := json.Unmarshal(in.ToolInput, &bashInput); err != nil {
		fmt.Fprint(c.Stdout, "{}")
		return nil
	}

	final, perCmd := evaluator.EvaluateAll(bashInput.Command)

	if loadErr != nil {
		// Fail safe: if policies couldn't even be loaded, there's nothing
		// meaningful to ALLOW against -- force a CONFIRM regardless of
		// what EvaluateAll produced from an empty policy set.
		final.Action = policy.ActionConfirm
		final.Reason = fmt.Sprintf("ポリシーの読み込みに失敗しました: %v", loadErr)
		final.Next = "ポリシー定義（.guard/policies/）を確認してください。"
	}

	if err := sidecar.AppendCheckLog(logDir, buildLogEntry("hook", in.SessionID, bashInput.Command, final, perCmd)); err != nil {
		fmt.Fprintf(c.Stderr, "warning: failed to write check log: %v\n", err)
	}

	out := hookproto.PreToolUseOutput{
		HookSpecificOutput: &hookproto.PreToolUseHookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       mapPermissionDecision(final.Action),
			PermissionDecisionReason: combineReasonNext(final.Reason, final.Next),
		},
	}
	if err := json.NewEncoder(c.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode hook output: %w", err)
	}
	return nil
}

func mapPermissionDecision(a policy.Action) string {
	switch a {
	case policy.ActionAllow:
		return hookproto.PermissionAllow
	case policy.ActionDeny:
		return hookproto.PermissionDeny
	default:
		return hookproto.PermissionAsk
	}
}

func combineReasonNext(reason, next string) string {
	switch {
	case reason == "":
		return next
	case next == "":
		return reason
	default:
		return reason + "\n" + next
	}
}

func buildLogEntry(mode, sessionID, raw string, final engine.Decision, perCmd []engine.Decision) sidecar.CheckLogEntry {
	entry := sidecar.CheckLogEntry{
		Timestamp:         time.Now().UTC(),
		Mode:              mode,
		SessionID:         sessionID,
		RawCommand:        raw,
		Action:            string(final.Action),
		Reason:            final.Reason,
		Next:              final.Next,
		MatchedPolicies:   final.MatchedPolicies,
		Default:           final.Default,
		UsedFallbackParse: final.UsedFallbackParse,
	}

	if final.CustomCheck != nil {
		entry.CustomCheck = &sidecar.CustomCheckLog{
			ExitCode: final.CustomCheck.ExitCode,
			TimedOut: final.CustomCheck.TimedOut,
			Override: string(final.CustomCheck.Override),
		}
	}

	for _, d := range perCmd {
		entry.Commands = append(entry.Commands, sidecar.CommandLog{
			Tool:     d.Tool,
			Action:   string(d.Action),
			Label:    d.Label,
			Function: d.Function,
		})
	}

	return entry
}
