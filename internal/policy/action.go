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
// doc in load.go and docs/policy-schema.md), so mixing e.g. an ALLOW
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

// DefaultAction is applied when no policy rule matches a given command.
const DefaultAction = ActionConfirm

// ActionRank ranks actions for strictest-wins: DENY > CONFIRM > ALLOW.
func ActionRank(a Action) int {
	switch a {
	case ActionDeny:
		return 3
	case ActionConfirm:
		return 2
	case ActionAllow:
		return 1
	default:
		return 0
	}
}
