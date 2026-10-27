package policy

import (
	"strings"

	"gopkg.in/yaml.v3"

	"ai-guard-tracker/internal/shellcmd"
)

// KindGuardDBPolicy is the `kind:` value of .guard/db/policy.yml.
const KindGuardDBPolicy = "GuardDBPolicy"

// dbPriority puts the DB policy after the per-tool CommandGatePolicy files
// (priority 0) and before the project manifest (priority 100).
const dbPriority = 10

// GuardDBPolicy decides what database clients (psql, mysql) may run, by the
// labels of the connection's environment (see GuardDBConnections) and the
// keyword of each SQL statement.
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

// DBTo is a rule's match condition, shaped like a CommandGatePolicy's: Labels
// restricts the rule to connections whose environment carries one of them
// (omit it for every connection), Functions lists SQL statement keywords
// (omit it to make the rule a catch-all for every statement).
type DBTo struct {
	// Label is descriptive only.
	Label     string   `yaml:"label,omitempty"`
	Labels    []string `yaml:"labels,omitempty" validate:"omitempty,dive,required"`
	Functions []string `yaml:"functions,omitempty" validate:"omitempty,dive,required"`
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

func (p *GuardDBPolicy) sqlRules() []sqlRule {
	rules := make([]sqlRule, len(p.Spec.Rules))
	for i, r := range p.Spec.Rules {
		rules[i] = sqlRule{Label: r.To.Label, Labels: r.To.Labels, Functions: r.To.Functions, Action: r.Action, Message: r.Message}
	}
	return rules
}

// Evaluate answers for psql/mysql commands. Each SQL statement the command
// runs is judged separately (see pickSQLRule) and the strictest result
// stands for the command. Without connections.yml, a rule that names labels
// never matches.
func (p *GuardDBPolicy) Evaluate(cmd shellcmd.SimpleCommand) (Verdict, bool) {
	if !contains(p.clients(), cmd.Tool) {
		return Verdict{}, false
	}

	var match DBMatch
	if p.deps.DB != nil {
		conn := connectionOf(cmd)
		match = p.deps.DB.Match(conn.Host, conn.Database)
	}

	where := ""
	if len(match.Environments) > 0 {
		where = " on " + strings.Join(match.Environments, ", ")
	}
	return evaluateSQL(cmd, p.sqlRules(), p.Spec.Target.Action, match.Labels, p.SourcePath, where)
}

func (p *GuardDBPolicy) Lint() []Issue {
	c := NewIssueCollector(p.SourcePath)
	validateStruct(c, p)

	for i, cl := range p.Spec.Target.Clients {
		if cl != "" && !contains(DBClients, cl) {
			c.Errorf("spec.target.clients[%d]: unknown client %q (supported: %s)", i, cl, strings.Join(DBClients, ", "))
		}
	}

	catchAll := false
	for i, r := range p.Spec.Rules {
		if len(r.To.Functions) == 0 && len(r.To.Labels) == 0 {
			if catchAll {
				c.Warnf("spec.rules[%d] is a second rule for every statement on every connection; the earlier one makes it unreachable", i)
			}
			catchAll = true
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

// lintDBPolicyRefs checks that every label a DB policy refers to is carried
// by some environment in the GuardDBConnections file.
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
			for _, label := range r.To.Labels {
				switch {
				case conns == nil:
					c.Errorf("spec.rules[%d].to.labels: %q is used but no GuardDBConnections file defines environments", i, label)
				case !contains(conns.LabelNames(), label):
					c.Errorf("spec.rules[%d].to.labels: unknown label %q (defined in %s: %s)", i, label, conns.SourcePath, strings.Join(conns.LabelNames(), ", "))
				}
			}
		}
		issues = append(issues, c.Issues()...)
	}
	return issues
}
