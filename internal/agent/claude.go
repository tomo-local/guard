// Package hookproto defines the JSON shapes Claude Code sends to and
// expects from PreToolUse/PostToolUse hook commands.
package hookproto

import "encoding/json"

// PreToolUseInput is the payload Claude Code pipes to a PreToolUse hook's
// stdin. ToolInput is kept raw because its shape depends on ToolName.
type PreToolUseInput struct {
	SessionID     string          `json:"session_id"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// BashToolInput is the shape of ToolInput when ToolName == "Bash".
type BashToolInput struct {
	Command string `json:"command"`
}

// PostToolUseInput is the payload Claude Code pipes to a PostToolUse
// hook's stdin, i.e. PreToolUseInput plus the tool's result. ToolInput
// and ToolResponse are kept raw because their shape depends on ToolName.
// DurationMs is optional (recent Claude Code versions only).
type PostToolUseInput struct {
	SessionID     string          `json:"session_id"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	ToolUseID     string          `json:"tool_use_id,omitempty"`
	DurationMs    *int            `json:"duration_ms,omitempty"`
}

// BashToolResponse is the shape of ToolResponse when ToolName == "Bash".
// As documented at the time of writing, Claude Code does not expose an
// exit code to hooks -- only stdout/stderr/interrupted/isImage. Don't
// assume an exit code field exists; infer at most "interrupted" here.
type BashToolResponse struct {
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	Interrupted bool   `json:"interrupted,omitempty"`
	IsImage     bool   `json:"isImage,omitempty"`
}

// PermissionDecision values accepted by Claude Code's hookSpecificOutput.
const (
	PermissionAllow = "allow"
	PermissionDeny  = "deny"
	PermissionAsk   = "ask"
)

// PreToolUseOutput is written to stdout by a PreToolUse hook. Leaving
// HookSpecificOutput nil (encoded as `{}`) tells Claude Code the hook has
// no opinion and its normal permission flow should decide.
type PreToolUseOutput struct {
	HookSpecificOutput *PreToolUseHookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type PreToolUseHookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}
