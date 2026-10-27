package policy

import "gopkg.in/yaml.v3"

const (
	APIVersion = "ai-guard/v1"
	// KindCommandGate is the `kind:` value of a CommandGatePolicy manifest.
	KindCommandGate = "CommandGatePolicy"

	// ModulePermission is the only module value supported in v1. It is kept
	// as an explicit field so future modules (e.g. network, notification)
	// can be introduced without a breaking schema change.
	ModulePermission = "permission"
)

// CommandGatePolicy is one CommandGatePolicy manifest, one file under
// .guard/policies/. It is the policy kind that gates Bash commands by
// tool/function/args.
//
// `validate` tags declare required-ness/enum constraints and are enforced
// by validateStruct (see validate.go); command_gate_lint.go only hand-codes checks that
// can't be expressed as a single field constraint (e.g. detecting two
// catch-all rules across a slice).
type CommandGatePolicy struct {
	APIVersion string   `yaml:"apiVersion" validate:"eq=ai-guard/v1"`
	Kind       string   `yaml:"kind" validate:"eq=CommandGatePolicy"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`

	// SourcePath is set by the loader to the file this policy came from, for
	// error messages and lint output. Not part of the YAML itself.
	SourcePath string `yaml:"-" validate:"-"`

	deps Deps
}

// NewCommandGate builds a CommandGatePolicy from an already-decoded spec.
// The loader goes through newCommandGate (YAML); this is for callers that
// hold a Spec directly, such as tests.
func NewCommandGate(source string, spec Spec, deps Deps) *CommandGatePolicy {
	return &CommandGatePolicy{
		APIVersion: APIVersion,
		Kind:       KindCommandGate,
		SourcePath: source,
		Spec:       spec,
		deps:       deps,
	}
}

func newCommandGate(data []byte, source string, deps Deps) (Policy, error) {
	var p CommandGatePolicy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.SourcePath = source
	p.deps = deps
	return &p, nil
}

func (p *CommandGatePolicy) Meta() Meta {
	return Meta{Name: p.Metadata.Name, Kind: KindCommandGate, Source: p.SourcePath}
}

// Priority is 0: individual tool policies are evaluated first.
func (p *CommandGatePolicy) Priority() int { return 0 }

type Metadata struct {
	Name string `yaml:"name" validate:"required"`
}

type Spec struct {
	Target Target `yaml:"target"`
	Action Action `yaml:"action" validate:"oneof=ALLOW DENY CONFIRM"`
	Rules  []Rule `yaml:"rules" validate:"min=1,dive"`
}

type Target struct {
	Module string `yaml:"module" validate:"eq=permission"`
	Tool   string `yaml:"tool" validate:"required"`
}

type Rule struct {
	To      To       `yaml:"to"`
	Options *Options `yaml:"options,omitempty"`
	Message *Message `yaml:"message,omitempty"`
}

// To is the match condition for a rule. Functions is optional: when absent,
// the rule is a catch-all for every invocation of the target tool (used for
// flat commands such as rm that have no subcommand concept). Args is also
// optional: when absent, the rule applies regardless of flags.
type To struct {
	// Label is a free-text, optional description of what this rule is
	// about (e.g. "write", "destructive", "network"). It plays no part in
	// matching -- matching is driven entirely by Functions/Args -- it
	// only exists to document intent and is carried into the check log
	// (sidecar.CommandLog) for readability.
	Label     string     `yaml:"label,omitempty"`
	Functions []string   `yaml:"functions,omitempty"`
	Args      *ArgsMatch `yaml:"args,omitempty"`
}

// ArgsMatch restricts a rule by literal tokens in the command's argv
// (e.g. distinguishing "git push" from "git push -f" without pattern
// matching). Deliberately no regex/globs -- keep matching predictable and
// auditable.
//
// AnyOf: the argv must contain at least one of these tokens.
// NoneOf: the argv must contain none of these tokens.
// At least one of the two must be set; both may be (they combine with AND).
// Because two policies may not match the same command (see
// lintCommandGateSet), NoneOf is how a rule steps aside for a stricter
// sibling, e.g. write-confirm's push excludes the -f that push-force-deny owns.
type ArgsMatch struct {
	AnyOf  []string `yaml:"anyOf,omitempty" validate:"omitempty,dive,required"`
	NoneOf []string `yaml:"noneOf,omitempty" validate:"omitempty,dive,required"`
}

// Message is attached to a rule's decision and surfaced to the AI (via
// Claude Code's permissionDecisionReason), not shown as end-user UI text --
// write Reason/Next in English. `{cmd}` in either field is replaced with
// the exact command that matched the rule (e.g. "git add -A"); it's the
// only placeholder supported in v1.
type Message struct {
	Reason string `yaml:"reason"`
	Next   string `yaml:"next"`
}

type Options struct {
	CustomCheck *CustomCheck `yaml:"customCheck,omitempty"`
}

// CustomCheck delegates part of the decision to an external script or
// inline shell command, mirroring Istio's CUSTOM authorization action.
// Exactly one of Path or Command must be set -- required_without/
// excluded_with together express that XOR.
type CustomCheck struct {
	Path           string   `yaml:"path,omitempty" validate:"required_without=Command,excluded_with=Command"`
	Command        string   `yaml:"command,omitempty" validate:"required_without=Path,excluded_with=Path"`
	Args           []string `yaml:"args,omitempty"`
	TimeoutSeconds int      `yaml:"timeoutSeconds,omitempty" validate:"gte=0"`
	When           []When   `yaml:"when,omitempty" validate:"dive"`
}

const DefaultCustomCheckTimeoutSeconds = 10

type When struct {
	Key    string `yaml:"key" validate:"eq=exit"`
	Value  int    `yaml:"value"`
	Action Action `yaml:"action" validate:"oneof=ALLOW DENY CONFIRM"`
}

var _ CommandEvaluator = (*CommandGatePolicy)(nil)
