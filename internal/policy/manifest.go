package policy

import (
	"fmt"
	"path"

	"gopkg.in/yaml.v3"

	"ai-guard-tracker/internal/shellcmd"
)

// KindGuardManifest is the `kind:` value of the project-wide manifest
// (.guard/manifest.yaml).
const KindGuardManifest = "GuardManifest"

// manifestPriority places the manifest after every individual tool policy
// (priority 0): it only decides commands those policies have no opinion on.
const manifestPriority = 100

// GuardManifest is the project-wide rule file. Its commandGate section
// restricts commands by the file paths they touch, as a fallback behind the
// per-tool CommandGatePolicy files. Its repositoryGate section labels
// branches for the `to.branch` conditions of CommandGatePolicy rules; sqlGate
// is read but not enforced yet.
type GuardManifest struct {
	APIVersion string       `yaml:"apiVersion" validate:"eq=ai-guard/v1"`
	Kind       string       `yaml:"kind" validate:"eq=GuardManifest"`
	Metadata   Metadata     `yaml:"metadata"`
	Spec       ManifestSpec `yaml:"spec"`

	// SourcePath is set by the loader; not part of the YAML.
	SourcePath string `yaml:"-" validate:"-"`

	deps Deps
}

type ManifestSpec struct {
	CommandGate    *CommandGateSection    `yaml:"commandGate,omitempty"`
	RepositoryGate *RepositoryGateSection `yaml:"repositoryGate,omitempty"`
	SQLGate        *SQLGateSection        `yaml:"sqlGate,omitempty"`
}

// RepositoryGateSection describes the git repositories the project works
// with and which of their branches carry the labels (LabelMain,
// LabelProtected) that CommandGatePolicy rules can match with `to.branch`.
// It makes no decisions itself.
type RepositoryGateSection struct {
	Repose []RepoSpec `yaml:"repose" validate:"dive"`
}

// RepoSpec labels the branches of one repository. Name is the repository's
// path on its host as seen in the origin URL, e.g. "owner/name".
type RepoSpec struct {
	Name      string   `yaml:"name" validate:"required"`
	Main      string   `yaml:"main,omitempty"`
	Protected []string `yaml:"protected,omitempty" validate:"omitempty,dive,required"`
}

// BranchLabels returns the labels the manifest gives branch in the
// repository named repo: LabelMain when it is the main branch, LabelProtected
// when it matches a protected pattern (path.Match, so "*" stays within one
// path segment). nil when the repository isn't described.
func (m *GuardManifest) BranchLabels(repo, branch string) []string {
	rg := m.Spec.RepositoryGate
	if rg == nil || branch == "" {
		return nil
	}
	for _, r := range rg.Repose {
		if r.Name != repo {
			continue
		}
		var labels []string
		if r.Main != "" && r.Main == branch {
			labels = append(labels, LabelMain)
		}
		for _, pat := range r.Protected {
			if ok, err := path.Match(pat, branch); err == nil && ok {
				labels = append(labels, LabelProtected)
				break
			}
		}
		return labels
	}
	return nil
}

// SQLGateSection holds SQL rules that apply whatever the environment: the
// same `functions` (statement keywords) rules as db/policy.yml, without
// labels. psql and mysql are covered unless target.clients narrows it.
type SQLGateSection struct {
	Target DBTarget  `yaml:"target"`
	Rules  []SQLRule `yaml:"rules" validate:"min=1,dive"`
}

type SQLRule struct {
	To      SQLTo    `yaml:"to"`
	Action  Action   `yaml:"action,omitempty" validate:"omitempty,oneof=ALLOW DENY CONFIRM"`
	Message *Message `yaml:"message,omitempty"`
}

type SQLTo struct {
	// Label is descriptive only.
	Label string `yaml:"label,omitempty"`
	// Functions lists SQL statement keywords; none makes a catch-all.
	Functions []string `yaml:"functions,omitempty" validate:"omitempty,dive,required"`
}

// CommandGateSection holds path-based command rules.
type CommandGateSection struct {
	Target ManifestTarget `yaml:"target"`
	Rules  []ManifestRule `yaml:"rules" validate:"min=1,dive"`
}

// ManifestTarget sets the action a rule gets when it doesn't name one.
// Empty means DENY.
type ManifestTarget struct {
	Action Action `yaml:"action,omitempty" validate:"omitempty,oneof=ALLOW DENY CONFIRM"`
}

// ManifestRule matches commands touching any of To.Paths and answers with
// Action (or the section's default).
type ManifestRule struct {
	To      ManifestTo `yaml:"to"`
	Action  Action     `yaml:"action,omitempty" validate:"omitempty,oneof=ALLOW DENY CONFIRM"`
	Message *Message   `yaml:"message,omitempty"`
}

type ManifestTo struct {
	// Label is descriptive only, like To.Label in a CommandGatePolicy.
	Label string      `yaml:"label,omitempty"`
	Paths *PathsMatch `yaml:"paths" validate:"required"`
}

// PathsMatch lists glob patterns anchored at the project root; see
// matchPath for the exact rules.
type PathsMatch struct {
	AnyOf []string `yaml:"anyOf" validate:"min=1,dive,required"`
}

func newGuardManifest(data []byte, source string, deps Deps) (Policy, error) {
	var m GuardManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	m.SourcePath = source
	m.deps = deps
	return &m, nil
}

var _ CommandEvaluator = (*GuardManifest)(nil)

func (m *GuardManifest) Meta() Meta {
	return Meta{Name: m.Metadata.Name, Kind: KindGuardManifest, Source: m.SourcePath}
}

func (m *GuardManifest) Priority() int { return manifestPriority }

// Evaluate answers for commands the manifest has a rule for: commandGate
// rules by the paths the command touches, sqlGate rules by the SQL a psql or
// mysql command runs. When both have an answer, the stricter one stands (the
// commandGate's on a tie).
func (m *GuardManifest) Evaluate(cmd shellcmd.SimpleCommand) (Verdict, bool) {
	paths, okPaths := m.evaluateCommandGate(cmd)
	sql, okSQL := m.evaluateSQLGate(cmd)
	switch {
	case okPaths && okSQL:
		if ActionRank(sql.Action) > ActionRank(paths.Action) {
			return sql, true
		}
		return paths, true
	case okSQL:
		return sql, true
	default:
		return paths, okPaths
	}
}

// evaluateSQLGate judges the SQL of a psql/mysql command with the sqlGate
// rules, whatever environment it connects to.
func (m *GuardManifest) evaluateSQLGate(cmd shellcmd.SimpleCommand) (Verdict, bool) {
	sg := m.Spec.SQLGate
	if sg == nil {
		return Verdict{}, false
	}
	clients := sg.Target.Clients
	if len(clients) == 0 {
		clients = DBClients
	}
	if !contains(clients, cmd.Tool) {
		return Verdict{}, false
	}

	rules := make([]sqlRule, len(sg.Rules))
	for i, r := range sg.Rules {
		rules[i] = sqlRule{Label: r.To.Label, Functions: r.To.Functions, Action: r.Action, Message: r.Message}
	}
	return evaluateSQL(cmd, rules, sg.Target.Action, nil, m.SourcePath, "")
}

// evaluateCommandGate answers for commands whose path arguments match a
// commandGate rule. When several rules match, the strictest action wins (the
// first one on a tie).
func (m *GuardManifest) evaluateCommandGate(cmd shellcmd.SimpleCommand) (Verdict, bool) {
	cg := m.Spec.CommandGate
	if cg == nil {
		return Verdict{}, false
	}
	paths := commandPaths(cmd.Args, m.deps.Root, m.deps.Cwd)
	if len(paths) == 0 {
		return Verdict{}, false
	}

	var (
		best        *ManifestRule
		bestAction  Action
		bestPath    string
		bestPattern string
	)
	for i := range cg.Rules {
		r := &cg.Rules[i]
		if r.To.Paths == nil {
			continue
		}
		path, pattern, ok := firstPathMatch(r.To.Paths.AnyOf, paths)
		if !ok {
			continue
		}
		action := r.Action
		if action == "" {
			action = cg.Target.Action
		}
		if action == "" {
			action = ActionDeny
		}
		if best == nil || ActionRank(action) > ActionRank(bestAction) {
			best, bestAction, bestPath, bestPattern = r, action, path, pattern
		}
	}
	if best == nil {
		return Verdict{}, false
	}

	v := Verdict{Action: bestAction, Label: best.To.Label, Source: m.SourcePath}
	if best.Message != nil {
		v.Reason = renderMessageTemplate(best.Message.Reason, cmd)
		v.Next = renderMessageTemplate(best.Message.Next, cmd)
	}
	if v.Reason == "" {
		v.Reason = fmt.Sprintf("%s touches %s, which the manifest restricts (%s).", cmd.String(), bestPath, bestPattern)
	}
	if v.Next == "" {
		v.Next = "Add an explicit policy under .guard/policies/ for this command, or confirm with the user."
	}
	return v, true
}

// firstPathMatch returns the first (path, pattern) pair where pattern matches path.
func firstPathMatch(patterns, paths []string) (path, pattern string, ok bool) {
	for _, pat := range patterns {
		for _, p := range paths {
			if matchPath(pat, p) {
				return p, pat, true
			}
		}
	}
	return "", "", false
}
