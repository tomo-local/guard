package policy

import (
	"fmt"

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
// per-tool CommandGatePolicy files. repositoryGate and sqlGate are read but
// not enforced yet.
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
	CommandGate    *CommandGateSection `yaml:"commandGate,omitempty"`
	RepositoryGate any                 `yaml:"repositoryGate,omitempty" validate:"-"`
	SQLGate        any                 `yaml:"sqlGate,omitempty" validate:"-"`
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

// Evaluate answers for commands whose path arguments match a commandGate
// rule. When several rules match, the strictest action wins (the first one
// on a tie).
func (m *GuardManifest) Evaluate(cmd shellcmd.SimpleCommand) (Verdict, bool) {
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
