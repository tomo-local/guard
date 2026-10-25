package policy

// Action is the decision a policy resource applies to matching commands.
//
// ALLOW/DENY/CONFIRM are all valid directly in the YAML schema
// (spec.action, when.action) as peer values of one enum -- there is no
// "confirm modifier" layered on top of ALLOW. This is a deliberate
// departure from Istio's binary ALLOW/DENY core: an earlier design kept
// CONFIRM derived-only (ALLOW + a confirm flag), but that made
// "DENY + confirm:true" a representable-but-contradictory combination
// that had to be caught by a dedicated lint rule. Making CONFIRM a peer
// enum value makes that combination impossible to write in the first
// place, since a resource can only ever hold one Action.
//
// Action lives on Spec (one per file/resource), not per rule: the file
// layout convention is "1 file = 1 tool + 1 action" (see the package
// doc in loader.go and docs/policy-schema.md), so mixing e.g. an ALLOW
// read rule and a CONFIRM write rule in the same resource means splitting
// them into two files, same as ALLOW/DENY always required.
type Action string

const (
	ActionAllow   Action = "ALLOW"
	ActionDeny    Action = "DENY"
	ActionConfirm Action = "CONFIRM"
)

// ValidActions lists every Action value that may appear directly in a
// policy file (spec.action / when.action). Keep in sync with the
// `oneof=...` validate tags on Spec.Action and When.Action below.
var ValidActions = []Action{ActionAllow, ActionDeny, ActionConfirm}

const (
	APIVersion = "ai-guard/v1"
	Kind       = "CommandGatePolicy"

	// ModulePermission is the only module value supported in v1. It is kept
	// as an explicit field so future modules (e.g. network, notification)
	// can be introduced without a breaking schema change.
	ModulePermission = "permission"
)

// Policy is one CommandGatePolicy manifest, one file under .guard/policies/.
//
// `validate` tags declare required-ness/enum constraints and are enforced
// by validateStruct (see validate.go); lint.go only hand-codes checks that
// can't be expressed as a single field constraint (e.g. detecting two
// catch-all rules across a slice).
type Policy struct {
	APIVersion string   `yaml:"apiVersion" validate:"eq=ai-guard/v1"`
	Kind       string   `yaml:"kind" validate:"eq=CommandGatePolicy"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`

	// SourcePath is set by the loader to the file this policy came from, for
	// error messages and lint output. Not part of the YAML itself.
	SourcePath string `yaml:"-" validate:"-"`
}

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

// ArgsMatch restricts a rule to commands whose argv contains at least one
// of AnyOf as a literal token (e.g. distinguishing "git push" from
// "git push -f" without pattern matching). Deliberately no regex/globs --
// keep matching predictable and auditable; this is usually paired with a
// separate, stricter rule (see git/push-force-deny.yaml) rather than baked
// into one rule.
type ArgsMatch struct {
	AnyOf []string `yaml:"anyOf" validate:"min=1,dive,required"`
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

// DefaultAction is applied when no policy rule matches a given command.
const DefaultAction = ActionConfirm
