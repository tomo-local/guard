package policy

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"ai-guard-tracker/internal/shellcmd"
)

// KindGuardDBPolicy is the `kind:` value of .guard/db/policy.yml.
const KindGuardDBPolicy = "GuardDBPolicy"

// dbPriority puts the DB policy after the per-tool CommandGatePolicy files
// (priority 0) and before the project manifest (priority 100).
const dbPriority = 10

// GuardDBPolicy decides what database clients (psql, mysql) may do, by the
// environment of the connection they open (see GuardDBConnections).
type GuardDBPolicy struct {
	APIVersion string       `yaml:"apiVersion" validate:"eq=ai-guard/v1"`
	Kind       string       `yaml:"kind" validate:"eq=GuardDBPolicy"`
	Metadata   Metadata     `yaml:"metadata"`
	Spec       DBPolicySpec `yaml:"spec"`
	SourcePath string       `yaml:"-" validate:"-"`

	deps Deps
}

type DBPolicySpec struct {
	Target DBTarget `yaml:"target"`
	Rules  []DBRule `yaml:"rules" validate:"min=1,dive"`
}

// DBTarget names the clients the policy covers (both psql and mysql when
// empty) and the action a rule gets when it doesn't name one (DENY when empty).
type DBTarget struct {
	Clients []string `yaml:"clients,omitempty" validate:"omitempty,dive,required"`
	Action  Action   `yaml:"action,omitempty" validate:"omitempty,oneof=ALLOW DENY CONFIRM"`
}

type DBRule struct {
	To      DBTo     `yaml:"to"`
	Action  Action   `yaml:"action,omitempty" validate:"omitempty,oneof=ALLOW DENY CONFIRM"`
	Message *Message `yaml:"message,omitempty"`
}

type DBTo struct {
	// Label is descriptive only.
	Label string `yaml:"label,omitempty"`
	// Environment matches on the names of the environments the connection
	// belongs to (anyOf: at least one of them; noneOf: none of them).
	Environment *LabelsMatch `yaml:"environment" validate:"required"`
}

func newGuardDBPolicy(data []byte, source string, deps Deps) (Policy, error) {
	var p GuardDBPolicy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.SourcePath = source
	p.deps = deps
	return &p, nil
}

var _ CommandEvaluator = (*GuardDBPolicy)(nil)

func (p *GuardDBPolicy) Meta() Meta {
	return Meta{Name: p.Metadata.Name, Kind: KindGuardDBPolicy, Source: p.SourcePath}
}

func (p *GuardDBPolicy) Priority() int { return dbPriority }

func (p *GuardDBPolicy) clients() []string {
	if len(p.Spec.Target.Clients) == 0 {
		return DBClients
	}
	return p.Spec.Target.Clients
}

// Evaluate answers for psql/mysql commands once the connection's
// environments are known. When several rules match, the strictest action
// wins (the first on a tie). Without defined environments no rule matches.
func (p *GuardDBPolicy) Evaluate(cmd shellcmd.SimpleCommand) (Verdict, bool) {
	if !contains(p.clients(), cmd.Tool) || p.deps.DB == nil {
		return Verdict{}, false
	}
	conn := connectionOf(cmd)
	envs := p.deps.DB.Environments(conn.Host, conn.Database)

	var best *DBRule
	var bestAction Action
	for i := range p.Spec.Rules {
		r := &p.Spec.Rules[i]
		if !environmentMatches(r.To.Environment, envs) {
			continue
		}
		action := r.Action
		if action == "" {
			action = p.Spec.Target.Action
		}
		if action == "" {
			action = ActionDeny
		}
		if best == nil || ActionRank(action) > ActionRank(bestAction) {
			best, bestAction = r, action
		}
	}
	if best == nil {
		return Verdict{}, false
	}

	v := Verdict{Action: bestAction, Label: best.To.Label, Source: p.SourcePath}
	if best.Message != nil {
		v.Reason = renderMessageTemplate(best.Message.Reason, cmd)
		v.Next = renderMessageTemplate(best.Message.Next, cmd)
	}
	if v.Reason == "" {
		where := conn.Host
		if where == "" {
			where = "the local database"
		}
		if len(envs) > 0 {
			where += " (" + strings.Join(envs, ", ") + ")"
		}
		v.Reason = fmt.Sprintf("%s connects to %s.", cmd.String(), where)
	}
	if v.Next == "" {
		v.Next = "Confirm with the user before running it against this database."
	}
	return v, true
}

// environmentMatches reports whether envs satisfies m: at least one AnyOf
// name (when set) and none of the NoneOf names.
func environmentMatches(m *LabelsMatch, envs []string) bool {
	if m == nil {
		return false
	}
	return (len(m.AnyOf) == 0 || intersects(envs, m.AnyOf)) && !intersects(envs, m.NoneOf)
}

func (p *GuardDBPolicy) Lint() []Issue {
	c := NewIssueCollector(p.SourcePath)
	validateStruct(c, p)

	for i, cl := range p.Spec.Target.Clients {
		if cl != "" && !contains(DBClients, cl) {
			c.Errorf("spec.target.clients[%d]: unknown client %q (supported: %s)", i, cl, strings.Join(DBClients, ", "))
		}
	}
	for i, r := range p.Spec.Rules {
		m := r.To.Environment
		if m == nil {
			continue
		}
		if len(m.AnyOf) == 0 && len(m.NoneOf) == 0 {
			c.Errorf("spec.rules[%d].to.environment.anyOf: set anyOf or noneOf with at least 1 name", i)
		}
		for _, name := range m.AnyOf {
			if contains(m.NoneOf, name) {
				c.Errorf("spec.rules[%d].to.environment: %q is in both anyOf and noneOf, so the rule can never match", i, name)
			}
		}
	}
	return c.Issues()
}

// lintDBPolicySet allows at most one DB policy file.
func lintDBPolicySet(policies []Policy) []Issue {
	var ps []*GuardDBPolicy
	for _, p := range policies {
		if g, ok := p.(*GuardDBPolicy); ok {
			ps = append(ps, g)
		}
	}
	var issues []Issue
	for _, g := range ps[min(1, len(ps)):] {
		issues = append(issues, Issue{
			Severity: SeverityError,
			Path:     g.SourcePath,
			Message:  "more than one GuardDBPolicy",
			Details:  []Detail{{"also", ps[0].SourcePath}},
		})
	}
	return issues
}

// lintDBPolicyRefs checks that every environment name a DB policy refers to
// is defined by the GuardDBConnections file.
func lintDBPolicyRefs(all []Policy) []Issue {
	var conns *GuardDBConnections
	var policies []*GuardDBPolicy
	for _, p := range all {
		switch v := p.(type) {
		case *GuardDBConnections:
			if conns == nil {
				conns = v
			}
		case *GuardDBPolicy:
			policies = append(policies, v)
		}
	}

	var issues []Issue
	for _, p := range policies {
		c := NewIssueCollector(p.SourcePath)
		for i, r := range p.Spec.Rules {
			m := r.To.Environment
			if m == nil {
				continue
			}
			for _, name := range append(append([]string{}, m.AnyOf...), m.NoneOf...) {
				switch {
				case conns == nil:
					c.Errorf("spec.rules[%d].to.environment: %q is used but no GuardDBConnections file defines environments", i, name)
				case !contains(conns.EnvironmentNames(), name):
					c.Errorf("spec.rules[%d].to.environment: unknown environment %q (defined in %s: %s)", i, name, conns.SourcePath, strings.Join(conns.EnvironmentNames(), ", "))
				}
			}
		}
		issues = append(issues, c.Issues()...)
	}
	return issues
}
